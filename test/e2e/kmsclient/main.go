package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/dc-tec/openbao-kubernetes-kms/internal/aad"
	"github.com/dc-tec/openbao-kubernetes-kms/internal/kmsv2"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	grpcstatus "google.golang.org/grpc/status"
	kmsapi "k8s.io/kms/apis/v2"
)

const (
	envKMSSocketPath          = "KMS_SOCKET_PATH"
	envKMSClientMode          = "KMS_CLIENT_MODE"
	envKMSSamplePath          = "KMS_SAMPLE_PATH"
	envKMSRotationSamplePath  = "KMS_ROTATION_SAMPLE_PATH"
	envKMSLoadSoakDuration    = "KMS_LOAD_SOAK_DURATION"
	envKMSLoadSoakWorkers     = "KMS_LOAD_SOAK_WORKERS"
	envKMSLoadSoakMaxP95      = "KMS_LOAD_SOAK_MAX_P95"
	envKMSLoadSoakMinOps      = "KMS_LOAD_SOAK_MIN_OPS"
	envKMSDecryptSoakDuration = "KMS_DECRYPT_SOAK_DURATION"
	envKMSDecryptSoakWorkers  = "KMS_DECRYPT_SOAK_WORKERS"
	envKMSDecryptSoakMaxP95   = "KMS_DECRYPT_SOAK_MAX_P95"
	envKMSDecryptSoakMinOps   = "KMS_DECRYPT_SOAK_MIN_OPS"
	envKMSDecryptSoakSamples  = "KMS_DECRYPT_SOAK_SAMPLES"

	modeFullStack               = "full-stack"
	modeCreateStaleSocket       = "create-stale-socket"
	modeWriteSample             = "write-sample"
	modeReadSample              = "read-sample"
	modeExpectOutage            = "expect-outage"
	modeExpectUnhealthy         = "expect-unhealthy"
	modeExpectPolicyDenied      = "expect-policy-denied"
	modeExpectSocketUnavailable = "expect-socket-unavailable"
	modeExpectStatusStaleness   = "expect-status-staleness"
	modeExpectJWTRefresh        = "expect-jwt-refresh"
	modeExpectRotationPromotion = "expect-rotation-promotion"
	modeExpectRotationRollback  = "expect-rotation-rollback"
	modeDecryptStorm            = "decrypt-storm"
	modeDecryptSoak             = "decrypt-soak"
	modeLoadSoak                = "load-soak"

	jwtRefreshWait             = 7 * time.Second
	loadSoakDurationDefault    = 20 * time.Second
	loadSoakMaxP95Default      = 2 * time.Second
	loadSoakMinOpsDefault      = 90
	loadSoakWorkersDefault     = 4
	decryptSoakDurationDefault = 30 * time.Second
	decryptSoakMaxP95Default   = 2 * time.Second
	decryptSoakMinOpsDefault   = 500
	decryptSoakSamplesDefault  = 128
	decryptSoakWorkersDefault  = 8
	samplePath                 = "/kms-sample/encrypted-sample.json"
	sampleMountRoot            = "/kms-sample"
	rotationSamplePathDefault  = "/kms-sample/rotated-sample.json"
	plaintext                  = "kubernetes secret payload"
	requestUID                 = "provider-container-full-stack-e2e"
	stormRequests              = 64
	stormWorkers               = 8
)

type encryptedSample struct {
	Ciphertext  []byte            `json:"ciphertext"`
	KeyID       string            `json:"keyId"`
	Annotations map[string][]byte `json:"annotations"`
}

type decryptSoakSample struct {
	encrypted encryptedSample
	plaintext string
}

type loadSoakSample struct {
	operation string
	duration  time.Duration
	err       error
}

type loadSoakStats struct {
	counts        map[string]int
	durations     map[string][]time.Duration
	firstError    string
	errorCount    int
	totalDuration time.Duration
}

