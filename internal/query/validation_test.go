package query

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wbreza/archie/contract"
	"github.com/wbreza/archie/internal/repository"
	"github.com/wbreza/archie/internal/testutil"
)

func TestValidationChecksAllReferencesWithoutEvidence(t *testing.T) {
	root := mkdirScratch(t, "complete-validation-")
	writeFiles(t, root, testutil.ValidationFixture())
	o := Defaults(contract.Validate)
	o.Root = root
	raw, code := Run(o)
	validateEnvelope(t, raw)
	r := decodeValidationResponse(t, raw)
	if code != 0 || r.Data == nil || !r.Data.Valid || len(r.Data.Evidence) != 0 ||
		len(raw) > 4096 || r.Coverage.Metadata.Discovered != 37 ||
		r.Coverage.References == nil || *r.Coverage.References != (contract.ReferenceCoverage{
		Total: 493, Checked: 493, Complete: true,
	}) || r.Baseline.State != "none" || r.Coverage.Evidence.Total != 0 {
		t.Fatalf("incomplete or misleading validation: %s", raw)
	}
	again, againCode := Run(o)
	if againCode != code || string(again) != string(raw) {
		t.Fatal("validation is not deterministic")
	}
	// The late target lies beyond both the historical 256-target ceiling and
	// the detailed-output cutoff. Optional evidence must not hide this failure.
	if err := os.Remove(filepath.Join(root, filepath.FromSlash(testutil.ValidationTarget(492)))); err != nil {
		t.Fatal(err)
	}
	for _, evidence := range []bool{false, true} {
		o.Evidence = evidence
		raw, code = Run(o)
		validateEnvelope(t, raw)
		r = decodeValidationResponse(t, raw)
		if code != contract.ExitInvalid || r.Data != nil || r.Coverage.References.Checked != 493 ||
			r.Coverage.References.Missing != 1 || !r.Coverage.References.Complete ||
			r.Diagnostics[0].Target != testutil.ValidationTarget(492) ||
			r.Diagnostics[0].Descriptor != "component-35.archie.yaml" {
			t.Fatalf("late missing reference escaped validation: %s", raw)
		}
	}
}

func TestValidationDiagnosticSamplesDoNotLimitWork(t *testing.T) {
	root := mkdirScratch(t, "validation-failures-")
	files := testutil.ValidationFixture()
	for i := 0; i < testutil.ValidationTargets; i++ {
		delete(files, testutil.ValidationTarget(i))
	}
	writeFiles(t, root, files)
	o := Defaults(contract.Validate)
	o.Root = root
	raw, code := Run(o)
	validateEnvelope(t, raw)
	r := decodeValidationResponse(t, raw)
	if code != 1 || len(raw) > contract.DefaultMaxBytes || len(r.Diagnostics) != 16 ||
		r.Coverage.References.Missing != 493 || r.Coverage.References.Checked != 493 ||
		r.Coverage.DiagnosticCounts["EVIDENCE_MISSING"] != 493 {
		t.Fatalf("failure samples stopped validation: %s", raw)
	}
}

func TestValidationPreservesSchemaCauseThroughRootLinks(t *testing.T) {
	root := mkdirScratch(t, "schema-cause-")
	writeFiles(t, root, map[string]string{
		"archie.yaml":                 "schema_version: \"1\"\nid: app\nname: App\nsummary: Root.\nlinks: [{relation: depends_on, target: conversation-ui, description: Uses component.}]\n",
		"conversation-ui.archie.yaml": "schema_version: \"1\"\nid: conversation-ui\nname: Conversations\nsummary: UI.\ndescription: " + strings.Repeat("x", 8995) + "\n",
	})
	o := Defaults(contract.Validate)
	o.Root = root
	raw, code := Run(o)
	validateEnvelope(t, raw)
	r := decodeValidationResponse(t, raw)
	if code != 1 || r.Data != nil || r.Coverage.References.Complete ||
		!r.Coverage.Metadata.Complete || r.Coverage.Metadata.Discovered != 2 ||
		r.Coverage.Metadata.Invalid != 2 || len(r.Diagnostics) != 2 {
		t.Fatalf("schema coverage lost: %s", raw)
	}
	cause := r.Diagnostics[0]
	if cause.Code != "SCHEMA_INVALID" || cause.Descriptor != "conversation-ui.archie.yaml" ||
		!strings.Contains(cause.Message, "/description") || !strings.Contains(cause.Message, "8995") ||
		!strings.Contains(cause.Message, "8192") || r.Diagnostics[1].Code != "LINK_UNRESOLVED" {
		t.Fatalf("underlying schema cause lost: %s", raw)
	}
}

