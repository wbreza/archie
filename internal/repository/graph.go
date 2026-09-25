package repository

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"hash"
	"os"
	"path"
	"path/filepath"
	"sort"

	"github.com/wbreza/archie/contract"
)

type Node struct {
	Record     contract.RootRecord
	Descriptor string
	Parent     string
	Children   []string
	Outgoing   []string
	Incoming   []string
	References []contract.Link
}

type Boundary struct{ Path, Reason string }

type Graph struct {
	FS          *Filesystem
	Nodes       map[string]*Node
	Ordered     []*Node
	Metadata    contract.MetadataCoverage
	Diagnostics []contract.Diagnostic
	Boundaries  []Boundary
	Invalid     []string
	Snapshot    string
}

var pruned = map[string]bool{
	".git": true, ".hg": true, ".svn": true, "node_modules": true,
	"vendor": true, "dist": true, "build": true, "coverage": true,
	".next": true, ".venv": true, "__pycache__": true,
}

// Load never publishes a graph if enumeration or identity is uncertain.
// Local schema errors with a provably unique ID are retained as explicit gaps.
func Load(root string) (*Graph, *Problem) {
	return loadRoot(root, false)
}

// LoadForValidation defers file-reference inspection to the exhaustive validator.
// Scoped queries still require the loader's global reference safety checks.
func LoadForValidation(root string) (*Graph, *Problem) {
	return loadRoot(root, true)
}

func loadRoot(root string, validation bool) (*Graph, *Problem) {
	fs, e := Open(root)
	if e != nil {
		return nil, e
	}
	if validation {
		fs.listings = map[string][]os.DirEntry{}
	}
	g, e := load(fs, validation)
	if e != nil {
		fs.Close()
		return nil, e
	}
	return g, nil
}

