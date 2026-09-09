package cli

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/wbreza/archie/contract"
	"github.com/wbreza/archie/internal/repository"
)

func scaffoldArgs(file, id string) []string {
	return []string{"scaffold", "--file", file, "--id", id, "--name", "Descriptive component", "--summary", "Owns an existing application boundary."}
}

type fileSnapshot struct {
	Mode fs.FileMode
	Size int64
	Hash [32]byte
	Link string
}

func snapshotTree(t *testing.T, root string) map[string]fileSnapshot {
	t.Helper()
	result := map[string]fileSnapshot{}
	err := filepath.WalkDir(root, func(p string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, p)
		s := fileSnapshot{Mode: info.Mode()}
		if info.Mode().IsRegular() {
			b, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			s.Size, s.Hash = info.Size(), sha256.Sum256(b)
		} else if info.Mode()&os.ModeSymlink != 0 {
			s.Link, err = os.Readlink(p)
			if err != nil {
				return err
			}
		}
		result[rel] = s
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func TestScaffoldProcessValidationReadback(t *testing.T) {
	root := newScratchDir(t)
	for _, item := range []struct{ file, id, parent string }{
		{"archie.yaml", "app", ""},
		{"render.archie.yaml", "renderer", "app"},
		{"mail.archie.yaml", "mailer", "app"},
		{"payments/archie.yaml", "payments", "app"},
		{"payments/refunds.archie.yaml", "refunds", "payments"},
	} {
		if item.id == "payments" {
			if err := os.Mkdir(filepath.Join(root, "payments"), 0o755); err != nil {
				t.Fatal(err)
			}
		}
		args := scaffoldArgs(item.file, item.id)
		if item.id == "renderer" {
			args[len(args)-3] = "Renderer: \"reports\""
			args[len(args)-1] = "Unicode 日本語\nOwns reusable rendering, not delivery. # literal"
		}
		before := snapshotTree(t, root)
		r := runCLI(t, root, args...)
		response := decodeResponse[contract.ScaffoldData](t, r.stdout)
		if r.exit != 0 || len(r.stderr) != 0 || response.Data == nil || response.Data.Path != item.file {
			t.Fatalf("scaffold %s: exit %d %s %s", item.file, r.exit, r.stdout, r.stderr)
		}
		after := snapshotTree(t, root)
		delete(after, filepath.FromSlash(item.file))
		if !reflect.DeepEqual(before, after) {
			t.Fatal("scaffold modified more than its new file")
		}
		raw, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(item.file)))
		if err != nil {
			t.Fatal(err)
		}
		record, _, p := repository.Parse(raw, item.file == "archie.yaml")
		if p != nil || record.ID != item.id || record.Name != args[len(args)-3] || record.Summary != args[len(args)-1] {
			t.Fatalf("generated YAML lost content: %+v %v", record, p)
		}
		check := runCLI(t, root, "validate")
		if check.exit != 0 {
			t.Fatalf("validate: %s", check.stdout)
		}
		decodeResponse[contract.ValidationData](t, check.stdout)
		get := runCLI(t, root, "get", "--id", item.id)
		got := decodeResponse[contract.QueryData](t, get.stdout)
		if get.exit != 0 || got.Data == nil || len(got.Data.Records) != 1 || got.Data.Records[0].Record.ID != item.id {
			t.Fatalf("get: %s", get.stdout)
		}
		if item.parent != "" && (len(got.Data.Records[0].Links) == 0 || got.Data.Records[0].Links[0].Target != item.parent) {
			t.Fatalf("derived parent: %s", get.stdout)
		}
	}
}

