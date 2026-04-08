package tools

import "testing"

func TestSessionFetchAllow_AddIsAllowed(t *testing.T) {
	s := NewSessionFetchAllow()
	if s.IsAllowed("docs.example.com") {
		t.Fatal("expected not allowed")
	}
	s.Add("Docs.Example.COM")
	if !s.IsAllowed("docs.example.com") {
		t.Fatal("expected allowed")
	}
}
