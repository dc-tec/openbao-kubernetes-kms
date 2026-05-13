package e2e

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

const e2eSuitesManifestPath = "suites.yaml"

const (
	ciWorkflowPath            = "../../.github/workflows/ci.yml"
	releaseWorkflowPath       = "../../.github/workflows/release.yml"
	versionsPolicyPath        = "../../.ci/versions.yaml"
	e2eMakefilePath           = "../../mk/e2e.mk"
	releaseOpenBaoMakeTarget  = "test-e2e-release-preview-openbao"
	releaseKindMakeTarget     = "test-e2e-release-preview-kind"
	releaseGateExpectedStatus = "active"
)

type e2eSuitesManifest struct {
	Version       int              `yaml:"version"`
	VersionPolicy e2eVersionPolicy `yaml:"versionPolicy"`
	Defaults      e2eSuiteDefaults `yaml:"defaults"`
	ReleaseGate   e2eReleaseGate   `yaml:"releaseGate"`
	Lanes         []e2eLane        `yaml:"lanes"`
}

type e2eVersionPolicy struct {
	Source     string `yaml:"source"`
	OpenBao    string `yaml:"openbao"`
	Kubernetes string `yaml:"kubernetes"`
}

type e2eSuiteDefaults struct {
	Package       string `yaml:"package"`
	ArtifactDir   string `yaml:"artifactDir"`
	Timeout       string `yaml:"timeout"`
	ParallelNodes int    `yaml:"parallelNodes"`
}

type e2eReleaseGate struct {
	Preview e2eReleaseGateDefinition `yaml:"preview"`
}

type e2eReleaseGateDefinition struct {
	Status string              `yaml:"status"`
	Groups map[string][]string `yaml:"groups"`
}

type e2eLane struct {
	ID            string   `yaml:"id"`
	Name          string   `yaml:"name"`
	MakeTarget    string   `yaml:"makeTarget"`
	Package       string   `yaml:"package"`
	LabelFilter   string   `yaml:"labelFilter"`
	PRScope       string   `yaml:"prScope"`
	Environment   string   `yaml:"environment"`
	Isolation     string   `yaml:"isolation"`
	Timeout       string   `yaml:"timeout"`
	ParallelNodes int      `yaml:"parallelNodes"`
	RequiredEnv   []string `yaml:"requiredEnv"`
	EnableEnv     string   `yaml:"enableEnv"`
	VersionRefs   []string `yaml:"versionRefs"`
	Status        string   `yaml:"status"`
	Reports       bool     `yaml:"reports"`
}

type versionsPolicy struct {
	Validation struct {
		OpenBao         openBaoValidationPolicy    `yaml:"openbao"`
		Kubernetes      kubernetesValidationPolicy `yaml:"kubernetes"`
		ReleaseGateRows []releaseGateRow           `yaml:"releaseGateRows"`
	} `yaml:"validation"`
}

type openBaoValidationPolicy struct {
	Primary      string `yaml:"primary"`
	Image        string `yaml:"image"`
	ImageDigest  string `yaml:"imageDigest"`
	DigestStatus string `yaml:"digestStatus"`
}

type kubernetesValidationPolicy struct {
	PreviewMatrix          []kubernetesPreviewMatrixEntry `yaml:"previewMatrix"`
	IntendedNextValidation []kubernetesNextValidationLine `yaml:"intendedNextValidation"`
	KMSV2StableMinimumLine string                         `yaml:"kmsV2StableMinimumLine"`
	MinimumLine            string                         `yaml:"minimumLine"`
	PrimaryLine            string                         `yaml:"primaryLine"`
	ExactVersion           string                         `yaml:"exactVersion"`
	KindNodeImage          string                         `yaml:"kindNodeImage"`
	KindNodeImageDigest    string                         `yaml:"kindNodeImageDigest"`
}

type kubernetesPreviewMatrixEntry struct {
	Line                string `yaml:"line"`
	ExactVersion        string `yaml:"exactVersion"`
	KindNodeImage       string `yaml:"kindNodeImage"`
	KindNodeImageDigest string `yaml:"kindNodeImageDigest"`
	ReleaseGate         bool   `yaml:"releaseGate"`
}