func TestRootFailureDiagnosticsRespectSmallQueryBudgets(t *testing.T) {
	root := mkdirScratch(t, "root-failure-budget-")
	files := map[string]string{
		"archie.yaml": "schema_version: \"1\"\nid: app\nname: App\nsummary: Root.\nlinks: [{relation: depends_on, target: child-00, description: Uses child.}]\n",
	}
	for i := 0; i < 16; i++ {
		files[fmt.Sprintf("%02d-%s.archie.yaml", i, strings.Repeat("x", 180))] =
			fmt.Sprintf("schema_version: \"1\"\nid: child-%02d\nname: Child\nsummary: Child.\ndescription: %s\n", i, strings.Repeat("x", 8995))
	}
	writeFiles(t, root, files)
	for _, command := range []contract.Command{contract.Discover, contract.Get} {
		o := Defaults(command)
		o.Root, o.IDs, o.Limits.OutputBytes = root, []string{"app"}, contract.MinMaxBytes
		raw, code := Run(o)
		validateEnvelope(t, raw)
		r := decodeRawResponse(t, raw)
		if code != 1 || len(raw) > contract.MinMaxBytes || r.Data != nil ||
			r.Diagnostics[0].Code != "SCHEMA_INVALID" ||
			r.Coverage.DiagnosticCounts["SCHEMA_INVALID"] != 16 ||
			r.Coverage.DiagnosticCounts["LINK_UNRESOLVED"] != 1 {
			t.Fatalf("unbounded root failure: %s", raw)
		}
	}
}

func TestFailureWithLongDescriptorFitsMinimumBudget(t *testing.T) {
	raw, code := failure(contract.Get, &repository.Problem{
		Code: "SCHEMA_INVALID", Message: "Schema violation.", Descriptor: strings.Repeat("\U0001f600", 1024),
	}, contract.MinMaxBytes)
	validateEnvelope(t, raw)
	r := decodeRawResponse(t, raw)
	if code != 1 || len(raw) > contract.MinMaxBytes || r.Diagnostics[0].Code != "SCHEMA_INVALID" ||
		r.Diagnostics[0].Descriptor != "" || r.Coverage.DiagnosticCounts["SCHEMA_INVALID"] != 1 {
		t.Fatalf("one oversized diagnostic did not fit: %s", raw)
	}
}

func referenceFixture(target string) string {
	return "schema_version: \"1\"\nid: app\nname: App\nsummary: Root.\nlinks: [{relation: source, target: " + target + ", description: Source.}]\n"
}

func TestValidationReferenceKindsAndExclusions(t *testing.T) {
	for _, tc := range []struct {
		name, target, extra, diagnostic string
		files                           map[string]string
		code, checked, missing, invalid int
		complete                        bool
	}{
		{"missing", "absent.txt", "", "EVIDENCE_MISSING", nil, 1, 1, 1, 0, true},
		{"directory", "src", "", "PATH_UNSAFE", map[string]string{"src/a.txt": ""}, 1, 1, 0, 1, true},
		{"case", "case.txt", "", "PATH_CASE", map[string]string{"Case.txt": ""}, 1, 1, 0, 1, true},
		{"excluded", "src/a.txt", "discovery: {exclude: [src]}\n", "REFERENCE_UNCHECKED", map[string]string{"src/a.txt": ""}, 4, 0, 0, 0, false},
		{"nested", "src/a.txt", "", "REFERENCE_UNCHECKED", map[string]string{"src/.git": "", "src/a.txt": ""}, 4, 0, 0, 0, false},
		{"pruned", "node_modules/a.txt", "", "REFERENCE_UNCHECKED", map[string]string{"node_modules/a.txt": ""}, 4, 0, 0, 0, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := mkdirScratch(t, "reference-kind-")
			writeFiles(t, root, tc.files)
			writeFiles(t, root, map[string]string{"archie.yaml": referenceFixture(tc.target) + tc.extra})
			o := Defaults(contract.Validate)
			o.Root = root
			raw, code := Run(o)
			validateEnvelope(t, raw)
			r := decodeValidationResponse(t, raw)
			c := r.Coverage.References
			if code != tc.code || c.Total != 1 || c.Checked != tc.checked || c.Missing != tc.missing ||
				c.Invalid != tc.invalid || c.Complete != tc.complete || r.Diagnostics[0].Code != tc.diagnostic ||
				r.Diagnostics[0].Target != tc.target {
				t.Fatalf("wrong reference result: %s", raw)
			}
		})
	}
}

