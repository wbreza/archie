package cli

import (
	"io"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/wbreza/archie/contract"
	"github.com/wbreza/archie/internal/query"
	"github.com/wbreza/archie/internal/repository"
)

const help = `Archie v1: bounded local architecture metadata
Commands: discover, get, context, impact, validate, scaffold, version
Use: archie COMMAND --help
Read commands emit bounded JSON; --help emits plain text.
Exit codes: 0 success, 1 invalid data/path, 2 usage/cursor, 3 I/O, 4 partial.
Scaffold creates one descriptor exclusively; no overwrite or directory creation.
`

func Run(args []string, out io.Writer) int {
	command := contract.UnknownCommand
	if len(args) == 1 && (args[0] == "--help" || args[0] == "-h") {
		return write(out, []byte(help), 0)
	}
	if len(args) > 0 {
		command = contract.Command(args[0])
	}
	o, text, err := parse(command, args)
	if err != nil {
		if _, ok := contract.Specification(command); !ok {
			command = contract.UnknownCommand
		}
		b, exit := query.Failure(command, err)
		return write(out, b, exit)
	}
	if text != "" {
		return write(out, []byte(text), 0)
	}
	b, exit := query.Run(o)
	return write(out, b, exit)
}

func write(out io.Writer, b []byte, exit int) int {
	n, err := out.Write(b)
	if err != nil || n != len(b) {
		return contract.ExitOperational
	}
	return exit
}

func parse(command contract.Command, args []string) (query.Options, string, *repository.Problem) {
	o := query.Defaults(command)
	usage := func(message string) (query.Options, string, *repository.Problem) {
		return o, "", &repository.Problem{Code: "USAGE", Message: message}
	}
	spec, ok := contract.Specification(command)
	if !ok {
		return usage("Unsupported command. Available: discover, get, context, impact, validate, scaffold, version.")
	}
	flags := map[string][]string{}
	for i := 1; i < len(args); i++ {
		arg := args[i]
		if !strings.HasPrefix(arg, "--") {
			return usage("Flags must follow the command; positional arguments are unsupported.")
		}
		pair := strings.SplitN(strings.TrimPrefix(arg, "--"), "=", 2)
		key := pair[0]
		if !has(spec.Allowed, key) {
			return usage("Unknown or incompatible flag.")
		}
		if len(flags[key]) > 0 && !has(spec.Repeated, key) {
			return usage("Singleton flags cannot be repeated.")
		}
		value := ""
		if key == "help" || key == "json" || key == "evidence" {
			if len(pair) == 2 {
				if pair[1] != "true" && pair[1] != "false" {
					return usage("Boolean flags require true or false.")
				}
				value = pair[1]
			} else {
				value = "true"
			}
		} else if len(pair) == 2 {
			value = pair[1]
		} else {
			i++
			if i >= len(args) || strings.HasPrefix(args[i], "--") {
				return usage("Flag requires a value.")
			}
			value = args[i]
		}
		if !utf8.ValidString(value) || value == "" {
			return usage("Flag values must be nonempty UTF-8.")
		}
		flags[key] = append(flags[key], value)
		if len(flags[key]) > 64 {
			return usage("Repeatable flag limit exceeded.")
		}
	}
	if first(flags, "help") == "true" {
		return o, help + "Flags for " + string(command) + ": --" + strings.Join(spec.Allowed, ", --") + "\nRequired: " + strings.Join(spec.Required, ", ") + "\n", nil
	}
	for _, key := range spec.Required {
		if len(flags[key]) == 0 {
			return usage("Required command flag is missing.")
		}
	}
	for key, values := range flags {
		for _, value := range values {
			if key == "id" && !query.IDPattern.MatchString(value) {
				return usage("ID is not a valid record identifier.")
			}
			if (key == "id" || key == "path" || key == "keyword" || key == "file") && utf8.RuneCountInString(value) > 1024 {
				return usage("Selector limit exceeded.")
			}
			if key == "query" && utf8.RuneCountInString(value) > 4096 {
				return usage("Query limit exceeded.")
			}
			if key == "keyword" && (len(query.Tokens(value)) != 1 || strings.IndexFunc(value, unicode.IsSpace) >= 0) {
				return usage("Each keyword must contain one token.")
			}
		}
	}
	if root := first(flags, "root"); root != "" {
		o.Root = root
	}
	o.Query, o.Baseline, o.Cursor = first(flags, "query"), first(flags, "baseline"), first(flags, "cursor")
	o.File, o.Name, o.Summary = first(flags, "file"), first(flags, "name"), first(flags, "summary")
	if o.Baseline != "" && !query.CommitPattern.MatchString(o.Baseline) {
		return usage("Baseline must be a full lowercase commit object ID.")
	}
	if len(o.Cursor) > 2048 {
		return usage("Cursor limit exceeded.")
	}
	if ids := flags["id"]; ids != nil {
		o.IDs = ids
	}
	if paths := flags["path"]; paths != nil {
		o.Paths = paths
	}
	if keywords := flags["keyword"]; keywords != nil {
		o.Keywords = keywords
	}
	if command == contract.Context && o.Query == "" && len(o.IDs)+len(o.Paths)+len(o.Keywords) == 0 {
		return usage("Context requires a query or selector.")
	}
	if command == contract.Validate {
		o.Evidence = first(flags, "evidence") == "true"
		if !o.Evidence && (o.Baseline != "" || flags["max-evidence"] != nil || flags["max-evidence-bytes"] != nil) {
			return usage("Validation evidence flags require --evidence.")
		}
	}
	limits := []struct {
		name     string
		dst      *int
		min, max int
	}{
		{"max-records", &o.Limits.Records, 1, contract.HardMaxRecords},
		{"max-links", &o.Limits.Links, 0, contract.HardMaxLinks},
		{"max-bytes", &o.Limits.OutputBytes, contract.MinMaxBytes, contract.HardMaxBytes},
		{"max-evidence", &o.Limits.EvidenceTargets, 0, contract.HardMaxEvidence},
		{"max-evidence-bytes", &o.Limits.EvidenceBytes, 0, contract.HardMaxEvidenceBytes},
	}
	for _, limit := range limits {
		if value := first(flags, limit.name); value != "" {
			n, err := strconv.Atoi(value)
			if err != nil || n < limit.min || n > limit.max {
				return usage("Budget is outside the supported range.")
			}
			*limit.dst = n
		}
	}
	return o, "", nil
}

func has(values []string, value string) bool {
	for _, v := range values {
		if v == value {
			return true
		}
	}
	return false
}

func first(flags map[string][]string, key string) string {
	if values := flags[key]; len(values) > 0 {
		return values[0]
	}
	return ""
}
