package repository

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"sort"
	"strings"
	"syscall"
	"testing"

	"github.com/wbreza/archie/contract"
	"github.com/wbreza/archie/internal/testutil"
	"gopkg.in/yaml.v3"
)

var (
	contractFixtureDir = filepath.Join("..", "..", "testdata", "contract")
	sessionScratchDir  = testutil.ScratchBase()
)

type recordFixture struct {
	Name  string `json:"name"`
	Root  bool   `json:"root"`
	Valid bool   `json:"valid"`
	YAML  string `json:"yaml"`
}

type yamlFixture struct {
	Name   string `json:"name"`
	Reason string `json:"reason"`
	YAML   string `json:"yaml"`
}

type repositoryFixture struct {
	Name     string              `json:"name"`
	Files    map[string]string   `json:"files"`
	Symlinks map[string]string   `json:"symlinks"`
	Parents  map[string]string   `json:"parents"`
	Ordered  []string            `json:"ordered_descriptors"`
	Resolved map[string][]string `json:"resolved"`
	Excluded []string            `json:"excluded"`
	Error    string              `json:"error"`
}

func readFixtureJSON(t *testing.T, name string, target any) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(contractFixtureDir, name))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, target); err != nil {
		t.Fatal(err)
	}
}

func fixtureIdentity(t *testing.T, text string) string {
	t.Helper()
	var doc map[string]any
	if err := yaml.Unmarshal([]byte(text), &doc); err != nil {
		t.Fatalf("fixture YAML must parse: %v", err)
	}
	id, _ := doc["id"].(string)
	if idPattern.MatchString(id) {
		return id
	}
	return ""
}

func sanitizeName(name string) string {
	replacer := strings.NewReplacer(" ", "-", "/", "-", "\\", "-", ":", "-", "*", "-", "?", "-", "\"", "-", "<", "-", ">", "-", "|", "-")
	return replacer.Replace(name)
}

func mkdirTemp(t *testing.T, prefix string) string {
	t.Helper()
	if err := os.MkdirAll(sessionScratchDir, 0o755); err != nil {
		t.Fatal(err)
	}
	dir, err := os.MkdirTemp(sessionScratchDir, prefix)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(dir); err != nil {
			t.Errorf("remove scratch dir: %v", err)
		}
	})
	return dir
}

