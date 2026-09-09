package query

import (
	"path"
	"sort"
	"strings"

	"github.com/wbreza/archie/contract"
	"github.com/wbreza/archie/internal/repository"
)

type candidate struct {
	node    *repository.Node
	reasons []string
}

func selectNodes(g *repository.Graph, o Options) ([]candidate, *repository.Problem) {
	for _, id := range o.IDs {
		if g.Nodes[id] == nil {
			return nil, &repository.Problem{Code: "NOT_FOUND", Message: "Requested record ID is not available."}
		}
	}
	result := []candidate{}
	indices := map[string]int{}
	add := func(n *repository.Node, reasons ...string) {
		if index, ok := indices[n.Record.ID]; ok {
			result[index].reasons = unique(append(result[index].reasons, reasons...))
		} else {
			indices[n.Record.ID] = len(result)
			result = append(result, candidate{n, unique(reasons)})
		}
	}
	if o.Command == contract.Get {
		add(g.Nodes[o.IDs[0]], "id")
		return result, nil
	}
	if o.Command == contract.Discover || o.Command == contract.Validate {
		for _, n := range g.Ordered {
			add(n, "discovered")
		}
		return result, nil
	}
	if o.Command == contract.Impact {
		direct := []*repository.Node{}
		for _, n := range g.Ordered {
			matched := false
			for _, p := range o.Paths {
				if repository.Contains(p, n.Descriptor) {
					matched = true
				}
				for _, ref := range n.References {
					if repository.Contains(p, ref.Target) {
						matched = true
					}
				}
			}
			if matched {
				direct = append(direct, n)
				add(n, "impact")
			}
		}
		for _, n := range direct {
			for _, id := range n.Incoming {
				add(g.Nodes[id], "linked")
			}
		}
		sort.Slice(result, func(i, j int) bool { return result[i].node.Record.ID < result[j].node.Record.ID })
		return result, nil
	}
	type seed struct {
		node     *repository.Node
		id, path bool
		score    int
	}
	seeds := []seed{}
	tokens := Tokens(o.Query + " " + strings.Join(o.Keywords, " "))
	for _, n := range g.Ordered {
		s := seed{node: n}
		for _, id := range o.IDs {
			if id == n.Record.ID {
				s.id = true
			}
		}
		for _, p := range o.Paths {
			if repository.Contains(path.Dir(n.Descriptor), p) {
				s.path = true
			}
			for _, ref := range n.References {
				if ref.Target == p {
					s.path = true
				}
			}
		}
		strong := Tokens(n.Record.ID + " " + n.Record.Name + " " + strings.Join(n.Record.Tags, " "))
		weak := Tokens(n.Record.Summary + " " + n.Record.Description)
		for _, t := range tokens {
			if contains(strong, t) {
				s.score += 3
			}
			if contains(weak, t) {
				s.score++
			}
		}
		if s.id || s.path || s.score > 0 {
			seeds = append(seeds, s)
		}
	}
	sort.Slice(seeds, func(i, j int) bool {
		a, b := seeds[i], seeds[j]
		if a.id != b.id {
			return a.id
		}
		if a.path != b.path {
			return a.path
		}
		if a.score != b.score {
			return a.score > b.score
		}
		return a.node.Record.ID < b.node.Record.ID
	})
	for _, s := range seeds {
		ancestors := []*repository.Node{}
		for parent := s.node.Parent; parent != ""; parent = g.Nodes[parent].Parent {
			ancestors = append(ancestors, g.Nodes[parent])
		}
		for i := len(ancestors) - 1; i >= 0; i-- {
			if ancestors[i].Parent == "" {
				add(ancestors[i], "ancestor", "root")
			} else {
				add(ancestors[i], "ancestor")
			}
		}
		reasons := []string{}
		if s.id {
			reasons = append(reasons, "id")
		}
		if s.path {
			reasons = append(reasons, "path")
		}
		if s.score > 0 {
			reasons = append(reasons, "keyword")
		}
		add(s.node, reasons...)
	}
	// Expansion uses only seeds, not ancestors or already-expanded targets.
	linked := []string{}
	for _, s := range seeds {
		linked = append(linked, s.node.Outgoing...)
	}
	for _, id := range unique(linked) {
		add(g.Nodes[id], "linked")
	}
	return result, nil
}

func contains(values []string, v string) bool {
	i := sort.SearchStrings(values, v)
	return i < len(values) && values[i] == v
}

func makeView(g *repository.Graph, c candidate, maxLinks int) (contract.RecordView, int) {
	n := c.node
	record := n.Record
	summaryOnly := true
	for _, reason := range c.reasons {
		if reason != "ancestor" && reason != "root" {
			summaryOnly = false
		}
	}
	if summaryOnly {
		record = contract.RootRecord{Record: contract.Record{SchemaVersion: n.Record.SchemaVersion, ID: n.Record.ID, Name: n.Record.Name, Summary: n.Record.Summary}}
	}
	links := []contract.Link{}
	if n.Parent != "" {
		links = append(links, contract.Link{Relation: contract.Parent, Target: n.Parent, Description: g.Nodes[n.Parent].Record.Summary})
	}
	for _, id := range n.Children {
		links = append(links, contract.Link{Relation: contract.Child, Target: id, Description: g.Nodes[id].Record.Summary})
	}
	explicit := []contract.Link{}
	for _, link := range n.Record.Links {
		if link.Relation == contract.DependsOn || link.Relation == contract.RelatedTo {
			explicit = append(explicit, link)
		}
	}
	explicit = append(explicit, n.References...)
	sort.Slice(explicit, func(i, j int) bool {
		a, b := explicit[i], explicit[j]
		if a.Relation != b.Relation {
			return a.Relation < b.Relation
		}
		if a.Target != b.Target {
			return a.Target < b.Target
		}
		return a.Description < b.Description
	})
	links = append(links, explicit...)
	omitted := 0
	if len(links) > maxLinks {
		omitted = len(links) - maxLinks
		links = links[:maxLinks]
	}
	return contract.RecordView{Record: record, Descriptor: n.Descriptor, Links: links, Reasons: c.reasons, SummaryOnly: summaryOnly}, omitted
}
