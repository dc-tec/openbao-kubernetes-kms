// Package health serves the local /live and /ready HTTP endpoints.
//
// The endpoints are node-local diagnostics. The KMS v2 Status RPC remains the
// canonical health signal that kube-apiserver consults; /live and /ready are
// for kubelet, monitoring, and operator tooling.
//
//   - /live: 200 once the runtime has started its listeners. It only flips to
//     503 after Stop. It does not depend on OpenBao reachability.
//   - /ready: derived from status.Diagnostics. 503 whenever the cached Status
//     is unhealthy, the cache is stale, or no active key snapshot is loaded.
package health

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/dc-tec/openbao-kubernetes-kms/internal/kmsv2"
	"github.com/dc-tec/openbao-kubernetes-kms/internal/status"
)

// PathLive is the HTTP path served for liveness probes.
const PathLive = "/live"

// PathReady is the HTTP path served for readiness probes.
const PathReady = "/ready"

const (
	statusOK          = "ok"
	statusUnavailable = "unavailable"
)

// ErrNotStarted indicates the runtime is not yet accepting traffic.
var ErrNotStarted = errors.New("runtime not started")

// LivenessProbe reports whether the process is up and its listeners are bound.
type LivenessProbe interface {
	Live() error
}

// ReadinessProbe reports cached KMS health and rotation diagnostics.
type ReadinessProbe interface {
	Ready(ctx context.Context) (status.Diagnostics, error)
}

// Handler exposes /live and /ready over HTTP.
type Handler struct {
	mux *http.ServeMux
}

// NewHandler builds an HTTP handler bound to the supplied probes.
func NewHandler(live LivenessProbe, ready ReadinessProbe) (*Handler, error) {
	if live == nil {
		return nil, errors.New("liveness probe is required")
	}
	if ready == nil {
		return nil, errors.New("readiness probe is required")
	}
	mux := http.NewServeMux()
	mux.HandleFunc(PathLive, liveHandler(live))
	mux.HandleFunc(PathReady, readyHandler(ready))
	return &Handler{mux: mux}, nil
}

// ServeHTTP routes incoming requests to /live or /ready.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.mux.ServeHTTP(w, r)
}

func liveHandler(probe LivenessProbe) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if err := probe.Live(); err != nil {
			writeJSON(w, http.StatusServiceUnavailable, liveBody{Status: statusUnavailable})
			return
		}
		writeJSON(w, http.StatusOK, liveBody{Status: statusOK})
	}
}

func readyHandler(probe ReadinessProbe) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		diagnostics, err := probe.Ready(r.Context())
		if err != nil {
			writeJSON(w, http.StatusServiceUnavailable, readyBody{Status: statusUnavailable})
			return
		}
		body := readyBody{
			Status:        readyStatusFor(diagnostics),
			Healthz:       diagnostics.Healthz,
			CacheAgeMs:    diagnostics.CacheAge.Milliseconds(),
			Stale:         diagnostics.Stale,
			RotationState: string(diagnostics.RotationState),
		}
		code := http.StatusOK
		if body.Status != statusOK {
			code = http.StatusServiceUnavailable
		}
		writeJSON(w, code, body)
	}
}

func readyStatusFor(diagnostics status.Diagnostics) string {
	if diagnostics.Healthz != kmsv2.HealthOK {
		return statusUnavailable
	}
	if diagnostics.Stale {
		return statusUnavailable
	}
	if diagnostics.ActiveKeyIDHash == "" {
		return statusUnavailable
	}
	return statusOK
}

type liveBody struct {
	Status string `json:"status"`
}

type readyBody struct {
	Status        string `json:"status"`
	Healthz       string `json:"healthz,omitempty"`
	CacheAgeMs    int64  `json:"cache_age_ms"`
	Stale         bool   `json:"stale"`
	RotationState string `json:"rotation_state,omitempty"`
}

func writeJSON(w http.ResponseWriter, code int, body liveOrReady) {
	encoded, err := json.Marshal(body)
	if err != nil {
		http.Error(w, "encode response", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_, _ = w.Write(encoded)
}

// liveOrReady is the closed set of body shapes accepted by writeJSON.
type liveOrReady interface {
	healthBody()
}

func (liveBody) healthBody()  {}
func (readyBody) healthBody() {}
