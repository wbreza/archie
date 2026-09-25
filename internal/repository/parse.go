package repository

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"path"
	"regexp"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/santhosh-tekuri/jsonschema/v5"
	"github.com/wbreza/archie/contract"
	"github.com/wbreza/archie/schemas"
	"gopkg.in/yaml.v3"
)

type Problem struct {
	Code, Message, Descriptor string
	Diagnostics               []contract.Diagnostic
	Metadata                  contract.MetadataCoverage
}

func (p *Problem) Error() string { return p.Code + ": " + p.Message }

func problem(code, message string) *Problem { return &Problem{Code: code, Message: message} }

var idPattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]{0,127}$`)
var recordSchema = sync.OnceValues(func() (*jsonschema.Schema, error) { return schemas.Compile("record") })
var rootSchema = sync.OnceValues(func() (*jsonschema.Schema, error) { return schemas.Compile("root") })

// Parse checks the YAML representation before handing JSON-compatible values to
// the authoritative embedded schema. Identity is retained on schema errors only.
func Parse(raw []byte, root bool) (record contract.RootRecord, identity string, err *Problem) {
	if len(raw) > contract.MaxDescriptorBytes {
		return record, "", problem("DISCOVERY_LIMIT", "Descriptor byte limit exceeded.")
	}
	if !utf8.Valid(raw) || bytes.HasPrefix(raw, []byte{0xef, 0xbb, 0xbf}) {
		return record, "", problem("YAML_INVALID", "YAML must be UTF-8 without a BOM.")
	}
	for _, line := range strings.Split(string(raw), "\n") {
		if strings.HasPrefix(line, "%") {
			return record, "", problem("YAML_INVALID", "YAML directives are not supported.")
		}
	}
	d := yaml.NewDecoder(bytes.NewReader(raw))
	var node, extra yaml.Node
	if e := d.Decode(&node); e != nil {
		return record, "", problem("YAML_INVALID", "Invalid YAML document.")
	}
	if e := d.Decode(&extra); e != io.EOF {
		return record, "", problem("YAML_INVALID", "Exactly one YAML document is required.")
	}
	count := 0
	if e := checkNode(&node, 1, &count); e != nil {
		return record, "", e
	}
	if len(node.Content) != 1 || node.Content[0].Kind != yaml.MappingNode {
		return record, "", problem("YAML_INVALID", "The document must be a mapping.")
	}
	value, e := jsonValue(node.Content[0])
	if e != nil {
		return record, "", e
	}
	object := value.(map[string]any)
	if id, ok := object["id"].(string); ok && idPattern.MatchString(id) {
		identity = id
	}
	data, marshalErr := json.Marshal(value)
	if marshalErr != nil {
		return record, identity, problem("SCHEMA_INVALID", "Value is not a JSON-compatible scalar.")
	}
	// A schema-invalid record can still carry paths requiring fatal safety checks.
	_ = json.Unmarshal(data, &record)
	s, compileErr := recordSchema()
	if root {
		s, compileErr = rootSchema()
	}
	if compileErr != nil {
		return record, identity, problem("INTERNAL", "Bundled schema could not be compiled.")
	}
	if e := s.Validate(value); e != nil {
		message := "Record does not conform to the bundled v1 schema."
		if validation, ok := e.(*jsonschema.ValidationError); ok {
			validation = firstSchemaCause(validation)
			keyword := path.Base(validation.KeywordLocation)
			message = fmt.Sprintf("Schema violation at %s (%s).", validation.InstanceLocation, keyword)
			// Length messages contain numeric limits, never authored values.
			if keyword == "maxLength" || keyword == "minLength" {
				message = fmt.Sprintf("Schema violation at %s: %s.", validation.InstanceLocation, validation.Message)
			}
			runes := []rune(message)
			if len(runes) > 256 {
				message = string(runes[:253]) + "..."
			}
		}
		return record, identity, problem("SCHEMA_INVALID", message)
	}
	return record, identity, nil
}

// Schema property errors can originate from map iteration. Choose the same leaf
// regardless of the validator's traversal order.
func firstSchemaCause(e *jsonschema.ValidationError) *jsonschema.ValidationError {
	if len(e.Causes) == 0 {
		return e
	}
	var first *jsonschema.ValidationError
	for _, cause := range e.Causes {
		leaf := firstSchemaCause(cause)
		if first == nil || leaf.InstanceLocation < first.InstanceLocation ||
			leaf.InstanceLocation == first.InstanceLocation && leaf.KeywordLocation < first.KeywordLocation {
			first = leaf
		}
	}
	return first
}

func checkNode(n *yaml.Node, depth int, count *int) *Problem {
	*count++
	if depth > contract.MaxYAMLDepth || *count > contract.MaxYAMLNodes {
		return problem("DISCOVERY_LIMIT", "YAML node or depth limit exceeded.")
	}
	if n.Kind == yaml.AliasNode || n.Anchor != "" || n.Style&yaml.TaggedStyle != 0 {
		return problem("YAML_INVALID", "Aliases, anchors and explicit tags are forbidden.")
	}
	if n.Kind == yaml.MappingNode {
		keys := map[string]bool{}
		for i := 0; i < len(n.Content); i += 2 {
			k := n.Content[i]
			if k.Kind != yaml.ScalarNode || k.Tag != "!!str" || k.Value == "<<" || keys[k.Value] {
				return problem("YAML_INVALID", "Mapping keys must be unique strings; merges are forbidden.")
			}
			keys[k.Value] = true
		}
	}
	for _, child := range n.Content {
		if err := checkNode(child, depth+1, count); err != nil {
			return err
		}
	}
	return nil
}

func jsonValue(n *yaml.Node) (any, *Problem) {
	switch n.Kind {
	case yaml.MappingNode:
		m := map[string]any{}
		for i := 0; i < len(n.Content); i += 2 {
			v, e := jsonValue(n.Content[i+1])
			if e != nil {
				return nil, e
			}
			m[n.Content[i].Value] = v
		}
		return m, nil
	case yaml.SequenceNode:
		a := []any{}
		for _, child := range n.Content {
			v, e := jsonValue(child)
			if e != nil {
				return nil, e
			}
			a = append(a, v)
		}
		return a, nil
	case yaml.ScalarNode:
		if n.Tag == "!!str" || n.Tag == "!!timestamp" {
			return n.Value, nil
		}
		if n.Tag != "!!int" && n.Tag != "!!float" && n.Tag != "!!bool" && n.Tag != "!!null" {
			return nil, problem("YAML_INVALID", "Unsupported YAML scalar.")
		}
		var v any
		if err := n.Decode(&v); err != nil {
			return nil, problem("YAML_INVALID", "Invalid scalar.")
		}
		return v, nil
	}
	return nil, problem("YAML_INVALID", "Unsupported YAML node.")
}
