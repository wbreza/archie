package schemas_test

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v5"
	"github.com/wbreza/archie/schemas"
	"gopkg.in/yaml.v3"
)

func compile(t *testing.T, name string) *jsonschema.Schema {
	t.Helper()
	s, err := schemas.Compile(name)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func readJSON(t *testing.T, file string, target any) {
	t.Helper()
	data, err := os.ReadFile("../testdata/contract/" + file)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, target); err != nil {
		t.Fatal(err)
	}
}

func TestRecordFixtures(t *testing.T) {
	var cases []struct {
		Name  string `json:"name"`
		Root  bool   `json:"root"`
		Valid bool   `json:"valid"`
		YAML  string `json:"yaml"`
	}
	readJSON(t, "records.json", &cases)
	record, root := compile(t, "record"), compile(t, "root")
	for _, tc := range cases {
		t.Run(tc.Name, func(t *testing.T) {
			var doc any
			if err := yaml.Unmarshal([]byte(tc.YAML), &doc); err != nil {
				t.Fatal("schema fixture must be syntactically valid YAML:", err)
			}
			s := record
			if tc.Root {
				s = root
			}
			if err := s.Validate(doc); (err == nil) != tc.Valid {
				t.Fatalf("valid=%v, error=%v", tc.Valid, err)
			}
		})
	}
}

func TestAllAuthoredRelationsAndUnsafeFileSyntax(t *testing.T) {
	s := compile(t, "record")
	for _, relation := range []string{"depends_on", "related_to", "source", "test", "doc", "adr"} {
		t.Run(relation, func(t *testing.T) {
			doc := map[string]any{
				"schema_version": "1", "id": "a", "name": "A", "summary": "A.",
				"links": []any{map[string]any{"relation": relation, "target": "b", "description": "B."}},
			}
			if err := s.Validate(doc); err != nil {
				t.Fatal(err)
			}
		})
	}
	for _, target := range []string{`C:\src\a.go`, `\\host\share\a.go`, "a.go:stream", "a.go#L3", "src//a.go", "src/[ab].go", "src/a\n.go"} {
		t.Run(target, func(t *testing.T) {
			doc := map[string]any{
				"schema_version": "1", "id": "a", "name": "A", "summary": "A.",
				"links": []any{map[string]any{"relation": "source", "target": target, "description": "A."}},
			}
			if err := s.Validate(doc); err == nil {
				t.Fatal("unsafe literal file syntax accepted")
			}
		})
	}
}

func response(t *testing.T) map[string]any {
	t.Helper()
	var doc map[string]any
	readJSON(t, "response.json", &doc)
	return doc
}

func TestResponseFixture(t *testing.T) {
	if err := compile(t, "response").Validate(response(t)); err != nil {
		t.Fatal(err)
	}
}

