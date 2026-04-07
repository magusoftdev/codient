// Package slashcmd implements a simple slash-command parser and registry for the codient REPL.
package slashcmd

import (
	"fmt"
	"sort"
	"strings"
)

// Command is a single slash command the user can invoke in the REPL.
type Command struct {
	Name        string
	Aliases     []string
	Description string
	Usage       string // e.g. "/model <name>"
	Run         func(args string) error
}

// Registry maps command names to implementations. Zero value is ready to use.
type Registry struct {
	order []string
	byName map[string]*Command
}

// Register adds a command. Panics on duplicate names (programmer error).
func (r *Registry) Register(cmd Command) {
	if r.byName == nil {
		r.byName = make(map[string]*Command)
	}
	c := cmd
	key := strings.ToLower(c.Name)
	if _, ok := r.byName[key]; ok {
		panic("slashcmd: duplicate command: " + key)
	}
	r.byName[key] = &c
	r.order = append(r.order, key)
	for _, alias := range c.Aliases {
		akey := strings.ToLower(alias)
		if _, ok := r.byName[akey]; ok {
			panic("slashcmd: duplicate alias: " + akey)
		}
		r.byName[akey] = &c
	}
}

// Parse checks whether line is a slash command. Returns the matched command and
// any trailing arguments. ok is true when the line starts with '/'.
// If the command name is not recognized, cmd is nil and args contains the full line.
func (r *Registry) Parse(line string) (cmd *Command, args string, ok bool) {
	trimmed := strings.TrimSpace(line)
	if !strings.HasPrefix(trimmed, "/") {
		return nil, "", false
	}
	parts := strings.SplitN(trimmed[1:], " ", 2)
	name := strings.ToLower(parts[0])
	if len(parts) > 1 {
		args = strings.TrimSpace(parts[1])
	}
	if r.byName != nil {
		if c, found := r.byName[name]; found {
			return c, args, true
		}
	}
	return nil, trimmed, true
}

// Help returns a formatted help string listing all registered commands.
func (r *Registry) Help() string {
	if len(r.order) == 0 {
		return "No commands registered.\n"
	}
	seen := make(map[string]bool)
	var lines []string
	for _, key := range r.order {
		if seen[key] {
			continue
		}
		seen[key] = true
		c := r.byName[key]
		usage := "/" + c.Name
		if c.Usage != "" {
			usage = c.Usage
		}
		line := fmt.Sprintf("  %-24s %s", usage, c.Description)
		if len(c.Aliases) > 0 {
			aliases := make([]string, len(c.Aliases))
			for i, a := range c.Aliases {
				aliases[i] = "/" + a
			}
			sort.Strings(aliases)
			line += fmt.Sprintf(" (also: %s)", strings.Join(aliases, ", "))
		}
		lines = append(lines, line)
	}
	return "Available commands:\n" + strings.Join(lines, "\n") + "\n"
}
