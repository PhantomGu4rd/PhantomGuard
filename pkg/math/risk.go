// Package math implements PhantomGuard's local typosquatting risk checks.
package math

import (
	"sort"
	"strings"

	"github.com/phantomguard/phantomguard/data"
	"github.com/phantomguard/phantomguard/pkg/model"
)

// RiskEngine is fully local: it compares a phantom name to bundled popular packages.
type RiskEngine struct {
	pypi []string
	npm  []string
	sets map[model.Ecosystem]map[string]bool
}

// NewRiskEngine loads the compile-time embedded package data.
func NewRiskEngine() *RiskEngine {
	engine := &RiskEngine{sets: make(map[model.Ecosystem]map[string]bool)}
	engine.pypi = packageLines(string(data.TopPyPI))
	engine.npm = packageLines(string(data.TopNPM))
	engine.sets[model.PyPI] = toSet(engine.pypi)
	engine.sets[model.NPM] = toSet(engine.npm)
	return engine
}

func packageLines(raw string) []string {
	var result []string
	for _, name := range strings.Split(raw, "\n") {
		if name = strings.ToLower(strings.TrimSpace(name)); name != "" {
			result = append(result, name)
		}
	}
	return result
}

func toSet(names []string) map[string]bool {
	result := make(map[string]bool, len(names))
	for _, name := range names {
		result[name] = true
	}
	return result
}

// IsBundled reports whether name is in the trusted local popular-package dataset.
func (r *RiskEngine) IsBundled(ecosystem model.Ecosystem, name string) bool {
	return r.sets[ecosystem][strings.ToLower(name)]
}

// Assess returns the closest high-risk package when the Damerau-Levenshtein distance is at most two.
func (r *RiskEngine) Assess(ecosystem model.Ecosystem, name string) (string, bool) {
	name = strings.ToLower(name)
	var candidates []string
	switch ecosystem {
	case model.PyPI:
		candidates = r.pypi
	case model.NPM:
		candidates = r.npm
	default:
		return "", false
	}
	bestName, bestDistance := "", 3
	for _, candidate := range candidates {
		distance := DamerauLevenshtein(name, candidate)
		if distance < bestDistance {
			bestName, bestDistance = candidate, distance
		}
	}
	return bestName, bestDistance <= 2
}

// Suggestions returns at most limit local candidates ordered by edit distance then name.
func (r *RiskEngine) Suggestions(ecosystem model.Ecosystem, name string, limit int) []string {
	if limit < 1 {
		return []string{}
	}
	if limit > 3 {
		limit = 3
	}
	var candidates []string
	if ecosystem == model.PyPI {
		candidates = r.pypi
	}
	if ecosystem == model.NPM {
		candidates = r.npm
	}
	type scored struct {
		name     string
		distance int
	}
	scores := make([]scored, 0, len(candidates))
	for _, candidate := range candidates {
		distance := DamerauLevenshtein(strings.ToLower(name), candidate)
		if distance <= maxSuggestionDistance(name) {
			scores = append(scores, scored{candidate, distance})
		}
	}
	sort.Slice(scores, func(i, j int) bool {
		if scores[i].distance == scores[j].distance {
			return scores[i].name < scores[j].name
		}
		return scores[i].distance < scores[j].distance
	})
	if limit > len(scores) {
		limit = len(scores)
	}
	result := make([]string, limit)
	for i := range result {
		result[i] = scores[i].name
	}
	return result
}

func maxSuggestionDistance(name string) int {
	if len(name) <= 5 {
		return 2
	}
	return 3
}

// DamerauLevenshtein implements the unrestricted Damerau-Levenshtein metric over Unicode code points.
func DamerauLevenshtein(left, right string) int {
	a, b := []rune(left), []rune(right)
	if len(a) == 0 {
		return len(b)
	}
	if len(b) == 0 {
		return len(a)
	}
	maxDistance := len(a) + len(b)
	distance := make([][]int, len(a)+2)
	for i := range distance {
		distance[i] = make([]int, len(b)+2)
	}
	distance[0][0] = maxDistance
	for i := 0; i <= len(a); i++ {
		distance[i+1][0] = maxDistance
		distance[i+1][1] = i
	}
	for j := 0; j <= len(b); j++ {
		distance[0][j+1] = maxDistance
		distance[1][j+1] = j
	}
	lastSeen := make(map[rune]int)
	for i := 1; i <= len(a); i++ {
		lastMatchColumn := 0
		for j := 1; j <= len(b); j++ {
			lastMatchingRow := lastSeen[b[j-1]]
			previousMatchColumn := lastMatchColumn
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
				lastMatchColumn = j
			}
			distance[i+1][j+1] = minimum(
				distance[i][j]+cost,
				distance[i+1][j]+1,
				distance[i][j+1]+1,
				distance[lastMatchingRow][previousMatchColumn]+(i-lastMatchingRow-1)+1+(j-previousMatchColumn-1),
			)
		}
		lastSeen[a[i-1]] = i
	}
	return distance[len(a)+1][len(b)+1]
}

func minimum(values ...int) int {
	result := values[0]
	for _, value := range values[1:] {
		if value < result {
			result = value
		}
	}
	return result
}