type kubernetesNextValidationLine struct {
	Line   string `yaml:"line"`
	Status string `yaml:"status"`
}

type releaseGateRow struct {
	Name           string `yaml:"name"`
	Component      string `yaml:"component"`
	Version        string `yaml:"version"`
	ExactPinStatus string `yaml:"exactPinStatus"`
}

func TestE2EManifest(t *testing.T) {
	manifest := readE2EManifest(t)
	validateE2EManifest(t, manifest)
}

func TestKubernetesPreviewMatrixPolicy(t *testing.T) {
	policy := readVersionsPolicy(t)
	validateKubernetesPreviewMatrix(t, policy)
}

func TestOpenBaoVersionPolicy(t *testing.T) {
	policy := readVersionsPolicy(t)
	validateOpenBaoVersionPolicy(t, policy)
}

func TestReleaseWorkflowUsesManifestGate(t *testing.T) {
	manifest := readE2EManifest(t)
	validateE2EManifest(t, manifest)

	workflow := readTextFile(t, releaseWorkflowPath)
	for _, target := range []string{releaseOpenBaoMakeTarget, releaseKindMakeTarget} {
		if !strings.Contains(workflow, "make "+target) {
			t.Fatalf("%s must call %q", releaseWorkflowPath, "make "+target)
		}
	}
	if !strings.Contains(workflow, "go run ./hack/tools/e2e_release_gate -group kind -matrix") {
		t.Fatalf("%s must resolve the Kind release gate matrix from .ci/versions.yaml", releaseWorkflowPath)
	}
	if !strings.Contains(workflow, "E2E_KUBERNETES_LINE: ${{ matrix.kubernetes_line }}") {
		t.Fatalf("%s must run Kind release gates per Kubernetes matrix line", releaseWorkflowPath)
	}

	for _, target := range laneMakeTargets(manifest) {
		if strings.Contains(workflow, "make "+target) {
			t.Fatalf("%s must call release gate aggregate targets, not lane target %q", releaseWorkflowPath, target)
		}
	}
}

func TestReleaseGateDefersUnsupportedSPIRELane(t *testing.T) {
	manifest := readE2EManifest(t)
	validateE2EManifest(t, manifest)

	const spireLaneID = "openbao-provider-certauth-spiffe-source-ci"
	for group, laneIDs := range manifest.ReleaseGate.Preview.Groups {
		for _, laneID := range laneIDs {
			if laneID == spireLaneID {
				t.Fatalf("releaseGate.preview.groups.%s must not include unsupported SPIRE lane %q", group, spireLaneID)
			}
		}
	}

	for _, lane := range manifest.Lanes {
		if lane.ID == spireLaneID {
			if lane.Status != "planned" {
				t.Fatalf("SPIRE lane status = %q, want planned", lane.Status)
			}
			return
		}
	}
	t.Fatalf("SPIRE lane %q not found", spireLaneID)
}

