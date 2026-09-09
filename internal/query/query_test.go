package query

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v5"
	"github.com/wbreza/archie/contract"
	"github.com/wbreza/archie/internal/repository"
	"github.com/wbreza/archie/internal/testutil"
	"github.com/wbreza/archie/schemas"
)

var queryScratchDir = testutil.ScratchBase()

type repositoryQueryFixture struct {
	Name    string            `json:"name"`
	Files   map[string]string `json:"files"`
	Context struct {
		IDs             []string `json:"ids"`
		MaxRecords      int      `json:"max_records"`
		FirstPage       []string `json:"first_page"`
		NextPage        []string `json:"next_page"`
		EvidenceTargets []string `json:"evidence_targets"`
	} `json:"context"`
}

type evidenceQueryFixture struct {
	Name       string  `json:"name"`
	Baseline   string  `json:"baseline"`
	Current    *string `json:"current"`
	Before     *string `json:"before"`
	Index      *string `json:"index"`
	Relation   string  `json:"relation"`
	MaxTargets int     `json:"max_targets"`
	MaxBytes   int     `json:"max_bytes"`
	State      string  `json:"state"`
	Reason     string  `json:"reason"`
	Unstable   bool    `json:"unstable"`
}

type evidenceRepo struct {
	root     string
	target   string
	id       string
	baseline string
	relation contract.Relation
}

func moduleRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("missing caller")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
}

func contractFixtureDir(t *testing.T) string {
	t.Helper()
	return filepath.Join(moduleRoot(t), "testdata", "contract")
}

func readContractFixture(t *testing.T, name string, target any) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(contractFixtureDir(t), name))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, target); err != nil {
		t.Fatal(err)
	}
}

