package contract_test

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"os"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/wbreza/archie/contract"
	"github.com/wbreza/archie/schemas"
)

func TestResponseTypeRoundTrip(t *testing.T) {
	raw, err := os.ReadFile("../testdata/contract/response.json")
	if err != nil {
		t.Fatal(err)
	}
	var response contract.Response[contract.QueryData]
	if err := json.Unmarshal(raw, &response); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(response)
	if err != nil {
		t.Fatal(err)
	}
	var want, got any
	if err := json.Unmarshal(raw, &want); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(want, got) {
		t.Fatal("transport types lost or added contract fields")
	}
	if response.Coverage.Selection.Matched != response.Coverage.Selection.Returned+response.Coverage.Selection.Omitted {
		t.Fatal("selection coverage arithmetic")
	}
	if response.Coverage.Evidence.Total != response.Coverage.Evidence.Checked+response.Coverage.Evidence.Unchecked {
		t.Fatal("evidence coverage arithmetic")
	}
	cursorJSON, err := base64.RawURLEncoding.DecodeString(response.Continuation.Cursor)
	if err != nil {
		t.Fatal(err)
	}
	var cursor contract.Cursor
	if err := json.Unmarshal(cursorJSON, &cursor); err != nil {
		t.Fatal(err)
	}
	if cursor.Version != "1" || cursor.Offset != 2 || len(cursor.Query) != 64 || len(cursor.Snapshot) != 64 {
		t.Fatalf("invalid illustrative cursor: %+v", cursor)
	}
}

func TestAllResponseDataTypes(t *testing.T) {
	s, err := schemas.Compile("response")
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		command contract.Command
		data    any
	}{
		{contract.Discover, contract.QueryData{Records: []contract.RecordView{}, Evidence: []contract.Evidence{}}},
		{contract.Get, contract.QueryData{Records: []contract.RecordView{{
			Record:     contract.RootRecord{Record: contract.Record{SchemaVersion: "1", ID: "app", Name: "Application", Summary: "Coordinates reports."}},
			Descriptor: "archie.yaml", Links: []contract.Link{}, Reasons: []string{"id"},
		}}, Evidence: []contract.Evidence{}}},
		{contract.Context, contract.QueryData{Records: []contract.RecordView{}, Evidence: []contract.Evidence{}}},
		{contract.Impact, contract.QueryData{Records: []contract.RecordView{}, Evidence: []contract.Evidence{}}},
		{contract.Validate, contract.ValidationData{Valid: true, Evidence: []contract.Evidence{}}},
		{contract.Scaffold, contract.ScaffoldData{Path: "archie.yaml"}},
		{contract.VersionCommand, contract.VersionData{Version: "0.1.0-dev"}},
	}
	for _, tc := range tests {
		t.Run(string(tc.command), func(t *testing.T) {
			envelope := contract.Response[any]{
				APIVersion: contract.Version, Command: tc.command, Status: contract.OK,
				Data: &tc.data, Diagnostics: []contract.Diagnostic{},
				Coverage: contract.Coverage{Omissions: []contract.Omission{}},
				Baseline: contract.Baseline{State: "none", Reason: "not_supplied"},
			}
			if tc.command == contract.Validate {
				envelope.Coverage.References = &contract.ReferenceCoverage{Complete: true}
			}
			data, err := json.Marshal(envelope)
			if err != nil {
				t.Fatal(err)
			}
			var value any
			if err := json.Unmarshal(data, &value); err != nil {
				t.Fatal(err)
			}
			if err := s.Validate(value); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestCommandSpecifications(t *testing.T) {
	for _, command := range []contract.Command{contract.Discover, contract.Get, contract.Context, contract.Impact, contract.Validate, contract.Scaffold, contract.VersionCommand} {
		t.Run(string(command), func(t *testing.T) {
			spec, ok := contract.Specification(command)
			if !ok {
				t.Fatal("missing command")
			}
			seen := map[string]bool{}
			for _, flag := range spec.Allowed {
				if seen[flag] {
					t.Fatalf("duplicate allowed flag %s", flag)
				}
				seen[flag] = true
			}
			for _, flag := range append(spec.Required, spec.Repeated...) {
				if !seen[flag] {
					t.Fatalf("required/repeated flag not allowed: %s", flag)
				}
			}
			spec.Allowed[0] = "mutated"
			fresh, _ := contract.Specification(command)
			if slices.Contains(fresh.Allowed, "mutated") {
				t.Fatal("specification leaked mutable shared storage")
			}
		})
	}
	if _, ok := contract.Specification("not-a-command"); ok {
		t.Fatal("unknown command accepted")
	}
	get, _ := contract.Specification(contract.Get)
	if !reflect.DeepEqual(get.Required, []string{"id"}) || slices.Contains(get.Allowed, "cursor") {
		t.Fatal("get contract drift")
	}
	scaffold, _ := contract.Specification(contract.Scaffold)
	if slices.Contains(scaffold.Allowed, "force") {
		t.Fatal("scaffolding must never overwrite")
	}
}

func TestBudgetsAndExitCodes(t *testing.T) {
	if contract.MaxReferences != 20000 {
		t.Fatal("mandatory reference inspection ceiling changed")
	}
	got := []int{contract.MaxEntries, contract.MaxDescriptors, contract.MaxDescriptorBytes, contract.MaxYAMLNodes, contract.MaxYAMLDepth,
		contract.DefaultMaxRecords, contract.HardMaxRecords, contract.DefaultMaxLinks, contract.HardMaxLinks,
		contract.DefaultMaxBytes, contract.MinMaxBytes, contract.HardMaxBytes, contract.DefaultMaxEvidence, contract.HardMaxEvidence,
		contract.DefaultMaxEvidenceBytes, contract.HardMaxEvidenceBytes}
	want := []int{20000, 2000, 65536, 4096, 32, 10, 100, 32, 256, 32768, 4096, 1048576, 16, 256, 1048576, 16777216}
	if !reflect.DeepEqual(want, got) {
		t.Fatal("budget contract changed without fixture update")
	}
	if !reflect.DeepEqual([]int{contract.ExitOK, contract.ExitInvalid, contract.ExitUsage, contract.ExitOperational, contract.ExitPartial}, []int{0, 1, 2, 3, 4}) {
		t.Fatal("exit code drift")
	}
}

func TestQueryIdentityEncoding(t *testing.T) {
	identity := contract.QueryIdentity{
		Baseline: "", Command: contract.Context, IDs: []string{"renderer"},
		Limits: contract.PageLimits{EvidenceBytes: 1048576, EvidenceTargets: 16, Links: 32, OutputBytes: 32768, Records: 10},
		Paths:  []string{}, Tokens: []string{"render"},
	}
	var out bytes.Buffer
	enc := json.NewEncoder(&out)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(identity); err != nil {
		t.Fatal(err)
	}
	want := `{"baseline":"","command":"context","ids":["renderer"],"limits":{"evidence_bytes":1048576,"evidence_targets":16,"links":32,"output_bytes":32768,"records":10},"paths":[],"tokens":["render"]}`
	if strings.TrimSuffix(out.String(), "\n") != want {
		t.Fatalf("canonical query preimage changed: %s", out.String())
	}
}