func writeRepositoryFiles(t *testing.T, root string, files map[string]string) {
	t.Helper()
	folded := map[string]string{}
	for rel, content := range files {
		if runtime.GOOS == "windows" && rel != strings.ToLower(rel) {
			if _, exists := files[strings.ToLower(rel)]; exists {
				t.Logf("case-sensitive duplicate %s cannot be materialized on Windows", rel)
				continue
			}
		}
		clean := filepath.Clean(filepath.FromSlash(rel))
		if clean == "." {
			t.Fatalf("invalid fixture path: %q", rel)
		}
		if runtime.GOOS == "windows" {
			key := strings.ToLower(clean)
			if prior, exists := folded[key]; exists && prior != clean {
				t.Logf("skipping %s because %s collides on a case-insensitive filesystem", clean, prior)
				continue
			}
			folded[key] = clean
		}
		absolute := filepath.Join(root, clean)
		if err := os.MkdirAll(filepath.Dir(absolute), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(absolute, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func createDirectoryLink(t *testing.T, link, target string) bool {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err == nil {
		return true
	} else if runtime.GOOS == "windows" {
		cmd := exec.Command("cmd", "/c", "mklink", "/J", link, target)
		output, junctionErr := cmd.CombinedOutput()
		if junctionErr == nil {
			return true
		}
		t.Logf("skipping reparse fixture: symlink=%v junction=%v output=%s", err, junctionErr, strings.TrimSpace(string(output)))
		return false
	} else if errors.Is(err, os.ErrPermission) || errors.Is(err, syscall.EPERM) {
		t.Logf("skipping symlink fixture without privileges: %v", err)
		return false
	} else {
		t.Fatalf("create symlink %s -> %s: %v", link, target, err)
	}
	return false
}

func materializeRepositoryFixture(t *testing.T, tc repositoryFixture) (string, bool) {
	t.Helper()
	root := mkdirTemp(t, sanitizeName(tc.Name)+"-")
	writeRepositoryFiles(t, root, tc.Files)
	for link, target := range tc.Symlinks {
		if ok := createDirectoryLink(t, filepath.Join(root, filepath.FromSlash(link)), filepath.Join(root, filepath.FromSlash(target))); !ok {
			return root, false
		}
	}
	return root, true
}

func closeGraph(t *testing.T, g *Graph) {
	t.Helper()
	if g == nil || g.FS == nil {
		return
	}
	if err := g.FS.Close(); err != nil {
		t.Errorf("close filesystem: %v", err)
	}
}

func sortedCopy(values []string) []string {
	out := append([]string(nil), values...)
	sort.Strings(out)
	return out
}

func orderedDescriptors(g *Graph) []string {
	descriptors := make([]string, 0, len(g.Ordered))
	for _, node := range g.Ordered {
		descriptors = append(descriptors, node.Descriptor)
	}
	return descriptors
}

func nodeIDs(g *Graph) []string {
	ids := make([]string, 0, len(g.Nodes))
	for id := range g.Nodes {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

func parentMap(g *Graph) map[string]string {
	parents := make(map[string]string, len(g.Nodes))
	for id, node := range g.Nodes {
		parents[id] = node.Parent
	}
	return parents
}

func boundaryPaths(g *Graph) []string {
	paths := make([]string, 0, len(g.Boundaries))
	for _, boundary := range g.Boundaries {
		paths = append(paths, boundary.Path)
	}
	sort.Strings(paths)
	return paths
}

func boundaryReasons(g *Graph) map[string]string {
	reasons := make(map[string]string, len(g.Boundaries))
	for _, boundary := range g.Boundaries {
		reasons[boundary.Path] = boundary.Reason
	}
	return reasons
}

func expectedChildren(parents map[string]string) map[string][]string {
	children := map[string][]string{}
	for child, parent := range parents {
		if parent == "" {
			continue
		}
		children[parent] = append(children[parent], child)
	}
	for parent := range children {
		sort.Strings(children[parent])
	}
	return children
}

func referenceTargets(node *Node) []string {
	targets := make([]string, 0, len(node.References))
	for _, link := range node.References {
		targets = append(targets, link.Target)
	}
	sort.Strings(targets)
	return targets
}

func requireProblemCode(t *testing.T, got *Problem, want string) {
	t.Helper()
	if got == nil {
		t.Fatalf("wanted %s, got nil", want)
	}
	if got.Code != want {
		t.Fatalf("wanted %s, got %+v", want, got)
	}
}

func nestedMappingYAML(depth int) string {
	var b strings.Builder
	for i := 0; i < depth; i++ {
		b.WriteString(strings.Repeat("  ", i))
		fmt.Fprintf(&b, "level%d:\n", i)
	}
	b.WriteString(strings.Repeat("  ", depth))
	b.WriteString("leaf: value\n")
	return b.String()
}

func wideMappingYAML(keys int) string {
	var b strings.Builder
	for i := 0; i < keys; i++ {
		fmt.Fprintf(&b, "k%04d: v\n", i)
	}
	return b.String()
}

func TestParseRecordFixtures(t *testing.T) {
	var cases []recordFixture
	readFixtureJSON(t, "records.json", &cases)

	for _, tc := range cases {
		t.Run(tc.Name, func(t *testing.T) {
			record, identity, err := Parse([]byte(tc.YAML), tc.Root)
			wantIdentity := fixtureIdentity(t, tc.YAML)
			if identity != wantIdentity {
				t.Fatalf("identity mismatch: got %q want %q", identity, wantIdentity)
			}
			if tc.Valid {
				if err != nil {
					t.Fatalf("unexpected parse error: %+v", err)
				}
				if record.ID != wantIdentity {
					t.Fatalf("record ID mismatch: got %q want %q", record.ID, wantIdentity)
				}
				return
			}
			requireProblemCode(t, err, "SCHEMA_INVALID")
		})
	}
}

func TestParseYAMLRejectionCatalog(t *testing.T) {
	var cases []yamlFixture
	readFixtureJSON(t, "yaml.json", &cases)

	for _, tc := range cases {
		t.Run(tc.Name, func(t *testing.T) {
			_, identity, err := Parse([]byte(tc.YAML), false)
			requireProblemCode(t, err, "YAML_INVALID")
			if identity != "" {
				t.Fatalf("YAML rejection should not expose identity, got %q", identity)
			}
		})
	}
}

func TestParseStrictLimits(t *testing.T) {
	t.Run("descriptor-bytes", func(t *testing.T) {
		raw := []byte(strings.Repeat("a", contract.MaxDescriptorBytes+1))
		_, _, err := Parse(raw, false)
		requireProblemCode(t, err, "DISCOVERY_LIMIT")
	})

	t.Run("yaml-depth", func(t *testing.T) {
		_, _, err := Parse([]byte(nestedMappingYAML(contract.MaxYAMLDepth+4)), false)
		requireProblemCode(t, err, "DISCOVERY_LIMIT")
	})

	t.Run("yaml-nodes", func(t *testing.T) {
		_, _, err := Parse([]byte(wideMappingYAML(contract.MaxYAMLNodes/2+32)), false)
		requireProblemCode(t, err, "DISCOVERY_LIMIT")
	})
}

func TestLoadRepositoryFixtures(t *testing.T) {
	var cases []repositoryFixture
	readFixtureJSON(t, "repositories.json", &cases)

	for _, tc := range cases {
		t.Run(tc.Name, func(t *testing.T) {
			root, ok := materializeRepositoryFixture(t, tc)
			if !ok {
				t.Skip("reparse fixture requires privileges")
			}

			g, err := Load(root)
			if tc.Error != "" {
				requireProblemCode(t, err, tc.Error)
				return
			}
			if err != nil {
				t.Fatalf("unexpected load error: %+v", err)
			}
			t.Cleanup(func() { closeGraph(t, g) })

			if got, want := len(g.Snapshot), 64; got != want {
				t.Fatalf("snapshot length = %d, want %d", got, want)
			}

			g2, err := Load(root)
			if err != nil {
				t.Fatalf("second load failed: %+v", err)
			}
			t.Cleanup(func() { closeGraph(t, g2) })
			if g.Snapshot != g2.Snapshot {
				t.Fatalf("snapshot mismatch: %s != %s", g.Snapshot, g2.Snapshot)
			}

			wantOrdered := sortedCopy(tc.Ordered)
			if got := orderedDescriptors(g); !slices.Equal(got, wantOrdered) {
				t.Fatalf("ordered descriptors = %v, want %v", got, wantOrdered)
			}
			if got := nodeIDs(g); !slices.Equal(got, sortedCopy(nodeIDsFromParents(tc.Parents))) {
				t.Fatalf("node IDs = %v, want %v", got, sortedCopy(nodeIDsFromParents(tc.Parents)))
			}
			if got := parentMap(g); !equalStringMap(got, tc.Parents) {
				t.Fatalf("parents = %#v, want %#v", got, tc.Parents)
			}
			for parent, wantChildren := range expectedChildren(tc.Parents) {
				if got := sortedCopy(g.Nodes[parent].Children); !slices.Equal(got, wantChildren) {
					t.Fatalf("children[%s] = %v, want %v", parent, got, wantChildren)
				}
			}
			for _, id := range nodeIDs(g) {
				wantChildren := expectedChildren(tc.Parents)[id]
				if got := sortedCopy(g.Nodes[id].Children); !slices.Equal(got, wantChildren) {
					t.Fatalf("children[%s] = %v, want %v", id, got, wantChildren)
				}
			}
			if got := boundaryPaths(g); !slices.Equal(got, sortedCopy(tc.Excluded)) {
				t.Fatalf("boundary paths = %v, want %v", got, sortedCopy(tc.Excluded))
			}
			for id, wantTargets := range tc.Resolved {
				node := g.Nodes[id]
				if node == nil {
					t.Fatalf("resolved targets declared for missing node %q", id)
				}
				if got := referenceTargets(node); !slices.Equal(got, sortedCopy(wantTargets)) {
					t.Fatalf("references[%s] = %v, want %v", id, got, sortedCopy(wantTargets))
				}
			}

			if g.Metadata.Discovered != len(wantOrdered) || g.Metadata.Loaded != len(tc.Parents) || g.Metadata.Excluded != len(tc.Excluded) || g.Metadata.Invalid != 0 || !g.Metadata.Complete {
				t.Fatalf("metadata = %+v", g.Metadata)
			}
			if len(g.Invalid) != 0 {
				t.Fatalf("unexpected invalid descriptors: %v", g.Invalid)
			}
			if len(g.Diagnostics) != 0 {
				t.Fatalf("unexpected diagnostics: %+v", g.Diagnostics)
			}

			if tc.Name == "root-exclusion-and-nested-repository" {
				wantReasons := map[string]string{
					"external":  "nested_repository",
					"generated": "excluded",
					"vendor":    "excluded",
				}
				if got := boundaryReasons(g); !equalStringMap(got, wantReasons) {
					t.Fatalf("boundary reasons = %#v, want %#v", got, wantReasons)
				}
			}
		})
	}
}

func nodeIDsFromParents(parents map[string]string) []string {
	ids := make([]string, 0, len(parents))
	for id := range parents {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

func equalStringMap(got, want map[string]string) bool {
	if len(got) != len(want) {
		return false
	}
	for key, wantValue := range want {
		if got[key] != wantValue {
			return false
		}
	}
	return true
}

func TestLoadMissingRoot(t *testing.T) {
	root := mkdirTemp(t, "missing-root-")
	got, err := Load(root)
	if got != nil {
		t.Fatal("expected no graph without root descriptor")
	}
	requireProblemCode(t, err, "ROOT_REQUIRED")
}

func TestLoadDescriptorByteLimit(t *testing.T) {
	root := mkdirTemp(t, "descriptor-limit-")
	huge := "schema_version: \"1\"\nid: app\nname: App\nsummary: \"" + strings.Repeat("a", contract.MaxDescriptorBytes) + "\"\n"
	writeRepositoryFiles(t, root, map[string]string{"archie.yaml": huge})

	got, err := Load(root)
	if got != nil {
		t.Fatal("expected no graph for oversized root descriptor")
	}
	requireProblemCode(t, err, "DISCOVERY_LIMIT")
}

func TestLoadIdentifiableSchemaErrorsAreLocalized(t *testing.T) {
	root := mkdirTemp(t, "localized-schema-")
	writeRepositoryFiles(t, root, map[string]string{
		"archie.yaml":        "schema_version: \"1\"\nid: app\nname: App\nsummary: Root.\n",
		"good.archie.yaml":   "schema_version: \"1\"\nid: good\nname: Good\nsummary: Good.\n",
		"known.archie.yaml":  "schema_version: \"1\"\nid: known\nname: Known\n",
		"notes.txt":          "ignored\n",
		"nested/ignore.yaml": "ignored\n",
	})

	g, err := Load(root)
	if err != nil {
		t.Fatalf("unexpected load error: %+v", err)
	}
	t.Cleanup(func() { closeGraph(t, g) })

	if got := orderedDescriptors(g); !slices.Equal(got, []string{"archie.yaml", "good.archie.yaml"}) {
		t.Fatalf("ordered descriptors = %v", got)
	}
	if got := g.Invalid; !slices.Equal(got, []string{"known.archie.yaml"}) {
		t.Fatalf("invalid descriptors = %v", got)
	}
	if len(g.Diagnostics) != 1 {
		t.Fatalf("diagnostics = %+v", g.Diagnostics)
	}
	if diag := g.Diagnostics[0]; diag.Code != "SCHEMA_INVALID" || diag.Descriptor != "known.archie.yaml" {
		t.Fatalf("diagnostic = %+v", diag)
	}
	if g.Metadata != (contract.MetadataCoverage{Discovered: 3, Loaded: 2, Excluded: 0, Invalid: 1, Complete: true}) {
		t.Fatalf("metadata = %+v", g.Metadata)
	}
	if _, exists := g.Nodes["known"]; exists {
		t.Fatal("invalid identifiable descriptor should not be loaded")
	}
}

func TestLoadUnknownIdentityFailsClosed(t *testing.T) {
	root := mkdirTemp(t, "unknown-identity-")
	writeRepositoryFiles(t, root, map[string]string{
		"archie.yaml":        "schema_version: \"1\"\nid: app\nname: App\nsummary: Root.\n",
		"broken.archie.yaml": "schema_version: \"1\"\nid: bad/id\nname: Broken\nsummary: Broken.\n",
	})

	got, err := Load(root)
	if got != nil {
		t.Fatal("expected no graph when identity is unavailable")
	}
	requireProblemCode(t, err, "SCHEMA_INVALID")
	if err.Descriptor != "broken.archie.yaml" {
		t.Fatalf("descriptor = %q, want broken.archie.yaml", err.Descriptor)
	}
}

func TestLoadDuplicateIdentityWithInvalidRecordIsFatal(t *testing.T) {
	tests := map[string]map[string]string{
		"invalid-after-valid": {
			"archie.yaml":         "schema_version: \"1\"\nid: app\nname: App\nsummary: Root.\n",
			"a-valid.archie.yaml": "schema_version: \"1\"\nid: dup\nname: Valid\nsummary: Valid.\n",
			"b-bad.archie.yaml":   "schema_version: \"1\"\nid: dup\nname: Invalid\n",
		},
		"invalid-before-valid": {
			"archie.yaml":         "schema_version: \"1\"\nid: app\nname: App\nsummary: Root.\n",
			"a-bad.archie.yaml":   "schema_version: \"1\"\nid: dup\nname: Invalid\n",
			"b-valid.archie.yaml": "schema_version: \"1\"\nid: dup\nname: Valid\nsummary: Valid.\n",
		},
	}

	for name, files := range tests {
		t.Run(name, func(t *testing.T) {
			root := mkdirTemp(t, sanitizeName(name)+"-")
			writeRepositoryFiles(t, root, files)

			got, err := Load(root)
			if got != nil {
				t.Fatal("expected no graph for duplicate identity")
			}
			requireProblemCode(t, err, "DUPLICATE_ID")
		})
	}
}

func TestLoadNonRootDiscoveryConfigIsLocalized(t *testing.T) {
	root := mkdirTemp(t, "child-discovery-")
	writeRepositoryFiles(t, root, map[string]string{
		"archie.yaml":         "schema_version: \"1\"\nid: app\nname: App\nsummary: Root.\n",
		"child.archie.yaml":   "schema_version: \"1\"\nid: child\nname: Child\nsummary: Child.\ndiscovery: {}\n",
		"healthy.archie.yaml": "schema_version: \"1\"\nid: healthy\nname: Healthy\nsummary: Healthy.\n",
	})

	g, err := Load(root)
	if err != nil {
		t.Fatalf("unexpected load error: %+v", err)
	}
	t.Cleanup(func() { closeGraph(t, g) })

	if got := g.Invalid; !slices.Equal(got, []string{"child.archie.yaml"}) {
		t.Fatalf("invalid descriptors = %v", got)
	}
	if len(g.Diagnostics) != 1 || g.Diagnostics[0].Code != "SCHEMA_INVALID" || g.Diagnostics[0].Descriptor != "child.archie.yaml" {
		t.Fatalf("diagnostics = %+v", g.Diagnostics)
	}
	if got := orderedDescriptors(g); !slices.Equal(got, []string{"archie.yaml", "healthy.archie.yaml"}) {
		t.Fatalf("ordered descriptors = %v", got)
	}
}

func TestLoadUnresolvedRecordLinks(t *testing.T) {
	t.Run("root-is-fatal", func(t *testing.T) {
		root := mkdirTemp(t, "root-unresolved-")
		writeRepositoryFiles(t, root, map[string]string{
			"archie.yaml": "schema_version: \"1\"\nid: app\nname: App\nsummary: Root.\nlinks: [{relation: depends_on, target: missing, description: Missing.}]\n",
		})

		got, err := Load(root)
		if got != nil {
			t.Fatal("expected no graph when root has unresolved record link")
		}
		requireProblemCode(t, err, "LINK_UNRESOLVED")
	})

	t.Run("child-is-localized", func(t *testing.T) {
		root := mkdirTemp(t, "child-unresolved-")
		writeRepositoryFiles(t, root, map[string]string{
			"archie.yaml":       "schema_version: \"1\"\nid: app\nname: App\nsummary: Root.\n",
			"child.archie.yaml": "schema_version: \"1\"\nid: child\nname: Child\nsummary: Child.\nlinks: [{relation: depends_on, target: missing, description: Missing.}]\n",
			"ready.archie.yaml": "schema_version: \"1\"\nid: ready\nname: Ready\nsummary: Ready.\n",
		})

		g, err := Load(root)
		if err != nil {
			t.Fatalf("unexpected load error: %+v", err)
		}
		t.Cleanup(func() { closeGraph(t, g) })

		if got := g.Invalid; !slices.Equal(got, []string{"child.archie.yaml"}) {
			t.Fatalf("invalid descriptors = %v", got)
		}
		if len(g.Diagnostics) != 1 || g.Diagnostics[0].Code != "LINK_UNRESOLVED" || g.Diagnostics[0].Descriptor != "child.archie.yaml" {
			t.Fatalf("diagnostics = %+v", g.Diagnostics)
		}
		if g.Metadata != (contract.MetadataCoverage{Discovered: 3, Loaded: 2, Excluded: 0, Invalid: 1, Complete: true}) {
			t.Fatalf("metadata = %+v", g.Metadata)
		}
	})
}

func TestLoadMissingFileTargetsAndExcludedLinks(t *testing.T) {
	t.Run("missing-file-target-is-retained", func(t *testing.T) {
		root := mkdirTemp(t, "missing-file-ref-")
		writeRepositoryFiles(t, root, map[string]string{
			"archie.yaml":       "schema_version: \"1\"\nid: app\nname: App\nsummary: Root.\n",
			"child.archie.yaml": "schema_version: \"1\"\nid: child\nname: Child\nsummary: Child.\nlinks: [{relation: source, target: src/missing.go, description: Missing source.}]\n",
		})

		g, err := Load(root)
		if err != nil {
			t.Fatalf("unexpected load error: %+v", err)
		}
		t.Cleanup(func() { closeGraph(t, g) })

		if got := referenceTargets(g.Nodes["child"]); !slices.Equal(got, []string{"src/missing.go"}) {
			t.Fatalf("references = %v", got)
		}
	})

	t.Run("excluded-link-skips-existence-check", func(t *testing.T) {
		root := mkdirTemp(t, "excluded-file-ref-")
		writeRepositoryFiles(t, root, map[string]string{
			"archie.yaml":       "schema_version: \"1\"\nid: app\nname: App\nsummary: Root.\ndiscovery: {exclude: [generated]}\n",
			"child.archie.yaml": "schema_version: \"1\"\nid: child\nname: Child\nsummary: Child.\nlinks: [{relation: source, target: generated/output.go, description: Generated.}]\n",
		})
		if err := os.MkdirAll(filepath.Join(root, "generated"), 0o755); err != nil {
			t.Fatal(err)
		}

		g, err := Load(root)
		if err != nil {
			t.Fatalf("unexpected load error: %+v", err)
		}
		t.Cleanup(func() { closeGraph(t, g) })

		if got := referenceTargets(g.Nodes["child"]); !slices.Equal(got, []string{"generated/output.go"}) {
			t.Fatalf("references = %v", got)
		}
		if got := g.Excluded("generated/output.go"); got != "excluded" {
			t.Fatalf("excluded reason = %q, want excluded", got)
		}
		if got := boundaryPaths(g); !slices.Equal(got, []string{"generated"}) {
			t.Fatalf("boundary paths = %v", got)
		}
	})
}
