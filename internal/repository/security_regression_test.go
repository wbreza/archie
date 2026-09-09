package repository

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRejectHardLinkedDescriptorsAndEvidence(t *testing.T) {
	for _, target := range []string{"alias.archie.yaml", "source.txt"} {
		t.Run(target, func(t *testing.T) {
			scratch := mkdirTemp(t, "hardlink-")
			root := filepath.Join(scratch, "repo")
			outside := filepath.Join(scratch, "outside.txt")
			text := "schema_version: \"1\"\nid: external\nname: External\nsummary: External descriptor.\n"
			if err := os.WriteFile(outside, []byte(text), 0o600); err != nil {
				t.Fatal(err)
			}

			record := "schema_version: \"1\"\nid: app\nname: App\nsummary: Root.\n"
			if target == "source.txt" {
				record += "links: [{relation: source, target: source.txt, description: Source.}]\n"
			}
			writeRepositoryFiles(t, root, map[string]string{"archie.yaml": record})
			if err := os.Link(outside, filepath.Join(root, target)); err != nil {
				t.Fatal(err)
			}
			g, err := Load(root)
			if g != nil {
				g.FS.Close()
				t.Fatal("published hard-linked external data")
			}
			requireProblemCode(t, err, "PATH_UNSAFE")
		})
	}
}

func TestDiscoveryDetectsConcurrentDirectoryChange(t *testing.T) {
	root := mkdirTemp(t, "discovery-race-")
	fs, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	defer fs.Close()
	if _, err := fs.entries(".", 10); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "new.archie.yaml"), []byte("new"), 0o600); err != nil {
		t.Fatal(err)
	}
	requireProblemCode(t, fs.Verify(), "IO_ERROR")
}
