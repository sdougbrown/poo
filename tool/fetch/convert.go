// The conversion turns an HTML body into Markdown.
package fetch

import (
	"bytes"
	"fmt"
	"io"
	"mime"
	"strings"

	htmltomarkdown "github.com/JohannesKaufmann/html-to-markdown/v2"
	"golang.org/x/net/html/charset"
)

// convert renders an HTML body as Markdown.
//
// The content type names the charset the body was sent in, and the
// conversion reads the body as that: a page that says iso-8859-1 and
// means it keeps its letters.
func convert(body []byte, contentType string) (string, error) {
	var input io.Reader = bytes.NewReader(body)
	if _, params, err := mime.ParseMediaType(contentType); err == nil {
		if label := params["charset"]; label != "" && !strings.EqualFold(label, "utf-8") {
			decoded, err := charset.NewReaderLabel(label, input)
			if err != nil {
				return "", fmt.Errorf("the page named the charset %q, and it is not one: %w", label, err)
			}
			input = decoded
		}
	}
	markdown, err := htmltomarkdown.ConvertReader(input)
	if err != nil {
		return "", fmt.Errorf("the page could not be read as HTML: %w", err)
	}
	return string(markdown), nil
}