var modeHandlers = map[string]func(context.Context, kmsapi.KeyManagementServiceClient){
	modeFullStack:               runFullStack,
	modeWriteSample:             writeEncryptedSample,
	modeReadSample:              readEncryptedSample,
	modeExpectOutage:            expectOutage,
	modeExpectUnhealthy:         expectUnhealthy,
	modeExpectPolicyDenied:      expectPolicyDenied,
	modeExpectSocketUnavailable: expectSocketUnavailable,
	modeExpectStatusStaleness:   expectStatusStaleness,
	modeExpectJWTRefresh:        expectJWTRefresh,
	modeExpectRotationPromotion: expectRotationPromotion,
	modeExpectRotationRollback:  expectRotationRollback,
	modeDecryptStorm:            decryptStorm,
	modeDecryptSoak:             decryptSoak,
	modeLoadSoak:                loadSoak,
}

func main() {
	socketPath := os.Getenv(envKMSSocketPath)
	if socketPath == "" {
		failf("%s is required", envKMSSocketPath)
	}
	mode := os.Getenv(envKMSClientMode)
	if mode == "" {
		mode = modeFullStack
	}
	if mode == modeCreateStaleSocket {
		createStaleSocket(socketPath)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	conn, err := grpc.NewClient("unix://"+socketPath, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		failf("dial KMS socket: %v", err)
	}
	defer func() {
		if err := conn.Close(); err != nil {
			failf("close KMS connection: %v", err)
		}
	}()

	client := kmsapi.NewKeyManagementServiceClient(conn)
	handler, ok := modeHandlers[mode]
	if !ok {
		failf("unsupported %s %q", envKMSClientMode, mode)
	}
	handler(ctx, client)
}

func runFullStack(ctx context.Context, client kmsapi.KeyManagementServiceClient) {
	statusResponse := waitForHealthyStatus(ctx, client)
	encrypted := encrypt(ctx, client, statusResponse.GetKeyId())
	decrypt(ctx, client, encrypted)
	rejectUnknownKey(ctx, client, encrypted)
	rejectTamperedAnnotations(ctx, client, encrypted)
}

func createStaleSocket(socketPath string) {
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: socketPath, Net: "unix"})
	if err != nil {
		failf("create stale socket: %v", err)
	}
	listener.SetUnlinkOnClose(false)
	if err := listener.Close(); err != nil {
		failf("close stale socket: %v", err)
	}
}

func writeEncryptedSample(ctx context.Context, client kmsapi.KeyManagementServiceClient) {
	statusResponse := waitForHealthyStatus(ctx, client)
	encrypted := encrypt(ctx, client, statusResponse.GetKeyId())
	decrypt(ctx, client, encrypted)
	rejectUnknownKey(ctx, client, encrypted)
	rejectTamperedAnnotations(ctx, client, encrypted)
	writeSample(encrypted)
}

func readEncryptedSample(ctx context.Context, client kmsapi.KeyManagementServiceClient) {
	waitForHealthyStatusWithin(ctx, client, 75*time.Second)
	sample := readSample()
	decrypted, err := client.Decrypt(ctx, &kmsapi.DecryptRequest{
		Ciphertext:  sample.Ciphertext,
		KeyId:       sample.KeyID,
		Annotations: sample.Annotations,
	})
	if err != nil {
		failf("decrypt stored sample through provider: %v", err)
	}
	if string(decrypted.GetPlaintext()) != plaintext {
		failf("stored sample decrypt returned unexpected plaintext")
	}
}

func expectOutage(ctx context.Context, client kmsapi.KeyManagementServiceClient) {
	sample := readSample()
	waitForUnhealthyStatus(ctx, client)

	_, err := client.Encrypt(ctx, &kmsapi.EncryptRequest{
		Plaintext: []byte(plaintext),
		Uid:       requestUID,
	})
	assertCode(err, codes.FailedPrecondition, "encrypt during OpenBao outage")

	_, err = client.Decrypt(ctx, &kmsapi.DecryptRequest{
		Ciphertext:  sample.Ciphertext,
		KeyId:       sample.KeyID,
		Annotations: sample.Annotations,
	})
	assertAnyCode(err, []codes.Code{codes.Unavailable, codes.DeadlineExceeded}, "decrypt during OpenBao outage")
}

func expectUnhealthy(ctx context.Context, client kmsapi.KeyManagementServiceClient) {
	waitForUnhealthyStatus(ctx, client)

	_, err := client.Encrypt(ctx, &kmsapi.EncryptRequest{
		Plaintext: []byte(plaintext),
		Uid:       requestUID,
	})
	assertCode(err, codes.FailedPrecondition, "encrypt while provider is unhealthy")
}

