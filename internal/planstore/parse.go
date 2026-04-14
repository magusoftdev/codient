package planstore

import (
	"fmt"
	"regexp"
	"strings"
)

// ParseFromMarkdown extracts structured plan fields from the agent's markdown
// design output. It looks for known section headings (Goal, Current state,
// Implementation steps, etc.) and maps them to Plan fields.
//
// Falls back to a single-step plan wrapping the full markdown if structured
// parsing cannot extract meaningful steps.
func ParseFromMarkdown(markdown, userRequest string) *Plan {
	p := &Plan{
		Phase:       PhaseDraft,
		UserRequest: userRequest,
		RawMarkdown: markdown,
	}

	sections := splitSections(markdown)

	for heading, body := range sections {
		lower := strings.ToLower(heading)
		switch {
		case containsAny(lower, "goal", "objective", "summary"):
			p.Summary = strings.TrimSpace(body)
		case containsAny(lower, "assumption"):
			p.Assumptions = extractBullets(body)
		case containsAny(lower, "risk", "open point", "open question"):
			p.OpenQuestions = extractBullets(body)
		case containsAny(lower, "files to modify", "files to change", "proposed structure"):
			p.FilesToModify = extractBullets(body)
		case containsAny(lower, "implementation step", "steps"):
			p.Steps = extractSteps(body)
		case containsAny(lower, "testing", "verification", "test strategy"):
			p.Verification = extractBullets(body)
		}
	}

	if p.Summary == "" {
		p.Summary = extractFirstParagraph(markdown)
	}

	if len(p.Steps) == 0 {
		p.Steps = []Step{{
			ID:          "step-1",
			Title:       "Implement plan",
			Description: "Execute the full plan as described in the design document.",
			PhaseGroup:  0,
			Status:      StepPending,
		}}
	}

	return p
}

var headingRe = regexp.MustCompile(`(?m)^#{1,3}\s+(.+)$`)

// splitSections splits markdown by ## or ### headings, returning heading→body.
func splitSections(md string) map[string]string {
	matches := headingRe.FindAllStringIndex(md, -1)
	if len(matches) == 0 {
		return nil
	}

	result := make(map[string]string, len(matches))
	for i, loc := range matches {
		heading := strings.TrimSpace(headingRe.FindString(md[loc[0]:loc[1]]))
		heading = strings.TrimLeft(heading, "# ")

		bodyStart := loc[1]
		bodyEnd := len(md)
		if i+1 < len(matches) {
			bodyEnd = matches[i+1][0]
		}
		result[heading] = strings.TrimSpace(md[bodyStart:bodyEnd])
	}
	return result
}

// extractBullets pulls lines starting with - or * or numbered items.
func extractBullets(text string) []string {
	var out []string
	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if strings.HasPrefix(trimmed, "- ") {
			out = append(out, strings.TrimSpace(trimmed[2:]))
		} else if strings.HasPrefix(trimmed, "* ") {
			out = append(out, strings.TrimSpace(trimmed[2:]))
		} else if isNumberedItem(trimmed) {
			_, rest := splitNumberedItem(trimmed)
			out = append(out, rest)
		}
	}
	return out
}

var numberedRe = regexp.MustCompile(`^\d+[\.\)]\s+`)

func isNumberedItem(s string) bool {
	return numberedRe.MatchString(s)
}

func splitNumberedItem(s string) (string, string) {
	loc := numberedRe.FindStringIndex(s)
	if loc == nil {
		return "", s
	}
	return strings.TrimSpace(s[:loc[1]]), strings.TrimSpace(s[loc[1]:])
}

// extractSteps parses numbered or bulleted items into plan steps.
func extractSteps(text string) []Step {
	var steps []Step
	var currentTitle string
	var currentDesc strings.Builder
	stepNum := 0

	flush := func() {
		if currentTitle == "" {
			return
		}
		stepNum++
		steps = append(steps, Step{
			ID:          fmt.Sprintf("step-%d", stepNum),
			Title:       currentTitle,
			Description: strings.TrimSpace(currentDesc.String()),
			PhaseGroup:  0,
			Status:      StepPending,
		})
		currentTitle = ""
		currentDesc.Reset()
	}

	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			if currentTitle != "" {
				currentDesc.WriteString("\n")
			}
			continue
		}

		if isNumberedItem(trimmed) {
			flush()
			_, currentTitle = splitNumberedItem(trimmed)
		} else if strings.HasPrefix(trimmed, "- ") || strings.HasPrefix(trimmed, "* ") {
			flush()
			currentTitle = strings.TrimSpace(trimmed[2:])
		} else if currentTitle != "" {
			if currentDesc.Len() > 0 {
				currentDesc.WriteString(" ")
			}
			currentDesc.WriteString(trimmed)
		}
	}
	flush()

	// Assign phase groups: roughly distribute steps across groups.
	assignPhaseGroups(steps)

	return steps
}

// assignPhaseGroups distributes steps across phase groups.
// Each group gets at most 4 steps, keeping related work together.
func assignPhaseGroups(steps []Step) {
	groupSize := 4
	if len(steps) <= groupSize {
		return
	}
	for i := range steps {
		steps[i].PhaseGroup = i / groupSize
	}
}

func extractFirstParagraph(md string) string {
	lines := strings.Split(md, "\n")
	var para []string
	started := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "#") {
			if started {
				break
			}
			continue
		}
		if trimmed == "" {
			if started {
				break
			}
			continue
		}
		started = true
		para = append(para, trimmed)
	}
	result := strings.Join(para, " ")
	if len(result) > 500 {
		result = result[:500] + "..."
	}
	return result
}

func containsAny(s string, substrs ...string) bool {
	for _, sub := range substrs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}