func TestCIWorkflowReusesPrebuiltE2EProviderImages(t *testing.T) {
	workflow := readTextFile(t, ciWorkflowPath)

	imageJob := workflowJobSection(t, workflow, "e2e-provider-images")
	for _, want := range []string{
		`make image IMAGE="${E2E_PROVIDER_IMAGE}"`,
		"make image-certauth-pkcs11-e2e",
		"docker save --output artifacts/e2e-images/provider-base.tar",
		"docker save --output artifacts/e2e-images/provider-openbao-variants.tar",
		"name: e2e-provider-base-image",
		"name: e2e-provider-openbao-images",
	} {
		requireContains(t, imageJob, want, "E2E image job")
	}
	requireNotContains(t, imageJob, "spiffe", "E2E image job")

	imageSecurity := workflowJobSection(t, workflow, "image-security")
	for _, want := range []string{
		"needs: [changes, e2e-provider-images]",
		"name: e2e-provider-base-image",
		"docker load --input artifacts/e2e-images/provider-base.tar",
		`make security-scan-image IMAGE="${E2E_PROVIDER_IMAGE}"`,
	} {
		requireContains(t, imageSecurity, want, "image security job")
	}

	openbao := workflowJobSection(t, workflow, "openbao-e2e")
	for _, want := range []string{
		"needs: [changes, core, e2e-provider-images]",
		"name: e2e-provider-base-image",
		"name: e2e-provider-openbao-images",
		"docker load --input artifacts/e2e-images/provider-base.tar",
		"docker load --input artifacts/e2e-images/provider-openbao-variants.tar",
		`E2E_PROVIDER_BUILD=false E2E_PROVIDER_IMAGE="${PROVIDER_IMAGE}" make test-e2e-provider-openbao-ci`,
		`E2E_PROVIDER_BUILD=false E2E_PROVIDER_IMAGE="${PROVIDER_IMAGE}" make test-e2e-provider-failure-openbao-ci`,
		`E2E_PROVIDER_BUILD=false E2E_PROVIDER_IMAGE="${PROVIDER_IMAGE}" make test-e2e-provider-rotation-openbao-ci`,
		`E2E_PROVIDER_BUILD=false E2E_PROVIDER_CERTAUTH_PKCS11_IMAGE="${CERTAUTH_PKCS11_IMAGE}" ` +
			`make test-e2e-provider-certauth-pkcs11-openbao-ci`,
	} {
		requireContains(t, openbao, want, "OpenBao E2E job")
	}
	requireNotContains(t, openbao, "\n      E2E_PROVIDER_IMAGE:", "OpenBao E2E job")
	requireNotContains(t, openbao, "SPIRE", "OpenBao E2E job")
	requireNotContains(t, openbao, "spiffe", "OpenBao E2E job")

	kind := workflowJobSection(t, workflow, "kind-e2e")
	for _, want := range []string{
		"needs: [changes, core, e2e-provider-images]",
		`E2E_PROVIDER_BUILD: "false"`,
		"name: e2e-provider-base-image",
		"docker load --input artifacts/e2e-images/provider-base.tar",
	} {
		requireContains(t, kind, want, "Kind E2E job")
	}
}

func TestReleaseWorkflowRunsE2EAgainstBuiltImage(t *testing.T) {
	workflow := readTextFile(t, releaseWorkflowPath)

	openbao := workflowJobSection(t, workflow, "e2e-openbao")
	for _, want := range []string{
		"needs: [prepare, build]",
		"packages: read",
		`E2E_PROVIDER_BUILD: "false"`,
		"E2E_PROVIDER_RELEASE_IMAGE: ${{ needs.prepare.outputs.image }}@${{ needs.build.outputs.image_digest }}",
		"E2E_PROVIDER_IMAGE: ${{ needs.prepare.outputs.image }}:release-e2e-${{ needs.prepare.outputs.commit }}",
		`docker pull "${E2E_PROVIDER_RELEASE_IMAGE}"`,
		`docker tag "${E2E_PROVIDER_RELEASE_IMAGE}" "${E2E_PROVIDER_IMAGE}"`,
		`make image IMAGE="${E2E_PROVIDER_OLD_IMAGE}"`,
		`make image IMAGE="${E2E_PROVIDER_NEW_IMAGE}"`,
		"make image-certauth-pkcs11-e2e",
		"make test-e2e-release-preview-openbao",
	} {
		requireContains(t, openbao, want, "release OpenBao E2E job")
	}
	requireNotContains(t, openbao, "spiffe", "release OpenBao E2E job")

	kind := workflowJobSection(t, workflow, "e2e-kind")
	for _, want := range []string{
		"needs: [prepare, build, kind-matrix]",
		"packages: read",
		`E2E_PROVIDER_BUILD: "false"`,
		"E2E_PROVIDER_RELEASE_IMAGE: ${{ needs.prepare.outputs.image }}@${{ needs.build.outputs.image_digest }}",
		"E2E_PROVIDER_IMAGE: ${{ needs.prepare.outputs.image }}:release-e2e-${{ needs.prepare.outputs.commit }}",
		`docker pull "${E2E_PROVIDER_RELEASE_IMAGE}"`,
		`docker tag "${E2E_PROVIDER_RELEASE_IMAGE}" "${E2E_PROVIDER_IMAGE}"`,
		"make test-e2e-release-preview-kind",
	} {
		requireContains(t, kind, want, "release Kind E2E job")
	}
}

