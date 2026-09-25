package repository

import (
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/wbreza/archie/contract"
)

type Filesystem struct {
	Root          *os.Root
	Path          string
	observed      map[string]os.FileInfo
	references    map[string]bool
	listings      map[string][]os.DirEntry
	listedEntries int
}

func Open(root string) (*Filesystem, *Problem) {
	absolute, err := filepath.Abs(root)
	if err != nil {
		return nil, problem("PATH_UNSAFE", "Invalid repository root.")
	}
	volume := filepath.VolumeName(absolute)
	current := volume + string(filepath.Separator)
	for _, segment := range strings.Split(strings.TrimPrefix(absolute, current), string(filepath.Separator)) {
		if segment == "" {
			continue
		}
		current = filepath.Join(current, segment)
		info, e := os.Lstat(current)
		if e != nil {
			return nil, problem("IO_ERROR", "Cannot access repository root.")
		}
		if reparse(info) || !info.IsDir() {
			return nil, problem("PATH_UNSAFE", "Repository root must not traverse links or reparse points.")
		}
	}
	r, err := os.OpenRoot(absolute)
	if err != nil {
		return nil, problem("IO_ERROR", "Cannot open repository root.")
	}
	return &Filesystem{Root: r, Path: absolute, observed: map[string]os.FileInfo{}, references: map[string]bool{}}, nil
}

func (f *Filesystem) Close() error { return f.Root.Close() }

func Canonical(base, target string, relative bool) (string, *Problem) {
	if target == "" || !utf8.ValidString(target) || utf8.RuneCountInString(target) > 1024 ||
		strings.ContainsAny(target, `\:*?[]#<>"|`) || strings.HasPrefix(target, "/") {
		return "", problem("PATH_UNSAFE", "Target is not a supported repository path.")
	}
	for _, r := range target {
		if r < 32 || r == 127 {
			return "", problem("PATH_UNSAFE", "Control characters are not allowed in paths.")
		}
	}
	for _, part := range strings.Split(target, "/") {
		if part == "" {
			return "", problem("PATH_UNSAFE", "Empty path components are forbidden.")
		}
		if part == "." || part == ".." {
			if relative || target == "." {
				continue
			}
			return "", problem("PATH_UNSAFE", "Selectors and exclusions must be normalized.")
		}
		if strings.HasSuffix(part, ".") || strings.HasSuffix(part, " ") || device(part) {
			return "", problem("PATH_UNSAFE", "Nonportable path component.")
		}
	}
	p := path.Clean(path.Join(base, target))
	if p == ".." || strings.HasPrefix(p, "../") || utf8.RuneCountInString(p) > 1024 {
		return "", problem("PATH_UNSAFE", "Path escapes repository containment or exceeds its limit.")
	}
	return p, nil
}

func device(s string) bool {
	s = strings.ToUpper(strings.SplitN(s, ".", 2)[0])
	if s == "CON" || s == "PRN" || s == "AUX" || s == "NUL" || s == "CONIN$" || s == "CONOUT$" {
		return true
	}
	if strings.HasPrefix(s, "COM") || strings.HasPrefix(s, "LPT") {
		return len([]rune(s)) == 4 && strings.ContainsRune("123456789¹²³", []rune(s)[3])
	}
	return false
}

// Inspect checks exact spelling and every existing component. Rooted operations
// additionally enforce containment if a directory is replaced during an access.
func (f *Filesystem) Inspect(p string) (os.FileInfo, *Problem) {
	dir := "."
	if p == "." {
		info, err := f.Root.Lstat(".")
		if err != nil {
			return nil, problem("IO_ERROR", "Cannot inspect repository.")
		}
		return info, nil
	}
	for i, component := range strings.Split(p, "/") {
		entries, e := f.entries(dir, contract.MaxEntries+1)
		if e != nil {
			return nil, e
		}
		exact, folded := false, false
		for _, entry := range entries {
			if entry.Name() == component {
				exact = true
			} else if strings.EqualFold(entry.Name(), component) {
				folded = true
			}
		}
		if !exact {
			if folded {
				return nil, problem("PATH_CASE", "Path spelling does not match repository case.")
			}
			return nil, nil
		}
		dir = path.Join(dir, component)
		info, err := f.Root.Lstat(filepath.FromSlash(dir))
		if err != nil {
			return nil, problem("IO_ERROR", "Path changed during inspection.")
		}
		if reparse(info) {
			return nil, problem("PATH_UNSAFE", "Links and reparse points are forbidden.")
		}
		if i == len(strings.Split(p, "/"))-1 {
			if info.Mode().IsRegular() {
				handle, err := f.Root.OpenFile(filepath.FromSlash(p), readFlags(), 0)
				if err != nil {
					return nil, problem("IO_ERROR", "Cannot inspect file identity.")
				}
				opened, statErr := handle.Stat()
				count, countErr := linkCount(handle)
				handle.Close()
				if statErr != nil || countErr != nil || !os.SameFile(info, opened) {
					return nil, problem("IO_ERROR", "File identity changed during inspection.")
				}
				if count != 1 {
					return nil, problem("PATH_UNSAFE", "Hard-linked files are not supported.")
				}
			}
			return info, nil
		}
		if !info.IsDir() {
			return nil, problem("PATH_UNSAFE", "An intermediate path component is not a directory.")
		}
	}
	return nil, nil
}

