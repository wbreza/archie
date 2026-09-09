package contract

type CommandSpec struct {
	Required []string
	Repeated []string
	Allowed  []string
}

// Specification returns a fresh flag specification; W4 owns argument parsing.
func Specification(command Command) (CommandSpec, bool) {
	spec := CommandSpec{Allowed: []string{"root", "json", "help"}}
	page := []string{"max-records", "max-links", "max-bytes", "cursor"}
	evidence := []string{"baseline", "max-evidence", "max-evidence-bytes"}
	switch command {
	case Discover:
		spec.Allowed = append(spec.Allowed, page...)
	case Get:
		spec.Required = []string{"id"}
		spec.Allowed = append(spec.Allowed, "id", "max-links", "max-bytes")
		spec.Allowed = append(spec.Allowed, evidence...)
	case Context:
		spec.Repeated = []string{"keyword", "path", "id"}
		spec.Allowed = append(spec.Allowed, "query", "keyword", "path", "id")
		spec.Allowed = append(spec.Allowed, page...)
		spec.Allowed = append(spec.Allowed, evidence...)
	case Impact:
		spec.Required = []string{"path"}
		spec.Repeated = []string{"path"}
		spec.Allowed = append(spec.Allowed, "path")
		spec.Allowed = append(spec.Allowed, page...)
		spec.Allowed = append(spec.Allowed, evidence...)
	case Validate:
		spec.Allowed = append(spec.Allowed, "evidence")
		spec.Allowed = append(spec.Allowed, evidence...)
	case Scaffold:
		spec.Required = []string{"file", "id", "name", "summary"}
		spec.Allowed = append(spec.Allowed, spec.Required...)
	case VersionCommand:
		spec.Allowed = []string{"json", "help"}
	default:
		return CommandSpec{}, false
	}
	return spec, true
}

// Cursor defines the fixed-order payload before base64url encoding.
type Cursor struct {
	Version  string `json:"v"`
	Query    string `json:"query"`
	Snapshot string `json:"snapshot"`
	Offset   int    `json:"offset"`
}

// QueryIdentity fields are lexical; encoders must disable HTML escaping.
type QueryIdentity struct {
	Baseline string     `json:"baseline"`
	Command  Command    `json:"command"`
	IDs      []string   `json:"ids"`
	Limits   PageLimits `json:"limits"`
	Paths    []string   `json:"paths"`
	Tokens   []string   `json:"tokens"`
}

type PageLimits struct {
	EvidenceBytes   int `json:"evidence_bytes"`
	EvidenceTargets int `json:"evidence_targets"`
	Links           int `json:"links"`
	OutputBytes     int `json:"output_bytes"`
	Records         int `json:"records"`
}