func TestReleaseGateMakeTargetsExist(t *testing.T) {
	manifest := readE2EManifest(t)
	validateE2EManifest(t, manifest)

	makefile := readTextFile(t, e2eMakefilePath)
	for _, target := range []string{releaseOpenBaoMakeTarget, releaseKindMakeTarget} {
		if !strings.Contains(makefile, ".PHONY: "+target) || !strings.Contains(makefile, target+":") {
			t.Fatalf("%s must define aggregate target %q", e2eMakefilePath, target)
		}
	}
	for _, target := range releaseGateMakeTargets(manifest) {
		if !strings.Contains(makefile, target) {
			t.Fatalf("%s must define or generate lane target %q", e2eMakefilePath, target)
		}
	}
}

func readE2EManifest(t *testing.T) e2eSuitesManifest {
	t.Helper()

	raw, err := os.ReadFile(e2eSuitesManifestPath)
	if err != nil {
		t.Fatalf("read %s: %v", e2eSuitesManifestPath, err)
	}
	if strings.Contains(strings.ToLower(string(raw)), "latest") {
		t.Fatalf("%s must not contain floating latest references", e2eSuitesManifestPath)
	}

	var manifest e2eSuitesManifest
	decoder := yaml.NewDecoder(bytes.NewReader(raw))
	decoder.KnownFields(true)
	if err := decoder.Decode(&manifest); err != nil {
		t.Fatalf("decode %s: %v", e2eSuitesManifestPath, err)
	}

	return manifest
}

func readTextFile(t *testing.T, path string) string {
	t.Helper()

	// #nosec G304 -- tests read fixed repository paths from local constants.
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(raw)
}

func workflowJobSection(t *testing.T, workflow string, job string) string {
	t.Helper()

	marker := "\n  " + job + ":\n"
	start := strings.Index(workflow, marker)
	if start == -1 {
		t.Fatalf("workflow job %q not found", job)
	}
	start++
	section := workflow[start : start+len(marker)-1]
	for _, line := range strings.Split(workflow[start+len(marker)-1:], "\n") {
		if strings.HasPrefix(line, "  ") &&
			!strings.HasPrefix(line, "    ") &&
			strings.HasSuffix(strings.TrimSpace(line), ":") {
			break
		}
		section += line + "\n"
	}
	return section
}

func requireContains(t *testing.T, text string, want string, context string) {
	t.Helper()

	if !strings.Contains(text, want) {
		t.Fatalf("%s must contain %q", context, want)
	}
}

func requireNotContains(t *testing.T, text string, unwanted string, context string) {
	t.Helper()

	if strings.Contains(text, unwanted) {
		t.Fatalf("%s must not contain %q", context, unwanted)
	}
}

func readVersionsPolicy(t *testing.T) versionsPolicy {
	t.Helper()

	raw, err := os.ReadFile(versionsPolicyPath)
	if err != nil {
		t.Fatalf("read %s: %v", versionsPolicyPath, err)
	}

	var policy versionsPolicy
	if err := yaml.Unmarshal(raw, &policy); err != nil {
		t.Fatalf("decode %s: %v", versionsPolicyPath, err)
	}
	return policy
}

