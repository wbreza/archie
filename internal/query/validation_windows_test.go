package query

import (
	"path/filepath"
	"syscall"
	"testing"

	"github.com/wbreza/archie/contract"
	"github.com/wbreza/archie/internal/repository"
)

func TestValidationReportsInaccessibleReference(t *testing.T) {
	root := mkdirScratch(t, "inaccessible-reference-")
	writeFiles(t, root, map[string]string{"archie.yaml": referenceFixture("locked.txt"), "locked.txt": ""})
	g, problem := repository.LoadForValidation(root)
	if problem != nil {
		t.Fatal(problem)
	}
	defer g.FS.Close()
	name, err := syscall.UTF16PtrFromString(filepath.Join(root, "locked.txt"))
	if err != nil {
		t.Fatal(err)
	}
	handle, err := syscall.CreateFile(name, syscall.GENERIC_READ, 0, nil, syscall.OPEN_EXISTING, syscall.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer syscall.CloseHandle(handle)
	o := Defaults(contract.Validate)
	o.Root = root
	raw, code := finishValidation(g, validateReferences(g, contract.MaxReferences), contract.DefaultMaxBytes)
	validateEnvelope(t, raw)
	r := decodeValidationResponse(t, raw)
	if code != 3 || r.Coverage.References.Invalid != 1 || r.Coverage.References.Complete ||
		r.Diagnostics[0].Target != "locked.txt" || r.Diagnostics[0].Code != "IO_ERROR" {
		t.Fatalf("inaccessible reference escaped validation: %s", raw)
	}
	// A new invocation may fail even earlier: Windows can deny Lstat during
	// discovery while this handle forbids sharing.
	raw, code = Run(o)
	validateEnvelope(t, raw)
	r = decodeValidationResponse(t, raw)
	if code != 3 || r.Coverage.References.Complete || r.Data != nil {
		t.Fatalf("inaccessible repository reported success: %s", raw)
	}
}
