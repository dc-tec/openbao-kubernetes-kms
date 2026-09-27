package metrics

import (
	"github.com/dc-tec/openbao-kubernetes-kms/internal/version"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
)

func (r *Recorder) registerRuntimeCollectors(info version.Info) error {
	build := prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "openbao_kms_build_info",
		Help: "Provider build metadata; always 1 for the running binary.",
		ConstLabels: prometheus.Labels{
			"version": info.Version, "commit": info.Commit, "build_date": info.BuildDate, "dirty": info.Dirty,
		},
	})
	build.Set(1)
	for _, collector := range []prometheus.Collector{
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
		build,
	} {
		if err := r.registry.Register(collector); err != nil {
			return err
		}
	}
	return nil
}
