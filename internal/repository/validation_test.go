package repository

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/wbreza/archie/contract"
)

func TestValidationReusesOnlyStableBoundedDirectoryListings(t *testing.T) {
	root := mkdirTemp(t, "validation-listings-")
	writeRepositoryFiles(t, root, map[string]string{
		"archie.yaml": "schema_version: \"1\"\nid: app\nname: App\nsummary: Root.\n",
		"source.txt":  "",
	})
	past := time.Unix(1, 0)
	if err := os.Chtimes(root, past, past); err != nil {
		t.Fatal(err)
	}
	g, err := LoadForValidation(root)
	if err != nil {
		t.Fatal(err)
	}
	defer g.FS.Close()
	first, err := g.FS.entries(".", contract.MaxEntries+1)
	if err != nil {
		t.Fatal(err)
	}
	second, err := g.FS.entries(".", contract.MaxEntries+1)
	if err != nil || &first[0] != &second[0] || g.FS.listedEntries != 2 {
		t.Fatal("validation did not reuse the stable listing")
	}
	_, err = g.FS.entries(".", 2)
	requireProblemCode(t, err, "DISCOVERY_LIMIT")
	writeRepositoryFiles(t, root, map[string]string{"new.txt": ""})
	_, err = g.FS.entries(".", contract.MaxEntries+1)
	requireProblemCode(t, err, "IO_ERROR")
}

func TestReferenceVerifyDetectsNewExternalHardlink(t *testing.T) {
	scratch := mkdirTemp(t, "reference-hardlink-race-")
	root := filepath.Join(scratch, "repo")
	writeRepositoryFiles(t, root, map[string]string{"source.txt": "source"})
	fs, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	defer fs.Close()
	if _, err := fs.InspectReference("source.txt"); err != nil {
		t.Fatal(err)
	}
	// A link outside root does not change its directory listing or source bytes.
	if err := os.Link(filepath.Join(root, "source.txt"), filepath.Join(scratch, "alias.txt")); err != nil {
		t.Fatal(err)
	}
	requireProblemCode(t, fs.Verify(), "PATH_UNSAFE")
}

func TestValidationListingReuseHasEntryCeiling(t *testing.T) {
	root := mkdirTemp(t, "listing-ceiling-")
	writeRepositoryFiles(t, root, map[string]string{"source.txt": ""})
	fs, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}

	defer fs.Close()
	fs.listings = map[string][]os.DirEntry{}
	fs.listedEntries = contract.MaxEntries
	if _, err := fs.entries(".", contract.MaxEntries+1); err != nil {
		t.Fatal(err)
	}
	if len(fs.listings) != 0 || fs.listedEntries != contract.MaxEntries {
		t.Fatal("directory listing reuse exceeded its memory bound")
	}
}

func TestSchemaCauseSelectionIsDeterministic(t *testing.T) {
	raw := []byte("schema_version: \"1\"\nid: app\nname: " + strings.Repeat("n", 257) +
		"\nsummary: " + strings.Repeat("s", 1025) +
		"\ndescription: " + strings.Repeat("d", 8995) + "\n")
	var first string
	for i := 0; i < 50; i++ {
		_, _, err := Parse(raw, true)
		requireProblemCode(t, err, "SCHEMA_INVALID")
		if i == 0 {
			first = err.Message
		}
		if err.Message != first || !strings.Contains(err.Message, "/description") ||
			!strings.Contains(err.Message, "8995") || !strings.Contains(err.Message, "8192") {
			t.Fatalf("unstable schema diagnostic: %q, first %q", err.Message, first)
		}
	}
}