func compileResponseSchema(t *testing.T) *jsonschema.Schema {
	t.Helper()
	s, err := schemas.Compile("response")
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func validateEnvelope(t *testing.T, raw []byte) map[string]any {
	t.Helper()
	if bytes.HasPrefix(raw, []byte{0xEF, 0xBB, 0xBF}) {
		t.Fatal("response must not start with a BOM")
	}
	if !bytes.HasSuffix(raw, []byte("\n")) || bytes.Count(raw, []byte("\n")) != 1 {
		t.Fatalf("response must be compact JSON with one trailing LF, got %q", raw)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	if err := compileResponseSchema(t).Validate(doc); err != nil {
		t.Fatal(err)
	}
	return doc
}

func decodeQueryResponse(t *testing.T, raw []byte) contract.Response[contract.QueryData] {
	t.Helper()
	var response contract.Response[contract.QueryData]
	if err := json.Unmarshal(raw, &response); err != nil {
		t.Fatal(err)
	}
	return response
}

func decodeValidationResponse(t *testing.T, raw []byte) contract.Response[contract.ValidationData] {
	t.Helper()
	var response contract.Response[contract.ValidationData]
	if err := json.Unmarshal(raw, &response); err != nil {
		t.Fatal(err)
	}
	return response
}

func decodeRawResponse(t *testing.T, raw []byte) contract.Response[json.RawMessage] {
	t.Helper()
	var response contract.Response[json.RawMessage]
	if err := json.Unmarshal(raw, &response); err != nil {
		t.Fatal(err)
	}
	return response
}

func queryRecordIDs(response contract.Response[contract.QueryData]) []string {
	if response.Data == nil {
		return nil
	}
	ids := make([]string, 0, len(response.Data.Records))
	for _, view := range response.Data.Records {
		ids = append(ids, view.Record.ID)
	}
	return ids
}

func uniqueEvidenceTargets(evidence []contract.Evidence) []string {
	seen := map[string]bool{}
	targets := []string{}
	for _, row := range evidence {
		if !seen[row.Target] {
			seen[row.Target] = true
			targets = append(targets, row.Target)
		}
	}
	sort.Strings(targets)
	return targets
}

func viewRecordKeys(t *testing.T, doc map[string]any, index int) []string {
	t.Helper()
	records := doc["data"].(map[string]any)["records"].([]any)
	record := records[index].(map[string]any)["record"].(map[string]any)
	keys := make([]string, 0, len(record))
	for key := range record {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func omissionCount(omissions []contract.Omission, reason string) int {
	for _, omission := range omissions {
		if omission.Reason == reason {
			return omission.Count
		}
	}
	return 0
}

func findRepositoryFixture(t *testing.T, name string) repositoryQueryFixture {
	t.Helper()
	var fixtures []repositoryQueryFixture
	readContractFixture(t, "repositories.json", &fixtures)
	for _, fixture := range fixtures {
		if fixture.Name == name {
			return fixture
		}
	}
	t.Fatalf("repository fixture %q not found", name)
	return repositoryQueryFixture{}
}

func loadEvidenceFixtures(t *testing.T) []evidenceQueryFixture {
	t.Helper()
	var fixtures []evidenceQueryFixture
	readContractFixture(t, "evidence.json", &fixtures)
	return fixtures
}

func sanitizeName(name string) string {
	replacer := strings.NewReplacer(" ", "-", "/", "-", "\\", "-", ":", "-", "*", "-", "?", "-", "\"", "-", "<", "-", ">", "-", "|", "-")
	return replacer.Replace(name)
}

func mkdirScratch(t *testing.T, prefix string) string {
	t.Helper()
	if err := os.MkdirAll(queryScratchDir, 0o755); err != nil {
		t.Fatal(err)
	}
	dir, err := os.MkdirTemp(queryScratchDir, prefix)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(dir); err != nil {
			t.Errorf("remove scratch dir: %v", err)
		}
	})
	return dir
}

func writeFiles(t *testing.T, root string, files map[string]string) {
	t.Helper()
	paths := make([]string, 0, len(files))
	for rel := range files {
		paths = append(paths, rel)
	}
	sort.Slice(paths, func(i, j int) bool {
		a, b := paths[i], paths[j]
		la, lb := strings.ToLower(a), strings.ToLower(b)
		if la != lb {
			return la < lb
		}
		return a > b
	})
	folded := map[string]string{}
	for _, rel := range paths {
		clean := filepath.Clean(filepath.FromSlash(rel))
		if clean == "." {
			t.Fatalf("invalid fixture path: %q", rel)
		}
		if runtime.GOOS == "windows" {
			key := strings.ToLower(clean)
			if prior, exists := folded[key]; exists && prior != clean {
				t.Logf("skipping %s because %s collides on a case-insensitive filesystem", clean, prior)
				continue
			}
			folded[key] = clean
		}
		absolute := filepath.Join(root, clean)
		if err := os.MkdirAll(filepath.Dir(absolute), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(absolute, []byte(files[rel]), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func materializeRepositoryFixture(t *testing.T, fixture repositoryQueryFixture) string {
	t.Helper()
	root := mkdirScratch(t, sanitizeName(fixture.Name)+"-")
	writeFiles(t, root, fixture.Files)
	return root
}

func gitEnv() []string {
	return append(os.Environ(),
		"GIT_TERMINAL_PROMPT=0",
		"GIT_AUTHOR_NAME=Copilot",
		"GIT_AUTHOR_EMAIL=copilot@example.com",
		"GIT_AUTHOR_DATE=2000-01-01T00:00:00Z",
		"GIT_COMMITTER_NAME=Copilot",
		"GIT_COMMITTER_EMAIL=copilot@example.com",
		"GIT_COMMITTER_DATE=2000-01-01T00:00:00Z",
	)
}

func runGit(t *testing.T, root string, stdin []byte, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = root
	cmd.Env = gitEnv()
	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, output)
	}
	return strings.TrimSpace(string(output))
}

func initGitRepo(t *testing.T, root string) {
	t.Helper()
	runGit(t, root, nil, "init", "--quiet")
}

func hashBlob(t *testing.T, root, content string) string {
	t.Helper()
	return runGit(t, root, []byte(content), "hash-object", "-w", "--stdin")
}

func writeTree(t *testing.T, root string, files map[string]string) string {
	t.Helper()
	fileEntries := map[string]string{}
	dirEntries := map[string]map[string]string{}
	for rel, content := range files {
		parts := strings.Split(rel, "/")
		if len(parts) == 1 {
			fileEntries[parts[0]] = content
			continue
		}
		if dirEntries[parts[0]] == nil {
			dirEntries[parts[0]] = map[string]string{}
		}
		dirEntries[parts[0]][strings.Join(parts[1:], "/")] = content
	}
	names := make([]string, 0, len(fileEntries)+len(dirEntries))
	for name := range fileEntries {
		names = append(names, name)
	}
	for name := range dirEntries {
		names = append(names, name)
	}
	sort.Strings(names)
	var tree bytes.Buffer
	for _, name := range names {
		if content, ok := fileEntries[name]; ok {
			fmt.Fprintf(&tree, "100644 blob %s\t%s\x00", hashBlob(t, root, content), name)
			continue
		}
		fmt.Fprintf(&tree, "040000 tree %s\t%s\x00", writeTree(t, root, dirEntries[name]), name)
	}
	return runGit(t, root, tree.Bytes(), "mktree", "-z")
}

func createCommit(t *testing.T, root string, files map[string]string) string {
	t.Helper()
	return runGit(t, root, nil, "commit-tree", writeTree(t, root, files), "-m", "synthetic baseline")
}

func stageFile(t *testing.T, root, rel, content string) {
	t.Helper()
	absolute := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(absolute), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(absolute, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, root, nil, "add", "--", rel)
}

func contentFor(label string) string {
	switch label {
	case "same":
		return "same"
	case "old":
		return "old"
	case "new":
		return "new"
	case "dirty":
		return "dirty"
	case "working":
		return "working"
	case "staged":
		return "staged"
	case "untracked":
		return "untracked"
	case "unstable":
		return "unstable"
	default:
		return label
	}
}

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func setupEvidenceRepo(t *testing.T, fixture evidenceQueryFixture) evidenceRepo {
	t.Helper()
	root := mkdirScratch(t, sanitizeName(fixture.Name)+"-")
	initGitRepo(t, root)

	id := "item"
	relation := contract.Source
	target := "src/evidence.txt"
	if fixture.Relation == "adr" {
		relation = contract.ADR
		target = "docs/decision.md"
	}

	rootDescriptor := "schema_version: \"1\"\nid: app\nname: App\nsummary: Root.\n"
	recordDescriptor := fmt.Sprintf(
		"schema_version: \"1\"\nid: %s\nname: Item\nsummary: Fixture.\nlinks:\n  - {relation: %s, target: %s, description: Fixture evidence.}\n",
		id, relation, target,
	)

	currentFiles := map[string]string{
		"archie.yaml":      rootDescriptor,
		"item.archie.yaml": recordDescriptor,
	}
	if fixture.Current != nil {
		currentFiles[target] = contentFor(*fixture.Current)
	}
	writeFiles(t, root, currentFiles)

	var baseline string
	switch fixture.Baseline {
	case "none":
	case "available":
		baselineFiles := map[string]string{
			"archie.yaml":      rootDescriptor,
			"item.archie.yaml": recordDescriptor,
		}
		if fixture.Before != nil {
			baselineFiles[target] = contentFor(*fixture.Before)
		}
		baseline = createCommit(t, root, baselineFiles)
	case "unavailable":
		_ = createCommit(t, root, map[string]string{
			"archie.yaml":      rootDescriptor,
			"item.archie.yaml": recordDescriptor,
		})
		baseline = strings.Repeat("1", 40)
	default:
		t.Fatalf("unknown baseline mode %q", fixture.Baseline)
	}

	if fixture.Index != nil {
		stageFile(t, root, target, contentFor(*fixture.Index))
		if fixture.Current == nil {
			if err := os.Remove(filepath.Join(root, filepath.FromSlash(target))); err != nil && !os.IsNotExist(err) {
				t.Fatal(err)
			}
		} else {
			if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(target)), []byte(contentFor(*fixture.Current)), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}

	return evidenceRepo{root: root, target: target, id: id, baseline: baseline, relation: relation}
}

func expectedEvidenceOutcome(state, reason string) (contract.Status, int) {
	if state == "missing" || state == "unchecked" || reason == "baseline_unavailable" || reason == "unattributable" || reason == "read_error" || reason == "not_regular" {
		return contract.Partial, contract.ExitPartial
	}
	return contract.OK, contract.ExitOK
}

func TestEvidenceCatalog(t *testing.T) {
	for _, fixture := range loadEvidenceFixtures(t) {
		t.Run(fixture.Name, func(t *testing.T) {
			repo := setupEvidenceRepo(t, fixture)
			options := Defaults(contract.Get)
			options.Root = repo.root
			options.IDs = []string{repo.id}
			options.Baseline = repo.baseline
			options.Limits.EvidenceTargets = fixture.MaxTargets
			options.Limits.EvidenceBytes = fixture.MaxBytes

			if fixture.Unstable {
				graph, err := repository.Load(repo.root)
				if err != nil {
					t.Fatal(err)
				}
				defer func() {
					if closeErr := graph.FS.Close(); closeErr != nil {
						t.Errorf("close filesystem: %v", closeErr)
					}
				}()
				checker := newChecker(graph, options)
				checker.beforeRead = func(target string) {
					absolute := filepath.Join(repo.root, filepath.FromSlash(target))
					if writeErr := os.WriteFile(absolute, []byte(contentFor(stringValue(fixture.Current))+"x"), 0o644); writeErr != nil {
						t.Fatal(writeErr)
					}
				}
				got := checker.check(repo.target, true)
				if got.State != fixture.State || got.Reason != fixture.Reason {
					t.Fatalf("evidence = %+v, want state=%s reason=%s", got, fixture.State, fixture.Reason)
				}
				return
			}

			raw, exitCode := Run(options)
			doc := validateEnvelope(t, raw)
			response := decodeQueryResponse(t, raw)

			wantStatus, wantExit := expectedEvidenceOutcome(fixture.State, fixture.Reason)
			if response.Status != wantStatus || exitCode != wantExit {
				t.Fatalf("status/exit = %s/%d, want %s/%d", response.Status, exitCode, wantStatus, wantExit)
			}
			if response.Data == nil || len(response.Data.Records) != 1 || len(response.Data.Evidence) != 1 {
				t.Fatalf("unexpected data shape: %+v", response.Data)
			}
			row := response.Data.Evidence[0]
			if row.RecordID != repo.id || row.Relation != repo.relation || row.Target != repo.target || row.State != fixture.State || row.Reason != fixture.Reason {
				t.Fatalf("evidence row = %+v", row)
			}
			switch fixture.Baseline {
			case "none":
				if response.Baseline.State != "none" || response.Baseline.Reason != "not_supplied" {
					t.Fatalf("baseline = %+v", response.Baseline)
				}
			case "available":
				if response.Baseline.State != "available" || response.Baseline.Resolved != repo.baseline {
					t.Fatalf("baseline = %+v, want resolved %s", response.Baseline, repo.baseline)
				}
			case "unavailable":
				if response.Baseline.State != "unavailable" || response.Baseline.Requested != repo.baseline {
					t.Fatalf("baseline = %+v, want unavailable %s", response.Baseline, repo.baseline)
				}
			}
			if got := doc["command"]; got != string(contract.Get) {
				t.Fatalf("command = %v, want %s", got, contract.Get)
			}
		})
	}
}

func TestContextUsesFixturePaginationAndCursorBinding(t *testing.T) {
	fixture := findRepositoryFixture(t, "cycles-and-shared-target")
	root := materializeRepositoryFixture(t, fixture)

	options := Defaults(contract.Context)
	options.Root = root
	options.IDs = append([]string(nil), fixture.Context.IDs...)
	options.Limits.Records = fixture.Context.MaxRecords

	raw, exitCode := Run(options)
	validateEnvelope(t, raw)
	response := decodeQueryResponse(t, raw)

	if response.Status != contract.Partial || exitCode != contract.ExitPartial {
		t.Fatalf("status/exit = %s/%d, want partial/%d", response.Status, exitCode, contract.ExitPartial)
	}
	if got := queryRecordIDs(response); !slices.Equal(got, fixture.Context.FirstPage) {
		t.Fatalf("first page IDs = %v, want %v", got, fixture.Context.FirstPage)
	}
	if got := uniqueEvidenceTargets(response.Data.Evidence); !slices.Equal(got, fixture.Context.EvidenceTargets) {
		t.Fatalf("first page evidence targets = %v, want %v", got, fixture.Context.EvidenceTargets)
	}
	if response.Continuation == nil {
		t.Fatal("expected continuation")
	}

	second := options
	second.Cursor = response.Continuation.Cursor
	rawNext, exitNext := Run(second)
	validateEnvelope(t, rawNext)
	next := decodeQueryResponse(t, rawNext)
	if next.Status != contract.Partial || exitNext != contract.ExitPartial {
		t.Fatalf("next status/exit = %s/%d, want partial/%d", next.Status, exitNext, contract.ExitPartial)
	}
	if got := queryRecordIDs(next); !slices.Equal(got, fixture.Context.NextPage) {
		t.Fatalf("next page IDs = %v, want %v", got, fixture.Context.NextPage)
	}

	mismatch := options
	mismatch.Cursor = response.Continuation.Cursor
	mismatch.Limits.Records++
	rawMismatch, exitMismatch := Run(mismatch)
	validateEnvelope(t, rawMismatch)
	mismatchResponse := decodeRawResponse(t, rawMismatch)
	if mismatchResponse.Status != contract.Error || exitMismatch != contract.ExitUsage || mismatchResponse.Diagnostics[0].Code != "CURSOR_INVALID" {
		t.Fatalf("query mismatch = %+v exit=%d", mismatchResponse, exitMismatch)
	}

	payload, err := base64.RawURLEncoding.DecodeString(response.Continuation.Cursor)
	if err != nil {
		t.Fatal(err)
	}
	badPayload := append(append([]byte{}, payload[:len(payload)-1]...), []byte(",\"extra\":true}")...)
	invalid := options
	invalid.Cursor = base64.RawURLEncoding.EncodeToString(badPayload)
	rawInvalid, exitInvalid := Run(invalid)
	validateEnvelope(t, rawInvalid)
	invalidResponse := decodeRawResponse(t, rawInvalid)
	if invalidResponse.Status != contract.Error || exitInvalid != contract.ExitUsage || invalidResponse.Diagnostics[0].Code != "CURSOR_INVALID" {
		t.Fatalf("invalid cursor = %+v exit=%d", invalidResponse, exitInvalid)
	}

	writeFiles(t, root, map[string]string{
		"b.archie.yaml": "schema_version: \"1\"\nid: b\nname: Model\nsummary: Changed after page one.\nlinks:\n  - {relation: depends_on, target: a, description: Renderer association.}\n  - {relation: source, target: src/shared.go, description: Shared source.}\n",
	})
	stale := options
	stale.Cursor = response.Continuation.Cursor
	rawStale, exitStale := Run(stale)
	validateEnvelope(t, rawStale)
	staleResponse := decodeRawResponse(t, rawStale)
	if staleResponse.Status != contract.Error || exitStale != contract.ExitUsage || staleResponse.Diagnostics[0].Code != "CURSOR_STALE" {
		t.Fatalf("stale cursor = %+v exit=%d", staleResponse, exitStale)
	}
}

func TestContextReturnsRootFirstSummaryOnlyAncestors(t *testing.T) {
	fixture := findRepositoryFixture(t, "flat-named-and-repeated-hierarchy")
	root := materializeRepositoryFixture(t, fixture)

	options := Defaults(contract.Context)
	options.Root = root
	options.IDs = []string{"region"}

	raw, exitCode := Run(options)
	doc := validateEnvelope(t, raw)
	response := decodeQueryResponse(t, raw)

	if response.Status != contract.OK || exitCode != contract.ExitOK {
		t.Fatalf("status/exit = %s/%d, want ok/%d", response.Status, exitCode, contract.ExitOK)
	}
	if got := queryRecordIDs(response); !slices.Equal(got, []string{"app", "payments", "region"}) {
		t.Fatalf("IDs = %v", got)
	}
	if got := response.Data.Records[0].Reasons; !slices.Equal(got, []string{"ancestor", "root"}) {
		t.Fatalf("root reasons = %v", got)
	}
	if got := response.Data.Records[1].Reasons; !slices.Equal(got, []string{"ancestor"}) {
		t.Fatalf("payments reasons = %v", got)
	}
	if got := response.Data.Records[2].Reasons; !slices.Equal(got, []string{"id"}) {
		t.Fatalf("region reasons = %v", got)
	}
	if !response.Data.Records[0].SummaryOnly || !response.Data.Records[1].SummaryOnly || response.Data.Records[2].SummaryOnly {
		t.Fatalf("summary_only flags = %+v", response.Data.Records)
	}
	if got := viewRecordKeys(t, doc, 0); !slices.Equal(got, []string{"id", "name", "schema_version", "summary"}) {
		t.Fatalf("root summary-only fields = %v", got)
	}
	if got := viewRecordKeys(t, doc, 1); !slices.Equal(got, []string{"id", "name", "schema_version", "summary"}) {
		t.Fatalf("ancestor summary-only fields = %v", got)
	}
}

func TestContextSelectionOrdersIDPathThenWeightedKeywords(t *testing.T) {
	root := mkdirScratch(t, "selection-order-")
	writeFiles(t, root, map[string]string{
		"archie.yaml":              "schema_version: \"1\"\nid: app\nname: App\nsummary: Root.\n",
		"gamma.archie.yaml":        "schema_version: \"1\"\nid: gamma\nname: Gamma\nsummary: Exact ID seed.\n",
		"pkg/archie.yaml":          "schema_version: \"1\"\nid: pkg\nname: Package\nsummary: Path seed.\n",
		"pkg/src/file.go":          "package pkg\n",
		"search/alpha.archie.yaml": "schema_version: \"1\"\nid: alpha\nname: Alpha\nsummary: common token.\n",
		"search/beta.archie.yaml":  "schema_version: \"1\"\nid: beta\nname: Beta\nsummary: common token.\n",
		"misc/zeta.archie.yaml":    "schema_version: \"1\"\nid: zeta\nname: Zeta\nsummary: unmatched.\n",
		"other/readme.txt":         "ignored\n",
		"pkg/docs/guide.txt":       "guide\n",
		"third/ignored.yaml":       "not a descriptor\n",
		"third/archie.YAML":        "not a descriptor\n",
	})

	options := Defaults(contract.Context)
	options.Root = root
	options.IDs = []string{"gamma"}
	options.Paths = []string{"pkg/src/file.go"}
	options.Query = "common"

	raw, exitCode := Run(options)
	validateEnvelope(t, raw)
	response := decodeQueryResponse(t, raw)

	if response.Status != contract.OK || exitCode != contract.ExitOK {
		t.Fatalf("status/exit = %s/%d, want ok/%d", response.Status, exitCode, contract.ExitOK)
	}
	if got := queryRecordIDs(response); !slices.Equal(got, []string{"app", "gamma", "pkg", "alpha", "beta"}) {
		t.Fatalf("ordered IDs = %v", got)
	}
}

func TestImpactDeduplicatesCyclesAndEvidenceCoverage(t *testing.T) {
	fixture := findRepositoryFixture(t, "cycles-and-shared-target")
	root := materializeRepositoryFixture(t, fixture)

	options := Defaults(contract.Impact)
	options.Root = root
	options.Paths = append([]string(nil), fixture.Context.EvidenceTargets...)

	raw, exitCode := Run(options)
	validateEnvelope(t, raw)
	response := decodeQueryResponse(t, raw)

	if response.Status != contract.OK || exitCode != contract.ExitOK {
		t.Fatalf("status/exit = %s/%d, want ok/%d", response.Status, exitCode, contract.ExitOK)
	}
	if got := queryRecordIDs(response); !slices.Equal(got, []string{"a", "b"}) {
		t.Fatalf("impact IDs = %v", got)
	}
	for _, view := range response.Data.Records {
		if !slices.Equal(view.Reasons, []string{"impact", "linked"}) {
			t.Fatalf("reasons[%s] = %v", view.Record.ID, view.Reasons)
		}
	}
	if got, want := len(response.Data.Evidence), 2; got != want {
		t.Fatalf("evidence rows = %d, want %d", got, want)
	}
	if response.Coverage.Evidence.Total != 1 || response.Coverage.Evidence.Checked != 1 || response.Coverage.Evidence.Unchecked != 0 {
		t.Fatalf("evidence coverage = %+v", response.Coverage.Evidence)
	}
}

func TestGetReturnsSingleRecordAndAppliesLinkBudget(t *testing.T) {
	fixture := findRepositoryFixture(t, "cycles-and-shared-target")
	root := materializeRepositoryFixture(t, fixture)

	t.Run("single-record", func(t *testing.T) {
		options := Defaults(contract.Get)
		options.Root = root
		options.IDs = []string{"a"}

		raw, exitCode := Run(options)
		validateEnvelope(t, raw)
		response := decodeQueryResponse(t, raw)

		if response.Status != contract.OK || exitCode != contract.ExitOK {
			t.Fatalf("status/exit = %s/%d, want ok/%d", response.Status, exitCode, contract.ExitOK)
		}
		if got := queryRecordIDs(response); !slices.Equal(got, []string{"a"}) {
			t.Fatalf("get IDs = %v", got)
		}
		if got := response.Data.Records[0].Reasons; !slices.Equal(got, []string{"id"}) {
			t.Fatalf("reasons = %v", got)
		}
		relations := []contract.Relation{}
		for _, link := range response.Data.Records[0].Links {
			relations = append(relations, link.Relation)
		}
		if !slices.Equal(relations, []contract.Relation{contract.Parent, contract.DependsOn, contract.RelatedTo, contract.Source}) {
			t.Fatalf("relations = %v", relations)
		}
	})

	t.Run("link-budget", func(t *testing.T) {
		options := Defaults(contract.Get)
		options.Root = root
		options.IDs = []string{"a"}
		options.Limits.Links = 1

		raw, exitCode := Run(options)
		validateEnvelope(t, raw)
		response := decodeQueryResponse(t, raw)

		if response.Status != contract.Partial || exitCode != contract.ExitPartial {
			t.Fatalf("status/exit = %s/%d, want partial/%d", response.Status, exitCode, contract.ExitPartial)
		}
		if got, want := len(response.Data.Records[0].Links), 1; got != want {
			t.Fatalf("links = %d, want %d", got, want)
		}
		if response.Data.Records[0].Links[0].Relation != contract.Parent {
			t.Fatalf("first link = %+v", response.Data.Records[0].Links[0])
		}
		if got := omissionCount(response.Coverage.Omissions, "link_budget"); got != 3 {
			t.Fatalf("link_budget omission = %d, want 3", got)
		}
	})
}

func TestContextNoMatchReturnsEmptyRecords(t *testing.T) {
	fixture := findRepositoryFixture(t, "flat-named-and-repeated-hierarchy")
	root := materializeRepositoryFixture(t, fixture)

	options := Defaults(contract.Context)
	options.Root = root
	options.Query = "zzznomatchzzz"

	raw, exitCode := Run(options)
	validateEnvelope(t, raw)
	response := decodeQueryResponse(t, raw)

	if response.Status != contract.OK || exitCode != contract.ExitOK {
		t.Fatalf("status/exit = %s/%d, want ok/%d", response.Status, exitCode, contract.ExitOK)
	}
	if response.Data == nil || len(response.Data.Records) != 0 || len(response.Data.Evidence) != 0 {
		t.Fatalf("expected empty query data, got %+v", response.Data)
	}
}

func TestGetCanReturnPartialWithZeroRecordsWhenRecordCannotFit(t *testing.T) {
	root := mkdirScratch(t, "oversized-get-")
	writeFiles(t, root, map[string]string{
		"archie.yaml":       "schema_version: \"1\"\nid: app\nname: App\nsummary: Root.\n",
		"huge.archie.yaml":  fmt.Sprintf("schema_version: \"1\"\nid: huge\nname: Huge\nsummary: Small.\ndescription: \"%s\"\n", strings.Repeat("x", 5000)),
		"notes/readme.txt":  "ignored\n",
		"other/config.json": "{}\n",
	})

	options := Defaults(contract.Get)
	options.Root = root
	options.IDs = []string{"huge"}
	options.Limits.OutputBytes = contract.MinMaxBytes

	raw, exitCode := Run(options)
	validateEnvelope(t, raw)
	response := decodeQueryResponse(t, raw)

	if response.Status != contract.Partial || exitCode != contract.ExitPartial {
		t.Fatalf("status/exit = %s/%d, want partial/%d", response.Status, exitCode, contract.ExitPartial)
	}
	if response.Data == nil || len(response.Data.Records) != 0 {
		t.Fatalf("expected zero returned records, got %+v", response.Data)
	}
	if got := omissionCount(response.Coverage.Omissions, "byte_budget"); got != 1 {
		t.Fatalf("byte_budget omission = %d, want 1", got)
	}
	if response.Continuation != nil {
		t.Fatalf("get continuation = %+v, want nil", response.Continuation)
	}
}

func TestMissingADRIsPartialForQueriesAndErrorForValidate(t *testing.T) {
	root := mkdirScratch(t, "missing-adr-")
	writeFiles(t, root, map[string]string{
		"archie.yaml":         "schema_version: \"1\"\nid: app\nname: App\nsummary: Root.\n",
		"policy.archie.yaml":  "schema_version: \"1\"\nid: policy\nname: Policy\nsummary: Needs ADR.\nlinks:\n  - {relation: adr, target: docs/adr/0001.md, description: Decision.}\n",
		"docs/readme.txt":     "placeholder\n",
		"generated/output.go": "ignored\n",
	})

	getOptions := Defaults(contract.Get)
	getOptions.Root = root
	getOptions.IDs = []string{"policy"}

	rawGet, exitGet := Run(getOptions)
	validateEnvelope(t, rawGet)
	getResponse := decodeQueryResponse(t, rawGet)

	if getResponse.Status != contract.Partial || exitGet != contract.ExitPartial {
		t.Fatalf("get status/exit = %s/%d, want partial/%d", getResponse.Status, exitGet, contract.ExitPartial)
	}
	if got := getResponse.Data.Evidence[0]; got.Relation != contract.ADR || got.State != "missing" || got.Reason != "missing" {
		t.Fatalf("ADR evidence = %+v", got)
	}

	validateOptions := Defaults(contract.Validate)
	validateOptions.Root = root
	validateOptions.Evidence = true

	rawValidate, exitValidate := Run(validateOptions)
	validateEnvelope(t, rawValidate)
	validateResponse := decodeValidationResponse(t, rawValidate)

	if validateResponse.Status != contract.Error || exitValidate != contract.ExitInvalid {
		t.Fatalf("validate status/exit = %s/%d, want error/%d", validateResponse.Status, exitValidate, contract.ExitInvalid)
	}
	if validateResponse.Data != nil {
		t.Fatalf("validate data = %+v, want nil", validateResponse.Data)
	}
	if validateResponse.Diagnostics[0].Code != "EVIDENCE_MISSING" {
		t.Fatalf("diagnostics = %+v", validateResponse.Diagnostics)
	}
}
