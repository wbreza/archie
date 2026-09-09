package contract

type Command string

const (
	Discover       Command = "discover"
	Get            Command = "get"
	Context        Command = "context"
	Impact         Command = "impact"
	Validate       Command = "validate"
	Scaffold       Command = "scaffold"
	VersionCommand Command = "version"
	UnknownCommand Command = "unknown"
)

type Status string

const (
	OK      Status = "ok"
	Partial Status = "partial"
	Error   Status = "error"
)

const (
	ExitOK          = 0
	ExitInvalid     = 1
	ExitUsage       = 2
	ExitOperational = 3
	ExitPartial     = 4
)

type Diagnostic struct {
	Code       string `json:"code"`
	Message    string `json:"message"`
	Descriptor string `json:"descriptor,omitempty"`
}

type MetadataCoverage struct {
	Discovered int  `json:"discovered"`
	Loaded     int  `json:"loaded"`
	Excluded   int  `json:"excluded"`
	Invalid    int  `json:"invalid"`
	Complete   bool `json:"complete"`
}

type SelectionCoverage struct {
	Matched  int `json:"matched"`
	Returned int `json:"returned"`
	Omitted  int `json:"omitted"`
}

type EvidenceCoverage struct {
	Total     int `json:"total"`
	Checked   int `json:"checked"`
	Unchecked int `json:"unchecked"`
}

type Omission struct {
	Reason string   `json:"reason"`
	Count  int      `json:"count"`
	Paths  []string `json:"paths"`
}

type Coverage struct {
	Metadata         MetadataCoverage  `json:"metadata"`
	Selection        SelectionCoverage `json:"selection"`
	Evidence         EvidenceCoverage  `json:"evidence"`
	Omissions        []Omission        `json:"omissions"`
	DiagnosticCounts map[string]int    `json:"diagnostic_counts,omitempty"`
}

type Continuation struct {
	Cursor    string `json:"cursor"`
	Remaining int    `json:"remaining"`
}

type Baseline struct {
	Requested string `json:"requested"`
	Resolved  string `json:"resolved"`
	State     string `json:"state"`
	Reason    string `json:"reason"`
}

type Evidence struct {
	RecordID string   `json:"record_id"`
	Relation Relation `json:"relation"`
	Target   string   `json:"target"`
	State    string   `json:"state"`
	Reason   string   `json:"reason"`
}

type QueryData struct {
	Records  []RecordView `json:"records"`
	Evidence []Evidence   `json:"evidence"`
}

type ValidationData struct {
	Valid    bool       `json:"valid"`
	Evidence []Evidence `json:"evidence"`
}

type ScaffoldData struct {
	Path string `json:"path"`
}

type VersionData struct {
	Version string `json:"version"`
}

// Data is nil on fatal failure. Empty collections must be [] rather than null.
type Response[T any] struct {
	APIVersion   string        `json:"api_version"`
	Command      Command       `json:"command"`
	Status       Status        `json:"status"`
	Data         *T            `json:"data"`
	Diagnostics  []Diagnostic  `json:"diagnostics"`
	Coverage     Coverage      `json:"coverage"`
	Continuation *Continuation `json:"continuation"`
	Baseline     Baseline      `json:"baseline"`
}