func expectPolicyDenied(ctx context.Context, client kmsapi.KeyManagementServiceClient) {
	sample := readSample()

	_, err := client.Encrypt(ctx, &kmsapi.EncryptRequest{
		Plaintext: []byte(plaintext),
		Uid:       requestUID,
	})
	assertCode(err, codes.PermissionDenied, "encrypt with reduced Transit policy")

	_, err = client.Decrypt(ctx, &kmsapi.DecryptRequest{
		Ciphertext:  sample.Ciphertext,
		KeyId:       sample.KeyID,
		Annotations: sample.Annotations,
	})
	assertCode(err, codes.PermissionDenied, "decrypt with reduced Transit policy")
}

func expectSocketUnavailable(ctx context.Context, client kmsapi.KeyManagementServiceClient) {
	requestCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	_, err := client.Status(requestCtx, &kmsapi.StatusRequest{})
	assertAnyCode(err, []codes.Code{codes.Unavailable, codes.DeadlineExceeded}, "status with unavailable KMS socket")
}

func expectStatusStaleness(ctx context.Context, client kmsapi.KeyManagementServiceClient) {
	waitForHealthyStatus(ctx, client)
	waitForUnhealthyStatus(ctx, client)

	_, err := client.Encrypt(ctx, &kmsapi.EncryptRequest{
		Plaintext: []byte(plaintext),
		Uid:       requestUID,
	})
	assertCode(err, codes.FailedPrecondition, "encrypt after Status staleness")
}

func expectJWTRefresh(ctx context.Context, client kmsapi.KeyManagementServiceClient) {
	waitForHealthyStatus(ctx, client)
	time.Sleep(jwtRefreshWait)
	statusResponse := waitForHealthyStatus(ctx, client)
	encrypted := encrypt(ctx, client, statusResponse.GetKeyId())
	decrypt(ctx, client, encrypted)
}

func decryptStorm(ctx context.Context, client kmsapi.KeyManagementServiceClient) {
	statusResponse := waitForHealthyStatus(ctx, client)
	encrypted := encrypt(ctx, client, statusResponse.GetKeyId())

	work := make(chan int, stormRequests)
	errs := make(chan error, stormWorkers)
	for request := 0; request < stormRequests; request++ {
		work <- request
	}
	close(work)
	for worker := 0; worker < stormWorkers; worker++ {
		go func() {
			for range work {
				decrypted, err := client.Decrypt(ctx, &kmsapi.DecryptRequest{
					Ciphertext:  encrypted.GetCiphertext(),
					KeyId:       encrypted.GetKeyId(),
					Annotations: encrypted.GetAnnotations(),
				})
				if err != nil {
					errs <- fmt.Errorf("decrypt storm request: %w", err)
					return
				}
				if string(decrypted.GetPlaintext()) != plaintext {
					errs <- fmt.Errorf("decrypt storm returned unexpected plaintext")
					return
				}
			}
			errs <- nil
		}()
	}
	for worker := 0; worker < stormWorkers; worker++ {
		if err := <-errs; err != nil {
			failf("%v", err)
		}
	}
}

func decryptSoak(ctx context.Context, client kmsapi.KeyManagementServiceClient) {
	statusResponse := waitForHealthyStatus(ctx, client)

	duration := durationFromEnv(envKMSDecryptSoakDuration, decryptSoakDurationDefault)
	maxP95 := durationFromEnv(envKMSDecryptSoakMaxP95, decryptSoakMaxP95Default)
	workers := intFromEnv(envKMSDecryptSoakWorkers, decryptSoakWorkersDefault)
	minOps := intFromEnv(envKMSDecryptSoakMinOps, decryptSoakMinOpsDefault)
	sampleCount := intFromEnv(envKMSDecryptSoakSamples, decryptSoakSamplesDefault)
	if workers <= 0 {
		failf("decrypt soak workers must be positive")
	}
	if sampleCount <= 0 {
		failf("decrypt soak samples must be positive")
	}

	samples := prepareDecryptSoakSamples(ctx, client, statusResponse.GetKeyId(), sampleCount)
	results := make(chan loadSoakSample, workers)
	soakCtx, cancel := context.WithTimeout(ctx, duration)
	defer cancel()

	var wg sync.WaitGroup
	startedAt := time.Now()
	for workerID := range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			runDecryptSoakWorker(soakCtx, client, workerID, workers, samples, results)
		}()
	}
	go func() {
		wg.Wait()
		close(results)
	}()

	stats := collectLoadSoakStats(results)
	stats.totalDuration = time.Since(startedAt)
	operations := []string{"decrypt"}
	printSoakSummary("decrypt_soak", stats, operations)
	if stats.errorCount > 0 {
		failf("decrypt soak recorded %d errors; first=%s", stats.errorCount, stats.firstError)
	}
	if stats.counts["decrypt"] < minOps {
		failf("decrypt soak operations = %d, want at least %d", stats.counts["decrypt"], minOps)
	}
	p95 := percentileDuration(stats.durations["decrypt"], 95)
	if p95 > maxP95 {
		failf("decrypt soak p95 = %s, max %s", p95, maxP95)
	}
}

