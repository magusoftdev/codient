// Package skills discovers Cursor-style SKILL.md trees under the codient state dir
// and under <workspace>/.codient/skills/.
package skills

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// WorkspaceSkillsRelDir is the skills root relative to the workspace root.
const WorkspaceSkillsRelDir = ".codient/skills"

// UserSkillsSubdir is the directory name under the codient state directory for user-wide skills.
const UserSkillsSubdir = "skills"

// MaxSkillIDLen matches common agent-skill name limits.
const MaxSkillIDLen = 64

// MaxCatalogBytes caps the rendered "## Agent skills" markdown block.
const MaxCatalogBytes = 10 * 1024

var skillIDRe = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// ValidSkillID reports whether id is a non-empty safe folder name for a skill.
func ValidSkillID(id string) bool {
	id = strings.TrimSpace(id)
	if id == "" || len(id) > MaxSkillIDLen {
		return false
	}
	return skillIDRe.MatchString(id)
}

// Entry is one discovered skill for catalog / listing.
type Entry struct {
	ID          string // directory name under the skills root
	Name        string // from frontmatter, or ID
	Description string
	Scope       string // "workspace" or "user"
	// ReadPath is the path to pass to read_file: workspace-relative for workspace skills,
	// or path relative to the user skills library root for user skills (read_file resolves
	// workspace first, then the user skills directory).
	ReadPath string
	DisableModelInvocation bool
}

type skillFrontmatter struct {
	Name                   string `yaml:"name"`
	Description            string `yaml:"description"`
	DisableModelInvocation bool   `yaml:"disable-model-invocation"`
}

// ParseFrontmatter extracts YAML frontmatter from SKILL.md bytes.
// If there is no leading --- block, returns empty frontmatter and the full string as body.
func ParseFrontmatter(data []byte) (skillFrontmatter, string, error) {
	var fm skillFrontmatter
	s := string(data)
	s = strings.TrimPrefix(s, "\ufeff")
	sTrim := strings.TrimLeft(s, " \t\r\n")
	if !strings.HasPrefix(sTrim, "---") {
		return fm, strings.TrimSpace(s), nil
	}
	// Normalize to start at ---
	if len(sTrim) < len(s) {
		s = sTrim
	}
	lines := strings.Split(s, "\n")
	if len(lines) < 2 || lines[0] != "---" {
		return fm, strings.TrimSpace(s), nil
	}
	var yamlLines []string
	end := -1
	for i := 1; i < len(lines); i++ {
		line := strings.TrimSuffix(lines[i], "\r")
		if line == "---" {
			end = i
			break
		}
		yamlLines = append(yamlLines, line)
	}
	if end < 0 {
		return fm, "", fmt.Errorf("unterminated YAML frontmatter")
	}
	yamlText := strings.Join(yamlLines, "\n")
	if err := yaml.Unmarshal([]byte(yamlText), &fm); err != nil {
		return fm, "", fmt.Errorf("frontmatter yaml: %w", err)
	}
	body := strings.TrimSpace(strings.Join(lines[end+1:], "\n"))
	return fm, body, nil
}

func normKey(s string) string { return strings.ToLower(strings.TrimSpace(s)) }

func readSkillEntry(scope, skillID, skillPath string) (Entry, error) {
	data, err := os.ReadFile(skillPath)
	if err != nil {
		return Entry{}, err
	}
	fm, _, err := ParseFrontmatter(data)
	if err != nil {
		return Entry{}, err
	}
	name := strings.TrimSpace(fm.Name)
	if name == "" {
		name = skillID
	}
	desc := strings.TrimSpace(fm.Description)
	var readPath string
	switch scope {
	case "workspace":
		readPath = filepath.ToSlash(filepath.Join(WorkspaceSkillsRelDir, skillID, "SKILL.md"))
	default:
		readPath = filepath.ToSlash(filepath.Join(skillID, "SKILL.md"))
	}
	return Entry{
		ID:                     skillID,
		Name:                   name,
		Description:            desc,
		Scope:                  scope,
		ReadPath:               readPath,
		DisableModelInvocation: fm.DisableModelInvocation,
	}, nil
}

