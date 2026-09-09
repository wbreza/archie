package cli

import (
	"io"
	"strings"
	"testing"

	"github.com/wbreza/archie/contract"
)

func TestParserCommandsAndBudgets(t *testing.T) {
	tests := []struct {
		command contract.Command
		args    []string
	}{
		{contract.Context, []string{"--query=mail reports", "--id", "renderer", "--id", "renderer", "--path", "src/a.go", "--keyword", "mail", "--max-records", "2", "--max-links=0", "--max-bytes=4096", "--max-evidence=0", "--max-evidence-bytes=0", "--json=true"}},
		{contract.Get, []string{"--id", "item", "--root=.", "--baseline", strings.Repeat("a", 40)}},
		{contract.Impact, []string{"--path=src/a.go", "--path", "deleted.go"}},
		{contract.Discover, []string{"--cursor=opaque"}},
		{contract.Validate, []string{"--evidence", "--max-evidence=1", "--max-evidence-bytes=4"}},
		{contract.Validate, []string{"--evidence=false"}},
		{contract.VersionCommand, []string{"--json"}},
		{contract.Get, []string{"--help"}},
	}
	for _, tc := range tests {
		t.Run(string(tc.command)+strings.Join(tc.args, " "), func(t *testing.T) {
			_, _, err := parse(tc.command, append([]string{string(tc.command)}, tc.args...))
			if err != nil {
				t.Fatal(err)
			}
		})
	}
	bad := [][]string{
		{"get"}, {"get", "--id"}, {"get", "--id", "--json"}, {"get", "--id", "a/b"},
		{"context", "--query", ""}, {"context", "--keyword", "two words"}, {"context", "--keyword", "word\u00a0"},
		{"context", "--query", strings.Repeat("a", 4097)}, {"context", "--path", strings.Repeat("a", 1025)},
		{"context", "--query", string([]byte{0xff})}, {"context", "word"}, {"get", "--id=a", "--id=b"},
		{"version", "--json=invalid"}, {"version", "--root=."}, {"context"},
		{"get", "--id=a", "--baseline=main"}, {"get", "--id=a", "--max-bytes=bad"},
		{"get", "--id=a", "--max-bytes=5"}, {"discover", "--cursor=" + strings.Repeat("a", 2049)},
		{"validate", "--max-evidence=0"}, {"get", "--unknown"}, {"scaffold"},
	}
	repeated := []string{"context"}
	for i := 0; i < 65; i++ {
		repeated = append(repeated, "--id=a")
	}
	bad = append(bad, repeated)
	for _, args := range bad {
		if _, _, err := parse(contract.Command(args[0]), args); err == nil {
			t.Fatalf("accepted %v", args)
		}
	}
	for _, args := range [][]string{{"version"}, {"--help"}, {"get", "--help"}, {"nonsense"}, {}} {
		code := Run(args, io.Discard)
		if code != 0 && code != contract.ExitUsage {
			t.Fatalf("unexpected command exit %d", code)
		}
	}
}

func TestProcessExactGetUsesActualByteSize(t *testing.T) {
	description := strings.Repeat("x", 3002)
	root := makeRepo(t, map[string]string{
		"archie.yaml":      "schema_version: \"1\"\nid: app\nname: App\nsummary: Root.\n",
		"huge.archie.yaml": "schema_version: \"1\"\nid: huge\nname: Huge\nsummary: Full record.\ndescription: " + description + "\n",
	})
	result := runCLI(t, repoRoot, "get", "--root", root, "--id", "huge", "--max-bytes", "4096")
	response := decodeResponse[contract.QueryData](t, result.stdout)
	if result.exit != 0 || len(result.stdout) > 4096 || len(response.Data.Records) != 1 || response.Data.Records[0].Record.Description != description {
		t.Fatalf("fitting record omitted: %s", result.stdout)
	}
}

func TestProcessOversizeDiagnosticCount(t *testing.T) {
	root := makeRepo(t, map[string]string{
		"archie.yaml": "schema_version: \"1\"\nid: app\nname: App\nsummary: Root.\ndescription: " + strings.Repeat("x", 8000) + "\n",
	})
	result := runCLI(t, repoRoot, "get", "--root", root, "--id", "app", "--max-bytes", "4096")
	response := decodeResponse[contract.QueryData](t, result.stdout)
	if result.exit != 4 || response.Coverage.DiagnosticCounts["BUDGET_EXHAUSTED"] != 1 {
		t.Fatalf("phantom record-budget diagnostic: %s", result.stdout)
	}
}
