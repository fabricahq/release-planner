package buildinfo

import "testing"

func TestMatches(t *testing.T) {
	sha := "1734f464f8ba0123456789abcdef0123456789ab"
	for _, tc := range []struct {
		pin, running string
		want         bool
	}{
		{"v0.2.0", "v0.2.0", true},
		{"v0.2.0", "v0.3.0", false},
		{sha, "v0.0.0-20260924172131-1734f464f8ba", true},
		{sha, "v0.0.0-20260924172131-999999999999", false},
		{sha, "v0.2.0", false},
	} {
		if got := Matches(tc.pin, tc.running); got != tc.want {
			t.Errorf("Matches(%q, %q) = %v", tc.pin, tc.running, got)
		}
	}
}

func TestStamped(t *testing.T) {
	stamped = "v0.1.0"
	t.Cleanup(func() { stamped = "" })
	if Version() != "v0.1.0" || Local() {
		t.Fatalf("stamped build: %s local=%v", Version(), Local())
	}
}
