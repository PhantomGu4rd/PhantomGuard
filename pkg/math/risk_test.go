package math

import "testing"

func TestDamerauLevenshtein(t *testing.T) {
	cases := []struct {
		left, right string
		want        int
	}{
		{"", "npm", 3}, {"react", "react", 0}, {"react", "raect", 1}, {"express", "expres", 1}, {"kitten", "sitting", 3},
	}
	for _, test := range cases {
		if got := DamerauLevenshtein(test.left, test.right); got != test.want {
			t.Errorf("%q/%q = %d, want %d", test.left, test.right, got, test.want)
		}
	}
}

func TestRiskEngineFlagsNearbyPopularPackage(t *testing.T) {
	match, high := NewRiskEngine().Assess("npm", "reaxt")
	if !high || match != "react" {
		t.Fatalf("match=%q high=%v", match, high)
	}
}

func TestEmbeddedPopularityCorporaContainOneThousandNamesEach(t *testing.T) {
	engine := NewRiskEngine()
	if len(engine.pypi) != 1000 || len(engine.npm) != 1000 {
		t.Fatalf("expected 1,000 names in each corpus; got PyPI=%d npm=%d", len(engine.pypi), len(engine.npm))
	}
}

func TestSuggestionsAreLocalOrderedAndCappedAtThree(t *testing.T) {
	engine := &RiskEngine{npm: []string{"abcf", "abce", "abcd", "abcde", "abcc"}}
	got := engine.Suggestions("npm", "abcd", 10)
	want := []string{"abcd", "abcc", "abcde"}
	if len(got) != len(want) {
		t.Fatalf("got %v; want %v", got, want)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("got %v; want %v", got, want)
		}
	}
}

func TestSuggestionsHandleEmptyOrInvalidLimitWithoutPanic(t *testing.T) {
	engine := &RiskEngine{npm: []string{"react"}}
	if got := engine.Suggestions("npm", "", 3); len(got) != 0 {
		t.Fatalf("empty input suggestions = %v; want empty", got)
	}
	if got := engine.Suggestions("npm", "react", -1); len(got) != 0 {
		t.Fatalf("negative limit suggestions = %v; want empty", got)
	}
}
