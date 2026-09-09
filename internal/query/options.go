package query

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"regexp"
	"sort"
	"strings"
	"unicode"

	"github.com/wbreza/archie/contract"
)

type Options struct {
	Command                       contract.Command
	Root, Query, Baseline, Cursor string
	File, Name, Summary           string
	IDs, Paths, Keywords          []string
	Limits                        contract.PageLimits
	Evidence                      bool
}

func Defaults(command contract.Command) Options {
	return Options{
		Command: command, Root: ".", IDs: []string{}, Paths: []string{}, Keywords: []string{},
		Evidence: command == contract.Get || command == contract.Context || command == contract.Impact,
		Limits: contract.PageLimits{Records: contract.DefaultMaxRecords, Links: contract.DefaultMaxLinks,
			OutputBytes: contract.DefaultMaxBytes, EvidenceTargets: contract.DefaultMaxEvidence,
			EvidenceBytes: contract.DefaultMaxEvidenceBytes},
	}
}

var CommitPattern = regexp.MustCompile(`^([0-9a-f]{40}|[0-9a-f]{64})$`)
var IDPattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]{0,127}$`)

func Tokens(text string) []string {
	return unique(strings.FieldsFunc(strings.ToLower(text), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) }))
}

func unique(values []string) []string {
	set := map[string]bool{}
	for _, value := range values {
		set[value] = true
	}
	result := []string{}
	for value := range set {
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func encode(value any) []byte {
	var b bytes.Buffer
	e := json.NewEncoder(&b)
	e.SetEscapeHTML(false)
	_ = e.Encode(value)
	return b.Bytes()
}

func queryHash(o Options) string {
	limits := o.Limits
	if o.Command == contract.Discover {
		limits.EvidenceBytes, limits.EvidenceTargets = 0, 0
	}
	q := contract.QueryIdentity{
		Baseline: o.Baseline, Command: o.Command, IDs: unique(o.IDs), Paths: unique(o.Paths),
		Tokens: Tokens(o.Query + " " + strings.Join(o.Keywords, " ")), Limits: limits,
	}
	sum := sha256.Sum256(bytes.TrimSuffix(encode(q), []byte("\n")))
	return hex.EncodeToString(sum[:])
}