func TestValidationReferenceBudgetAndLateFailures(t *testing.T) {
	root := mkdirScratch(t, "reference-budget-")
	writeFiles(t, root, map[string]string{"archie.yaml": referenceFixture("a.txt"), "a.txt": ""})
	g, err := repository.LoadForValidation(root)
	if err != nil {
		t.Fatal(err)
	}
	defer g.FS.Close()
	r := validateReferences(g, 0)
	if exit(r) != 4 || r.Coverage.References.Checked != 0 || r.Coverage.References.Unchecked != 1 ||
		r.Coverage.References.Complete || r.Coverage.DiagnosticCounts["BUDGET_EXHAUSTED"] != 1 {
		t.Fatalf("reference limit ignored: %+v", r)
	}
}

func TestValidationDetectsMutationBeforeReturning(t *testing.T) {
	for _, mutation := range []string{"file", "directory", "missing-created", "closed-root"} {
		t.Run(mutation, func(t *testing.T) {
			root := mkdirScratch(t, "reference-race-")
			files := map[string]string{"archie.yaml": referenceFixture("a.txt"), "a.txt": "old"}
			if mutation == "missing-created" {
				delete(files, "a.txt")
			}
			writeFiles(t, root, files)
			g, err := repository.LoadForValidation(root)
			if err != nil {
				t.Fatal(err)
			}
			defer g.FS.Close()
			r := validateReferences(g, contract.MaxReferences)
			switch mutation {
			case "file", "missing-created":
				writeFiles(t, root, map[string]string{"a.txt": "changed file content"})
			case "directory":
				writeFiles(t, root, map[string]string{"new.archie.yaml": "new descriptor"})
			case "closed-root":
				g.FS.Close()
			}
			raw, code := finishValidation(g, r, contract.DefaultMaxBytes)
			validateEnvelope(t, raw)
			got := decodeValidationResponse(t, raw)
			if code != 3 || got.Coverage.References.Complete || got.Data != nil {
				t.Fatalf("mutation reported success: %s", raw)
			}
		})
	}
}

