// Package fetch implements the fetch tool: an HTTP(S) GET whose body
// comes back as text, with HTML converted to Markdown.
//
// The tool is read-only: it changes nothing, so the permission engine
// allows it without a rule, and a batch of nested calls runs it
// concurrently. What bounds it is what it refuses: a private network
// address, a body that is not text, and a body past the download limit.
package fetch

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/internal/urlnorm"
	"github.com/sdougbrown/poo/prompt"
)

// Defaults for the fetch tool.
const (
	// DefaultTimeoutS is how long one fetch waits, in seconds.
	DefaultTimeoutS = 30
	// DefaultMaxBytes is the largest body one fetch reads.
	DefaultMaxBytes = 4 << 20
	// DefaultUserAgent is the user agent the tool sends. It is generic
	// on purpose: it carries nothing about the user.
	DefaultUserAgent = "poo-fetch"
	// MaxRedirects is how many redirects one fetch follows.
	MaxRedirects = 5
	// RedirectError is what a fetch past MaxRedirects fails with.
	RedirectError = "too many redirects"
)

// The formats the tool returns.
const (
	// FormatRaw is the body as it came, for anything that is already
	// text.
	FormatRaw = "raw"
	// FormatMarkdown is the body converted from HTML to Markdown.
	FormatMarkdown = "markdown"
)

// Tool is the fetch tool.
type Tool struct {
	// Client is the HTTP client the tool fetches with. Nil means one
	// the tool builds: the transport honors the proxy environment, and
	// a check refuses the private network addresses the settings do
	// not allow.
	Client *http.Client

	// strictClient and permissiveClient are the default clients, built
	// once at the posture the settings name and kept for the pool they
	// hold.
	strictClient     *http.Client
	permissiveClient *http.Client
	strict           sync.Once
	permissive       sync.Once
}

// New returns a fetch tool.
func New() *Tool { return &Tool{} }

