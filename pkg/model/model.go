// Package model holds dependency findings and registry outcomes shared by the CLI.
package model

// Ecosystem identifies the public registry that owns a dependency.
type Ecosystem string

const (
	PyPI Ecosystem = "pypi"
	NPM  Ecosystem = "npm"
	Go   Ecosystem = "go"
)

// Status is deliberately explicit: Unknown never means Exists.
type Status string

const (
	Exists     Status = "exists"
	Phantom    Status = "phantom"
	Unknown    Status = "unknown"
	Suspicious Status = "suspicious"
)

// Finding is a static reference to a public dependency candidate.
type Finding struct {
	Name      string    `json:"name"`
	Ecosystem Ecosystem `json:"ecosystem"`
	Path      string    `json:"path"`
	Line      int       `json:"line"`
	Snippet   string    `json:"-"`
}

// Result joins a finding to a validated registry result.
type Result struct {
	Finding            Finding  `json:"finding"`
	Package            string   `json:"package"`
	Status             Status   `json:"status"`
	Provenance         string   `json:"provenance,omitempty"`
	ProvenanceVerified bool     `json:"provenance_verified"`
	Suggestions        []string `json:"suggestions,omitempty"`
	RiskMatch          string   `json:"risk_match,omitempty"`
}

// Report contains all output from one scan without imposing presentation policy.
type Report struct {
	FilesScanned       int      `json:"files_scanned"`
	Results            []Result `json:"results"`
	Notices            []string `json:"notices"`
	AnalysisIncomplete []string `json:"analysis_incomplete,omitempty"`
	Unscannable        int      `json:"unscannable"`
}
