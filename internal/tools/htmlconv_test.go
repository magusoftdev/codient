package tools

import (
	"strings"
	"testing"
)

func TestHtmlToMarkdown_BasicParagraph(t *testing.T) {
	html := `<html><body><p>Hello world</p></body></html>`
	got := htmlToMarkdown(html)
	if !strings.Contains(got, "Hello world") {
		t.Fatalf("expected paragraph text, got: %q", got)
	}
	if strings.Contains(got, "<p>") {
		t.Fatalf("expected no raw HTML tags, got: %q", got)
	}
}

func TestHtmlToMarkdown_Headings(t *testing.T) {
	html := `<html><body>
		<h1>Title</h1>
		<h2>Subtitle</h2>
		<h3>Section</h3>
	</body></html>`
	got := htmlToMarkdown(html)
	if !strings.Contains(got, "# Title") {
		t.Errorf("expected h1 as '# Title', got:\n%s", got)
	}
	if !strings.Contains(got, "## Subtitle") {
		t.Errorf("expected h2 as '## Subtitle', got:\n%s", got)
	}
	if !strings.Contains(got, "### Section") {
		t.Errorf("expected h3 as '### Section', got:\n%s", got)
	}
}

func TestHtmlToMarkdown_Links(t *testing.T) {
	html := `<p>Visit <a href="https://example.com">Example</a> for details.</p>`
	got := htmlToMarkdown(html)
	if !strings.Contains(got, "[Example](https://example.com)") {
		t.Fatalf("expected markdown link, got: %q", got)
	}
}

func TestHtmlToMarkdown_Links_JavascriptSkipped(t *testing.T) {
	html := `<p><a href="javascript:void(0)">Click</a></p>`
	got := htmlToMarkdown(html)
	if strings.Contains(got, "javascript:") {
		t.Fatalf("expected javascript: href to be stripped, got: %q", got)
	}
	if !strings.Contains(got, "Click") {
		t.Fatalf("expected link text preserved, got: %q", got)
	}
}

func TestHtmlToMarkdown_ScriptStyleNavStripped(t *testing.T) {
	html := `<html><body>
		<script>alert("xss")</script>
		<style>.x{color:red}</style>
		<nav><a href="/">Home</a></nav>
		<p>Content here</p>
	</body></html>`
	got := htmlToMarkdown(html)
	if strings.Contains(got, "alert") {
		t.Errorf("script content should be stripped, got:\n%s", got)
	}
	if strings.Contains(got, "color:red") {
		t.Errorf("style content should be stripped, got:\n%s", got)
	}
	if strings.Contains(got, "Home") {
		t.Errorf("nav content should be stripped, got:\n%s", got)
	}
	if !strings.Contains(got, "Content here") {
		t.Errorf("body content should be preserved, got:\n%s", got)
	}
}

func TestHtmlToMarkdown_InlineCode(t *testing.T) {
	html := `<p>Use <code>fmt.Println</code> to print.</p>`
	got := htmlToMarkdown(html)
	if !strings.Contains(got, "`fmt.Println`") {
		t.Fatalf("expected backtick-wrapped code, got: %q", got)
	}
}

func TestHtmlToMarkdown_PreCodeBlock(t *testing.T) {
	html := "<pre><code>func main() {\n  fmt.Println(\"hi\")\n}</code></pre>"
	got := htmlToMarkdown(html)
	if !strings.Contains(got, "```\nfunc main()") {
		t.Errorf("expected fenced code block, got:\n%s", got)
	}
	if !strings.Contains(got, "\n```") {
		t.Errorf("expected closing fence, got:\n%s", got)
	}
	if strings.Contains(got, "`func") {
		t.Errorf("code inside pre should not be backtick-wrapped, got:\n%s", got)
	}
}

func TestHtmlToMarkdown_Lists(t *testing.T) {
	html := `<ul><li>Alpha</li><li>Beta</li><li>Gamma</li></ul>`
	got := htmlToMarkdown(html)
	if !strings.Contains(got, "- Alpha") {
		t.Errorf("expected list items, got:\n%s", got)
	}
	if !strings.Contains(got, "- Beta") {
		t.Errorf("expected list items, got:\n%s", got)
	}
}

