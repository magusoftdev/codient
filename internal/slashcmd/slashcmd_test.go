package slashcmd

import (
	"strings"
	"testing"
)

func TestParse_NotSlashCommand(t *testing.T) {
	r := &Registry{}
	r.Register(Command{Name: "help", Description: "help", Run: func(string) error { return nil }})
	_, _, ok := r.Parse("hello world")
	if ok {
		t.Fatal("expected ok=false for non-slash input")
	}
	_, _, ok = r.Parse("")
	if ok {
		t.Fatal("expected ok=false for empty string")
	}
}

func TestParse_KnownCommand(t *testing.T) {
	r := &Registry{}
	r.Register(Command{
		Name:        "build",
		Aliases:     []string{"b"},
		Description: "switch to build mode",
		Run:         func(string) error { return nil },
	})

	cmd, args, ok := r.Parse("/build")
	if !ok || cmd == nil {
		t.Fatal("expected known command")
	}
	if cmd.Name != "build" {
		t.Fatalf("got name %q", cmd.Name)
	}
	if args != "" {
		t.Fatalf("got args %q", args)
	}

	cmd, args, ok = r.Parse("/b some args")
	if !ok || cmd == nil {
		t.Fatal("expected alias match")
	}
	if cmd.Name != "build" {
		t.Fatalf("alias resolved to %q", cmd.Name)
	}
	if args != "some args" {
		t.Fatalf("got args %q", args)
	}
}

func TestParse_UnknownCommand(t *testing.T) {
	r := &Registry{}
	r.Register(Command{Name: "help", Description: "help", Run: func(string) error { return nil }})
	cmd, _, ok := r.Parse("/foo bar")
	if !ok {
		t.Fatal("expected ok=true for slash prefix")
	}
	if cmd != nil {
		t.Fatal("expected nil cmd for unknown command")
	}
}

func TestParse_CaseInsensitive(t *testing.T) {
	r := &Registry{}
	r.Register(Command{Name: "Help", Description: "help", Run: func(string) error { return nil }})
	cmd, _, ok := r.Parse("/HELP")
	if !ok || cmd == nil {
		t.Fatal("expected case-insensitive match")
	}
}

func TestHelp(t *testing.T) {
	r := &Registry{}
	r.Register(Command{Name: "ask", Description: "switch to ask mode", Run: func(string) error { return nil }})
	r.Register(Command{Name: "exit", Aliases: []string{"quit", "q"}, Description: "quit the session", Run: func(string) error { return nil }})
	h := r.Help()
	if !strings.Contains(h, "/ask") {
		t.Fatalf("missing /ask in help: %s", h)
	}
	if !strings.Contains(h, "/exit") || !strings.Contains(h, "/quit") {
		t.Fatalf("missing /exit or alias in help: %s", h)
	}
}

func TestRegister_PanicOnDuplicate(t *testing.T) {
	r := &Registry{}
	r.Register(Command{Name: "foo", Description: "test", Run: func(string) error { return nil }})
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic on duplicate")
		}
	}()
	r.Register(Command{Name: "foo", Description: "dup", Run: func(string) error { return nil }})
}

func TestLookup_NoCommands(t *testing.T) {
	r := &Registry{}
	if got := r.Lookup("b"); got != nil {
		t.Fatalf("expected nil, got %v", got)
	}
}

func TestLookup_PrefixMatch(t *testing.T) {
	r := &Registry{}
	r.Register(Command{Name: "build", Description: "switch to build mode"})
	r.Register(Command{Name: "plan", Description: "switch to plan mode"})
	r.Register(Command{Name: "ask", Description: "switch to ask mode"})

	tests := []struct {
		prefix string
		want   []string // expected command names in order
	}{
		{"", []string{"build", "plan", "ask"}},
		{"b", []string{"build"}},
		{"pl", []string{"plan"}},
		{"a", []string{"ask", "plan"}},
		{"bu", []string{"build"}},
		{"x", nil},
	}
	for _, tt := range tests {
		t.Run(tt.prefix, func(t *testing.T) {
			got := r.Lookup(tt.prefix)
			var names []string
			for _, m := range got {
				names = append(names, m.Name)
			}
			if tt.prefix == "" {
				if len(names) != 3 {
					t.Fatalf("empty prefix: got %d results, want 3", len(names))
				}
				return
			}
			if tt.want == nil {
				if len(names) != 0 {
					t.Fatalf("prefix %q: got %v, want no matches", tt.prefix, names)
				}
				return
			}
			if len(names) != len(tt.want) {
				t.Fatalf("prefix %q: got %v, want %v", tt.prefix, names, tt.want)
			}
			for i, n := range names {
				if n != tt.want[i] {
					t.Fatalf("prefix %q: got [%v], want [%v]", tt.prefix, names, tt.want)
				}
			}
		})
	}
}