func prepareDecryptSoakSamples(
	ctx context.Context,
	client kmsapi.KeyManagementServiceClient,
	statusKeyID string,
	count int,
) []decryptSoakSample {
	samples := make([]decryptSoakSample, 0, count)
	for index := range count {
		plain := fmt.Sprintf("%s-%d", plaintext, index)
		encrypted, err := client.Encrypt(ctx, &kmsapi.EncryptRequest{
			Plaintext: []byte(plain),
			Uid:       requestUID,
		})
		if err != nil {
			failf("prepare decrypt soak sample %d: %v", index, err)
		}
		if encrypted.GetKeyId() != statusKeyID {
			failf("prepare decrypt soak sample %d key_id does not match status key_id", index)
		}
		samples = append(samples, decryptSoakSample{
			encrypted: encryptedSample{
				Ciphertext:  encrypted.GetCiphertext(),
				KeyID:       encrypted.GetKeyId(),
				Annotations: encrypted.GetAnnotations(),
			},
			plaintext: plain,
		})
	}
	return samples
}

func runDecryptSoakWorker(
	ctx context.Context,
	client kmsapi.KeyManagementServiceClient,
	workerID int,
	workers int,
	samples []decryptSoakSample,
	results chan<- loadSoakSample,
) {
	iteration := 0
	for {
		if ctx.Err() != nil {
			return
		}
		sample := samples[(workerID+(iteration*workers))%len(samples)]
		iteration++
		recordDecryptSoak(ctx, client, sample, results)
	}
}

func recordDecryptSoak(
	ctx context.Context,
	client kmsapi.KeyManagementServiceClient,
	sample decryptSoakSample,
	results chan<- loadSoakSample,
) {
	_ = recordLoadSoakOperation(ctx, "decrypt", results, func(requestCtx context.Context) error {
		decrypted, err := decryptStored(requestCtx, client, sample.encrypted)
		if err != nil {
			return err
		}
		if string(decrypted.GetPlaintext()) != sample.plaintext {
			return fmt.Errorf("decrypt returned unexpected plaintext")
		}
		return nil
	})
}

func loadSoak(ctx context.Context, client kmsapi.KeyManagementServiceClient) {
	waitForHealthyStatus(ctx, client)

	duration := durationFromEnv(envKMSLoadSoakDuration, loadSoakDurationDefault)
	maxP95 := durationFromEnv(envKMSLoadSoakMaxP95, loadSoakMaxP95Default)
	workers := intFromEnv(envKMSLoadSoakWorkers, loadSoakWorkersDefault)
	minOps := intFromEnv(envKMSLoadSoakMinOps, loadSoakMinOpsDefault)
	results := make(chan loadSoakSample, workers*3)
	soakCtx, cancel := context.WithTimeout(ctx, duration)
	defer cancel()

	var wg sync.WaitGroup
	startedAt := time.Now()
	for workerID := range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			runLoadSoakWorker(soakCtx, client, workerID, results)
		}()
	}
	go func() {
		wg.Wait()
		close(results)
	}()

	stats := collectLoadSoakStats(results)
	stats.totalDuration = time.Since(startedAt)
	operations := []string{"status", "encrypt", "decrypt"}
	printSoakSummary("load_soak", stats, operations)
	if stats.errorCount > 0 {
		failf("load soak recorded %d errors; first=%s", stats.errorCount, stats.firstError)
	}
	for _, operation := range operations {
		if stats.counts[operation] < minOps {
			failf("load soak %s operations = %d, want at least %d", operation, stats.counts[operation], minOps)
		}
		p95 := percentileDuration(stats.durations[operation], 95)
		if p95 > maxP95 {
			failf("load soak %s p95 = %s, max %s", operation, p95, maxP95)
		}
	}
}

