package semver

import (
	"slices"
	"testing"
)

func TestPrecedence(t *testing.T) {
	ordered := []string{"v1.0.0-alpha", "v1.0.0-alpha.1", "v1.0.0-alpha.beta", "v1.0.0-beta",
		"v1.0.0-beta.2", "v1.0.0-beta.11", "v1.0.0-rc.1", "v1.0.0", "v1.0.1", "v1.10.0", "v2.0.0"}
	shuffled := slices.Clone(ordered)
	slices.Reverse(shuffled)
	slices.SortFunc(shuffled, func(a, b string) int { return Compare(MustParse(a), MustParse(b)) })
	if !slices.Equal(shuffled, ordered) {
		t.Fatalf("got %v", shuffled)
	}
}

func TestParse(t *testing.T) {
	for tag, ok := range map[string]bool{
		"v1.2.3": true, "v0.1.0": true, "v1.2.3-rc.1": true, "v1.2.3-0.a-b": true,
		"1.2.3": false, "v1.2": false, "v01.2.3": false, "v1.2.3+build": false, "v1.2.3-01": false, "v-preview": false,
	} {
		v, got := Parse(tag)
		if got != ok {
			t.Errorf("Parse(%q) = %v, want %v", tag, got, ok)
		}
		if ok && v.String() != tag {
			t.Errorf("String() = %q, want %q", v.String(), tag)
		}
	}
}

func TestCandidates(t *testing.T) {
	for prev, want := range map[string][3]string{
		"v1.0.0":      {"v1.0.1", "v1.1.0", "v2.0.0"},
		"v1.1.0-rc.1": {"v1.1.0", "v1.1.0", "v2.0.0"},
		"v2.0.0-rc.1": {"v2.0.0", "v2.0.0", "v2.0.0"},
		"v1.2.3-rc.1": {"v1.2.3", "v1.3.0", "v2.0.0"},
	} {
		p, mi, ma := Candidates(MustParse(prev))
		if got := [3]string{p.String(), mi.String(), ma.String()}; got != want {
			t.Errorf("Candidates(%s) = %v, want %v", prev, got, want)
		}
	}
}