func TestLookup_AliasMatch(t *testing.T) {
	r := &Registry{}
	r.Register(Command{Name: "build", Aliases: []string{"b"}})
	r.Register(Command{Name: "plan", Aliases: []string{"p"}})
	r.Register(Command{Name: "help", Aliases: []string{"h"}})

	tests := []struct {
		prefix string
		want   []string
	}{
		{"b", []string{"build"}},
		{"p", []string{"plan", "help"}},
		{"h", []string{"help"}},
	}
	for _, tt := range tests {
		t.Run(tt.prefix, func(t *testing.T) {
			got := r.Lookup(tt.prefix)
			var names []string
			for _, m := range got {
				names = append(names, m.Name)
			}
			if len(names) != len(tt.want) {
				t.Fatalf("prefix %q: got %v, want %v", tt.prefix, names, tt.want)
			}
			for i, n := range names {
				if n != tt.want[i] {
					t.Fatalf("prefix %q: got [%v], want [%v]", tt.prefix, names, tt.want)
				}
			}
		})
	}
}

func TestLookup_Dedup(t *testing.T) {
	r := &Registry{}
	r.Register(Command{Name: "build", Aliases: []string{"b"}})
	r.Register(Command{Name: "branches", Aliases: []string{"cbranch"}})
	r.Register(Command{Name: "branch", Aliases: []string{}})

	// "b" matches all three by prefix, each should appear exactly once
	got := r.Lookup("b")
	if len(got) != 3 {
		t.Fatalf("got %d results, want 3", len(got))
	}
	names := make(map[string]bool)
	for _, m := range got {
		if names[m.Name] {
			t.Fatalf("duplicate command %q in results", m.Name)
		}
		names[m.Name] = true
	}
}

func TestLookup_ContainsMatch(t *testing.T) {
	r := &Registry{}
	r.Register(Command{Name: "build", Description: "switch to build mode"})
	r.Register(Command{Name: "checkpoint", Description: "save snapshot"})
	r.Register(Command{Name: "branch", Description: "show branch"})
	r.Register(Command{Name: "help", Description: "show help"})

	// "ck" doesn't match any prefix but contains in "checkpoint"
	got := r.Lookup("ck")
	if len(got) != 1 || got[0].Name != "checkpoint" {
		t.Fatalf("got %v, want [checkpoint]", got)
	}

	// "b" should match all prefix matches first, then contains
	got = r.Lookup("b")
	names := make([]string, len(got))
	for i, m := range got {
		names[i] = m.Name
	}
	if len(names) != 2 {
		t.Fatalf("got %v, want [build, branch]", names)
	}
}

func TestLookup_CommandMatchFields(t *testing.T) {
	r := &Registry{}
	r.Register(Command{
		Name:        "test-cmd",
		Aliases:     []string{"t", "test"},
		Description: "test description",
		Usage:       "/test-cmd <arg>",
	})
	got := r.Lookup("test")
	if len(got) != 1 {
		t.Fatalf("got %d results, want 1", len(got))
	}
	m := got[0]
	if m.Name != "test-cmd" {
		t.Fatalf("Name: got %q, want %q", m.Name, "test-cmd")
	}
	if len(m.Aliases) != 2 {
		t.Fatalf("Aliases: got %v, want [t, test]", m.Aliases)
	}
	if m.Description != "test description" {
		t.Fatalf("Description: got %q, want %q", m.Description, "test description")
	}
	if m.Usage != "/test-cmd <arg>" {
		t.Fatalf("Usage: got %q, want %q", m.Usage, "/test-cmd <arg>")
	}
}