func runLoadSoakWorker(
	ctx context.Context,
	client kmsapi.KeyManagementServiceClient,
	workerID int,
	results chan<- loadSoakSample,
) {
	for {
		if ctx.Err() != nil {
			return
		}
		statusResponse, ok := recordLoadSoakStatus(ctx, client, results)
		if !ok {
			continue
		}
		encrypted, ok := recordLoadSoakEncrypt(ctx, client, statusResponse.GetKeyId(), workerID, results)
		if !ok {
			continue
		}
		recordLoadSoakDecrypt(ctx, client, encrypted, results)
	}
}

func recordLoadSoakStatus(
	ctx context.Context,
	client kmsapi.KeyManagementServiceClient,
	results chan<- loadSoakSample,
) (*kmsapi.StatusResponse, bool) {
	var response *kmsapi.StatusResponse
	err := recordLoadSoakOperation(ctx, "status", results, func(requestCtx context.Context) error {
		var statusErr error
		response, statusErr = client.Status(requestCtx, &kmsapi.StatusRequest{})
		if statusErr != nil {
			return statusErr
		}
		if response.GetVersion() != kmsv2.APIVersion ||
			response.GetHealthz() != kmsv2.HealthOK ||
			response.GetKeyId() == "" {
			return fmt.Errorf("unexpected unhealthy Status response")
		}
		return nil
	})
	return response, err == nil
}

func recordLoadSoakEncrypt(
	ctx context.Context,
	client kmsapi.KeyManagementServiceClient,
	keyID string,
	workerID int,
	results chan<- loadSoakSample,
) (*kmsapi.EncryptResponse, bool) {
	var response *kmsapi.EncryptResponse
	err := recordLoadSoakOperation(ctx, "encrypt", results, func(requestCtx context.Context) error {
		var encryptErr error
		response, encryptErr = client.Encrypt(requestCtx, &kmsapi.EncryptRequest{
			Plaintext: []byte(fmt.Sprintf("%s-%d", plaintext, workerID)),
			Uid:       requestUID,
		})
		if encryptErr != nil {
			return encryptErr
		}
		if response.GetKeyId() != keyID {
			return fmt.Errorf("encrypt key_id did not match Status key_id")
		}
		if len(response.GetCiphertext()) == 0 || len(response.GetAnnotations()) == 0 {
			return fmt.Errorf("encrypt returned incomplete response")
		}
		return nil
	})
	return response, err == nil
}

func recordLoadSoakDecrypt(
	ctx context.Context,
	client kmsapi.KeyManagementServiceClient,
	encrypted *kmsapi.EncryptResponse,
	results chan<- loadSoakSample,
) {
	_ = recordLoadSoakOperation(ctx, "decrypt", results, func(requestCtx context.Context) error {
		decrypted, err := client.Decrypt(requestCtx, &kmsapi.DecryptRequest{
			Ciphertext:  encrypted.GetCiphertext(),
			KeyId:       encrypted.GetKeyId(),
			Annotations: encrypted.GetAnnotations(),
		})
		if err != nil {
			return err
		}
		if !bytes.HasPrefix(decrypted.GetPlaintext(), []byte(plaintext)) {
			return fmt.Errorf("decrypt returned unexpected plaintext")
		}
		return nil
	})
}

func recordLoadSoakOperation(
	ctx context.Context,
	operation string,
	results chan<- loadSoakSample,
	fn func(context.Context) error,
) error {
	requestCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	startedAt := time.Now()
	err := fn(requestCtx)
	duration := time.Since(startedAt)
	if ctx.Err() != nil && err != nil {
		return err
	}
	results <- loadSoakSample{operation: operation, duration: duration, err: err}
	return err
}

