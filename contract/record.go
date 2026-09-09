// Package contract defines Archie's v1 public data and command contracts.
// JSON Schema in schemas/ is authoritative; these types are transport types,
// not a substitute for strict parsing, schema validation or path safety.
package contract

const Version = "1"

type Relation string

const (
	DependsOn Relation = "depends_on"
	RelatedTo Relation = "related_to"
	Source    Relation = "source"
	Test      Relation = "test"
	Doc       Relation = "doc"
	ADR       Relation = "adr"
	Parent    Relation = "parent"
	Child     Relation = "child"
)

type Link struct {
	Relation    Relation `json:"relation" yaml:"relation"`
	Target      string   `json:"target" yaml:"target"`
	Description string   `json:"description" yaml:"description"`
}

type Record struct {
	SchemaVersion string   `json:"schema_version" yaml:"schema_version"`
	ID            string   `json:"id" yaml:"id"`
	Name          string   `json:"name" yaml:"name"`
	Summary       string   `json:"summary" yaml:"summary"`
	Kind          string   `json:"kind,omitempty" yaml:"kind,omitempty"`
	Tags          []string `json:"tags,omitempty" yaml:"tags,omitempty"`
	Description   string   `json:"description,omitempty" yaml:"description,omitempty"`
	Rationale     string   `json:"rationale,omitempty" yaml:"rationale,omitempty"`
	Links         []Link   `json:"links,omitempty" yaml:"links,omitempty"`
}

type Discovery struct {
	Exclude []string `json:"exclude,omitempty" yaml:"exclude,omitempty"`
}

type RootRecord struct {
	Record    `yaml:",inline"`
	Discovery *Discovery `json:"discovery,omitempty" yaml:"discovery,omitempty"`
}

// RecordView keeps a single read-link collection: derived hierarchy, explicit ID
// links, and canonical file references. SummaryOnly marks ancestor projections.
type RecordView struct {
	Record      RootRecord `json:"record"`
	Descriptor  string     `json:"descriptor"`
	Links       []Link     `json:"links"`
	Reasons     []string   `json:"reasons"`
	SummaryOnly bool       `json:"summary_only,omitempty"`
}
