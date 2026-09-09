package contract_test

import (
	"encoding/json"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/wbreza/archie/contract"
	"github.com/wbreza/archie/schemas"
	"gopkg.in/yaml.v3"
)

func fixture(t *testing.T, name string, out any) {
	t.Helper()
	data, err := os.ReadFile("../testdata/contract/" + name + ".json")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, out); err != nil {
		t.Fatal(err)
	}
}

// W1 checks that the downstream fixture catalog is internally coherent and
// uses the shared schema. W2/W3 must assert these expectations against runtime.
func TestRepositoryFixtureContracts(t *testing.T) {
	var cases []struct {
		Name    string            `json:"name"`
		Files   map[string]string `json:"files"`
		Parents map[string]string `json:"parents"`
		Ordered []string          `json:"ordered_descriptors"`
		Error   string            `json:"error"`
		Context *struct {
			First []string `json:"first_page"`
			Next  []string `json:"next_page"`
		} `json:"context"`
	}
	fixture(t, "repositories", &cases)
	record, err := schemas.Compile("record")
	if err != nil {
		t.Fatal(err)
	}
	root, err := schemas.Compile("root")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range cases {
		t.Run(tc.Name, func(t *testing.T) {
			if !slices.IsSorted(tc.Ordered) {
				t.Fatal("descriptor expectation must be lexical")
			}
			ids := map[string]bool{}
			duplicate := false
			for _, path := range tc.Ordered {
				text, exists := tc.Files[path]
				if !exists {
					t.Fatalf("expected descriptor absent from fixture: %s", path)
				}
				var doc map[string]any
				if err := yaml.Unmarshal([]byte(text), &doc); err != nil {
					t.Fatal(err)
				}
				s := record
				if path == "archie.yaml" {
					s = root
				}
				if err := s.Validate(doc); err != nil {
					t.Fatal(err)
				}
				id := doc["id"].(string)
				duplicate = duplicate || ids[id]
				ids[id] = true
			}
			if duplicate != (tc.Error == "DUPLICATE_ID") {
				t.Fatal("duplicate fixture expectation is inconsistent")
			}
			for child, parent := range tc.Parents {
				if !ids[child] || parent != "" && !ids[parent] || child == parent {
					t.Fatalf("invalid expected hierarchy edge %s -> %s", child, parent)
				}
			}
			if tc.Context != nil {
				seen := map[string]bool{}
				for _, id := range append(tc.Context.First, tc.Context.Next...) {
					if seen[id] || !ids[id] {
						t.Fatal("context pages must deduplicate known IDs")
					}
					seen[id] = true
				}
			}
		})
	}
}

func TestYAMLRejectionCatalog(t *testing.T) {
	var cases []struct {
		Name   string `json:"name"`
		Reason string `json:"reason"`
		YAML   string `json:"yaml"`
	}
	fixture(t, "yaml", &cases)
	reasons := map[string]bool{}
	names := map[string]bool{}
	for _, tc := range cases {
		if names[tc.Name] || tc.YAML == "" || tc.Reason == "" {
			t.Fatal("empty or duplicate YAML rejection fixture")
		}
		names[tc.Name], reasons[tc.Reason] = true, true
	}
	for _, required := range []string{"duplicate_key", "multiple_documents", "syntax", "anchor", "alias", "merge", "tag", "non_string_key", "directive", "bom"} {
		if !reasons[required] {
			t.Fatalf("missing strict parsing scenario: %s", required)
		}
	}
}

func TestEvidenceFixturesConformToResponseSchema(t *testing.T) {
	var cases []struct {
		Name     string `json:"name"`
		Baseline string `json:"baseline"`
		State    string `json:"state"`
		Reason   string `json:"reason"`
		Relation string `json:"relation"`
	}
	fixture(t, "evidence", &cases)
	s, err := schemas.Compile("response")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range cases {
		t.Run(tc.Name, func(t *testing.T) {
			relation := tc.Relation
			if relation == "" {
				relation = "source"
			}
			baseline := contract.Baseline{State: tc.Baseline, Reason: "not_supplied"}
			switch tc.Baseline {
			case "available":
				baseline.Requested, baseline.Resolved = strings.Repeat("a", 40), strings.Repeat("a", 40)
				baseline.Reason = "resolved"
			case "unavailable":
				baseline.Requested = strings.Repeat("a", 40)
				baseline.Reason = "history_unavailable"
			}
			envelope := contract.Response[contract.QueryData]{
				APIVersion: "1", Command: contract.Context, Status: contract.Partial,
				Data: &contract.QueryData{Records: []contract.RecordView{}, Evidence: []contract.Evidence{
					{RecordID: "a", Relation: contract.Relation(relation), Target: "src/a.go", State: tc.State, Reason: tc.Reason},
				}},
				Diagnostics: []contract.Diagnostic{{Code: "BUDGET_EXHAUSTED", Message: "Fixture."}},
				Coverage:    contract.Coverage{Omissions: []contract.Omission{}}, Baseline: baseline,
			}
			raw, err := json.Marshal(envelope)
			if err != nil {
				t.Fatal(err)
			}
			var value any
			if err := json.Unmarshal(raw, &value); err != nil {
				t.Fatal(err)
			}
			if err := s.Validate(value); err != nil {
				t.Fatal(err)
			}
		})
	}
}
