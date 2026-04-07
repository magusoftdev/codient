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