func validateE2EManifest(t *testing.T, manifest e2eSuitesManifest) {
	t.Helper()

	if manifest.Version != 1 {
		t.Fatalf("manifest version = %d, want 1", manifest.Version)
	}
	if manifest.VersionPolicy.Source != ".ci/versions.yaml" {
		t.Fatalf("version policy source = %q, want .ci/versions.yaml", manifest.VersionPolicy.Source)
	}
	if manifest.VersionPolicy.OpenBao == "" || manifest.VersionPolicy.Kubernetes == "" {
		t.Fatalf("version policy must reference OpenBao and Kubernetes version fields")
	}
	if manifest.Defaults.Package == "" {
		t.Fatalf("defaults.package is required")
	}
	if manifest.Defaults.ArtifactDir == "" {
		t.Fatalf("defaults.artifactDir is required")
	}
	if manifest.Defaults.Timeout == "" {
		t.Fatalf("defaults.timeout is required")
	}
	if manifest.Defaults.ParallelNodes < 1 {
		t.Fatalf("defaults.parallelNodes must be at least 1")
	}
	if len(manifest.Lanes) == 0 {
		t.Fatalf("at least one e2e lane is required")
	}

	seenIDs := make(map[string]struct{}, len(manifest.Lanes))
	lanesByID := make(map[string]e2eLane, len(manifest.Lanes))
	for _, lane := range manifest.Lanes {
		validateE2ELane(t, lane, seenIDs)
		lanesByID[lane.ID] = lane
	}
	validateReleaseGate(t, manifest.ReleaseGate, lanesByID)
}

func validateKubernetesPreviewMatrix(t *testing.T, policy versionsPolicy) {
	t.Helper()

	kubernetes := policy.Validation.Kubernetes
	validateKubernetesLinePolicy(t, kubernetes)
	releaseGateLines := collectKubernetesReleaseGateLines(t, kubernetes)
	validateKubernetesRequiredReleaseGateLines(t, releaseGateLines)
	validateKubernetesPrimaryLine(t, kubernetes, releaseGateLines)
	validateKubernetesIntendedNextValidation(t, kubernetes)
	validateKubernetesReleaseGateRows(t, policy.Validation.ReleaseGateRows, releaseGateLines)
}

func validateKubernetesLinePolicy(t *testing.T, kubernetes kubernetesValidationPolicy) {
	t.Helper()

	if kubernetes.KMSV2StableMinimumLine != "1.29" {
		t.Fatalf("kmsV2StableMinimumLine = %q, want 1.29", kubernetes.KMSV2StableMinimumLine)
	}
	if kubernetes.MinimumLine != "1.34" {
		t.Fatalf("minimumLine = %q, want 1.34", kubernetes.MinimumLine)
	}
	if kubernetes.PrimaryLine != "1.34" {
		t.Fatalf("primaryLine = %q, want 1.34", kubernetes.PrimaryLine)
	}
}

func collectKubernetesReleaseGateLines(
	t *testing.T,
	kubernetes kubernetesValidationPolicy,
) map[string]kubernetesPreviewMatrixEntry {
	t.Helper()

	releaseGateLines := make(map[string]kubernetesPreviewMatrixEntry, len(kubernetes.PreviewMatrix))
	for _, entry := range kubernetes.PreviewMatrix {
		if !entry.ReleaseGate {
			continue
		}
		validateKubernetesReleaseGateEntry(t, entry)
		releaseGateLines[entry.Line] = entry
	}
	return releaseGateLines
}

func validateKubernetesReleaseGateEntry(t *testing.T, entry kubernetesPreviewMatrixEntry) {
	t.Helper()

	if entry.Line == "" || entry.ExactVersion == "" ||
		entry.KindNodeImage == "" || entry.KindNodeImageDigest == "" {
		t.Fatalf(
			"release-gated Kubernetes matrix entry must have line, exactVersion, kindNodeImage, and digest: %#v",
			entry,
		)
	}
	if !strings.Contains(entry.KindNodeImage, "@"+entry.KindNodeImageDigest) {
		t.Fatalf("Kubernetes %s kindNodeImage must include kindNodeImageDigest", entry.Line)
	}
}

func validateKubernetesRequiredReleaseGateLines(
	t *testing.T,
	releaseGateLines map[string]kubernetesPreviewMatrixEntry,
) {
	t.Helper()

	for _, line := range []string{"1.34", "1.35"} {
		if _, ok := releaseGateLines[line]; !ok {
			t.Fatalf("Kubernetes preview release gate must include line %s", line)
		}
	}
	if _, ok := releaseGateLines["1.36"]; ok {
		t.Fatalf("Kubernetes 1.36 must remain outside the release gate until a pinned Kind node image exists")
	}
}