func collectLoadSoakStats(results <-chan loadSoakSample) loadSoakStats {
	stats := loadSoakStats{
		counts:    make(map[string]int),
		durations: make(map[string][]time.Duration),
	}
	for result := range results {
		stats.counts[result.operation]++
		stats.durations[result.operation] = append(stats.durations[result.operation], result.duration)
		if result.err != nil {
			stats.errorCount++
			if stats.firstError == "" {
				stats.firstError = result.err.Error()
			}
		}
	}
	return stats
}

func printSoakSummary(name string, stats loadSoakStats, operations []string) {
	summary := fmt.Sprintf(
		"%s duration=%s errors=%d",
		name,
		stats.totalDuration.Round(time.Millisecond),
		stats.errorCount,
	)
	for _, operation := range operations {
		durations := stats.durations[operation]
		summary += fmt.Sprintf(
			" %s_count=%d %s_p95=%s %s_max=%s",
			operation,
			stats.counts[operation],
			operation,
			percentileDuration(durations, 95).Round(time.Millisecond),
			operation,
			maxDuration(durations).Round(time.Millisecond),
		)
	}
	_, _ = fmt.Fprintln(os.Stdout, summary)
}

func expectRotationPromotion(ctx context.Context, client kmsapi.KeyManagementServiceClient) {
	preRotation := readSampleAt(currentSamplePath())
	decryptSample(ctx, client, preRotation, "pre-rotation sample before promotion")

	statusResponse := waitForHealthyStatusWithNewKeyID(ctx, client, preRotation.KeyID, 75*time.Second)
	rotated := encrypt(ctx, client, statusResponse.GetKeyId())
	if rotated.GetKeyId() == preRotation.KeyID {
		failf("rotation promotion did not change key_id")
	}
	decrypt(ctx, client, rotated)
	writeSampleAt(currentRotationSamplePath(), rotated)
	decryptSample(ctx, client, preRotation, "pre-rotation sample after promotion")
}

func expectRotationRollback(ctx context.Context, client kmsapi.KeyManagementServiceClient) {
	rotated := readSampleAt(currentRotationSamplePath())
	waitForUnhealthyStatus(ctx, client)

	_, err := client.Encrypt(ctx, &kmsapi.EncryptRequest{
		Plaintext: []byte(plaintext),
		Uid:       requestUID,
	})
	assertCode(err, codes.FailedPrecondition, "encrypt after Transit rollback")

	_, err = client.Decrypt(ctx, &kmsapi.DecryptRequest{
		Ciphertext:  rotated.Ciphertext,
		KeyId:       rotated.KeyID,
		Annotations: rotated.Annotations,
	})
	if err == nil {
		failf("post-rotation sample decrypted after OpenBao was restored before that key version")
	}
}

func waitForHealthyStatus(ctx context.Context, client kmsapi.KeyManagementServiceClient) *kmsapi.StatusResponse {
	return waitForHealthyStatusWithin(ctx, client, 15*time.Second)
}

func waitForHealthyStatusWithin(
	ctx context.Context,
	client kmsapi.KeyManagementServiceClient,
	timeout time.Duration,
) *kmsapi.StatusResponse {
	deadline := time.Now().Add(timeout)
	var lastErr error
	for time.Now().Before(deadline) {
		statusResponse, err := client.Status(ctx, &kmsapi.StatusRequest{})
		if err == nil &&
			statusResponse.GetVersion() == kmsv2.APIVersion &&
			statusResponse.GetHealthz() == kmsv2.HealthOK &&
			statusResponse.GetKeyId() != "" {
			return statusResponse
		}
		lastErr = err
		time.Sleep(100 * time.Millisecond)
	}
	failf("KMS provider did not report healthy status: %v", lastErr)
	return nil
}

func waitForHealthyStatusWithNewKeyID(
	ctx context.Context,
	client kmsapi.KeyManagementServiceClient,
	previousKeyID string,
	timeout time.Duration,
) *kmsapi.StatusResponse {
	deadline := time.Now().Add(timeout)
	var lastErr error
	var lastKeyID string
	for time.Now().Before(deadline) {
		statusResponse, err := client.Status(ctx, &kmsapi.StatusRequest{})
		if err == nil &&
			statusResponse.GetVersion() == kmsv2.APIVersion &&
			statusResponse.GetHealthz() == kmsv2.HealthOK &&
			statusResponse.GetKeyId() != "" &&
			statusResponse.GetKeyId() != previousKeyID {
			return statusResponse
		}
		lastErr = err
		if statusResponse != nil {
			lastKeyID = statusResponse.GetKeyId()
		}
		time.Sleep(250 * time.Millisecond)
	}
	failf("KMS provider did not promote a new rotation key_id: last_key_id=%q err=%v", lastKeyID, lastErr)
	return nil
}

