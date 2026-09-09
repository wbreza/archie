package query

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"io"
	"sort"
	"strings"

	"github.com/wbreza/archie/contract"
	"github.com/wbreza/archie/internal/repository"
)

type Response = contract.Response[any]

// BuildVersion may be set at build time using go build -ldflags -X.
var BuildVersion = "0.1.0-dev"

func empty(command contract.Command) Response {
	return Response{APIVersion: contract.Version, Command: command, Status: contract.OK,
		Diagnostics: []contract.Diagnostic{}, Coverage: contract.Coverage{Omissions: []contract.Omission{}},
		Baseline: contract.Baseline{State: "none", Reason: "not_supplied"}}
}

func Failure(command contract.Command, p *repository.Problem) ([]byte, int) {
	r := empty(command)
	r.Status = contract.Error
	r.Diagnostics = []contract.Diagnostic{{Code: p.Code, Message: p.Message, Descriptor: p.Descriptor}}
	return encode(r), exit(r)
}

func exit(r Response) int {
	if r.Status == contract.Partial {
		return contract.ExitPartial
	}
	if r.Status == contract.OK {
		return contract.ExitOK
	}
	code := contract.ExitInvalid
	for _, d := range r.Diagnostics {
		if d.Code == "USAGE" || strings.HasPrefix(d.Code, "CURSOR_") {
			return contract.ExitUsage
		}
		if d.Code == "IO_ERROR" || d.Code == "INTERNAL" || d.Code == "ALREADY_EXISTS" {
			code = contract.ExitOperational
		}
	}
	return code
}

func data(r *Response, value any) { r.Data = &value }

// Run performs a fresh invocation. Only explicit scaffold creates a file.
// All graph state dies with the call.
func Run(o Options) ([]byte, int) {
	if o.Command == contract.VersionCommand {
		r := empty(o.Command)
		data(&r, contract.VersionData{Version: BuildVersion})
		return encode(r), 0
	}
	if o.Command == contract.Scaffold {
		if len(o.IDs) != 1 {
			return Failure(o.Command, &repository.Problem{Code: "USAGE", Message: "Scaffold requires one ID."})
		}
		p, err := repository.Scaffold(o.Root, o.File, contract.Record{
			SchemaVersion: contract.Version, ID: o.IDs[0], Name: o.Name, Summary: o.Summary,
		})
		if err != nil {
			return Failure(o.Command, err)
		}
		r := empty(o.Command)
		data(&r, contract.ScaffoldData{Path: p})
		return encode(r), 0
	}
	g, err := repository.Load(o.Root)
	if err != nil {
		return Failure(o.Command, err)
	}
	defer g.FS.Close()
	for i, p := range o.Paths {
		c, e := repository.Canonical(".", p, false)
		if e != nil {
			return Failure(o.Command, e)
		}
		if _, e := g.FS.Inspect(c); e != nil {
			return Failure(o.Command, e)
		}
		o.Paths[i] = c
	}
	candidates, err := selectNodes(g, o)
	if err != nil {
		return Failure(o.Command, err)
	}
	checker := newChecker(g, o)
	if o.Command == contract.Validate {
		if len(g.Diagnostics) != 0 {
			r := empty(o.Command)
			r.Coverage.Metadata, r.Baseline = g.Metadata, checker.baseline
			for _, d := range g.Diagnostics {
				addDiagnostic(&r, d.Code, d.Message)
			}
			r.Status = contract.Error
			addOmission(&r, "invalid", len(g.Invalid))
			return bounded(r, o.Limits.OutputBytes)
		}
		return validate(g, o, candidates, checker)
	}
	if o.Command == contract.Get {
		return get(g, o, candidates, checker)
	}
	query := queryHash(o)
	offset := 0
	if o.Cursor != "" {
		offset, err = cursorOffset(o.Cursor, query, g.Snapshot, len(candidates))
		if err != nil {
			return Failure(o.Command, err)
		}
	}
	limit := o.Limits.Records
	page := []candidate{}
	byteOmitted := 0
	next := -1
	placeholder := func(p string) contract.Evidence {
		return contract.Evidence{Target: p, State: "unknown", Reason: "baseline_unavailable"}
	}
	estimate := func(selected []candidate, nextIndex, remaining int) int {
		var cont *contract.Continuation
		if nextIndex >= 0 {
			cont = continuation(query, g.Snapshot, nextIndex, remaining)
		}
		projected := evidenceFor(selected, placeholder)
		r := assemble(g, o, candidates, selected, projected, checker.baseline, byteOmitted, cont)
		if o.Evidence && len(projected) > 0 {
			addDiagnostic(&r, "EVIDENCE_MISSING", "Selected file evidence is missing.")
			addDiagnostic(&r, "EVIDENCE_UNKNOWN", "Selected evidence could not be compared reliably.")
			addOmission(&r, "evidence_budget", contract.HardMaxEvidence)
		}
		return len(encode(r))
	}
	// Fit includes the next page's cursor, not just a final-page envelope.
	// Removing a record adds aggregate omission bytes, so converge before
	// advertising eligibility; removed records never re-enter the sequence.
	excluded := map[int]bool{}
	eligible := []int{}
	for {
		kept := []int{}
		changed := false
		after := -1
		for i := len(candidates) - 1; i >= 0; i-- {
			if excluded[i] {
				continue
			}
			if estimate([]candidate{candidates[i]}, after, len(kept)) > o.Limits.OutputBytes {
				excluded[i], changed = true, true
				byteOmitted++
			} else {
				kept = append(kept, i)
				after = i
			}
		}
		if !changed {
			for i := len(kept) - 1; i >= 0; i-- {
				if kept[i] >= offset {
					eligible = append(eligible, kept[i])
				}
			}
			break
		}
	}
	remaining := 0
	for j, i := range eligible {
		c := candidates[i]
		after := -1
		if j+1 < len(eligible) {
			after = eligible[j+1]
		}
		if len(page) >= limit || estimate(append(append([]candidate{}, page...), c), after, len(eligible)-j-1) > o.Limits.OutputBytes {
			next = i
			remaining = len(eligible) - j
			break
		}
		page = append(page, c)
	}
	var cont *contract.Continuation
	if next >= 0 {
		cont = continuation(query, g.Snapshot, next, remaining)
	}
	evidence := evidenceFor(page, func(p string) contract.Evidence { return checker.check(p, o.Evidence) })
	r := assemble(g, o, candidates, page, evidence, checker.baseline, byteOmitted, cont)
	return bounded(r, o.Limits.OutputBytes)
}

