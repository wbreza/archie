package query

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/wbreza/archie/contract"
	"github.com/wbreza/archie/internal/repository"
)

type cappedOutput struct {
	bytes.Buffer
	limit int
}

func (b *cappedOutput) Write(p []byte) (int, error) {
	if len(p) > b.limit-b.Len() {
		return 0, errors.New("output limit")
	}
	return b.Buffer.Write(p)
}

// Git is used only as an offline object reader, with bounded output, no
// replacement objects, no lazy fetch, and no inherited repository overrides.
func gitRead(root string, max int, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	all := []string{"--no-optional-locks", "-c", "protocol.allow=never", "-c", "core.fsmonitor=false"}
	cmd := exec.CommandContext(ctx, "git", append(all, args...)...)
	cmd.Dir = root
	for _, e := range os.Environ() {
		key := strings.ToUpper(strings.SplitN(e, "=", 2)[0])
		if !strings.HasPrefix(key, "GIT_") {
			cmd.Env = append(cmd.Env, e)
		}
	}
	cmd.Env = append(cmd.Env, "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL="+os.DevNull,
		"GIT_NO_REPLACE_OBJECTS=1", "GIT_OPTIONAL_LOCKS=0", "GIT_TERMINAL_PROMPT=0", "GIT_NO_LAZY_FETCH=1")
	output := &cappedOutput{limit: max}
	cmd.Stdout = output
	if err := cmd.Run(); err != nil {
		return nil, err
	}
	return output.Bytes(), nil
}

type evidenceChecker struct {
	graph              *repository.Graph
	baseline           contract.Baseline
	prefix             string
	targets, remaining int
	cache              map[string]contract.Evidence
	beforeRead         func(string)
}

func newChecker(g *repository.Graph, o Options) *evidenceChecker {
	c := &evidenceChecker{graph: g, targets: o.Limits.EvidenceTargets, remaining: o.Limits.EvidenceBytes,
		cache: map[string]contract.Evidence{}, baseline: contract.Baseline{State: "none", Reason: "not_supplied"}}
	if o.Baseline == "" {
		return c
	}
	c.baseline = contract.Baseline{Requested: o.Baseline, State: "unavailable", Reason: "not_git"}
	prefix, err := gitRead(g.FS.Path, 8192, "rev-parse", "--show-prefix")
	if err != nil {
		return c
	}
	c.prefix = strings.TrimSuffix(string(prefix), "\n")
	c.baseline.Reason = "not_found"
	kind, err := gitRead(g.FS.Path, 128, "cat-file", "-t", o.Baseline)
	if err != nil {
		shallow, shallowErr := gitRead(g.FS.Path, 128, "rev-parse", "--is-shallow-repository")
		if shallowErr == nil && strings.TrimSpace(string(shallow)) == "true" {
			c.baseline.Reason = "history_unavailable"
		}
		return c
	}
	if strings.TrimSpace(string(kind)) != "commit" {
		c.baseline.Reason = "not_commit"
		return c
	}
	c.baseline.State, c.baseline.Reason, c.baseline.Resolved = "available", "resolved", o.Baseline
	return c
}

func (c *evidenceChecker) check(target string, requested bool) contract.Evidence {
	if cached, ok := c.cache[target]; ok {
		return cached
	}
	result := contract.Evidence{Target: target, State: "unchecked", Reason: "not_requested"}
	defer func() { c.cache[target] = result }()
	if !requested || c.graph.Excluded(target) != "" {
		return result
	}
	result.Reason = "budget"
	if c.targets == 0 {
		return result
	}
	info, e := c.graph.FS.Inspect(target)
	if e != nil {
		result.State, result.Reason = "unknown", "read_error"
		c.targets--
		return result
	}
	if info == nil {
		result.State, result.Reason = "missing", "missing"
		c.targets--
		return result
	}
	if !info.Mode().IsRegular() {
		result.State, result.Reason = "unknown", "not_regular"
		c.targets--
		return result
	}
	if c.remaining == 0 || info.Size() > int64(c.remaining) {
		return result
	}
	if c.baseline.State != "available" {
		result.State, result.Reason = "unknown", "no_baseline"
		if c.baseline.State == "unavailable" {
			result.Reason = "baseline_unavailable"
		}
		c.targets--
		return result
	}
	// ls-tree supplies type/mode and object ID without reading source bytes.
	name := c.prefix + target
	tree, err := gitRead(c.graph.FS.Path, 8192, "ls-tree", "--full-tree", "-z", c.baseline.Resolved, "--", ":(literal)"+name)
	if err != nil {
		result.State, result.Reason = "unknown", "read_error"
		c.targets--
		return result
	}
	if len(tree) == 0 {
		result.State, result.Reason = "unknown", "not_in_baseline"
		c.targets--
		return result
	}
	header := strings.SplitN(string(tree), "\t", 2)
	fields := strings.Fields(header[0])
	if len(fields) != 3 || fields[1] != "blob" || (fields[0] != "100644" && fields[0] != "100755") {
		result.State, result.Reason = "unknown", "not_regular"
		c.targets--
		return result
	}
	sizeBytes, err := gitRead(c.graph.FS.Path, 128, "cat-file", "-s", fields[2])
	size, parseErr := strconv.ParseInt(strings.TrimSpace(string(sizeBytes)), 10, 64)
	if err != nil || parseErr != nil || size < 0 {
		result.State, result.Reason = "unknown", "read_error"
		c.targets--
		return result
	}
	if size > int64(c.remaining)-info.Size() {
		return result
	}
	c.targets--
	// Failed or racing reads consume their reserved workload too.
	c.remaining -= int(info.Size() + size)
	if c.beforeRead != nil {
		c.beforeRead(target)
	}
	current, e := c.graph.FS.Read(target, info.Size())
	after, inspectErr := c.graph.FS.Inspect(target)
	if e != nil || inspectErr != nil || after == nil || !os.SameFile(info, after) ||
		info.Size() != after.Size() || !info.ModTime().Equal(after.ModTime()) {
		result.State, result.Reason = "unknown", "unattributable"
		return result
	}
	before, err := gitRead(c.graph.FS.Path, int(size)+1, "cat-file", "blob", fields[2])
	if err != nil || int64(len(before)) != size {
		result.State, result.Reason = "unknown", "read_error"
		return result
	}
	result.State, result.Reason = "changed", "bytes_differ"
	if bytes.Equal(current, before) {
		result.State, result.Reason = "unchanged", "bytes_equal"
	}
	return result
}

func evidenceFor(candidates []candidate, check func(string) contract.Evidence) []contract.Evidence {
	result := []contract.Evidence{}
	seen := map[string]bool{}
	for _, c := range candidates {
		for _, ref := range c.node.References {
			key := c.node.Record.ID + "\x00" + string(ref.Relation) + "\x00" + ref.Target
			if seen[key] {
				continue
			}
			seen[key] = true
			e := check(ref.Target)
			e.RecordID, e.Relation = c.node.Record.ID, ref.Relation
			result = append(result, e)
		}
	}
	return result
}