func (f *Filesystem) entries(p string, max int) ([]os.DirEntry, *Problem) {
	before, err := f.Root.Lstat(filepath.FromSlash(p))
	if err != nil {
		return nil, problem("IO_ERROR", "Cannot inspect repository directory.")
	}
	if reparse(before) || !before.IsDir() {
		return nil, problem("PATH_UNSAFE", "Directory links are forbidden.")
	}
	if e := f.observe(p, before); e != nil {
		return nil, e
	}
	// Validation can reuse names only while the observed directory is stable.
	// Identity and metadata are rechecked even when enumeration is reused.
	if entries, ok := f.listings[p]; ok {
		if len(entries) >= max {
			return nil, problem("DISCOVERY_LIMIT", "Repository entry limit exceeded.")
		}
		return entries, nil
	}
	d, err := f.Root.OpenFile(filepath.FromSlash(p), readFlags(), 0)
	if err != nil {
		return nil, problem("IO_ERROR", "Cannot enumerate repository directory.")
	}
	defer d.Close()
	opened, statErr := d.Stat()
	if statErr != nil || !os.SameFile(before, opened) {
		return nil, problem("IO_ERROR", "Directory changed during access.")
	}
	entries, err := d.ReadDir(max)
	if err != nil && err != io.EOF {
		return nil, problem("IO_ERROR", "Cannot completely enumerate repository directory.")
	}
	if len(entries) >= max {
		return nil, problem("DISCOVERY_LIMIT", "Repository entry limit exceeded.")
	}
	after, statErr := f.Root.Lstat(filepath.FromSlash(p))
	if statErr != nil || reparse(after) || !os.SameFile(before, after) ||
		before.Size() != after.Size() || !before.ModTime().Equal(after.ModTime()) {
		return nil, problem("IO_ERROR", "Directory changed during enumeration.")
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	if f.listings != nil && len(entries) <= contract.MaxEntries-f.listedEntries {
		f.listings[p] = entries
		f.listedEntries += len(entries)
	}
	return entries, nil
}

// Read returns stable regular-file bytes only. The caller bounds allocation
// before opening; the extra byte detects growth without unbounded reads.
func (f *Filesystem) Read(p string, max int64) ([]byte, *Problem) {
	before, e := f.Inspect(p)
	if e != nil {
		return nil, e
	}
	if before == nil || !before.Mode().IsRegular() {
		return nil, problem("IO_ERROR", "Target is missing or not a regular file.")
	}
	if before.Size() > max {
		return nil, problem("BUDGET_EXHAUSTED", "File byte limit exceeded.")
	}
	handle, err := f.Root.OpenFile(filepath.FromSlash(p), readFlags(), 0)
	if err != nil {
		return nil, problem("IO_ERROR", "Cannot open selected file.")
	}
	defer handle.Close()
	opened, err := handle.Stat()
	if err != nil || !os.SameFile(before, opened) || !opened.Mode().IsRegular() {
		return nil, problem("IO_ERROR", "Target changed during access.")
	}
	if count, err := linkCount(handle); err != nil || count != 1 {
		return nil, problem("PATH_UNSAFE", "Hard-linked files are not supported.")
	}
	data, err := io.ReadAll(io.LimitReader(handle, max+1))
	after, statErr := handle.Stat()
	current, inspectErr := f.Inspect(p)
	if err != nil || statErr != nil || inspectErr != nil || current == nil ||
		!os.SameFile(before, current) || before.Size() != after.Size() ||
		before.ModTime() != after.ModTime() || int64(len(data)) != before.Size() {
		return nil, problem("IO_ERROR", "Target changed during reading.")
	}
	if int64(len(data)) > max {
		return nil, problem("BUDGET_EXHAUSTED", "File byte limit exceeded.")
	}
	if e := f.observe(p, before); e != nil {
		return nil, e
	}
	return data, nil
}

func (f *Filesystem) observe(p string, info os.FileInfo) *Problem {
	if before, ok := f.observed[p]; ok {
		if !os.SameFile(before, info) || before.Size() != info.Size() || !before.ModTime().Equal(info.ModTime()) {
			return problem("IO_ERROR", "Repository changed during discovery or reading.")
		}
	} else {
		f.observed[p] = info
	}
	return nil
}

// InspectReference retains the inspection for the invocation's final Verify.
// Missing targets are covered by the existing ancestor-directory observations.
func (f *Filesystem) InspectReference(p string) (os.FileInfo, *Problem) {
	info, e := f.Inspect(p)
	if e == nil && info != nil {
		e = f.observe(p, info)
		if e == nil {
			f.references[p] = true
		}
	}
	return info, e
}

func (f *Filesystem) Verify() *Problem {
	for p, before := range f.observed {
		info, err := f.Root.Lstat(filepath.FromSlash(p))
		if f.references[p] && err == nil {
			var e *Problem
			info, e = f.Inspect(p)
			if e != nil {
				return e
			}
		}
		if err != nil || info == nil || reparse(info) || !os.SameFile(before, info) || before.Size() != info.Size() || !before.ModTime().Equal(info.ModTime()) {
			return problem("IO_ERROR", "Repository changed during discovery.")
		}
	}
	return nil
}

func Contains(dir, p string) bool { return dir == "." || dir == p || strings.HasPrefix(p, dir+"/") }

func Descriptor(p string) bool {
	name := path.Base(p)
	return name == "archie.yaml" || strings.HasSuffix(name, ".archie.yaml") && len(name) > len(".archie.yaml")
}
