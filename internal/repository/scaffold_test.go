package repository

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/wbreza/archie/contract"
)

func TestScaffoldMovedParentDoesNotFollowOutsideRoot(t *testing.T) {
	root := mkdirTemp(t, "scaffold-move-")
	outside := mkdirTemp(t, "scaffold-outside-")
	parent := filepath.Join(root, "parent")
	if err := os.Mkdir(parent, 0o755); err != nil {
		t.Fatal(err)
	}
	fs, e := Open(root)
	if e != nil {
		t.Fatal(e)
	}
	defer fs.Close()
	before, e := fs.Inspect("parent")
	if e != nil {
		t.Fatal(e)
	}
	moved := filepath.Join(outside, "moved")
	if err := os.Rename(parent, moved); err != nil {
		t.Fatal(err)
	}
	if _, e := fs.createDescriptor("parent/new.archie.yaml", []byte("must not write"), before); e == nil {
		t.Fatal("moved parent accepted")
	}
	if _, err := os.Stat(filepath.Join(moved, "new.archie.yaml")); !os.IsNotExist(err) {
		t.Fatal("created output through moved parent")
	}
}

func TestScaffoldRejectsDirectoryLinkOrJunction(t *testing.T) {
	root, outside := mkdirTemp(t, "scaffold-link-"), mkdirTemp(t, "scaffold-link-outside-")
	link := filepath.Join(root, "linked")
	if !createDirectoryLink(t, link, outside) {
		t.Skip("directory links and junctions unavailable")
	}
	record := contract.Record{SchemaVersion: "1", ID: "new", Name: "New", Summary: "Boundary"}
	for _, tc := range []struct{ root, target string }{{root, "linked/new.archie.yaml"}, {link, "archie.yaml"}} {
		if _, e := Scaffold(tc.root, tc.target, record); e == nil || e.Code != "PATH_UNSAFE" {
			t.Fatalf("directory alias accepted: %v", e)
		}
	}
	entries, err := os.ReadDir(outside)
	if err != nil || len(entries) != 0 {
		t.Fatal("directory alias changed outside files")
	}
}

func TestScaffold(t *testing.T) {
	record := contract.Record{SchemaVersion: "1", ID: "app", Name: "Application", Summary: "Owns application boundaries."}
	root := mkdirTemp(t, "scaffold-")
	if _, e := Scaffold(root, "archie.yaml", record); e != nil {
		t.Fatal(e)
	}
	record.ID = "child"
	if _, e := Scaffold(root, "child.archie.yaml", record); e != nil {
		t.Fatal(e)
	}
	g, e := Load(root)
	if e != nil {
		t.Fatal(e)
	}
	defer g.FS.Close()
	if len(g.Nodes) != 2 || g.Nodes["child"].Parent != "app" {
		t.Fatalf("bad scaffold graph: %+v", g.Nodes)
	}
	for _, tc := range []struct{ file, code string }{
		{"child.archie.yaml", "ALREADY_EXISTS"},
		{"other.archie.yaml", "DUPLICATE_ID"},
		{"missing/archie.yaml", "IO_ERROR"},
		{"../escape.archie.yaml", "PATH_UNSAFE"},
		{"no.yaml", "USAGE"},
	} {
		if _, e := Scaffold(root, tc.file, record); e == nil || e.Code != tc.code {
			t.Errorf("%s: %v", tc.file, e)
		}
	}
	record.Name = " "
	if _, e := Scaffold(root, "invalid.archie.yaml", record); e == nil || e.Code != "SCHEMA_INVALID" {
		t.Fatal(e)
	}
	record.Name = "Application"
	for _, tc := range []struct {
		name, target, code string
		files              map[string]string
	}{
		{"no-root", "child.archie.yaml", "ROOT_REQUIRED", nil},
		{"invalid", "child.archie.yaml", "SCHEMA_INVALID", map[string]string{"bad.archie.yaml": "schema_version: \"1\"\nid: bad\nname: Bad\nsummary: Broken\nextra: invalid\n"}},
		{"excluded", "new.archie.yaml", "PATH_UNSAFE", map[string]string{"archie.yaml": "schema_version: \"1\"\nid: app\nname: Application\nsummary: Boundary\ndiscovery:\n  exclude: [new.archie.yaml]\n"}},
		{"pruned", "vendor/new.archie.yaml", "PATH_UNSAFE", map[string]string{"vendor/file": "keep"}},
		{"not-dir", "file/new.archie.yaml", "PATH_UNSAFE", map[string]string{"file": "keep"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := mkdirTemp(t, "scaffold-refusal-")
			if tc.name != "no-root" {
				record.ID = "app"
				if _, e := Scaffold(r, "archie.yaml", record); e != nil {
					t.Fatal(e)
				}
			}
			writeRepositoryFiles(t, r, tc.files)
			record.ID = "new"
			if _, e := Scaffold(r, tc.target, record); e == nil || e.Code != tc.code {
				t.Fatalf("expected %s: %v", tc.code, e)
			}
		})
	}
	if _, e := Scaffold(filepath.Join(root, "missing"), "archie.yaml", record); e == nil || e.Code != "IO_ERROR" {
		t.Fatal(e)
	}
	if _, err := os.Stat(filepath.Join(root, "invalid.archie.yaml")); !os.IsNotExist(err) {
		t.Fatal("invalid document created")
	}
}