func waitForUnhealthyStatus(ctx context.Context, client kmsapi.KeyManagementServiceClient) {
	deadline := time.Now().Add(30 * time.Second)
	var lastErr error
	var lastHealthz string
	for time.Now().Before(deadline) {
		statusResponse, err := client.Status(ctx, &kmsapi.StatusRequest{})
		if err == nil &&
			statusResponse.GetVersion() == kmsv2.APIVersion &&
			statusResponse.GetHealthz() == kmsv2.HealthUnhealthy &&
			statusResponse.GetKeyId() == "" {
			return
		}
		lastErr = err
		if statusResponse != nil {
			lastHealthz = statusResponse.GetHealthz()
		}
		time.Sleep(250 * time.Millisecond)
	}
	failf("KMS provider did not report unhealthy status: healthz=%q err=%v", lastHealthz, lastErr)
}

func encrypt(
	ctx context.Context,
	client kmsapi.KeyManagementServiceClient,
	statusKeyID string,
) *kmsapi.EncryptResponse {
	encrypted, err := client.Encrypt(ctx, &kmsapi.EncryptRequest{
		Plaintext: []byte(plaintext),
		Uid:       requestUID,
	})
	if err != nil {
		failf("encrypt through provider: %v", err)
	}
	if encrypted.GetKeyId() != statusKeyID {
		failf("encrypt key_id does not match status key_id")
	}
	if len(encrypted.GetCiphertext()) == 0 {
		failf("encrypt returned empty ciphertext")
	}
	if len(encrypted.GetAnnotations()) == 0 {
		failf("encrypt returned no annotations")
	}
	return encrypted
}

func decrypt(ctx context.Context, client kmsapi.KeyManagementServiceClient, encrypted *kmsapi.EncryptResponse) {
	decrypted, err := decryptStored(ctx, client, encryptedSample{
		Ciphertext:  encrypted.GetCiphertext(),
		KeyID:       encrypted.GetKeyId(),
		Annotations: encrypted.GetAnnotations(),
	})
	if err != nil {
		failf("decrypt through provider: %v", err)
	}
	if string(decrypted.GetPlaintext()) != plaintext {
		failf("decrypt returned unexpected plaintext")
	}
}

func decryptSample(
	ctx context.Context,
	client kmsapi.KeyManagementServiceClient,
	sample encryptedSample,
	description string,
) {
	decrypted, err := decryptStored(ctx, client, sample)
	if err != nil {
		failf("decrypt %s: %v", description, err)
	}
	if string(decrypted.GetPlaintext()) != plaintext {
		failf("decrypt %s returned unexpected plaintext", description)
	}
}

func decryptStored(
	ctx context.Context,
	client kmsapi.KeyManagementServiceClient,
	sample encryptedSample,
) (*kmsapi.DecryptResponse, error) {
	return client.Decrypt(ctx, &kmsapi.DecryptRequest{
		Ciphertext:  sample.Ciphertext,
		KeyId:       sample.KeyID,
		Annotations: sample.Annotations,
	})
}

func rejectUnknownKey(
	ctx context.Context,
	client kmsapi.KeyManagementServiceClient,
	encrypted *kmsapi.EncryptResponse,
) {
	unknownKeyID := "obk2." + base64.RawURLEncoding.EncodeToString(make([]byte, 32))
	_, err := client.Decrypt(ctx, &kmsapi.DecryptRequest{
		Ciphertext:  encrypted.GetCiphertext(),
		KeyId:       unknownKeyID,
		Annotations: encrypted.GetAnnotations(),
	})
	assertCode(err, codes.NotFound, "unknown key_id")
}

func rejectTamperedAnnotations(
	ctx context.Context,
	client kmsapi.KeyManagementServiceClient,
	encrypted *kmsapi.EncryptResponse,
) {
	modified := cloneAnnotations(encrypted.GetAnnotations())
	modified[aad.KeyTransitKeyVersion] = []byte("999")
	_, err := client.Decrypt(ctx, &kmsapi.DecryptRequest{
		Ciphertext:  encrypted.GetCiphertext(),
		KeyId:       encrypted.GetKeyId(),
		Annotations: modified,
	})
	assertCode(err, codes.InvalidArgument, "tampered annotations")
}

