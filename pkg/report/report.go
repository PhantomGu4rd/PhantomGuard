// Package report renders scan output and determines the hook's exit policy.
package report

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/phantomguard/phantomguard/pkg/model"
)

// Blocked applies PhantomGuard's honest policy: phantoms and suspicious names always block; unknowns block in strict mode.
func Blocked(scan model.Report, failMode string) bool {
	if failMode == "strict" && len(scan.AnalysisIncomplete) > 0 {
		return true
	}
	for _, result := range scan.Results {
		if result.Status == model.Phantom || result.Status == model.Suspicious || failMode == "strict" && (result.Status == model.Unknown || !result.ProvenanceVerified) {
			return true
		}
	}
	return false
}

// Render returns either human-readable output or an indented machine-readable report.
func Render(scan model.Report, failMode string, asJSON bool) (string, error) {
	if asJSON {
		raw, err := json.MarshalIndent(struct {
			Report  model.Report `json:"report"`
			Blocked bool         `json:"blocked"`
		}{scan, Blocked(scan, failMode)}, "", "  ")
		return string(raw), err
	}
	var output strings.Builder
	blocked := Blocked(scan, failMode)
	if blocked {
		output.WriteString("BLOCKED  Deterministic policy found a dependency that cannot be committed.\n")
	} else if hasWarnings(scan) {
		output.WriteString("WARN  Deterministic verification completed with explicit review warnings.\n")
	} else {
		output.WriteString("VERIFIED  Deterministic verification completed without a policy block.\n")
	}
	fmt.Fprintf(&output, "PhantomGuard scanned %d file(s).\n", scan.FilesScanned)
	advisoryPackages := make([]model.Result, 0)
	seenAdvisories := make(map[string]bool)
	incomplete := make(map[string]bool, len(scan.AnalysisIncomplete))
	for _, reason := range scan.AnalysisIncomplete {
		incomplete[reason] = true
	}
	for _, notice := range scan.Notices {
		if incomplete[notice] {
			continue
		}
		fmt.Fprintf(&output, "NOTICE  %s\n", notice)
	}
	for _, reason := range scan.AnalysisIncomplete {
		fmt.Fprintf(&output, "INCOMPLETE  %s\n", reason)
	}
	for _, result := range scan.Results {
		fmt.Fprintf(&output, "%s:%d  %s  %s (%s)\n", result.Finding.Path, result.Finding.Line, strings.ToUpper(string(result.Status)), result.Package, result.Finding.Ecosystem)
		if result.Provenance != "" {
			fmt.Fprintf(&output, "  provenance: %s\n", result.Provenance)
		}
		if result.RiskMatch != "" {
			fmt.Fprintf(&output, "  HIGH-RISK TYPOSQUAT: resembles %s\n", result.RiskMatch)
		}
		if len(result.Suggestions) > 0 {
			fmt.Fprintf(&output, "  did you mean: %s\n", strings.Join(result.Suggestions, ", "))
		}
		if result.Status == model.Phantom && result.Finding.Ecosystem != model.Go {
			key := string(result.Finding.Ecosystem) + ":" + result.Package
			if !seenAdvisories[key] {
				seenAdvisories[key] = true
				advisoryPackages = append(advisoryPackages, result)
			}
		}
	}
	if scan.Unscannable > 0 {
		fmt.Fprintf(&output, "NOTICE  %d dynamic import(s) could not be statically resolved.\n", scan.Unscannable)
	}
	for _, result := range advisoryPackages {
		fmt.Fprintf(&output, "Optional advisory (not used to determine this result): phantomguard ai explain --ecosystem %s %s\n", result.Finding.Ecosystem, result.Package)
	}
	if blocked {
		output.WriteString("PhantomGuard blocked this commit.\n")
	} else {
		output.WriteString("PhantomGuard completed without a policy block.\n")
	}
	return output.String(), nil
}

func hasWarnings(scan model.Report) bool {
	if scan.Unscannable > 0 || len(scan.Notices) > 0 || len(scan.AnalysisIncomplete) > 0 {
		return true
	}
	for _, result := range scan.Results {
		if result.Status == model.Unknown || !result.ProvenanceVerified {
			return true
		}
	}
	return false
}