func load(fs *Filesystem, validation bool) (*Graph, *Problem) {
	info, e := fs.Inspect("archie.yaml")
	if e != nil {
		return nil, e
	}
	if info == nil {
		return nil, problem("ROOT_REQUIRED", "Root archie.yaml is required.")
	}
	rootRaw, e := fs.Read("archie.yaml", contract.MaxDescriptorBytes)
	if e != nil {
		if e.Code == "BUDGET_EXHAUSTED" {
			e.Code = "DISCOVERY_LIMIT"
		}
		return nil, e
	}
	root, _, e := Parse(rootRaw, true)
	if e != nil {
		e.Descriptor = "archie.yaml"
		return nil, e
	}
	excludes := []string{}
	if root.Discovery != nil {
		for _, p := range root.Discovery.Exclude {
			c, err := Canonical(".", p, false)
			if err != nil {
				return nil, err
			}
			if c == "." || c == "archie.yaml" {
				return nil, problem("PATH_UNSAFE", "Root metadata cannot be excluded.")
			}
			if _, err := fs.Inspect(c); err != nil {
				return nil, err
			}
			excludes = append(excludes, c)
		}
	}
	g := &Graph{FS: fs, Nodes: map[string]*Node{}, Diagnostics: []contract.Diagnostic{}, Invalid: []string{}}
	raws := map[string][]byte{}
	allNodes := []*Node{}
	identities := map[string]bool{}
	entries := 0
	var walk func(string) *Problem
	walk = func(dir string) *Problem {
		list, err := fs.entries(dir, contract.MaxEntries-entries+1)
		if err != nil {
			return err
		}
		for _, entry := range list {
			entries++
			if entries > contract.MaxEntries {
				return problem("DISCOVERY_LIMIT", "Repository entry limit exceeded.")
			}
			p := path.Join(dir, entry.Name())
			reason := ""
			for _, exclude := range excludes {
				if Contains(exclude, p) {
					reason = "excluded"
				}
			}
			info, statErr := fs.Root.Lstat(filepath.FromSlash(p))
			if statErr != nil {
				return problem("IO_ERROR", "Discovery changed during enumeration.")
			}
			if reparse(info) {
				if Descriptor(p) {
					return problem("PATH_UNSAFE", "Descriptor links and reparse points are forbidden.")
				}
				reason = "symlink"
			} else if info.IsDir() && pruned[entry.Name()] {
				reason = "excluded"
			} else if info.IsDir() && reason == "" {
				if _, nestedErr := fs.Root.Lstat(filepath.FromSlash(path.Join(p, ".git"))); nestedErr == nil {
					reason = "nested_repository"
				} else if !os.IsNotExist(nestedErr) {
					return problem("IO_ERROR", "Cannot establish nested repository boundary.")
				}
			}
			if reason != "" {
				if _, err := Canonical(".", p, false); err != nil {
					return err
				}
				g.Boundaries = append(g.Boundaries, Boundary{p, reason})
				continue
			}
			if info.IsDir() {
				if err := walk(p); err != nil {
					return err
				}
				continue
			}
			if !Descriptor(p) {
				continue
			}
			if _, err := Canonical(".", p, false); err != nil {
				return err
			}
			g.Metadata.Discovered++
			if g.Metadata.Discovered > contract.MaxDescriptors {
				return problem("DISCOVERY_LIMIT", "Descriptor count limit exceeded.")
			}
			raw, err := fs.Read(p, contract.MaxDescriptorBytes)
			if err != nil {
				if err.Code == "BUDGET_EXHAUSTED" {
					err.Code = "DISCOVERY_LIMIT"
				}
				return err
			}
			if p == "archie.yaml" && string(raw) != string(rootRaw) {
				return problem("IO_ERROR", "Root configuration changed during discovery.")
			}
			raws[p] = raw
			record, id, parseErr := Parse(raw, p == "archie.yaml")
			if id == "" || parseErr != nil && (p == "archie.yaml" || parseErr.Code != "SCHEMA_INVALID") {
				if parseErr == nil {
					parseErr = problem("SCHEMA_INVALID", "Identity is unavailable.")
				}
				parseErr.Descriptor = p
				return parseErr
			}
			if identities[id] {
				return problem("DUPLICATE_ID", "Duplicate record identity.")
			}
			identities[id] = true
			n := &Node{Record: record, Descriptor: p, Children: []string{}, Outgoing: []string{}, Incoming: []string{}, References: []contract.Link{}}
			for _, link := range record.Links {
				if link.Relation == contract.DependsOn || link.Relation == contract.RelatedTo {
					n.Outgoing = append(n.Outgoing, link.Target)
					continue
				}
				canonical, err := Canonical(path.Dir(p), link.Target, true)
				if err != nil {
					return err
				}
				if canonical == "." {
					return problem("PATH_UNSAFE", "File links must name a file, not the repository root.")
				}
				link.Target = canonical
				n.References = append(n.References, link)
			}
			allNodes = append(allNodes, n)
			if parseErr != nil {
				g.invalidate(p, parseErr.Code, parseErr.Message)
				continue
			}
			g.Nodes[id] = n
			g.Ordered = append(g.Ordered, n)
		}
		return nil
	}
	if e := walk("."); e != nil {
		return nil, e
	}
	sort.Slice(g.Ordered, func(i, j int) bool { return g.Ordered[i].Descriptor < g.Ordered[j].Descriptor })
	sort.Slice(g.Boundaries, func(i, j int) bool { return g.Boundaries[i].Path < g.Boundaries[j].Path })
	if !validation {
		for _, n := range allNodes {
			for _, ref := range n.References {
				if boundary := g.Excluded(ref.Target); boundary != "" && boundary != "symlink" {
					continue
				}
				if _, e := fs.Inspect(ref.Target); e != nil {
					return nil, e
				}
			}
		}
	}
	// Remove unresolved outgoing references to a fixed point, retaining every gap.
	for {
		removed := false
		for _, n := range g.Ordered {
			if g.Nodes[n.Record.ID] == nil {
				continue
			}
			for _, target := range n.Outgoing {
				if g.Nodes[target] == nil {
					g.invalidate(n.Descriptor, "LINK_UNRESOLVED", "Record contains an unresolved link to "+target+".")
					delete(g.Nodes, n.Record.ID)
					removed = true
					break
				}
			}
		}
		if !removed {
			break
		}
	}
	valid := []*Node{}
	directories := map[string]string{}
	for _, n := range g.Ordered {
		if g.Nodes[n.Record.ID] != nil {
			valid = append(valid, n)
			if path.Base(n.Descriptor) == "archie.yaml" {
				directories[path.Dir(n.Descriptor)] = n.Record.ID
			}
		}
	}
	g.Ordered = valid
	for _, n := range valid {
		if n.Descriptor != "archie.yaml" {
			dir := path.Dir(n.Descriptor)
			if path.Base(n.Descriptor) == "archie.yaml" {
				dir = path.Dir(dir)
			}
			for {
				if parent := directories[dir]; parent != "" {
					n.Parent = parent
					g.Nodes[parent].Children = append(g.Nodes[parent].Children, n.Record.ID)
					break
				}
				if dir == "." {
					break
				}
				dir = path.Dir(dir)
			}
		}
		n.Outgoing = unique(n.Outgoing)
		for _, target := range n.Outgoing {
			g.Nodes[target].Incoming = append(g.Nodes[target].Incoming, n.Record.ID)
		}
		sort.Slice(n.References, func(i, j int) bool {
			a, b := n.References[i], n.References[j]
			if a.Relation != b.Relation {
				return a.Relation < b.Relation
			}
			if a.Target != b.Target {
				return a.Target < b.Target
			}
			return a.Description < b.Description
		})
	}
	for _, n := range valid {
		n.Children, n.Incoming = unique(n.Children), unique(n.Incoming)
	}
	g.Metadata.Loaded, g.Metadata.Invalid = len(valid), len(g.Invalid)
	g.Metadata.Excluded, g.Metadata.Complete = len(g.Boundaries), true
	if g.Nodes[root.ID] == nil {
		if e := fs.Verify(); e != nil {
			return nil, e
		}
		return nil, &Problem{Code: "LINK_UNRESOLVED", Message: "Root contains an unresolved record link.",
			Descriptor: "archie.yaml", Diagnostics: g.Diagnostics, Metadata: g.Metadata}
	}
	keys := make([]string, 0, len(raws))
	for p := range raws {
		keys = append(keys, p)
	}
	sort.Strings(keys)
	h := sha256.New()
	for _, p := range keys {
		hashPart(h, []byte(p))
		hashPart(h, raws[p])
	}
	for _, b := range g.Boundaries {
		hashPart(h, []byte(b.Path))
		hashPart(h, []byte(b.Reason))
	}
	config, _ := json.Marshal(root.Discovery)
	hashPart(h, config)
	g.Snapshot = hex.EncodeToString(h.Sum(nil))
	if e := fs.Verify(); e != nil {
		return nil, e
	}
	return g, nil
}

func (g *Graph) invalidate(p, code, message string) {
	g.Invalid = append(g.Invalid, p)
	g.Diagnostics = append(g.Diagnostics, contract.Diagnostic{Code: code, Message: message, Descriptor: p})
}

func (g *Graph) Excluded(p string) string {
	for _, b := range g.Boundaries {
		if Contains(b.Path, p) {
			return b.Reason
		}
	}
	return ""
}

func hashPart(h hash.Hash, b []byte) {
	_ = binary.Write(h, binary.BigEndian, uint64(len(b)))
	_, _ = h.Write(b)
}

func unique(s []string) []string {
	sort.Strings(s)
	r := []string{}
	for _, v := range s {
		if len(r) == 0 || r[len(r)-1] != v {
			r = append(r, v)
		}
	}
	return r
}
