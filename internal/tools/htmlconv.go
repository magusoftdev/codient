package tools

import (
	"regexp"
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// htmlToMarkdown converts an HTML string to a simplified markdown representation.
// It strips scripts, styles, navigation, and other non-content elements while
// preserving headings, links, lists, emphasis, and code blocks.
// On parse failure it returns the input unchanged.
func htmlToMarkdown(raw string) string {
	doc, err := html.Parse(strings.NewReader(raw))
	if err != nil {
		return raw
	}
	var w mdWriter
	w.walk(doc)
	return w.finish()
}

type mdWriter struct {
	buf       strings.Builder
	inPre     bool
	listDepth int
}

var skipAtoms = map[atom.Atom]bool{
	atom.Script:   true,
	atom.Style:    true,
	atom.Noscript: true,
	atom.Svg:      true,
	atom.Nav:      true,
	atom.Footer:   true,
	atom.Iframe:   true,
}

var blockAtoms = map[atom.Atom]bool{
	atom.P:          true,
	atom.Div:        true,
	atom.Section:    true,
	atom.Article:    true,
	atom.Main:       true,
	atom.Blockquote: true,
	atom.Header:     true,
	atom.Aside:      true,
	atom.Figure:     true,
	atom.Figcaption: true,
	atom.Details:    true,
	atom.Summary:    true,
	atom.Dd:         true,
	atom.Dt:         true,
	atom.Dl:         true,
	atom.Tr:         true,
	atom.Td:         true,
	atom.Th:         true,
	atom.Table:      true,
	atom.Thead:      true,
	atom.Tbody:      true,
	atom.Tfoot:      true,
}

var headingLevel = map[atom.Atom]int{
	atom.H1: 1, atom.H2: 2, atom.H3: 3,
	atom.H4: 4, atom.H5: 5, atom.H6: 6,
}

func (w *mdWriter) walk(n *html.Node) {
	switch n.Type {
	case html.TextNode:
		w.handleText(n)
		return
	case html.ElementNode:
		// handled below
	default:
		w.walkChildren(n)
		return
	}

	tag := n.DataAtom
	if tag == 0 {
		w.walkChildren(n)
		return
	}

	if skipAtoms[tag] {
		return
	}

	if tag == atom.Head {
		return
	}

	if lvl, ok := headingLevel[tag]; ok {
		w.handleHeading(n, lvl)
		return
	}

	switch tag {
	case atom.A:
		w.handleLink(n)
	case atom.Ul, atom.Ol:
		w.handleList(n)
	case atom.Li:
		w.handleListItem(n)
	case atom.Strong, atom.B:
		w.buf.WriteString("**")
		w.walkChildren(n)
		w.buf.WriteString("**")
	case atom.Em, atom.I:
		w.buf.WriteString("*")
		w.walkChildren(n)
		w.buf.WriteString("*")
	case atom.Code:
		if w.inPre {
			w.walkChildren(n)
		} else {
			w.buf.WriteString("`")
			w.walkChildren(n)
			w.buf.WriteString("`")
		}
	case atom.Pre:
		w.ensureBlankLine()
		w.buf.WriteString("```\n")
		w.inPre = true
		w.walkChildren(n)
		w.inPre = false
		w.ensureNewline()
		w.buf.WriteString("```\n\n")
	case atom.Br:
		w.buf.WriteString("\n")
	case atom.Hr:
		w.ensureBlankLine()
		w.buf.WriteString("---\n\n")
	default:
		if blockAtoms[tag] {
			w.ensureBlankLine()
			w.walkChildren(n)
			w.ensureBlankLine()
		} else {
			w.walkChildren(n)
		}
	}
}

func (w *mdWriter) walkChildren(n *html.Node) {
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		w.walk(c)
	}
}

func (w *mdWriter) handleText(n *html.Node) {
	if w.inPre {
		w.buf.WriteString(n.Data)
		return
	}
	text := collapseWS(n.Data)
	if text == "" {
		return
	}
	if text == " " {
		if w.buf.Len() > 0 && !w.endsWith('\n') && !w.endsWith(' ') {
			w.buf.WriteString(" ")
		}
		return
	}
	w.buf.WriteString(text)
}

func (w *mdWriter) handleHeading(n *html.Node, level int) {
	w.ensureBlankLine()
	w.buf.WriteString(strings.Repeat("#", level))
	w.buf.WriteString(" ")
	w.walkChildren(n)
	w.buf.WriteString("\n\n")
}

func (w *mdWriter) handleLink(n *html.Node) {
	href := getAttr(n, "href")
	if href == "" || strings.HasPrefix(href, "javascript:") {
		w.walkChildren(n)
		return
	}
	w.buf.WriteString("[")
	w.walkChildren(n)
	w.buf.WriteString("](")
	w.buf.WriteString(href)
	w.buf.WriteString(")")
}

func (w *mdWriter) handleList(n *html.Node) {
	w.ensureBlankLine()
	w.listDepth++
	w.walkChildren(n)
	w.listDepth--
	w.ensureNewline()
}

func (w *mdWriter) handleListItem(n *html.Node) {
	w.ensureNewline()
	if w.listDepth > 1 {
		w.buf.WriteString(strings.Repeat("  ", w.listDepth-1))
	}
	w.buf.WriteString("- ")
	w.walkChildren(n)
}

func (w *mdWriter) endsWith(b byte) bool {
	if w.buf.Len() == 0 {
		return false
	}
	s := w.buf.String()
	return s[len(s)-1] == b
}

func (w *mdWriter) ensureBlankLine() {
	n := w.buf.Len()
	if n == 0 {
		return
	}
	s := w.buf.String()
	if strings.HasSuffix(s, "\n\n") {
		return
	}
	if s[n-1] == '\n' {
		w.buf.WriteString("\n")
		return
	}
	w.buf.WriteString("\n\n")
}

func (w *mdWriter) ensureNewline() {
	if w.buf.Len() == 0 {
		return
	}
	if !w.endsWith('\n') {
		w.buf.WriteString("\n")
	}
}

var reMultiNewline = regexp.MustCompile(`\n{3,}`)

func (w *mdWriter) finish() string {
	s := w.buf.String()
	s = reMultiNewline.ReplaceAllString(s, "\n\n")
	return strings.TrimSpace(s)
}

func getAttr(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if a.Key == key {
			return a.Val
		}
	}
	return ""
}

func collapseWS(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	inWS := false
	for _, r := range s {
		if r == ' ' || r == '\t' || r == '\n' || r == '\r' || r == '\f' {
			if !inWS {
				b.WriteByte(' ')
				inWS = true
			}
		} else {
			b.WriteRune(r)
			inWS = false
		}
	}
	return b.String()
}