func validateKubernetesPrimaryLine(
	t *testing.T,
	kubernetes kubernetesValidationPolicy,
	releaseGateLines map[string]kubernetesPreviewMatrixEntry,
) {
	t.Helper()

	primary, ok := releaseGateLines[kubernetes.PrimaryLine]
	if !ok {
		t.Fatalf("primaryLine %q must be present in the release-gated preview matrix", kubernetes.PrimaryLine)
	}
	if kubernetes.ExactVersion != primary.ExactVersion {
		t.Fatalf("primary exactVersion = %q, want %q from preview matrix", kubernetes.ExactVersion, primary.ExactVersion)
	}
	if kubernetes.KindNodeImage != primary.KindNodeImage {
		t.Fatalf("primary kindNodeImage = %q, want %q from preview matrix", kubernetes.KindNodeImage, primary.KindNodeImage)
	}
	if kubernetes.KindNodeImageDigest != primary.KindNodeImageDigest {
		t.Fatalf(
			"primary kindNodeImageDigest = %q, want %q from preview matrix",
			kubernetes.KindNodeImageDigest,
			primary.KindNodeImageDigest,
		)
	}
}

func validateKubernetesIntendedNextValidation(t *testing.T, kubernetes kubernetesValidationPolicy) {
	t.Helper()

	found136 := false
	for _, entry := range kubernetes.IntendedNextValidation {
		if entry.Line == "1.36" && entry.Status == "awaiting-pinned-kind-node-image" {
			found136 = true
		}
	}
	if !found136 {
		t.Fatalf("Kubernetes intendedNextValidation must include 1.36 awaiting a pinned Kind node image")
	}
}

func validateKubernetesReleaseGateRows(
	t *testing.T,
	rows []releaseGateRow,
	releaseGateLines map[string]kubernetesPreviewMatrixEntry,
) {
	t.Helper()

	releaseGateRowsByVersion := make(map[string]releaseGateRow, len(rows))
	for _, row := range rows {
		if row.Component == "kubernetes" {
			releaseGateRowsByVersion[row.Version] = row
		}
	}
	for _, entry := range releaseGateLines {
		row, ok := releaseGateRowsByVersion[entry.ExactVersion]
		if !ok {
			t.Fatalf("releaseGateRows must include Kubernetes exact version %s", entry.ExactVersion)
		}
		if row.ExactPinStatus != "pinned-kind-node" {
			t.Fatalf(
				"releaseGateRows Kubernetes %s exactPinStatus = %q, want pinned-kind-node",
				entry.ExactVersion,
				row.ExactPinStatus,
			)
		}
	}
}

func validateOpenBaoVersionPolicy(t *testing.T, policy versionsPolicy) {
	t.Helper()

	openbao := policy.Validation.OpenBao
	if openbao.Primary == "" {
		t.Fatalf("OpenBao primary version is required")
	}
	if openbao.Image == "" || openbao.ImageDigest == "" {
		t.Fatalf("OpenBao image and imageDigest are required")
	}
	if openbao.DigestStatus != "pinned" {
		t.Fatalf("OpenBao digestStatus = %q, want pinned", openbao.DigestStatus)
	}
	if !strings.Contains(openbao.Image, ":"+openbao.Primary+"@"+openbao.ImageDigest) {
		t.Fatalf("OpenBao image must include primary version and imageDigest, got %q", openbao.Image)
	}

	for _, row := range policy.Validation.ReleaseGateRows {
		if row.Component == "openbao" && row.Version == openbao.Primary {
			return
		}
	}
	t.Fatalf("releaseGateRows must include OpenBao primary version %s", openbao.Primary)
}

