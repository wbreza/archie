package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"runtime"
	"slices"
	"testing"
	"time"

	"github.com/wbreza/archie/contract"
)

type processMeasurement struct {
	Command       []string  `json:"command"`
	FirstPass     string    `json:"first_pass_condition"`
	FirstMS       float64   `json:"first_ms"`
	SubsequentMS  []float64 `json:"subsequent_ms"`
	MinMS         float64   `json:"min_ms"`
	P50MS         float64   `json:"p50_ms"`
	P95MS         float64   `json:"p95_ms"`
	MaxMS         float64   `json:"max_ms"`
	Exit          int       `json:"exit"`
	StdoutBytes   int       `json:"stdout_bytes"`
	MetadataCount int       `json:"metadata_count"`
	Checked       int       `json:"evidence_checked"`
}

// TestRepresentativeProcessWorkload is opt-in, not a performance threshold.
// Timing surrounds exec.Run and output capture only; fixture creation, JSON
// validation, binary building and report serialization are outside measurements.
func TestRepresentativeProcessWorkload(t *testing.T) {
	if os.Getenv("ARCHIE_MEASURE") != "1" {
		t.Skip("set ARCHIE_MEASURE=1 for the representative full-process workload")
	}
	reportPath := os.Getenv("ARCHIE_MEASURE_REPORT")
	if reportPath == "" {
		t.Fatal("ARCHIE_MEASURE_REPORT must name a new report file")
	}
	report, err := os.OpenFile(reportPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	defer report.Close()
	root := newScratchDir(t)
	files := map[string]string{"archie.yaml": minimalRecord("app")}
	id := 0
	for area := 0; area < 100; area++ {
		for file := 0; file < 95; file++ {
			files[fmt.Sprintf("area%03d/source%03d.txt", area, file)] = fmt.Sprintf("Representative application source area=%d file=%d\n", area, file)
		}
		for record := 0; record < 5 && id < 499; record++ {
			name := "archie.yaml"
			if record > 0 {
				name = fmt.Sprintf("component%d.archie.yaml", record)
			}
			files[fmt.Sprintf("area%03d/%s", area, name)] = fmt.Sprintf(`schema_version: "1"
id: component-%03d
name: Report delivery component %03d
summary: Coordinates reusable rendering, scheduling, and report delivery boundaries.
description: Owns one local application mechanism; delivery orchestration remains separate from rendering.
rationale: Describes current behavior without claiming historical decision authority.
tags: [reports, rendering, delivery]
links:
  - relation: source
    target: source%03d.txt
    description: Local implementation pointer.
  - relation: related_to
    target: component-%03d
    description: Adjacent reusable mechanism.
`, id, id, record, (id+1)%499)
			id++
		}
	}
	if len(files) != 10000 || id != 499 {
		t.Fatalf("bad fixture files=%d non-root records=%d", len(files), id)
	}
	writeFiles(t, root, files)
	workloads := [][]string{
		{"discover"},
		{"get", "--id", "component-042"},
		{"context", "--query", "report delivery", "--max-records", "10"},
		{"validate", "--evidence"},
		{"version"},
	}
	measurements := []processMeasurement{}
	for i, args := range workloads {
		m := processMeasurement{Command: args, FirstPass: "filesystem already visited by earlier CLI commands"}
		if i == 0 {
			m.FirstPass = "first CLI filesystem pass after fixture creation (not cold cache)"
		} else if args[0] == "version" {
			m.FirstPass = "startup-only; repository not read"
		}
		for sample := 0; sample <= 20; sample++ {
			start := time.Now()
			got := runCLI(t, root, args...)
			ms := float64(time.Since(start).Nanoseconds()) / 1e6
			response := decodeResponse[json.RawMessage](t, got.stdout)
			if len(got.stderr) != 0 || (got.exit != 0 && got.exit != contract.ExitPartial) {
				t.Fatalf("measurement failed: %v %d %s %s", args, got.exit, got.stdout, got.stderr)
			}
			if args[0] != "version" && (response.Coverage.Metadata.Loaded != 500 || !response.Coverage.Metadata.Complete) {
				t.Fatalf("incomplete workload: %s", got.stdout)
			}
			if args[0] == "get" || args[0] == "context" {
				var data contract.QueryData
				if response.Data == nil || json.Unmarshal(*response.Data, &data) != nil || len(data.Records) == 0 || response.Coverage.Evidence.Checked == 0 {
					t.Fatalf("retrieval/evidence not exercised: %s", got.stdout)
				}
			}
			if len(got.stdout) > contract.DefaultMaxBytes {
				t.Fatalf("output budget exceeded: %d", len(got.stdout))
			}
			if sample == 0 {
				m.FirstMS, m.Exit, m.StdoutBytes = ms, got.exit, len(got.stdout)
				m.MetadataCount, m.Checked = response.Coverage.Metadata.Loaded, response.Coverage.Evidence.Checked
			} else {
				if got.exit != m.Exit || len(got.stdout) != m.StdoutBytes {
					t.Fatal("non-deterministic response during measurement")
				}
				m.SubsequentMS = append(m.SubsequentMS, ms)
			}
		}
		sorted := slices.Clone(m.SubsequentMS)
		slices.Sort(sorted)
		m.MinMS, m.P50MS, m.P95MS, m.MaxMS = sorted[0], sorted[9], sorted[18], sorted[19]
		t.Logf("%v first %.2f ms subsequent min/p50/p95/max %.2f/%.2f/%.2f/%.2f ms exit=%d bytes=%d",
			args, m.FirstMS, m.MinMS, m.P50MS, m.P95MS, m.MaxMS, m.Exit, m.StdoutBytes)
		measurements = append(measurements, m)
	}
	result := struct {
		Platform     string               `json:"platform"`
		GoVersion    string               `json:"go_version"`
		LogicalCPUs  int                  `json:"logical_cpus"`
		Files        int                  `json:"files"`
		Records      int                  `json:"records"`
		Areas        int                  `json:"areas"`
		Conditions   string               `json:"conditions"`
		Measurements []processMeasurement `json:"measurements"`
	}{
		runtime.GOOS + "/" + runtime.GOARCH, runtime.Version(), runtime.NumCPU(),
		10000, 500, 100,
		"New process per sample; no cache eviction; no baseline supplied; no source execution. p50/p95 use nearest-rank of 20 subsequent observations. Fixture creation, build and validation excluded from timing.",
		measurements,
	}
	encoder := json.NewEncoder(report)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(result); err != nil {
		t.Fatal(err)
	}
	if err := report.Sync(); err != nil {
		t.Fatal(err)
	}
}