func scanSkillsRoot(scope, root string) ([]Entry, error) {
	if strings.TrimSpace(root) == "" {
		return nil, nil
	}
	fi, err := os.Stat(root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	if !fi.IsDir() {
		return nil, nil
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	var out []Entry
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		id := e.Name()
		if !ValidSkillID(id) {
			continue
		}
		skillPath := filepath.Join(root, id, "SKILL.md")
		if _, err := os.Stat(skillPath); err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, err
		}
		ent, err := readSkillEntry(scope, id, skillPath)
		if err != nil {
			continue
		}
		out = append(out, ent)
	}
	return out, nil
}

// Discover returns merged skill entries: workspace overrides user when names collide (case-insensitive).
func Discover(stateDir, workspace string) ([]Entry, error) {
	var userRoot string
	if sd := strings.TrimSpace(stateDir); sd != "" {
		userRoot = filepath.Join(sd, UserSkillsSubdir)
	}
	userEntries, err := scanSkillsRoot("user", userRoot)
	if err != nil {
		return nil, fmt.Errorf("user skills: %w", err)
	}
	var wsRoot string
	if ws := strings.TrimSpace(workspace); ws != "" {
		absWs, err := filepath.Abs(ws)
		if err != nil {
			return nil, err
		}
		wsRoot = filepath.Join(absWs, WorkspaceSkillsRelDir)
	}
	wsEntries, err := scanSkillsRoot("workspace", wsRoot)
	if err != nil {
		return nil, fmt.Errorf("workspace skills: %w", err)
	}
	byKey := make(map[string]Entry)
	for _, e := range userEntries {
		byKey[normKey(e.Name)] = e
	}
	for _, e := range wsEntries {
		byKey[normKey(e.Name)] = e
	}
	keys := make([]string, 0, len(byKey))
	for k := range byKey {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]Entry, 0, len(keys))
	for _, k := range keys {
		out = append(out, byKey[k])
	}
	return out, nil
}

// FormatCatalog renders markdown for the system prompt (without the ## heading).
func FormatCatalog(entries []Entry) string {
	if len(entries) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("These optional **agent skills** live on disk. Each is a folder with `SKILL.md` (YAML frontmatter + markdown instructions).\n\n")
	b.WriteString("When a task matches a skill’s description, **read that skill’s file with read_file** using the path shown before relying on it. ")
	b.WriteString("For **user** skills, `read_file` checks the workspace first, then the codient user skills library if the path is not found under the workspace.\n\n")
	for _, e := range entries {
		scopeNote := ""
		if e.Scope == "user" {
			scopeNote = " *(user / global)*"
		} else {
			scopeNote = " *(workspace)*"
		}
		dmi := ""
		if e.DisableModelInvocation {
			dmi = " — `disable-model-invocation`: apply only when the user asks for this skill or names it explicitly."
		}
		line := fmt.Sprintf("- **%s**%s: %s — `read_file` path: `%s`%s\n",
			e.Name, scopeNote, e.Description, e.ReadPath, dmi)
		if b.Len()+len(line) > MaxCatalogBytes {
			b.WriteString("\n[truncated: skill catalog size cap]\n")
			break
		}
		b.WriteString(line)
	}
	return strings.TrimSpace(b.String())
}

// CatalogMarkdown returns the full "## Agent skills" section or empty if none.
func CatalogMarkdown(entries []Entry) string {
	body := FormatCatalog(entries)
	if body == "" {
		return ""
	}
	return "## Agent skills\n\n" + body
}

// LoadCatalogMarkdown discovers skills and returns the "## Agent skills" section or empty.
func LoadCatalogMarkdown(stateDir, workspace string) (string, error) {
	entries, err := Discover(stateDir, workspace)
	if err != nil {
		return "", err
	}
	return CatalogMarkdown(entries), nil
}
