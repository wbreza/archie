package repository

import (
	"errors"
	"os"
	"path"
	"path/filepath"

	"github.com/wbreza/archie/contract"
	"gopkg.in/yaml.v3"
)

// Scaffold validates before exclusively creating one descriptor. It never creates
// directories or removes output, including on failure: removal could race a human
// replacement. A failed write can leave a partial new file and must be inspected.
func Scaffold(root, target string, record contract.Record) (string, *Problem) {
	p, e := Canonical(".", target, false)
	if e != nil {
		return "", e
	}
	if !Descriptor(p) {
		return "", problem("USAGE", "Scaffold filename must be archie.yaml or NAME.archie.yaml.")
	}
	raw, err := yaml.Marshal(record)
	if err != nil {
		return "", problem("SCHEMA_INVALID", "Cannot encode scaffold record.")
	}
	if _, _, e := Parse(raw, p == "archie.yaml"); e != nil {
		return "", e
	}
	fs, e := Open(root)
	if e != nil {
		return "", e
	}
	defer fs.Close()
	if info, e := fs.Inspect(p); e != nil {
		return "", e
	} else if info != nil {
		return "", problem("ALREADY_EXISTS", "Scaffold output already exists; nothing was overwritten.")
	}
	parent := path.Dir(p)
	before, e := fs.Inspect(parent)
	if e != nil {
		return "", e
	}
	if before == nil || !before.IsDir() {
		return "", problem("IO_ERROR", "Scaffold parent directory must already exist.")
	}
	if p != "archie.yaml" {
		g, e := load(fs, false)
		if e != nil {
			return "", e
		}
		if len(g.Diagnostics) != 0 {
			return "", problem("SCHEMA_INVALID", "Scaffold requires a valid repository graph.")
		}
		if _, exists := g.Nodes[record.ID]; exists {
			return "", problem("DUPLICATE_ID", "Scaffold ID already exists.")
		}
		if g.Excluded(p) != "" {
			return "", problem("PATH_UNSAFE", "Scaffold output is outside discoverable scope.")
		}
		for _, n := range g.Ordered {
			if n.Descriptor == "archie.yaml" && n.Record.Discovery != nil {
				for _, exclude := range n.Record.Discovery.Exclude {
					if Contains(exclude, p) {
						return "", problem("PATH_UNSAFE", "Scaffold output is explicitly excluded.")
					}
				}
			}
		}
		if g.Metadata.Discovered >= contract.MaxDescriptors {
			return "", problem("DISCOVERY_LIMIT", "Scaffold would exceed the descriptor limit.")
		}
	}
	if e := fs.Verify(); e != nil {
		return "", e
	}
	return fs.createDescriptor(p, raw, before)
}

func (fs *Filesystem) createDescriptor(p string, raw []byte, parentBefore os.FileInfo) (string, *Problem) {
	if _, e := fs.Inspect(path.Dir(p)); e != nil {
		return "", e
	}
	// Keep the original repository root as the containment boundary. A separate
	// child Root would follow that directory if it were moved out of the repo.
	file, err := fs.Root.OpenFile(filepath.FromSlash(p), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if errors.Is(err, os.ErrExist) {
		return "", problem("ALREADY_EXISTS", "Scaffold output already exists; nothing was overwritten.")
	}
	if err != nil {
		return "", problem("IO_ERROR", "Cannot exclusively create scaffold output.")
	}
	parentAfter, parentErr := fs.Root.Lstat(filepath.FromSlash(path.Dir(p)))
	if parentErr != nil || reparse(parentAfter) || !os.SameFile(parentBefore, parentAfter) {
		file.Close()
		return "", problem("IO_ERROR", "Scaffold parent changed; inspect the new file before retrying.")
	}
	n, writeErr := file.Write(raw)
	syncErr := file.Sync()
	closeErr := file.Close()
	if writeErr != nil || n != len(raw) || syncErr != nil || closeErr != nil {
		return "", problem("IO_ERROR", "Scaffold write failed; inspect the new file before retrying.")
	}
	return p, nil
}
