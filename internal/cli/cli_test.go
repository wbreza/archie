package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v5"
	"github.com/wbreza/archie/contract"
	"github.com/wbreza/archie/internal/testutil"
	"github.com/wbreza/archie/schemas"
)

var allowedScratchBase = testutil.ScratchBase()

var (
	suiteScratch string
	cliBinary    string
	repoRoot     string
	loadSchema   = sync.OnceValues(func() (*jsonschema.Schema, error) { return schemas.Compile("response") })
)

type cliResult struct {
	stdout []byte
	stderr []byte
	exit   int
}

type shortWriter struct{}

func (shortWriter) Write(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	return len(p) - 1, nil
}

func TestMain(m *testing.M) {
	root, err := findRepoRoot()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	repoRoot = root
	if err := os.MkdirAll(allowedScratchBase, 0o755); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	suiteScratch, err = os.MkdirTemp(allowedScratchBase, "cli-tests-")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	cliBinary, err = buildCLI(suiteScratch)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		_ = os.RemoveAll(suiteScratch)
		os.Exit(1)
	}
	code := m.Run()
	_ = os.RemoveAll(suiteScratch)
	os.Exit(code)
}

func findRepoRoot() (string, error) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return "", fmt.Errorf("cannot locate test file")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..")), nil
}

func buildCLI(root string) (string, error) {
	exe := filepath.Join(root, "archie.exe")
	cmd := exec.Command("go", "build", "-o", exe, "."+string(filepath.Separator)+filepath.Join("cmd", "archie"))
	cmd.Dir = repoRoot
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("go build failed: %w: %s", err, stderr.String())
	}
	return exe, nil
}

func newScratchDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp(suiteScratch, "case-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}

func writeFiles(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for name, contents := range files {
		full := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(contents), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func makeRepo(t *testing.T, files map[string]string) string {
	t.Helper()
	root := newScratchDir(t)
	writeFiles(t, root, files)
	return root
}

func runCLI(t *testing.T, dir string, args ...string) cliResult {
	t.Helper()
	if dir == "" {
		dir = repoRoot
	}
	cmd := exec.Command(cliBinary, args...)
	cmd.Dir = dir
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err == nil {
		return cliResult{stdout: stdout.Bytes(), stderr: stderr.Bytes(), exit: 0}
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return cliResult{stdout: stdout.Bytes(), stderr: stderr.Bytes(), exit: ee.ExitCode()}
	}
	t.Fatalf("run %v: %v", args, err)
	return cliResult{}
}

func decodeResponse[T any](t *testing.T, stdout []byte) contract.Response[T] {
	t.Helper()
	assertJSONContract(t, stdout)
	var response contract.Response[T]
	if err := json.Unmarshal(stdout, &response); err != nil {
		t.Fatal(err)
	}
	return response
}

func assertJSONContract(t *testing.T, stdout []byte) {
	t.Helper()
	if len(stdout) == 0 {
		t.Fatal("stdout is empty")
	}
	if bytes.HasPrefix(stdout, []byte{0xef, 0xbb, 0xbf}) {
		t.Fatal("stdout has BOM")
	}
	if stdout[len(stdout)-1] != '\n' {
		t.Fatal("stdout must end with LF")
	}
	if bytes.Count(stdout, []byte{'\n'}) != 1 {
		t.Fatal("JSON output must be compact with one trailing LF")
	}
	if bytes.Contains(stdout, []byte{0x1b}) {
		t.Fatal("stdout unexpectedly contains ANSI escapes")
	}
	var value any
	if err := json.Unmarshal(stdout, &value); err != nil {
		t.Fatal(err)
	}
	schema, err := loadSchema()
	if err != nil {
		t.Fatal(err)
	}
	if err := schema.Validate(value); err != nil {
		t.Fatal(err)
	}
}

func ids(records []contract.RecordView) []string {
	result := make([]string, 0, len(records))
	for _, record := range records {
		result = append(result, record.Record.ID)
	}
	return result
}

func viewByID(t *testing.T, records []contract.RecordView, id string) contract.RecordView {
	t.Helper()
	for _, record := range records {
		if record.Record.ID == id {
			return record
		}
	}
	t.Fatalf("record %q not found in response", id)
	return contract.RecordView{}
}

func linkTargets(links []contract.Link, relation contract.Relation) []string {
	var result []string
	for _, link := range links {
		if link.Relation == relation {
			result = append(result, link.Target)
		}
	}
	return result
}

func hasDiagnostic(diags []contract.Diagnostic, code string) bool {
	for _, diag := range diags {
		if diag.Code == code {
			return true
		}
	}
	return false
}

func readRepoFiles() map[string]string {
	return map[string]string{
		"archie.yaml": `schema_version: "1"
id: app
name: Application
summary: Application root.
`,
		"renderer.archie.yaml": `schema_version: "1"
id: renderer
name: Renderer
summary: Renders reports.
links:
  - {relation: depends_on, target: billing, description: Uses billing signals.}
  - {relation: source, target: src/render.go, description: Rendering source.}
  - {relation: doc, target: docs/render.md, description: Renderer docs.}
`,
		"mailer.archie.yaml": `schema_version: "1"
id: mailer
name: Mailer
summary: Sends notifications.
`,
		"mechanisms/archie.yaml": `schema_version: "1"
id: mechanisms
name: Mechanisms
summary: Cross-repository mechanisms.
links:
  - {relation: related_to, target: renderer, description: Shares rendering contract.}
  - {relation: source, target: ../src/mechanisms.go, description: Mechanism source.}
`,
		"mechanisms/audit.archie.yaml": `schema_version: "1"
id: audit
name: Audit
summary: Audits billing activity.
`,
		"mechanisms/billing.archie.yaml": `schema_version: "1"
id: billing
name: Billing
summary: Handles billing.
links:
  - {relation: related_to, target: renderer, description: Coordinates rendering.}
  - {relation: source, target: ../src/billing.go, description: Billing source.}
  - {relation: test, target: ../tests/billing_test.go, description: Billing tests.}
  - {relation: adr, target: ../docs/decisions/billing.md, description: Billing ADR.}
`,
		"src/render.go":             "package render\nconst Secret = \"TOP_SECRET_RENDER_BYTES\"\n",
		"src/mechanisms.go":         "package mechanisms\nconst Secret = \"TOP_SECRET_MECHANISM_BYTES\"\n",
		"src/billing.go":            "package billing\nconst Secret = \"TOP_SECRET_BILLING_BYTES\"\n",
		"docs/render.md":            "# Renderer\n",
		"docs/decisions/billing.md": "# Billing ADR\n",
		"tests/billing_test.go":     "package billing_test\n",
	}
}

func cursorRepoFiles() map[string]string {
	return map[string]string{
		"archie.yaml": `schema_version: "1"
id: app
name: Application
summary: Root.
`,
		"alpha.archie.yaml": `schema_version: "1"
id: alpha
name: Alpha
summary: First child.
`,
		"beta.archie.yaml": `schema_version: "1"
id: beta
name: Beta
summary: Second child.
`,
		"gamma.archie.yaml": `schema_version: "1"
id: gamma
name: Gamma
summary: Third child.
`,
	}
}

func TestVersionAndHelpSurface(t *testing.T) {
	version := runCLI(t, repoRoot, "version", "--json")
	if version.exit != contract.ExitOK {
		t.Fatalf("version exit=%d stderr=%s", version.exit, version.stderr)
	}
	if len(version.stderr) != 0 {
		t.Fatalf("unexpected stderr: %s", version.stderr)
	}
	v := decodeResponse[contract.VersionData](t, version.stdout)
	if v.Command != contract.VersionCommand || v.Status != contract.OK || v.Data == nil || v.Data.Version == "" {
		t.Fatalf("unexpected version response: %+v", v)
	}

	help := runCLI(t, repoRoot, "--help")
	if help.exit != contract.ExitOK {
		t.Fatalf("help exit=%d stderr=%s", help.exit, help.stderr)
	}
	text := string(help.stdout)
	if strings.HasPrefix(text, "{") || !strings.Contains(text, "Commands: discover, get, context, impact, validate, scaffold, version") {
		t.Fatalf("unexpected help output: %q", text)
	}
	if !strings.Contains(text, "no overwrite or directory creation") {
		t.Fatalf("help missing scaffolding notice: %q", text)
	}

	discoverHelp := runCLI(t, repoRoot, "discover", "--help")
	if discoverHelp.exit != contract.ExitOK {
		t.Fatalf("discover --help exit=%d stderr=%s", discoverHelp.exit, discoverHelp.stderr)
	}
	discoverText := string(discoverHelp.stdout)
	if strings.HasPrefix(discoverText, "{") || !strings.Contains(discoverText, "Flags for discover:") {
		t.Fatalf("unexpected discover help: %q", discoverText)
	}

	scaffold := runCLI(t, newScratchDir(t), "scaffold", "--file", "x.archie.yaml", "--id", "x", "--name", "X", "--summary", "X")
	if scaffold.exit != contract.ExitInvalid {
		t.Fatalf("scaffold exit=%d stderr=%s", scaffold.exit, scaffold.stderr)
	}
	scaffoldResponse := decodeResponse[any](t, scaffold.stdout)
	if !hasDiagnostic(scaffoldResponse.Diagnostics, "ROOT_REQUIRED") {
		t.Fatalf("expected scaffold root diagnostic: %+v", scaffoldResponse.Diagnostics)
	}
}

func TestDirectRunReturnsOperationalOnWriterFailure(t *testing.T) {
	if exit := Run([]string{"version"}, shortWriter{}); exit != contract.ExitOperational {
		t.Fatalf("exit=%d", exit)
	}
}

func TestReadCommandsRespectContract(t *testing.T) {
	repo := makeRepo(t, readRepoFiles())

	get := runCLI(t, repoRoot, "get", "--root", repo, "--id", "mechanisms")
	if get.exit != contract.ExitOK {
		t.Fatalf("get exit=%d stderr=%s", get.exit, get.stderr)
	}
	if bytes.Contains(get.stdout, []byte("TOP_SECRET_MECHANISM_BYTES")) {
		t.Fatal("source bytes leaked into get output")
	}
	getResponse := decodeResponse[contract.QueryData](t, get.stdout)
	if ids(getResponse.Data.Records); !slices.Equal(ids(getResponse.Data.Records), []string{"mechanisms"}) {
		t.Fatalf("unexpected get ids: %v", ids(getResponse.Data.Records))
	}
	mechanisms := getResponse.Data.Records[0]
	if mechanisms.Descriptor != "mechanisms/archie.yaml" || mechanisms.SummaryOnly {
		t.Fatalf("unexpected get view: %+v", mechanisms)
	}
	wantLinks := []contract.Link{
		{Relation: contract.Parent, Target: "app", Description: "Application root."},
		{Relation: contract.Child, Target: "audit", Description: "Audits billing activity."},
		{Relation: contract.Child, Target: "billing", Description: "Handles billing."},
		{Relation: contract.RelatedTo, Target: "renderer", Description: "Shares rendering contract."},
		{Relation: contract.Source, Target: "src/mechanisms.go", Description: "Mechanism source."},
	}
	if !slices.Equal(mechanisms.Links, wantLinks) {
		t.Fatalf("unexpected get links: %#v", mechanisms.Links)
	}
	if len(mechanisms.Record.Links) != 2 || mechanisms.Record.Links[1].Target != "../src/mechanisms.go" {
		t.Fatalf("authored links were not preserved: %#v", mechanisms.Record.Links)
	}

	context := runCLI(t, repoRoot, "context", "--root", repo, "--id", "billing")
	if context.exit != contract.ExitOK {
		t.Fatalf("context exit=%d stderr=%s", context.exit, context.stderr)
	}
	contextResponse := decodeResponse[contract.QueryData](t, context.stdout)
	if !slices.Equal(ids(contextResponse.Data.Records), []string{"app", "mechanisms", "billing", "renderer"}) {
		t.Fatalf("unexpected context ids: %v", ids(contextResponse.Data.Records))
	}
	rootView := viewByID(t, contextResponse.Data.Records, "app")
	if !rootView.SummaryOnly || rootView.Record.Description != "" || rootView.Record.Discovery != nil {
		t.Fatalf("unexpected ancestor projection: %+v", rootView)
	}
	if !slices.Equal(linkTargets(rootView.Links, contract.Child), []string{"mailer", "mechanisms", "renderer"}) {
		t.Fatalf("unexpected named siblings: %#v", rootView.Links)
	}
	billingView := viewByID(t, contextResponse.Data.Records, "billing")
	if billingView.SummaryOnly || !slices.Equal(billingView.Reasons, []string{"id"}) {
		t.Fatalf("unexpected billing view: %+v", billingView)
	}
	rendererView := viewByID(t, contextResponse.Data.Records, "renderer")
	if !slices.Equal(rendererView.Reasons, []string{"linked"}) {
		t.Fatalf("unexpected linked renderer reasons: %#v", rendererView.Reasons)
	}

	impact := runCLI(t, repoRoot, "impact", "--root", repo, "--path", "src/billing.go")
	if impact.exit != contract.ExitOK {
		t.Fatalf("impact exit=%d stderr=%s", impact.exit, impact.stderr)
	}
	impactResponse := decodeResponse[contract.QueryData](t, impact.stdout)
	if !slices.Equal(ids(impactResponse.Data.Records), []string{"billing", "renderer"}) {
		t.Fatalf("unexpected impact ids: %v", ids(impactResponse.Data.Records))
	}
	if !slices.Equal(viewByID(t, impactResponse.Data.Records, "billing").Reasons, []string{"impact"}) {
		t.Fatalf("unexpected direct impact reasons: %#v", viewByID(t, impactResponse.Data.Records, "billing").Reasons)
	}
	if !slices.Equal(viewByID(t, impactResponse.Data.Records, "renderer").Reasons, []string{"linked"}) {
		t.Fatalf("unexpected reverse impact reasons: %#v", viewByID(t, impactResponse.Data.Records, "renderer").Reasons)
	}

	validate := runCLI(t, repoRoot, "validate", "--root", repo, "--evidence")
	if validate.exit != contract.ExitOK {
		t.Fatalf("validate exit=%d stderr=%s", validate.exit, validate.stderr)
	}
	validateResponse := decodeResponse[contract.ValidationData](t, validate.stdout)
	if validateResponse.Data == nil || !validateResponse.Data.Valid || len(validateResponse.Diagnostics) != 0 {
		t.Fatalf("unexpected validate response: %+v", validateResponse)
	}
	if len(validateResponse.Data.Evidence) != 6 {
		t.Fatalf("unexpected evidence count: %d", len(validateResponse.Data.Evidence))
	}
	for _, evidence := range validateResponse.Data.Evidence {
		if evidence.State != "unknown" || evidence.Reason != "no_baseline" {
			t.Fatalf("unexpected evidence: %+v", evidence)
		}
	}
}

func TestCLIErrorAndPartialScenarios(t *testing.T) {
	t.Run("missing-root", func(t *testing.T) {
		empty := newScratchDir(t)
		result := runCLI(t, repoRoot, "discover", "--root", empty)
		if result.exit != contract.ExitInvalid {
			t.Fatalf("exit=%d stderr=%s", result.exit, result.stderr)
		}
		response := decodeResponse[any](t, result.stdout)
		if !hasDiagnostic(response.Diagnostics, "ROOT_REQUIRED") {
			t.Fatalf("diagnostics=%+v", response.Diagnostics)
		}
	})

	t.Run("missing-id", func(t *testing.T) {
		repo := makeRepo(t, readRepoFiles())
		result := runCLI(t, repoRoot, "get", "--root", repo, "--id", "missing")
		if result.exit != contract.ExitInvalid {
			t.Fatalf("exit=%d stderr=%s", result.exit, result.stderr)
		}
		response := decodeResponse[any](t, result.stdout)
		if !hasDiagnostic(response.Diagnostics, "NOT_FOUND") {
			t.Fatalf("diagnostics=%+v", response.Diagnostics)
		}
	})

	t.Run("malformed-yaml", func(t *testing.T) {
		repo := makeRepo(t, map[string]string{
			"archie.yaml": "schema_version: \"1\"\nid: app\nid: again\nname: Application\nsummary: Root.\n",
		})
		result := runCLI(t, repoRoot, "validate", "--root", repo)
		if result.exit != contract.ExitInvalid {
			t.Fatalf("exit=%d stderr=%s", result.exit, result.stderr)
		}
		response := decodeResponse[any](t, result.stdout)
		if !hasDiagnostic(response.Diagnostics, "YAML_INVALID") {
			t.Fatalf("diagnostics=%+v", response.Diagnostics)
		}
	})

	t.Run("malformed-schema", func(t *testing.T) {
		repo := makeRepo(t, map[string]string{
			"archie.yaml": "schema_version: \"1\"\nid: app\nname: Application\n",
		})
		result := runCLI(t, repoRoot, "discover", "--root", repo)
		if result.exit != contract.ExitInvalid {
			t.Fatalf("exit=%d stderr=%s", result.exit, result.stderr)
		}
		response := decodeResponse[any](t, result.stdout)
		if !hasDiagnostic(response.Diagnostics, "SCHEMA_INVALID") {
			t.Fatalf("diagnostics=%+v", response.Diagnostics)
		}
	})

	t.Run("localized-invalid-schema-is-partial", func(t *testing.T) {
		repo := makeRepo(t, map[string]string{
			"archie.yaml": `schema_version: "1"
id: app
name: Application
summary: Root.
`,
			"good.archie.yaml": `schema_version: "1"
id: good
name: Good
summary: Healthy.
`,
			"broken.archie.yaml": `schema_version: "1"
id: broken
name: Broken
summary: ""
`,
		})
		result := runCLI(t, repoRoot, "discover", "--root", repo)
		if result.exit != contract.ExitPartial {
			t.Fatalf("exit=%d stderr=%s", result.exit, result.stderr)
		}
		response := decodeResponse[contract.QueryData](t, result.stdout)
		if !hasDiagnostic(response.Diagnostics, "SCHEMA_INVALID") || !slices.Equal(ids(response.Data.Records), []string{"app", "good"}) {
			t.Fatalf("unexpected response: %+v", response)
		}
		if response.Coverage.Metadata.Invalid != 1 {
			t.Fatalf("unexpected invalid count: %+v", response.Coverage.Metadata)
		}
	})

	t.Run("empty-matches-are-ok", func(t *testing.T) {
		repo := makeRepo(t, map[string]string{
			"archie.yaml": `schema_version: "1"
id: app
name: Application
summary: Root.
`,
			"service.archie.yaml": `schema_version: "1"
id: service
name: Service
summary: Handles requests.
`,
		})
		result := runCLI(t, repoRoot, "context", "--root", repo, "--query", "zzzzzz")
		if result.exit != contract.ExitOK {
			t.Fatalf("exit=%d stderr=%s", result.exit, result.stderr)
		}
		response := decodeResponse[contract.QueryData](t, result.stdout)
		if response.Status != contract.OK || len(response.Diagnostics) != 0 || len(response.Data.Records) != 0 {
			t.Fatalf("unexpected no-match response: %+v", response)
		}
		lower := strings.ToLower(string(result.stdout))
		if strings.Contains(lower, "not implemented") || strings.Contains(lower, "no implementation") {
			t.Fatalf("unexpected implementation claim: %s", result.stdout)
		}
	})

	t.Run("fresh-process-evidence-sees-source-removal", func(t *testing.T) {
		repo := makeRepo(t, readRepoFiles())
		before := runCLI(t, repoRoot, "get", "--root", repo, "--id", "renderer")
		if before.exit != contract.ExitOK {
			t.Fatalf("before exit=%d stderr=%s", before.exit, before.stderr)
		}
		if bytes.Contains(before.stdout, []byte("TOP_SECRET_RENDER_BYTES")) {
			t.Fatal("source bytes leaked before deletion")
		}
		beforeResponse := decodeResponse[contract.QueryData](t, before.stdout)
		if len(beforeResponse.Data.Evidence) != 2 || beforeResponse.Data.Evidence[0].State != "unknown" {
			t.Fatalf("unexpected pre-delete evidence: %+v", beforeResponse.Data.Evidence)
		}
		if err := os.Remove(filepath.Join(repo, "src", "render.go")); err != nil {
			t.Fatal(err)
		}
		after := runCLI(t, repoRoot, "get", "--root", repo, "--id", "renderer")
		if after.exit != contract.ExitPartial {
			t.Fatalf("after exit=%d stderr=%s", after.exit, after.stderr)
		}
		afterResponse := decodeResponse[contract.QueryData](t, after.stdout)
		if !hasDiagnostic(afterResponse.Diagnostics, "EVIDENCE_MISSING") {
			t.Fatalf("diagnostics=%+v", afterResponse.Diagnostics)
		}
		foundMissing := false
		for _, evidence := range afterResponse.Data.Evidence {
			if evidence.Target == "src/render.go" && evidence.State == "missing" && evidence.Reason == "missing" {
				foundMissing = true
			}
		}
		if !foundMissing {
			t.Fatalf("unexpected post-delete evidence: %+v", afterResponse.Data.Evidence)
		}
	})
}

func TestCLICursorAndFlagValidation(t *testing.T) {
	t.Run("cursor-followup-binding-and-stale", func(t *testing.T) {
		repo := makeRepo(t, cursorRepoFiles())
		first := runCLI(t, repoRoot, "discover", "--root", repo, "--max-records", "2")
		if first.exit != contract.ExitPartial {
			t.Fatalf("first exit=%d stderr=%s", first.exit, first.stderr)
		}
		firstResponse := decodeResponse[contract.QueryData](t, first.stdout)
		if firstResponse.Continuation == nil || !slices.Equal(ids(firstResponse.Data.Records), []string{"alpha", "app"}) {
			t.Fatalf("unexpected first page: %+v", firstResponse)
		}

		second := runCLI(t, repoRoot, "discover", "--root", repo, "--max-records", "2", "--cursor", firstResponse.Continuation.Cursor)
		if second.exit != contract.ExitPartial {
			t.Fatalf("second exit=%d stderr=%s", second.exit, second.stderr)
		}
		secondResponse := decodeResponse[contract.QueryData](t, second.stdout)
		if secondResponse.Continuation != nil || !slices.Equal(ids(secondResponse.Data.Records), []string{"beta", "gamma"}) {
			t.Fatalf("unexpected second page: %+v", secondResponse)
		}

		bound := runCLI(t, repoRoot, "discover", "--root", repo, "--max-records", "3", "--cursor", firstResponse.Continuation.Cursor)
		if bound.exit != contract.ExitUsage {
			t.Fatalf("bound exit=%d stderr=%s", bound.exit, bound.stderr)
		}
		boundResponse := decodeResponse[any](t, bound.stdout)
		if !hasDiagnostic(boundResponse.Diagnostics, "CURSOR_INVALID") {
			t.Fatalf("diagnostics=%+v", boundResponse.Diagnostics)
		}

		if err := os.WriteFile(filepath.Join(repo, "gamma.archie.yaml"), []byte(`schema_version: "1"
id: gamma
name: Gamma
summary: Changed.
`), 0o644); err != nil {
			t.Fatal(err)
		}
		stale := runCLI(t, repoRoot, "discover", "--root", repo, "--max-records", "2", "--cursor", firstResponse.Continuation.Cursor)
		if stale.exit != contract.ExitUsage {
			t.Fatalf("stale exit=%d stderr=%s", stale.exit, stale.stderr)
		}
		staleResponse := decodeResponse[any](t, stale.stdout)
		if !hasDiagnostic(staleResponse.Diagnostics, "CURSOR_STALE") {
			t.Fatalf("diagnostics=%+v", staleResponse.Diagnostics)
		}
	})

	t.Run("strict-flags", func(t *testing.T) {
		repo := makeRepo(t, cursorRepoFiles())
		cases := []struct {
			name string
			args []string
		}{
			{name: "unknown", args: []string{"discover", "--root", repo, "--bogus", "1"}},
			{name: "duplicate-singleton", args: []string{"get", "--root", repo, "--id", "app", "--id", "alpha"}},
			{name: "incompatible", args: []string{"discover", "--root", repo, "--baseline", strings.Repeat("a", 40)}},
			{name: "required", args: []string{"impact", "--root", repo}},
			{name: "numeric-low", args: []string{"discover", "--root", repo, "--max-records", "0"}},
			{name: "numeric-nonnumeric", args: []string{"get", "--root", repo, "--id", "app", "--max-bytes", "NaN"}},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				result := runCLI(t, repoRoot, tc.args...)
				if result.exit != contract.ExitUsage {
					t.Fatalf("exit=%d stderr=%s", result.exit, result.stderr)
				}
				response := decodeResponse[any](t, result.stdout)
				if !hasDiagnostic(response.Diagnostics, "USAGE") {
					t.Fatalf("diagnostics=%+v", response.Diagnostics)
				}
			})
		}
	})

	t.Run("noncommit-baseline", func(t *testing.T) {
		repo := makeRepo(t, map[string]string{
			"archie.yaml": `schema_version: "1"
id: app
name: Application
summary: Root.
`,
		})
		init := exec.Command("git", "init", "-q")
		init.Dir = repo
		if output, err := init.CombinedOutput(); err != nil {
			t.Fatalf("git init: %v: %s", err, output)
		}
		const emptyTree = "4b825dc642cb6eb9a060e54bf8d69288fbee4904"
		result := runCLI(t, repoRoot, "get", "--root", repo, "--id", "app", "--baseline", emptyTree)
		if result.exit != contract.ExitPartial {
			t.Fatalf("exit=%d stderr=%s", result.exit, result.stderr)
		}
		response := decodeResponse[contract.QueryData](t, result.stdout)
		if response.Baseline.State != "unavailable" || response.Baseline.Reason != "not_commit" {
			t.Fatalf("unexpected baseline: %+v", response.Baseline)
		}
		if !hasDiagnostic(response.Diagnostics, "BASELINE_UNAVAILABLE") {
			t.Fatalf("diagnostics=%+v", response.Diagnostics)
		}
	})
}

func TestCLIBudgetLimitsAndOversizeGet(t *testing.T) {
	description := strings.Repeat("Large record detail. ", 260)
	repo := makeRepo(t, map[string]string{
		"archie.yaml": `schema_version: "1"
id: app
name: Application
summary: Root.
`,
		"huge.archie.yaml": fmt.Sprintf(`schema_version: "1"
id: huge
name: Huge
summary: Large record.
description: %q
links:
  - {relation: source, target: src/huge.go, description: Huge source.}
  - {relation: doc, target: docs/huge.md, description: Huge docs.}
  - {relation: test, target: tests/huge_test.go, description: Huge tests.}
`, description),
		"src/huge.go":        "package huge\nconst Secret = \"TOP_SECRET_HUGE_BYTES\"\n",
		"docs/huge.md":       "# Huge\n",
		"tests/huge_test.go": "package huge_test\n",
	})

	limitedLinks := runCLI(t, repoRoot, "get", "--root", repo, "--id", "huge", "--max-links", "2")
	if limitedLinks.exit != contract.ExitPartial {
		t.Fatalf("limited-links exit=%d stderr=%s", limitedLinks.exit, limitedLinks.stderr)
	}
	limitedResponse := decodeResponse[contract.QueryData](t, limitedLinks.stdout)
	if len(limitedResponse.Data.Records) != 1 {
		t.Fatalf("unexpected record count: %d", len(limitedResponse.Data.Records))
	}
	huge := limitedResponse.Data.Records[0]
	if huge.Record.Description != description || len(huge.Record.Links) != 3 {
		t.Fatalf("authored record was truncated: %+v", huge.Record)
	}
	if len(huge.Links) != 2 || !hasDiagnostic(limitedResponse.Diagnostics, "BUDGET_EXHAUSTED") {
		t.Fatalf("unexpected limited links response: %+v", limitedResponse)
	}

	oversized := runCLI(t, repoRoot, "get", "--root", repo, "--id", "huge", "--max-bytes", "4096")
	if oversized.exit != contract.ExitPartial {
		t.Fatalf("oversized exit=%d stderr=%s", oversized.exit, oversized.stderr)
	}
	if len(oversized.stdout) > 4096 {
		t.Fatalf("stdout exceeded requested max bytes: %d", len(oversized.stdout))
	}
	oversizedResponse := decodeResponse[contract.QueryData](t, oversized.stdout)
	if len(oversizedResponse.Data.Records) != 0 || oversizedResponse.Continuation != nil {
		t.Fatalf("unexpected oversized response: %+v", oversizedResponse)
	}
	foundByteBudget := false
	for _, omission := range oversizedResponse.Coverage.Omissions {
		if omission.Reason == "byte_budget" && omission.Count == 1 {
			foundByteBudget = true
		}
	}
	if !foundByteBudget || !hasDiagnostic(oversizedResponse.Diagnostics, "BUDGET_EXHAUSTED") {
		t.Fatalf("unexpected omissions: %+v", oversizedResponse.Coverage.Omissions)
	}
}
