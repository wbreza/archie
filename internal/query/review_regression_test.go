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
)

func TestValidationBoundsBeforeReadingEvidence(t *testing.T) {
	root := mkdirScratch(t, "validation-output-")
	record := "schema_version: \"1\"\nid: app\nname: App\nsummary: Root.\nlinks:\n"
	files := map[string]string{}
	for i := 0; i < 128; i++ {
		target := fmt.Sprintf("%03d-%s.txt", i, strings.Repeat("x", 180))
		record += fmt.Sprintf("  - {relation: source, target: %s, description: Source.}\n", target)
		files[target] = "source"
	}
	files["archie.yaml"] = record
	writeFiles(t, root, files)
	o := Defaults(contract.Validate)
	o.Root, o.Evidence, o.Limits.EvidenceTargets = root, true, 256
	raw, code := Run(o)
	validateEnvelope(t, raw)
	var response contract.Response[contract.ValidationData]
	if err := json.Unmarshal(raw, &response); err != nil {
		t.Fatal(err)
	}
	if code != 4 || len(raw) > 32768 || response.Data.Valid || response.Coverage.Evidence.Total != 128 ||
		response.Coverage.Evidence.Checked == 128 || response.Coverage.Evidence.Checked+response.Coverage.Evidence.Unchecked != 128 {
		t.Fatalf("unbounded or dishonest validation: %s", raw)
	}
	o.Evidence = false
	raw, code = Run(o)
	if err := json.Unmarshal(raw, &response); err != nil {
		t.Fatal(err)
	}
	if code != 0 || len(response.Data.Evidence) != 0 || response.Coverage.Evidence.Total != 0 {
		t.Fatalf("unexpected evidence without request: %s", raw)
	}
}

func TestContinuationCountsOnlyPageableRecords(t *testing.T) {
	root := mkdirScratch(t, "pageable-")
	files := map[string]string{"archie.yaml": "schema_version: \"1\"\nid: app\nname: App\nsummary: Root.\n"}
	for _, id := range []string{"a", "b", "c", "z"} {
		files[id+".archie.yaml"] = "schema_version: \"1\"\nid: " + id + "\nname: Item\nsummary: Item.\n"
	}
	files["z.archie.yaml"] += "description: " + strings.Repeat("x", 8000) + "\n"
	writeFiles(t, root, files)
	o := Defaults(contract.Discover)
	o.Root, o.Limits.OutputBytes, o.Limits.Records = root, 4096, 2
	raw, code := Run(o)
	validateEnvelope(t, raw)
	var response contract.Response[contract.QueryData]
	if err := json.Unmarshal(raw, &response); err != nil {
		t.Fatal(err)
	}
	if code != 4 || response.Continuation == nil || response.Continuation.Remaining != 2 {
		t.Fatalf("unpageable candidate advertised: %s", raw)
	}
	o.Cursor = response.Continuation.Cursor
	raw, code = Run(o)
	if err := json.Unmarshal(raw, &response); err != nil {
		t.Fatal(err)
	}
	if code != 4 || response.Continuation != nil || len(response.Data.Records) != 2 {
		t.Fatalf("bad final page: %s", raw)
	}
}

func TestShallowUnavailableBaselineAndSubdirectoryPrefix(t *testing.T) {
	root := mkdirScratch(t, "baseline-prefix-")
	initGitRepo(t, root)
	record := "schema_version: \"1\"\nid: app\nname: App\nsummary: Root.\nlinks: [{relation: source, target: source.txt, description: Source.}]\n"
	files := map[string]string{"area/archie.yaml": record, "area/source.txt": "old"}
	writeFiles(t, root, files)
	baseline := createCommit(t, root, files)
	o := Defaults(contract.Get)
	o.Root, o.IDs, o.Baseline = filepath.Join(root, "area"), []string{"app"}, baseline
	raw, code := Run(o)
	validateEnvelope(t, raw)
	var response contract.Response[contract.QueryData]
	if err := json.Unmarshal(raw, &response); err != nil {
		t.Fatal(err)
	}
	if code != 0 || response.Data.Evidence[0].State != "unchanged" {
		t.Fatalf("prefix comparison: %s", raw)
	}
	if err := os.WriteFile(filepath.Join(root, ".git", "shallow"), []byte(baseline+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	o.Baseline = strings.Repeat("f", 40)
	raw, code = Run(o)
	if err := json.Unmarshal(raw, &response); err != nil {
		t.Fatal(err)
	}
	if code != 4 || response.Baseline.Reason != "history_unavailable" {
		t.Fatalf("shallow comparison: %s", raw)
	}
}

func TestFailedEvidenceReadsConsumeReservedBytes(t *testing.T) {
	root := mkdirScratch(t, "racing-budget-")
	initGitRepo(t, root)
	files := map[string]string{"archie.yaml": "schema_version: \"1\"\nid: app\nname: App\nsummary: Root.\n", "a.txt": "old", "b.txt": "old"}
	writeFiles(t, root, files)
	base := createCommit(t, root, files)
	g, err := repository.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	defer g.FS.Close()
	o := Defaults(contract.Get)
	o.Baseline, o.Limits.EvidenceBytes = base, 6
	checker := newChecker(g, o)
	checker.beforeRead = func(target string) {
		if e := os.WriteFile(filepath.Join(root, target), []byte("longer"), 0o600); e != nil {
			t.Fatal(e)
		}
	}
	if e := checker.check("a.txt", true); e.Reason != "unattributable" {
		t.Fatalf("expected racing read, got %+v", e)
	}
	if e := checker.check("b.txt", true); e.State != "unchecked" || e.Reason != "budget" {
		t.Fatalf("failed read bypassed budget: %+v", e)
	}
}
