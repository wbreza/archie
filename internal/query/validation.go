package query

import (
	"sort"

	"github.com/wbreza/archie/contract"
	"github.com/wbreza/archie/internal/repository"
)

func validateReferences(g *repository.Graph, limit int) Response {
	r := empty(contract.Validate)
	r.Coverage.Metadata = g.Metadata
	coverage := r.Coverage.References
	origins := map[string]string{}
	for _, n := range g.Ordered {
		for _, ref := range n.References {
			if _, ok := origins[ref.Target]; !ok {
				origins[ref.Target] = n.Descriptor
			}
		}
	}
	targets := make([]string, 0, len(origins))
	for target := range origins {
		targets = append(targets, target)
	}
	sort.Strings(targets)
	coverage.Total = len(targets)
	fatal := false
	for _, target := range targets {
		d := contract.Diagnostic{Descriptor: origins[target], Target: target}
		boundary := g.Excluded(target)
		if boundary != "" && boundary != "symlink" {
			d.Code, d.Message = "REFERENCE_UNCHECKED", "Referenced file is outside discovery scope; it was not inspected."
			addDetailedDiagnostic(&r, d)
			addOmission(&r, "reference_excluded", 1)
			continue
		}
		if coverage.Checked >= limit {
			d.Code, d.Message = "BUDGET_EXHAUSTED", "Reference inspection limit reached; target was not inspected."
			addDetailedDiagnostic(&r, d)
			addOmission(&r, "reference_budget", 1)
			continue
		}
		coverage.Checked++
		info, err := g.FS.InspectReference(target)
		switch {
		case err != nil:
			coverage.Invalid++
			d.Code, d.Message = err.Code, err.Message
		case info == nil:
			coverage.Missing++
			d.Code, d.Message = "EVIDENCE_MISSING", "Referenced file does not exist."
		case !info.Mode().IsRegular():
			coverage.Invalid++
			d.Code, d.Message = "PATH_UNSAFE", "Reference must name a regular file."
		default:
			continue
		}
		fatal = true
		addDetailedDiagnostic(&r, d)
	}
	coverage.Unchecked = coverage.Total - coverage.Checked
	coverage.Complete = g.Metadata.Complete && coverage.Unchecked == 0 && r.Coverage.DiagnosticCounts["IO_ERROR"] == 0
	data(&r, contract.ValidationData{Valid: r.Status == contract.OK, Evidence: []contract.Evidence{}})
	if fatal {
		r.Status, r.Data = contract.Error, nil
	}
	return r
}

func finishValidation(g *repository.Graph, r Response, maxBytes int) ([]byte, int) {
	if err := g.FS.Verify(); err != nil {
		addDetailedDiagnostic(&r, contract.Diagnostic{Code: err.Code, Message: err.Message})
		r.Status, r.Data = contract.Error, nil
		r.Coverage.References.Complete = false
	}
	return bounded(r, maxBytes)
}