func assemble(g *repository.Graph, o Options, all, page []candidate, evidence []contract.Evidence, baseline contract.Baseline, oversized int, cont *contract.Continuation) Response {
	r := empty(o.Command)
	r.Baseline, r.Coverage.Metadata, r.Continuation = baseline, g.Metadata, cont
	r.Coverage.Selection = contract.SelectionCoverage{Matched: len(all), Returned: len(page), Omitted: len(all) - len(page)}
	views := []contract.RecordView{}
	for _, c := range page {
		v, omitted := makeView(g, c, o.Limits.Links)
		views = append(views, v)
		if omitted > 0 {
			addOmission(&r, "link_budget", omitted)
			addDiagnostic(&r, "BUDGET_EXHAUSTED", "A response budget was exhausted.")
		}
	}
	for _, d := range g.Diagnostics {
		addDiagnostic(&r, d.Code, d.Message)
	}
	addOmission(&r, "invalid", len(g.Invalid))
	for _, b := range g.Boundaries {
		addOmission(&r, b.Reason, 1)
	}
	addOmission(&r, "not_selected", len(g.Nodes)-len(all))
	if oversized > 0 {
		addOmission(&r, "byte_budget", oversized)
		addDiagnostic(&r, "BUDGET_EXHAUSTED", "A response budget was exhausted.")
	}
	if len(all)-len(page)-oversized > 0 && o.Command != contract.Validate {
		addOmission(&r, "record_budget", len(all)-len(page)-oversized)
		addDiagnostic(&r, "BUDGET_EXHAUSTED", "A response budget was exhausted.")
	}

	if baseline.State == "unavailable" {
		addDiagnostic(&r, "BASELINE_UNAVAILABLE", "Requested commit baseline is unavailable; comparison is unknown.")
	}
	targets := map[string]bool{}
	for _, e := range evidence {
		if !targets[e.Target] {
			targets[e.Target] = true
			r.Coverage.Evidence.Total++
			if e.State == "unchecked" {
				r.Coverage.Evidence.Unchecked++
			} else {
				r.Coverage.Evidence.Checked++
			}
			if e.Reason == "budget" {
				addOmission(&r, "evidence_budget", 1)
				addDiagnostic(&r, "BUDGET_EXHAUSTED", "A response budget was exhausted.")
			}
		}
		if e.State == "missing" {
			addDiagnostic(&r, "EVIDENCE_MISSING", "Selected file evidence is missing.")
		}
		if e.State == "unknown" && (e.Reason == "read_error" || e.Reason == "unattributable" || e.Reason == "not_regular") {
			addDiagnostic(&r, "EVIDENCE_UNKNOWN", "Selected evidence could not be compared reliably.")
		}
	}
	data(&r, contract.QueryData{Records: views, Evidence: evidence})
	return r
}

func get(g *repository.Graph, o Options, candidates []candidate, checker *evidenceChecker) ([]byte, int) {
	// Exact get evaluates its one selected record, not hypothetical diagnostics.
	r := assemble(g, o, candidates, candidates, []contract.Evidence{}, checker.baseline, 0, nil)
	if len(encode(r)) <= o.Limits.OutputBytes {
		evidence := evidenceFor(candidates, func(p string) contract.Evidence { return checker.check(p, o.Evidence) })
		r = assemble(g, o, candidates, candidates, evidence, checker.baseline, 0, nil)
		if len(encode(r)) <= o.Limits.OutputBytes {
			return bounded(r, o.Limits.OutputBytes)
		}
	}
	r = assemble(g, o, candidates, nil, []contract.Evidence{}, checker.baseline, 1, nil)
	return bounded(r, o.Limits.OutputBytes)
}