// Spec describes the tool.
func (t *Tool) Spec() poo.ToolSpec {
	return poo.ToolSpec{
		Name:        "fetch",
		Description: "Fetch a URL over HTTP(S) and return its body; HTML pages come back as Markdown.",
		Schema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "url": {"type": "string", "description": "The URL to fetch."},
    "format": {"type": "string", "enum": ["raw", "markdown"], "description": "raw returns the body as it came; markdown converts an HTML page to Markdown. The default is markdown for an HTML response and raw for anything else."}
  },
  "required": ["url"]
}`),
		Class:      poo.ClassReadOnly,
		PrimaryArg: "url",
	}
}

// PromptSection renders the tool's own section of the system prompt.
func (t *Tool) PromptSection(env prompt.Env) string { return section.Render(env) }

// args are the tool's arguments.
type args struct {
	URL    string `json:"url"`
	Format string `json:"format,omitempty"`
}

// prepared is a validated fetch call.
type prepared struct {
	// raw is the URL as it was written.
	raw string
	// parsed is the URL the call fetches.
	parsed *url.URL
	// format is the format the call asks for, empty for the default.
	format string
}

// Summary describes the call for a human reader.
func (p *prepared) Summary() string {
	if p.format == "" {
		return p.raw
	}
	return p.raw + " (" + p.format + ")"
}

// CanonicalKey returns the call's prepared input.
//
// The URL is the canonical form, so one host spelled the ways a URL
// can be spelled counts once toward the doom loop: the rule matcher
// reads the same form, and the two consumers of a URL's spelling
// agree.
func (p *prepared) CanonicalKey() json.RawMessage {
	url := p.raw
	if canonical, ok := urlnorm.Canonical(p.raw); ok {
		url = canonical
	}
	key, _ := json.Marshal(struct {
		URL    string `json:"url"`
		Format string `json:"format,omitempty"`
	}{url, p.format})
	return key
}

// EvidenceArguments returns the arguments a decision model may see.
func (p *prepared) EvidenceArguments() json.RawMessage { return p.CanonicalKey() }

// Prepare validates the arguments. It has no side effects.
func (t *Tool) Prepare(raw json.RawMessage, env poo.PrepareEnv) (poo.Prepared, error) {
	var a args
	if err := decodeStrict(raw, &a); err != nil {
		return nil, poo.InvalidArgsf("fetch: %v", err)
	}
	if strings.TrimSpace(a.URL) == "" {
		return nil, poo.InvalidArgsf("fetch: url is empty")
	}
	parsed, err := url.Parse(a.URL)
	if err != nil {
		return nil, poo.InvalidArgsf("fetch: the url %q is malformed: %v", a.URL, err)
	}
	switch parsed.Scheme {
	case "http", "https":
	case "":
		return nil, poo.InvalidArgsf("fetch: the url %q has no scheme", a.URL)
	default:
		return nil, poo.InvalidArgsf("fetch: the url %q is not http or https", a.URL)
	}
	switch a.Format {
	case "", FormatRaw, FormatMarkdown:
	default:
		return nil, poo.InvalidArgsf("fetch: format must be %s or %s, not %q", FormatRaw, FormatMarkdown, a.Format)
	}
	return &prepared{raw: a.URL, parsed: parsed, format: a.Format}, nil
}

// Call fetches the URL.
//
// The result text is a header of fields, then the body; the structured
// payload holds the same fields as JSON. A body the cap cannot hold is
// elided the way the bash observation is, and the header says so.
func (t *Tool) Call(ctx context.Context, in poo.Prepared, env poo.ToolEnv) (poo.ToolResult, error) {
	p, ok := in.(*prepared)
	if !ok {
		return poo.ToolResult{}, fmt.Errorf("fetch: prepared value is not a fetch call")
	}

	timeout := DefaultTimeoutS
	if env.Settings.FetchTimeoutS > 0 {
		timeout = env.Settings.FetchTimeoutS
	}
	maxBytes := int64(DefaultMaxBytes)
	if env.Settings.FetchMaxBytes > 0 {
		maxBytes = int64(env.Settings.FetchMaxBytes)
	}
	userAgent := env.Settings.FetchUserAgent
	if userAgent == "" {
		userAgent = DefaultUserAgent
	}

	client := t.client(env.Settings.FetchAllowPrivateHosts)
	ctx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.parsed.String(), nil)
	if err != nil {
		return poo.ToolResult{Text: fmt.Sprintf("fetch: %v", err), IsError: true}, nil
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "*/*")

	// The request's own host is checked as well as the dial. A proxy
	// configured in the environment makes the dial the proxy's address,
	// and the target's host then goes unchecked there.
	if err := checkHost(ctx, p.parsed.Hostname(), env.Settings.FetchAllowPrivateHosts); err != nil {
		return poo.ToolResult{Text: err.Error(), IsError: true}, nil
	}

	resp, err := client.Do(req)
	if err != nil {
		return poo.ToolResult{Text: fmt.Sprintf("fetch: %v", err), IsError: true}, nil
	}
	defer resp.Body.Close()

	observation := Observation{
		Status:      resp.StatusCode,
		FinalURL:    resp.Request.URL.String(),
		ContentType: resp.Header.Get("Content-Type"),
	}

	body, truncated, err := readBody(resp.Body, maxBytes)
	if err != nil {
		return poo.ToolResult{Text: fmt.Sprintf("fetch: %v", err), IsError: true}, nil
	}
	observation.Truncated = truncated
	observation.Size = int64(len(body))

	format := p.format
	if format == "" {
		format = defaultFormat(observation.ContentType)
	}
	// A body that is not text has no format worth applying. The tool
	// returns its metadata alone, so a binary download does not flood
	// the context.
	if keepBody(observation.ContentType, body) {
		if format == FormatMarkdown {
			markdown, convErr := convert(body, observation.ContentType)
			if convErr != nil {
				return poo.ToolResult{Text: "fetch: " + convErr.Error(), IsError: true}, nil
			}
			observation.Body = markdown
		} else {
			observation.Body = string(body)
		}
	}

	text, structured := envelope(observation, env.Settings.ObservationCapChars)
	return poo.ToolResult{Text: text, Structured: structured}, nil
}

// defaultFormat picks the format a response comes back in when the
// call does not name one. An HTML page converts to Markdown, and
// anything else comes back as it came.
func defaultFormat(contentType string) string {
	if mediaType(contentType) == "text/html" {
		return FormatMarkdown
	}
	return FormatRaw
}

// keepBody reports whether a response's body is text the model reads.
// A body of anything else is metadata alone.
func keepBody(contentType string, body []byte) bool {
	media := mediaType(contentType)
	switch {
	case strings.HasPrefix(media, "text/"):
		return true
	case media == "application/json", media == "application/xml",
		media == "application/javascript",
		strings.HasSuffix(media, "+json"), strings.HasSuffix(media, "+xml"):
		return true
	default:
		// A response that named no type, or named one this check does
		// not know, is text when its bytes are.
		return utf8.Valid(body)
	}
}

// mediaType returns the media type a Content-Type header names, or
// the empty string when the header is absent or malformed.
func mediaType(contentType string) string {
	media, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		return ""
	}
	return media
}

// decodeStrict decodes one JSON value and nothing else.
func decodeStrict(raw json.RawMessage, into any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(into); err != nil {
		return err
	}
	if decoder.More() {
		return fmt.Errorf("the arguments hold more than one value")
	}
	return nil
}

// client returns the HTTP client the call fetches with.
//
// A tool built with its own client gets it. The default clients are
// built once per posture and kept, so a session's fetches share
// connections. A fresh transport per call would drop the pool and
// leave its idle connections to the garbage collector.
//
// The transport dials a checked address, so a URL that names loopback,
// link-local or a private network fails before a connection is made.
// A redirect to one is included. The client's redirect check repeats
// the request's own host check at every hop. The reason is the proxy:
// an environment proxy makes the dial the proxy's address, and the
// target's host goes unchecked there without the check.
func (t *Tool) client(allowPrivate bool) *http.Client {
	if t.Client != nil {
		return t.Client
	}
	if allowPrivate {
		t.permissive.Do(func() { t.permissiveClient = t.build(true) })
		return t.permissiveClient
	}
	t.strict.Do(func() { t.strictClient = t.build(false) })
	return t.strictClient
}

// build returns a client with the guard at the posture the settings
// name.
func (t *Tool) build(allowPrivate bool) *http.Client {
	dialer := &net.Dialer{Timeout: 10 * time.Second}
	transport := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           guardedDial(dialer, allowPrivate),
		ForceAttemptHTTP2:     true,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 10 * time.Second,
	}
	return &http.Client{
		Transport: transport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= MaxRedirects {
				return fmt.Errorf("fetch: %s", RedirectError)
			}
			if err := checkHost(req.Context(), req.URL.Hostname(), allowPrivate); err != nil {
				return err
			}
			return nil
		},
	}
}

// readBody reads a body up to maxBytes, and reports that it stopped
// early.
func readBody(body io.Reader, maxBytes int64) ([]byte, bool, error) {
	read, err := io.ReadAll(io.LimitReader(body, maxBytes+1))
	if err != nil {
		return nil, false, err
	}
	return read, int64(len(read)) > maxBytes, nil
}
