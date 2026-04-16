package agent

import (
	"strings"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/packages/param"
)

// UserMessageText returns a plain-text summary of the user message for heuristics
// (e.g. Ask-mode post-reply checks). Image parts are represented as "[image]".
func UserMessageText(m openai.ChatCompletionMessageParamUnion) string {
	u := m.OfUser
	if u == nil {
		return ""
	}
	c := u.Content
	if !param.IsOmitted(c.OfString) {
		return c.OfString.Value
	}
	if len(c.OfArrayOfContentParts) == 0 {
		return ""
	}
	var b strings.Builder
	for i, part := range c.OfArrayOfContentParts {
		if i > 0 {
			b.WriteString(" ")
		}
		if t := part.GetText(); t != nil && *t != "" {
			b.WriteString(*t)
			continue
		}
		if part.GetImageURL() != nil {
			b.WriteString("[image]")
			continue
		}
		b.WriteString("[content]")
	}
	return b.String()
}