func TestResponseRejectsContractDrift(t *testing.T) {
	tests := map[string]func(map[string]any){
		"numeric-api":  func(v map[string]any) { v["api_version"] = 1 },
		"unknown-root": func(v map[string]any) { v["secret_extra"] = "x" },
		"null-array":   func(v map[string]any) { v["diagnostics"] = nil },
		"unknown-code": func(v map[string]any) {
			v["diagnostics"].([]any)[0].(map[string]any)["code"] = "FUTURE"
		},
		"negative-count": func(v map[string]any) {
			v["coverage"].(map[string]any)["evidence"].(map[string]any)["checked"] = -1
		},
		"unknown-coverage": func(v map[string]any) {
			v["coverage"].(map[string]any)["cache_hit"] = true
		},
		"false-baseline": func(v map[string]any) {
			v["baseline"].(map[string]any)["state"] = "available"
		},
		"no-diagnostic-partial":    func(v map[string]any) { v["diagnostics"] = []any{} },
		"data-on-error":            func(v map[string]any) { v["status"] = "error" },
		"cursor-on-ok":             func(v map[string]any) { v["status"] = "ok" },
		"cursor-on-get":            func(v map[string]any) { v["command"] = "get" },
		"multi-record-partial-get": func(v map[string]any) { v["command"], v["continuation"] = "get", nil },
		"wrong-command-data":       func(v map[string]any) { v["command"] = "version"; v["continuation"] = nil },
		"unknown-command-success":  func(v map[string]any) { v["command"] = "unknown"; v["continuation"] = nil },
		"child-root-config": func(v map[string]any) {
			views := v["data"].(map[string]any)["records"].([]any)
			views[1].(map[string]any)["record"].(map[string]any)["discovery"] = map[string]any{}
		},
		"authored-derived-relation": func(v map[string]any) {
			views := v["data"].(map[string]any)["records"].([]any)
			record := views[1].(map[string]any)["record"].(map[string]any)
			record["links"].([]any)[0].(map[string]any)["relation"] = "parent"
		},
		"unknown-read-relation": func(v map[string]any) {
			view := v["data"].(map[string]any)["records"].([]any)[0].(map[string]any)
			view["links"].([]any)[0].(map[string]any)["relation"] = "execute"
		},
		"reference-id-link": func(v map[string]any) {
			view := v["data"].(map[string]any)["records"].([]any)[1].(map[string]any)
			view["links"].([]any)[1].(map[string]any)["relation"] = "depends_on"
		},
		"false-evidence-reason": func(v map[string]any) {
			evidence := v["data"].(map[string]any)["evidence"].([]any)[0].(map[string]any)
			evidence["state"] = "unchanged"
		},
		"empty-successful-get": func(v map[string]any) {
			v["command"], v["continuation"], v["status"] = "get", nil, "ok"
			v["data"].(map[string]any)["records"] = []any{}
		},
		"false-valid-on-ok": func(v map[string]any) {
			v["command"], v["status"], v["continuation"] = "validate", "ok", nil
			v["data"] = map[string]any{"valid": false, "evidence": []any{}}
		},
		"true-valid-on-partial": func(v map[string]any) {
			v["command"], v["continuation"] = "validate", nil
			v["data"] = map[string]any{"valid": true, "evidence": []any{}}
		},
		"uniform-link-extra": func(v map[string]any) {
			view := v["data"].(map[string]any)["records"].([]any)[0].(map[string]any)
			view["links"].([]any)[0].(map[string]any)["children"] = []any{}
		},
		"noncanonical-descriptor": func(v map[string]any) {
			view := v["data"].(map[string]any)["records"].([]any)[0].(map[string]any)
			view["descriptor"] = "../archie.yaml"
		},
		"noncanonical-reference": func(v map[string]any) {
			view := v["data"].(map[string]any)["records"].([]any)[1].(map[string]any)
			view["links"].([]any)[1].(map[string]any)["target"] = "src/../render.go"
		},
		"noncanonical-evidence": func(v map[string]any) {
			v["data"].(map[string]any)["evidence"].([]any)[0].(map[string]any)["target"] = "./src/render.go"
		},
		"noncanonical-scaffold": func(v map[string]any) {
			v["command"], v["status"], v["continuation"] = "scaffold", "ok", nil
			v["data"] = map[string]any{"path": "a/../archie.yaml"}
		},
	}
	s := compile(t, "response")
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			doc := response(t)
			mutate(doc)
			if err := s.Validate(doc); err == nil {
				t.Fatal("accepted invalid response")
			}
		})
	}
}

func TestUnknownSchemaIsRejected(t *testing.T) {
	for _, name := range []string{"missing", "../record", "https://example.org"} {
		_, err := schemas.Compile(name)
		if err == nil || !strings.Contains(err.Error(), "unknown schema") {
			t.Fatalf("unexpected result: %v", err)
		}
	}
}

func TestCanonicalRootExclusions(t *testing.T) {
	s := compile(t, "root")
	for _, path := range []string{".", "..", "../src", "src/../out", "src/.", "src. /a", "src./a"} {
		t.Run(path, func(t *testing.T) {
			doc := map[string]any{
				"schema_version": "1", "id": "app", "name": "App", "summary": "App.",
				"discovery": map[string]any{"exclude": []any{path}},
			}
			if err := s.Validate(doc); err == nil {
				t.Fatal("noncanonical exclusion accepted")
			}
		})
	}
}