func assertCode(err error, code codes.Code, operation string) {
	if got := grpcstatus.Code(err); got != code {
		failf("%s returned %s, want %s: %v", operation, got, code, err)
	}
}

func assertAnyCode(err error, codesAllowed []codes.Code, operation string) {
	got := grpcstatus.Code(err)
	for _, code := range codesAllowed {
		if got == code {
			return
		}
	}
	failf("%s returned %s, want one of %v: %v", operation, got, codesAllowed, err)
}

func writeSample(encrypted *kmsapi.EncryptResponse) {
	writeSampleAt(currentSamplePath(), encrypted)
}

func writeSampleAt(path string, encrypted *kmsapi.EncryptResponse) {
	path = requireSamplePath(path)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		failf("create encrypted sample directory: %v", err)
	}
	payload, err := json.Marshal(encryptedSample{
		Ciphertext:  encrypted.GetCiphertext(),
		KeyID:       encrypted.GetKeyId(),
		Annotations: encrypted.GetAnnotations(),
	})
	if err != nil {
		failf("encode encrypted sample: %v", err)
	}
	if err := os.WriteFile(path, payload, 0o600); err != nil {
		failf("write encrypted sample: %v", err)
	}
}

func readSample() encryptedSample {
	return readSampleAt(currentSamplePath())
}

func readSampleAt(path string) encryptedSample {
	path = requireSamplePath(path)
	// #nosec G304 -- the e2e client only reads encrypted sample artifacts from the mounted sample volume.
	payload, err := os.ReadFile(path)
	if err != nil {
		failf("read encrypted sample: %v", err)
	}
	var sample encryptedSample
	if err := json.Unmarshal(payload, &sample); err != nil {
		failf("decode encrypted sample: %v", err)
	}
	if len(sample.Ciphertext) == 0 || sample.KeyID == "" || len(sample.Annotations) == 0 {
		failf("encrypted sample is incomplete")
	}
	return sample
}

func requireSamplePath(path string) string {
	cleaned := filepath.Clean(path)
	if cleaned != path || !strings.HasPrefix(cleaned, sampleMountRoot+"/") {
		failf("encrypted sample path must stay under %s", sampleMountRoot)
	}
	return cleaned
}

func currentSamplePath() string {
	if path := os.Getenv(envKMSSamplePath); path != "" {
		return path
	}
	return samplePath
}

func currentRotationSamplePath() string {
	if path := os.Getenv(envKMSRotationSamplePath); path != "" {
		return path
	}
	return rotationSamplePathDefault
}

func durationFromEnv(name string, fallback time.Duration) time.Duration {
	raw := os.Getenv(name)
	if raw == "" {
		return fallback
	}
	parsed, err := time.ParseDuration(raw)
	if err != nil || parsed <= 0 {
		failf("%s must be a positive duration", name)
	}
	return parsed
}

func intFromEnv(name string, fallback int) int {
	raw := os.Getenv(name)
	if raw == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(raw)
	if err != nil || parsed <= 0 {
		failf("%s must be a positive integer", name)
	}
	return parsed
}

func percentileDuration(values []time.Duration, percentile int) time.Duration {
	if len(values) == 0 {
		return 0
	}
	sorted := append([]time.Duration(nil), values...)
	sort.Slice(sorted, func(i int, j int) bool {
		return sorted[i] < sorted[j]
	})
	index := (len(sorted)*percentile + 99) / 100
	if index <= 0 {
		index = 1
	}
	if index > len(sorted) {
		index = len(sorted)
	}
	return sorted[index-1]
}

func maxDuration(values []time.Duration) time.Duration {
	var maxValue time.Duration
	for _, value := range values {
		if value > maxValue {
			maxValue = value
		}
	}
	return maxValue
}

func cloneAnnotations(annotations map[string][]byte) map[string][]byte {
	cloned := make(map[string][]byte, len(annotations))
	for key, value := range annotations {
		cloned[key] = bytes.Clone(value)
	}
	return cloned
}

func failf(format string, args ...interface{}) {
	_, _ = fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