func validateE2ELane(t *testing.T, lane e2eLane, seenIDs map[string]struct{}) {
	t.Helper()

	if lane.ID == "" {
		t.Fatalf("lane id is required")
	}
	if _, ok := seenIDs[lane.ID]; ok {
		t.Fatalf("duplicate lane id %q", lane.ID)
	}
	seenIDs[lane.ID] = struct{}{}

	if lane.Name == "" {
		t.Fatalf("lane %q name is required", lane.ID)
	}
	if lane.ID == "release-gate" {
		t.Fatalf("release gate must be represented by releaseGate.preview, not a placeholder lane")
	}
	if lane.MakeTarget == "" {
		t.Fatalf("lane %q makeTarget is required", lane.ID)
	}
	if lane.Package == "" {
		t.Fatalf("lane %q package is required", lane.ID)
	}
	if lane.LabelFilter == "" {
		t.Fatalf("lane %q labelFilter is required", lane.ID)
	}
	if lane.PRScope == "" {
		t.Fatalf("lane %q prScope is required", lane.ID)
	}
	if lane.Environment == "" {
		t.Fatalf("lane %q environment is required", lane.ID)
	}
	if lane.Isolation == "" {
		t.Fatalf("lane %q isolation is required", lane.ID)
	}
	if lane.Timeout == "" {
		t.Fatalf("lane %q timeout is required", lane.ID)
	}
	if lane.ParallelNodes < 1 {
		t.Fatalf("lane %q parallelNodes must be at least 1", lane.ID)
	}
	if lane.Status != "active" && lane.Status != "planned" {
		t.Fatalf("lane %q status = %q, want active or planned", lane.ID, lane.Status)
	}
}

func validateReleaseGate(t *testing.T, gate e2eReleaseGate, lanesByID map[string]e2eLane) {
	t.Helper()

	if gate.Preview.Status != releaseGateExpectedStatus {
		t.Fatalf("releaseGate.preview.status = %q, want %q", gate.Preview.Status, releaseGateExpectedStatus)
	}
	if len(gate.Preview.Groups) == 0 {
		t.Fatalf("releaseGate.preview.groups is required")
	}
	for _, group := range []string{"openbao", "kind"} {
		if len(gate.Preview.Groups[group]) == 0 {
			t.Fatalf("releaseGate.preview.groups.%s is required", group)
		}
	}

	seen := make(map[string]string)
	for group, laneIDs := range gate.Preview.Groups {
		if group == "" {
			t.Fatalf("releaseGate.preview.groups must not contain an empty group name")
		}
		for _, laneID := range laneIDs {
			if laneID == "" {
				t.Fatalf("releaseGate.preview.groups.%s must not contain an empty lane id", group)
			}
			if previousGroup, ok := seen[laneID]; ok {
				t.Fatalf("release gate lane %q appears in both %q and %q", laneID, previousGroup, group)
			}
			seen[laneID] = group
			lane, ok := lanesByID[laneID]
			if !ok {
				t.Fatalf("release gate references unknown lane %q", laneID)
			}
			if lane.Status != "active" {
				t.Fatalf("release gate lane %q status = %q, want active", laneID, lane.Status)
			}
			if lane.MakeTarget == "" {
				t.Fatalf("release gate lane %q makeTarget is required", laneID)
			}
		}
	}
}

func releaseGateMakeTargets(manifest e2eSuitesManifest) []string {
	lanesByID := make(map[string]e2eLane, len(manifest.Lanes))
	for _, lane := range manifest.Lanes {
		lanesByID[lane.ID] = lane
	}

	var targets []string
	for _, laneIDs := range manifest.ReleaseGate.Preview.Groups {
		for _, laneID := range laneIDs {
			if lane, ok := lanesByID[laneID]; ok && lane.MakeTarget != "" {
				targets = append(targets, lane.MakeTarget)
			}
		}
	}
	return targets
}

func laneMakeTargets(manifest e2eSuitesManifest) []string {
	targets := make([]string, 0, len(manifest.Lanes))
	for _, lane := range manifest.Lanes {
		if lane.MakeTarget != "" {
			targets = append(targets, lane.MakeTarget)
		}
	}
	return targets
}