func TestHtmlToMarkdown_NestedLists(t *testing.T) {
	html := `<ul><li>Outer<ul><li>Inner</li></ul></li></ul>`
	got := htmlToMarkdown(html)
	if !strings.Contains(got, "- Outer") {
		t.Errorf("expected outer item, got:\n%s", got)
	}
	if !strings.Contains(got, "  - Inner") {
		t.Errorf("expected indented inner item, got:\n%s", got)
	}
}

func TestHtmlToMarkdown_Emphasis(t *testing.T) {
	html := `<p><strong>bold</strong> and <em>italic</em></p>`
	got := htmlToMarkdown(html)
	if !strings.Contains(got, "**bold**") {
		t.Errorf("expected **bold**, got: %q", got)
	}
	if !strings.Contains(got, "*italic*") {
		t.Errorf("expected *italic*, got: %q", got)
	}
}

func TestHtmlToMarkdown_WhitespaceCollapsing(t *testing.T) {
	html := `<p>  lots   of    spaces   </p>`
	got := htmlToMarkdown(html)
	if strings.Contains(got, "  ") {
		t.Errorf("expected collapsed whitespace, got: %q", got)
	}
	if !strings.Contains(got, "lots of spaces") {
		t.Errorf("expected clean text, got: %q", got)
	}
}

func TestHtmlToMarkdown_NestedStructure(t *testing.T) {
	html := `<section><div><ul><li>Item</li></ul></div></section>`
	got := htmlToMarkdown(html)
	if !strings.Contains(got, "- Item") {
		t.Errorf("expected nested content preserved, got:\n%s", got)
	}
}

func TestHtmlToMarkdown_EmptyInput(t *testing.T) {
	got := htmlToMarkdown("")
	if got != "" {
		t.Errorf("expected empty output for empty input, got: %q", got)
	}
}

func TestHtmlToMarkdown_MalformedHTML(t *testing.T) {
	html := `<p>Unclosed paragraph<div>and a div`
	got := htmlToMarkdown(html)
	if !strings.Contains(got, "Unclosed paragraph") {
		t.Errorf("expected graceful handling of malformed HTML, got: %q", got)
	}
	if !strings.Contains(got, "and a div") {
		t.Errorf("expected all text preserved, got: %q", got)
	}
}

func TestHtmlToMarkdown_FooterStripped(t *testing.T) {
	html := `<body><p>Main</p><footer>Copyright 2025</footer></body>`
	got := htmlToMarkdown(html)
	if strings.Contains(got, "Copyright") {
		t.Errorf("footer content should be stripped, got:\n%s", got)
	}
	if !strings.Contains(got, "Main") {
		t.Errorf("body content should be preserved, got:\n%s", got)
	}
}

func TestHtmlToMarkdown_BrEmitsNewline(t *testing.T) {
	html := `<p>Line one<br>Line two</p>`
	got := htmlToMarkdown(html)
	if !strings.Contains(got, "Line one\nLine two") {
		t.Errorf("expected br to produce newline, got: %q", got)
	}
}

func TestHtmlToMarkdown_HrEmitsRule(t *testing.T) {
	html := `<p>Above</p><hr><p>Below</p>`
	got := htmlToMarkdown(html)
	if !strings.Contains(got, "---") {
		t.Errorf("expected hr to produce ---, got:\n%s", got)
	}
}

func TestHtmlToMarkdown_HeadStripped(t *testing.T) {
	html := `<html><head><title>Page Title</title><meta charset="utf-8"></head><body><p>Body</p></body></html>`
	got := htmlToMarkdown(html)
	if strings.Contains(got, "Page Title") {
		t.Errorf("head content should be stripped, got:\n%s", got)
	}
	if !strings.Contains(got, "Body") {
		t.Errorf("body content should be preserved, got:\n%s", got)
	}
}

func TestHtmlToMarkdown_NoTripleNewlines(t *testing.T) {
	html := `<p>A</p><p></p><p></p><p>B</p>`
	got := htmlToMarkdown(html)
	if strings.Contains(got, "\n\n\n") {
		t.Errorf("expected no triple newlines, got: %q", got)
	}
}