func addDiagnostic(r *Response, code, message string) {
	r.Status = contract.Partial
	if r.Coverage.DiagnosticCounts == nil {
		r.Coverage.DiagnosticCounts = map[string]int{}
	}
	r.Coverage.DiagnosticCounts[code]++
	for _, d := range r.Diagnostics {
		if d.Code == code {
			return
		}
	}
	if len(r.Diagnostics) < 16 {
		r.Diagnostics = append(r.Diagnostics, contract.Diagnostic{Code: code, Message: message})
	}
}

func addOmission(r *Response, reason string, count int) {
	if count <= 0 {
		return
	}
	for i := range r.Coverage.Omissions {
		if r.Coverage.Omissions[i].Reason == reason {
			r.Coverage.Omissions[i].Count += count
			return
		}
	}
	r.Coverage.Omissions = append(r.Coverage.Omissions, contract.Omission{Reason: reason, Count: count, Paths: []string{}})
}

func bounded(r Response, max int) ([]byte, int) {
	sort.Slice(r.Coverage.Omissions, func(i, j int) bool { return r.Coverage.Omissions[i].Reason < r.Coverage.Omissions[j].Reason })
	b := encode(r)
	if len(b) > max {
		// Defensive fail closed: never emit an over-budget or truncated envelope.
		return Failure(r.Command, &repository.Problem{Code: "INTERNAL", Message: "Response could not be represented within its byte budget."})
	}
	return b, exit(r)
}

func continuation(query, snapshot string, offset, remaining int) *contract.Continuation {
	payload := bytes.TrimSuffix(encode(contract.Cursor{Version: contract.Version, Query: query, Snapshot: snapshot, Offset: offset}), []byte("\n"))
	return &contract.Continuation{Cursor: base64.RawURLEncoding.EncodeToString(payload), Remaining: remaining}
}

func cursorOffset(token, query, snapshot string, count int) (int, *repository.Problem) {
	invalid := &repository.Problem{Code: "CURSOR_INVALID", Message: "Cursor does not match this query."}
	if len(token) > 2048 {
		return 0, invalid
	}
	b, err := base64.RawURLEncoding.Strict().DecodeString(token)
	if err != nil {
		return 0, invalid
	}
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	var cursor contract.Cursor
	if d.Decode(&cursor) != nil || cursor.Version != contract.Version || cursor.Query != query || cursor.Offset <= 0 {
		return 0, invalid
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		return 0, invalid
	}
	// Require the unique wire representation too, rejecting duplicate keys,
	// missing fields, alternate numbers, whitespace and field reordering.
	canonical := bytes.TrimSuffix(encode(cursor), []byte("\n"))
	if !bytes.Equal(b, canonical) {
		return 0, invalid
	}
	if cursor.Snapshot != snapshot {
		return 0, &repository.Problem{Code: "CURSOR_STALE", Message: "Metadata changed; request a fresh first page."}
	}
	if cursor.Offset >= count {
		return 0, invalid
	}
	return cursor.Offset, nil
}

func validate(g *repository.Graph, o Options, candidates []candidate, checker *evidenceChecker) ([]byte, int) {
	tasks := []contract.Evidence{}
	if o.Evidence {
		tasks = evidenceFor(candidates, func(p string) contract.Evidence {
			return contract.Evidence{Target: p, State: "unknown", Reason: "baseline_unavailable"}
		})
	}
	evidence := []contract.Evidence{}
	size := 2048 // envelope, bounded code diagnostics and aggregate coverage
	for _, task := range tasks {
		n := len(encode(task)) + 1
		if size+n > o.Limits.OutputBytes {
			break
		}
		size += n
		e := checker.check(task.Target, true)
		e.RecordID, e.Relation = task.RecordID, task.Relation
		evidence = append(evidence, e)
	}
	r := assemble(g, o, candidates, nil, evidence, checker.baseline, 0, nil)
	r.Coverage.Selection = contract.SelectionCoverage{}
	if len(evidence) < len(tasks) {
		addOmission(&r, "byte_budget", len(tasks)-len(evidence))
		addDiagnostic(&r, "BUDGET_EXHAUSTED", "Validation evidence output reached its byte budget.")
		unique := map[string]bool{}
		for _, task := range tasks {
			unique[task.Target] = true
		}
		r.Coverage.Evidence.Total = len(unique)
		r.Coverage.Evidence.Unchecked = len(unique) - r.Coverage.Evidence.Checked
	}
	data(&r, contract.ValidationData{Valid: r.Status == contract.OK, Evidence: evidence})
	for _, e := range evidence {
		if e.State == "missing" {
			r.Status, r.Data = contract.Error, nil
			break
		}
	}
	return bounded(r, o.Limits.OutputBytes)
}
