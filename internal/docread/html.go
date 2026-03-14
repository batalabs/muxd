package docread

import (
	"fmt"
	"os"
	"strings"

	"golang.org/x/net/html"
)

// blockElements are HTML elements that should be followed by a newline.
var blockElements = map[string]bool{
	"p": true, "div": true, "section": true, "article": true,
	"header": true, "footer": true, "nav": true, "main": true,
	"h1": true, "h2": true, "h3": true, "h4": true, "h5": true, "h6": true,
	"li": true, "tr": true, "br": true, "hr": true,
	"blockquote": true, "pre": true, "figure": true,
}

// skipElements are HTML elements whose content should be ignored entirely.
var skipElements = map[string]bool{
	"script": true, "style": true, "noscript": true,
}

// extractHTML extracts readable text from an HTML file.
// Block elements add newlines. Links are rendered as [text](href).
func extractHTML(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("opening HTML: %w", err)
	}
	defer f.Close()

	doc, err := html.Parse(f)
	if err != nil {
		return "", fmt.Errorf("parsing HTML: %w", err)
	}

	var sb strings.Builder
	walkHTML(&sb, doc)

	// Collapse runs of 3+ newlines down to 2.
	result := sb.String()
	for strings.Contains(result, "\n\n\n") {
		result = strings.ReplaceAll(result, "\n\n\n", "\n\n")
	}
	return strings.TrimSpace(result) + "\n", nil
}

// walkHTML recursively walks the HTML node tree and writes text to sb.
func walkHTML(sb *strings.Builder, n *html.Node) {
	if n.Type == html.ElementNode && skipElements[n.Data] {
		return
	}

	if n.Type == html.ElementNode && n.Data == "a" {
		// Render link as [text](href).
		href := attrVal(n, "href")
		var linkText strings.Builder
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			collectText(&linkText, c)
		}
		text := strings.TrimSpace(linkText.String())
		if text != "" && href != "" {
			sb.WriteString(fmt.Sprintf("[%s](%s)", text, href))
		} else if text != "" {
			sb.WriteString(text)
		}
		// Skip children — we already processed them.
		if blockElements[n.Data] {
			sb.WriteByte('\n')
		}
		return
	}

	if n.Type == html.TextNode {
		text := strings.TrimSpace(n.Data)
		if text != "" {
			sb.WriteString(text)
			sb.WriteByte(' ')
		}
		return
	}

	for c := n.FirstChild; c != nil; c = c.NextSibling {
		walkHTML(sb, c)
	}

	if n.Type == html.ElementNode && blockElements[n.Data] {
		sb.WriteByte('\n')
	}
}

// collectText gathers all text content from a subtree.
func collectText(sb *strings.Builder, n *html.Node) {
	if n.Type == html.ElementNode && skipElements[n.Data] {
		return
	}
	if n.Type == html.TextNode {
		sb.WriteString(n.Data)
		return
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		collectText(sb, c)
	}
}

// attrVal returns the value of the named attribute, or "".
func attrVal(n *html.Node, name string) string {
	for _, a := range n.Attr {
		if a.Key == name {
			return a.Val
		}
	}
	return ""
}
