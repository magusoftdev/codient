package codientcli

import "testing"

func TestForkSlug(t *testing.T) {
	t.Parallel()
	if got := forkSlug("Try Alternative!"); got != "try-alternative" {
		t.Fatalf("got %q", got)
	}
	if got := forkSlug(""); got != "fork" {
		t.Fatalf("got %q", got)
	}
}
