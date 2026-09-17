package tui

import "testing"

func TestParent(t *testing.T) {
	tests := map[string]string{"": "", "apps/": "", "apps/prod/": "apps/", "apps/prod/db/": "apps/prod/"}
	for input, want := range tests {
		if got := parent(input); got != want {
			t.Errorf("parent(%q) = %q, want %q", input, got, want)
		}
	}
}