func TestScaffoldRefusalsNeverMutate(t *testing.T) {
	for _, tc := range []struct {
		name, file, id, code string
		files                map[string]string
		args                 []string
	}{
		{name: "existing", file: "archie.yaml", id: "new", code: "ALREADY_EXISTS"},
		{name: "duplicate", file: "new.archie.yaml", id: "app", code: "DUPLICATE_ID"},
		{name: "missing-parent", file: "absent/new.archie.yaml", id: "new", code: "IO_ERROR"},
		{name: "traversal", file: "../outside.archie.yaml", id: "new", code: "PATH_UNSAFE"},
		{name: "absolute", file: "/outside.archie.yaml", id: "new", code: "PATH_UNSAFE"},
		{name: "device", file: "CON.archie.yaml", id: "new", code: "PATH_UNSAFE"},
		{name: "case", file: "Case/NEW.archie.yaml", id: "new", code: "PATH_CASE", files: map[string]string{"Case/new.archie.yaml": minimalRecord("existing")}},
		{name: "wrong-name", file: "record.yaml", id: "new", code: "USAGE"},
		{name: "empty-prefix", file: ".archie.yaml", id: "new", code: "USAGE"},
		{name: "bad-graph", file: "new.archie.yaml", id: "new", code: "SCHEMA_INVALID", files: map[string]string{"bad.archie.yaml": minimalRecord("bad") + "extra: invalid\n"}},
		{name: "excluded-file", file: "new.archie.yaml", id: "new", code: "PATH_UNSAFE", files: map[string]string{"archie.yaml": minimalRecord("app") + "discovery:\n  exclude: [new.archie.yaml]\n"}},
		{name: "generated", file: "vendor/new.archie.yaml", id: "new", code: "PATH_UNSAFE", files: map[string]string{"vendor/sentinel.txt": "keep"}},
		{name: "nested", file: "nested/new.archie.yaml", id: "new", code: "PATH_UNSAFE", files: map[string]string{"nested/.git": "gitdir: elsewhere"}},
		{name: "blank-name", file: "new.archie.yaml", id: "new", code: "SCHEMA_INVALID", args: []string{"--name", " "}},
		{name: "long-name", file: "new.archie.yaml", id: "new", code: "SCHEMA_INVALID", args: []string{"--name", strings.Repeat("a", 257)}},
		{name: "long-summary", file: "new.archie.yaml", id: "new", code: "SCHEMA_INVALID", args: []string{"--summary", strings.Repeat("a", 1025)}},
		{name: "force", file: "new.archie.yaml", id: "new", code: "USAGE", args: []string{"--force"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			files := map[string]string{"archie.yaml": minimalRecord("app"), "sentinel": "preserve"}
			for k, v := range tc.files {
				files[k] = v
			}
			root := makeRepo(t, files)
			before := snapshotTree(t, root)
			args := scaffoldArgs(tc.file, tc.id)
			if len(tc.args) == 2 {
				for i := range args[:len(args)-1] {
					if args[i] == tc.args[0] {
						args[i+1] = tc.args[1]
					}
				}
			} else {
				args = append(args, tc.args...)
			}
			got := runCLI(t, root, args...)
			response := decodeResponse[any](t, got.stdout)
			if got.exit == 0 || len(got.stderr) != 0 || response.Data != nil || !hasDiagnostic(response.Diagnostics, tc.code) {
				t.Fatalf("expected %s: exit %d %s", tc.code, got.exit, got.stdout)
			}
			if !reflect.DeepEqual(before, snapshotTree(t, root)) {
				t.Fatal("refused scaffold mutated files")
			}
			var direct bytes.Buffer
			exit := Run(append(args, "--root", root), &direct)
			if exit != got.exit || !bytes.Equal(direct.Bytes(), got.stdout) {
				t.Fatal("in-process and process scaffold refusals differ")
			}
		})
	}
}

func minimalRecord(id string) string {
	return fmt.Sprintf("schema_version: \"1\"\nid: %s\nname: Component\nsummary: Existing boundary.\n", id)
}

func TestScaffoldExclusiveCompetingProcesses(t *testing.T) {
	root := newScratchDir(t)
	const count = 8
	results := make(chan cliResult, count)
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < count; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			cmd := exec.Command(cliBinary, scaffoldArgs("archie.yaml", fmt.Sprintf("app%d", i))...)
			cmd.Dir = root
			var out, stderr bytes.Buffer
			cmd.Stdout, cmd.Stderr = &out, &stderr
			err := cmd.Run()
			code := 0
			if err != nil {
				if ee, ok := err.(*exec.ExitError); ok {
					code = ee.ExitCode()
				} else {
					code = -1
				}
			}
			results <- cliResult{stdout: out.Bytes(), stderr: stderr.Bytes(), exit: code}
		}(i)
	}
	close(start)
	wg.Wait()
	close(results)
	winners := 0
	for r := range results {
		response := decodeResponse[any](t, r.stdout)
		if r.exit == 0 {
			winners++
		} else if r.exit != contract.ExitOperational || len(response.Diagnostics) != 1 ||
			(response.Diagnostics[0].Code != "ALREADY_EXISTS" && response.Diagnostics[0].Code != "IO_ERROR") {
			t.Fatalf("competing create: %d %s", r.exit, r.stdout)
		}
	}
	if winners != 1 {
		t.Fatalf("exclusive create winners=%d", winners)
	}
	if got := runCLI(t, root, "validate"); got.exit != 0 {
		t.Fatalf("winner was not valid: %s", got.stdout)
	}
}