func TestValidationInspectsWithoutContentByteBudget(t *testing.T) {
	root := mkdirScratch(t, "large-reference-")
	writeFiles(t, root, map[string]string{"archie.yaml": referenceFixture("large.txt"), "large.txt": ""})
	f, err := os.OpenFile(filepath.Join(root, "large.txt"), os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	err = f.Truncate(contract.HardMaxEvidenceBytes + 1)
	f.Close()
	if err != nil {
		t.Fatal(err)
	}
	o := Defaults(contract.Validate)
	o.Root = root
	o.Limits.EvidenceBytes, o.Limits.EvidenceTargets = 0, 0
	raw, code := Run(o)
	validateEnvelope(t, raw)
	r := decodeValidationResponse(t, raw)
	if code != 0 || !r.Coverage.References.Complete || r.Coverage.References.Checked != 1 {
		t.Fatalf("unused evidence budget restricted inspection: %s", raw)
	}
}

func TestDetailedDiagnosticsStayBoundedAndKeepExitPrecedence(t *testing.T) {
	r := empty(contract.Validate)
	long := strings.Repeat("\U0001f600", 1024)
	for i := 0; i < 20; i++ {
		addDetailedDiagnostic(&r, contract.Diagnostic{Code: "EVIDENCE_MISSING", Message: "Missing.", Descriptor: long, Target: long})
	}
	addDetailedDiagnostic(&r, contract.Diagnostic{Code: "IO_ERROR", Message: "Inspection failed.", Descriptor: long, Target: long})
	r.Status = contract.Error
	raw, code := bounded(r, contract.DefaultMaxBytes)
	validateEnvelope(t, raw)
	if code != 3 || len(r.Diagnostics) == 0 || len(r.Diagnostics) >= 16 ||
		r.Coverage.DiagnosticCounts["EVIDENCE_MISSING"] != 20 || len(raw) > contract.DefaultMaxBytes {
		t.Fatalf("diagnostic budget/precedence: %s", raw)
	}
}

func TestValidationReadFailureIsNotComplete(t *testing.T) {
	root := mkdirScratch(t, "inspection-error-")
	writeFiles(t, root, map[string]string{"archie.yaml": referenceFixture("a.txt"), "a.txt": ""})
	g, err := repository.LoadForValidation(root)
	if err != nil {
		t.Fatal(err)
	}
	g.FS.Close()
	r := validateReferences(g, contract.MaxReferences)
	if r.Coverage.References.Complete || r.Coverage.References.Invalid != 1 || exit(r) != 3 {
		t.Fatalf("failed inspection claimed completeness: %+v", r)
	}
}

func TestValidationRejectsHardlinkedReferences(t *testing.T) {
	root := mkdirScratch(t, "validation-hardlink-")
	writeFiles(t, root, map[string]string{"archie.yaml": referenceFixture("source.txt"), "source.txt": ""})
	if err := os.Link(filepath.Join(root, "source.txt"), filepath.Join(root, "alias.txt")); err != nil {
		t.Fatal(err)
	}
	o := Defaults(contract.Validate)
	o.Root = root
	raw, code := Run(o)
	validateEnvelope(t, raw)
	r := decodeValidationResponse(t, raw)
	if code != 1 || r.Coverage.References.Invalid != 1 || r.Diagnostics[0].Code != "PATH_UNSAFE" {
		t.Fatalf("hardlinked reference escaped validation: %s", raw)
	}
}

func TestValidationRejectsSymlinkReferences(t *testing.T) {
	root := mkdirScratch(t, "validation-symlink-")
	writeFiles(t, root, map[string]string{"archie.yaml": referenceFixture("link.txt"), "source.txt": ""})
	if err := os.Symlink(filepath.Join(root, "source.txt"), filepath.Join(root, "link.txt")); err != nil {
		t.Skipf("symlink creation unavailable: %v", err)
	}
	o := Defaults(contract.Validate)
	o.Root = root
	raw, code := Run(o)
	validateEnvelope(t, raw)
	r := decodeValidationResponse(t, raw)
	if code != 1 || r.Coverage.References.Invalid != 1 || r.Diagnostics[0].Code != "PATH_UNSAFE" {
		t.Fatalf("symlink reference escaped validation: %s", raw)
	}
}

func BenchmarkValidateReferences493(b *testing.B) {
	base := testutil.ScratchBase()
	if err := os.MkdirAll(base, 0o755); err != nil {
		b.Fatal(err)
	}
	root, err := os.MkdirTemp(base, "validation-benchmark-")
	if err != nil {
		b.Fatal(err)
	}
	defer os.RemoveAll(root)
	for name, content := range testutil.ValidationFixture() {
		full := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			b.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o600); err != nil {
			b.Fatal(err)
		}
	}
	o := Defaults(contract.Validate)
	o.Root = root
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		raw, code := Run(o)
		var r contract.Response[contract.ValidationData]
		if code != 0 || json.Unmarshal(raw, &r) != nil || r.Coverage.References.Checked != 493 || !r.Coverage.References.Complete {
			b.Fatalf("benchmark did not validate all targets: %s", raw)
		}
		b.ReportMetric(float64(len(raw)), "stdout-bytes")
	}
	b.StopTimer()
}
