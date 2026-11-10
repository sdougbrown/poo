package fetch

import (
	"strings"
	"testing"
)

// TestAConvertKeepsTheCharsetThePageNames covers the charset: a page
// that names iso-8859-1 and means it keeps its letters, instead of
// turning them into mojibake.
func TestAConvertKeepsTheCharsetThePageNames(t *testing.T) {
	// "crème brûlée" in iso-8859-1.
	body := []byte("<html><body><p>cr\xe8me br\xfbl\xe9e</p></body></html>")
	markdown, err := convert(body, "text/html; charset=iso-8859-1")
	if err != nil {
		t.Fatalf("the conversion: %v", err)
	}
	if !strings.Contains(markdown, "crème brûlée") {
		t.Errorf("the markdown is %q, want the letters the page meant", markdown)
	}
}

// TestAConvertOfAPlainBodyPassesThrough covers the body that is not
// HTML: a page that converts when the model asked for Markdown but the
// response was plain text comes back readable, and not as an error.
func TestAConvertOfAPlainBodyPassesThrough(t *testing.T) {
	markdown, err := convert([]byte("just some words"), "text/plain")
	if err != nil {
		t.Fatalf("the conversion: %v", err)
	}
	if !strings.Contains(markdown, "just some words") {
		t.Errorf("the markdown is %q, want the words", markdown)
	}
}