func TestReadOnlyProcessesNeverMutate(t *testing.T) {
	root := makeRepo(t, map[string]string{
		"archie.yaml":           minimalRecord("app"),
		"component.archie.yaml": minimalRecord("component") + "links:\n- relation: source\n  target: code.go\n  description: implementation\n",
		"code.go":               "package component\n", "untracked.txt": "preserve",
	})
	before := snapshotTree(t, root)
	for _, args := range [][]string{
		{"discover"}, {"get", "--id", "component"}, {"context", "--query", "component"},
		{"impact", "--path", "code.go"}, {"validate"}, {"validate", "--evidence"},
		{"version"}, {"discover", "--cursor", "invalid"}, {"get", "--id", "missing"},
	} {
		r := runCLI(t, root, args...)
		decodeResponse[any](t, r.stdout)
		if !reflect.DeepEqual(before, snapshotTree(t, root)) {
			t.Fatalf("%v mutated application files", args)
		}
	}
	for _, command := range []string{"discover", "get", "context", "impact", "validate", "scaffold", "version"} {
		r := runCLI(t, root, command, "--help")
		if r.exit != 0 || len(r.stderr) != 0 || !strings.Contains(string(r.stdout), "Flags for "+command) {
			t.Fatalf("help %s: %+v", command, r)
		}
	}
	if !reflect.DeepEqual(before, snapshotTree(t, root)) {
		t.Fatal("help mutated files")
	}
}

func TestScaffoldRejectsFilesystemAliases(t *testing.T) {
	for _, kind := range []string{"symlink-output", "symlink-parent", "hardlink-output"} {
		t.Run(kind, func(t *testing.T) {
			root := makeRepo(t, map[string]string{"archie.yaml": minimalRecord("app")})
			outside := makeRepo(t, map[string]string{"sentinel": "keep outside"})
			target := "new.archie.yaml"
			var err error
			switch kind {
			case "symlink-output":
				err = os.Symlink(filepath.Join(outside, "sentinel"), filepath.Join(root, target))
			case "symlink-parent":
				err = os.Symlink(outside, filepath.Join(root, "linked"))
				target = "linked/new.archie.yaml"
			case "hardlink-output":
				err = os.Link(filepath.Join(outside, "sentinel"), filepath.Join(root, target))
			}
			if err != nil {
				t.Skipf("%s unavailable: %v", kind, err)
			}
			before, external := snapshotTree(t, root), snapshotTree(t, outside)
			r := runCLI(t, root, scaffoldArgs(target, "new")...)
			response := decodeResponse[any](t, r.stdout)
			if r.exit != contract.ExitInvalid || !hasDiagnostic(response.Diagnostics, "PATH_UNSAFE") {
				t.Fatalf("alias accepted: %s", r.stdout)
			}
			if !reflect.DeepEqual(before, snapshotTree(t, root)) || !reflect.DeepEqual(external, snapshotTree(t, outside)) {
				t.Fatal("alias refusal changed files")
			}
		})
	}
}

func TestDirectScaffoldDispatch(t *testing.T) {
	root := newScratchDir(t)
	args := append(scaffoldArgs("archie.yaml", "app"), "--root", root)
	var out bytes.Buffer
	if code := Run(args, &out); code != 0 {
		t.Fatalf("scaffold exit=%d: %s", code, out.Bytes())
	}
	got := decodeResponse[contract.ScaffoldData](t, out.Bytes())
	if got.Data == nil || got.Data.Path != "archie.yaml" {
		t.Fatal("wrong direct scaffold response")
	}
}

func TestInjectedBuildVersion(t *testing.T) {
	exe := filepath.Join(newScratchDir(t), "versioned.exe")
	const version = "1.2.3-test.4"
	cmd := exec.Command("go", "build", "-ldflags", "-X github.com/wbreza/archie/internal/query.BuildVersion="+version, "-o", exe, "./cmd/archie")
	cmd.Dir = repoRoot
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("versioned build: %v %s", err, out)
	}
	run := exec.Command(exe, "version")
	run.Dir = newScratchDir(t)
	out, err := run.Output()
	if err != nil {
		t.Fatal(err)
	}
	got := decodeResponse[contract.VersionData](t, out)
	if got.Data == nil || got.Data.Version != version {
		t.Fatalf("injected version: %s", out)
	}
}
