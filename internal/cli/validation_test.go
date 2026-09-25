package cli

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/wbreza/archie/contract"
	"github.com/wbreza/archie/internal/testutil"
)

func TestPlainValidateProcessChecks493References(t *testing.T) {
	root := makeRepo(t, testutil.ValidationFixture())
	got := runCLI(t, root, "validate")
	r := decodeResponse[contract.ValidationData](t, got.stdout)
	if got.exit != 0 || len(got.stderr) != 0 || r.Coverage.References.Checked != 493 ||
		!r.Coverage.References.Complete || len(got.stdout) > 4096 || len(r.Data.Evidence) != 0 {
		t.Fatalf("incomplete default validation: %s %s", got.stdout, got.stderr)
	}
	if err := os.Remove(filepath.Join(root, filepath.FromSlash(testutil.ValidationTarget(492)))); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"validate"}, {"validate", "--evidence=false"}, {"validate", "--evidence", "--max-evidence=0", "--max-evidence-bytes=0"}} {
		got = runCLI(t, root, args...)
		r = decodeResponse[contract.ValidationData](t, got.stdout)
		if got.exit != 1 || r.Coverage.References.Checked != 493 || r.Coverage.References.Missing != 1 {
			t.Fatalf("flags bypassed mandatory validation: %s", got.stdout)
		}
	}
}

func TestValidationProcessMeasurement(t *testing.T) {
	if os.Getenv("ARCHIE_MEASURE_VALIDATION") != "1" {
		t.Skip("set ARCHIE_MEASURE_VALIDATION=1 for representative native measurements")
	}
	root := makeRepo(t, testutil.ValidationFixture())
	samples := []float64{}
	var first float64
	var outputBytes int
	for i := 0; i < 11; i++ {
		start := time.Now()
		got := runCLI(t, root, "validate")
		ms := float64(time.Since(start).Nanoseconds()) / 1e6
		r := decodeResponse[contract.ValidationData](t, got.stdout)
		if got.exit != 0 || r.Coverage.References.Checked != 493 || !r.Coverage.References.Complete {
			t.Fatalf("measurement incomplete: %s", got.stdout)
		}
		if i == 0 {
			first, outputBytes = ms, len(got.stdout)
		} else {
			if len(got.stdout) != outputBytes {
				t.Fatal("nondeterministic output during measurement")
			}
			samples = append(samples, ms)
		}
	}
	slices.Sort(samples)
	t.Logf("37 descriptors/493 unique references; native process including capture, excluding fixture/build/JSON validation; no cache eviction; first=%.2fms subsequent min/p50/p95/max=%.2f/%.2f/%.2f/%.2fms stdout=%d bytes",
		first, samples[0], samples[4], samples[9], samples[9], outputBytes)
}
