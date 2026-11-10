package fetch

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/perm"
)

// fixtureServer starts an HTTP server whose answers are the calls', in
// order, the way the wire tests script answers. The last answer
// repeats, so a test that does not script every request still gets one.
func fixtureServer(t *testing.T, answers ...fixtureAnswer) *httptest.Server {
	t.Helper()
	i := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "only GET", http.StatusMethodNotAllowed)
			return
		}
		if i >= len(answers) {
			i = len(answers) - 1
		}
		answer := answers[i]
		i++
		if answer.Location != "" {
			w.Header().Set("Location", answer.Location)
		}
		if answer.ContentType != "" {
			w.Header().Set("Content-Type", answer.ContentType)
		}
		w.WriteHeader(answer.Status)
		w.Write([]byte(answer.Body))
	}))
	t.Cleanup(server.Close)
	return server
}

// fixtureAnswer is one scripted response.
type fixtureAnswer struct {
	Status      int
	ContentType string
	Location    string
	Body        string
}

// fetchCall prepares and runs one call against the tool, with the
// settings a test names.
func fetchCall(t *testing.T, tool *Tool, settings poo.ToolSettings, arguments string) poo.ToolResult {
	t.Helper()
	prepared, err := tool.Prepare([]byte(arguments), poo.PrepareEnv{})
	if err != nil {
		t.Fatalf("prepare %s: %v", arguments, err)
	}
	result, err := tool.Call(context.Background(), prepared, poo.ToolEnv{Settings: settings})
	if err != nil {
		t.Fatalf("call %s: %v", arguments, err)
	}
	return result
}

// structuredOf decodes a result's structured payload.
func structuredOf(t *testing.T, result poo.ToolResult) map[string]any {
	t.Helper()
	var structured map[string]any
	if err := json.Unmarshal(result.Structured, &structured); err != nil {
		t.Fatalf("the structured payload is not JSON: %v", err)
	}
	return structured
}

// TestAFetchReturnsTheBodyAndTheHeader covers the plain case: a text
// response comes back whole, with the header the model reads and the
// structured payload beside it.
func TestAFetchReturnsTheBodyAndTheHeader(t *testing.T) {
	server := fixtureServer(t, fixtureAnswer{
		Status:      http.StatusOK,
		ContentType: "text/plain; charset=utf-8",
		Body:        "hello",
	})
	tool := New()
	result := fetchCall(t, tool, poo.ToolSettings{FetchAllowPrivateHosts: true},
		fmt.Sprintf(`{"url": %q}`, server.URL))

	for _, want := range []string{
		"status: 200",
		"final_url: " + server.URL,
		"content_type: text/plain; charset=utf-8",
		"size: 5",
		"hello",
	} {
		if !strings.Contains(result.Text, want) {
			t.Errorf("the result is %q, want it to hold %q", result.Text, want)
		}
	}
	structured := structuredOf(t, result)
	if structured["body"] != "hello" {
		t.Errorf("the structured body is %v, want hello", structured["body"])
	}
	if structured["status"] != float64(200) {
		t.Errorf("the structured status is %v, want 200", structured["status"])
	}
}

// TestAnHTMLPageComesBackAsMarkdownUnlessRawCovers the format default:
// an HTML page converts, and a call that names raw gets the body as it
// came.
func TestAnHTMLPageComesBackAsMarkdownUnlessRaw(t *testing.T) {
	server := fixtureServer(t, fixtureAnswer{
		Status:      http.StatusOK,
		ContentType: "text/html; charset=utf-8",
		Body:        "<html><body><h1>Title</h1><p>Hello</p></body></html>",
	})
	tool := New()
	result := fetchCall(t, tool, poo.ToolSettings{FetchAllowPrivateHosts: true},
		fmt.Sprintf(`{"url": %q}`, server.URL))
	if !strings.Contains(result.Text, "# Title") || !strings.Contains(result.Text, "Hello") {
		t.Errorf("the HTML page did not convert to Markdown: %q", result.Text)
	}

	raw := fetchCall(t, tool, poo.ToolSettings{FetchAllowPrivateHosts: true},
		fmt.Sprintf(`{"url": %q, "format": "raw"}`, server.URL))
	if !strings.Contains(raw.Text, "<h1>Title</h1>") {
		t.Errorf("the raw fetch kept the HTML: %q", raw.Text)
	}
}

// TestARedirectNamesWhereTheBodyCameFrom covers the redirect: the
// header's final_url is the response the body came from, not the URL
// the call named.
func TestARedirectNamesWhereTheBodyCameFrom(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.Write([]byte("arrived"))
	}))
	defer target.Close()
	server := fixtureServer(t, fixtureAnswer{
		Status:   http.StatusFound,
		Location: target.URL,
	})
	tool := New()
	result := fetchCall(t, tool, poo.ToolSettings{FetchAllowPrivateHosts: true},
		fmt.Sprintf(`{"url": %q}`, server.URL))
	if !strings.Contains(result.Text, "final_url: "+target.URL) {
		t.Errorf("the result is %q, want the final_url %q", result.Text, target.URL)
	}
	if !strings.Contains(result.Text, "arrived") {
		t.Errorf("the result is %q, want the redirected body", result.Text)
	}
}

// TestABodyPastTheDownloadLimitIsCutAndSaysSo covers the download
// limit: the body stops at max_bytes, and the header and the payload
// both say the body was cut.
func TestABodyPastTheDownloadLimitIsCutAndSaysSo(t *testing.T) {
	server := fixtureServer(t, fixtureAnswer{
		Status:      http.StatusOK,
		ContentType: "text/plain",
		Body:        strings.Repeat("a", 100),
	})
	tool := New()
	result := fetchCall(t, tool, poo.ToolSettings{
		FetchAllowPrivateHosts: true,
		FetchMaxBytes:          40,
	}, fmt.Sprintf(`{"url": %q}`, server.URL))
	if !strings.Contains(result.Text, "truncated: true") {
		t.Errorf("the result is %q, want the truncation in the header", result.Text)
	}
	structured := structuredOf(t, result)
	if structured["truncated"] != true {
		t.Errorf("the structured payload is %v, want truncated", structured)
	}
	if structured["size"] != float64(41) {
		t.Errorf("the size is %v, want the limit plus the one byte that said so", structured["size"])
	}
	if strings.Contains(result.Text, strings.Repeat("a", 50)) {
		t.Errorf("the result is %q, want the body cut at the limit", result.Text)
	}
}

// TestABodyPastTheCapKeepsItsHeadAndTail covers the observation cap: a
// long body is elided to its head and tail, and the payload says how
// much the middle held.
func TestABodyPastTheCapKeepsItsHeadAndTail(t *testing.T) {
	server := fixtureServer(t, fixtureAnswer{
		Status:      http.StatusOK,
		ContentType: "text/plain",
		Body:        strings.Repeat("a", 10) + strings.Repeat("b", 2000) + strings.Repeat("c", 10),
	})
	tool := New()
	result := fetchCall(t, tool, poo.ToolSettings{
		FetchAllowPrivateHosts: true,
		ObservationCapChars:    500,
	}, fmt.Sprintf(`{"url": %q}`, server.URL))
	structured := structuredOf(t, result)
	if body, ok := structured["body"].(string); ok {
		t.Errorf("the structured payload holds a whole body of %d characters, want head and tail", len(body))
	}
	head, _ := structured["body_head"].(string)
	tail, _ := structured["body_tail"].(string)
	if !strings.HasPrefix(head, "aaaaaaaaaa") || !strings.HasSuffix(tail, "cccccccccc") {
		t.Errorf("the head is %q and the tail is %q, want the body's ends", head, tail)
	}
	if !strings.Contains(result.Text, "elided_chars: ") {
		t.Errorf("the result is %q, want the elided count in the header", result.Text)
	}
}

// TestATimeoutFailsAsData covers the timeout: a fetch that outlasts it
// is an error result the model reads, and not a failure of the call.
func TestATimeoutFailsAsData(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(2 * time.Second)
		w.Write([]byte("late"))
	}))
	defer server.Close()
	tool := New()
	result := fetchCall(t, tool, poo.ToolSettings{FetchTimeoutS: 1, FetchAllowPrivateHosts: true},
		fmt.Sprintf(`{"url": %q}`, server.URL))
	if !result.IsError {
		t.Errorf("the result is %v, want an error the model reads", result)
	}
	if !strings.Contains(result.Text, "deadline") && !strings.Contains(result.Text, "timeout") {
		t.Errorf("the result is %q, want the timeout in the text", result.Text)
	}
}

// TestABinaryBodyComesBackAsMetadata covers the refusal: a body that
// is not text is not read, and the result carries the fields that say
// what it was.
func TestABinaryBodyComesBackAsMetadata(t *testing.T) {
	server := fixtureServer(t, fixtureAnswer{
		Status:      http.StatusOK,
		ContentType: "image/png",
		Body:        "\x89PNG\r\n\x1a\nnot really an image",
	})
	tool := New()
	result := fetchCall(t, tool, poo.ToolSettings{FetchAllowPrivateHosts: true},
		fmt.Sprintf(`{"url": %q}`, server.URL))
	if result.IsError {
		t.Errorf("the result is an error, want the metadata as data: %v", result)
	}
	if !strings.Contains(result.Text, "content_type: image/png") {
		t.Errorf("the result is %q, want the content type in the header", result.Text)
	}
	if strings.Contains(result.Text, "PNG") && strings.Contains(result.Text, "not really") {
		t.Errorf("the result is %q, want the body withheld", result.Text)
	}
	structured := structuredOf(t, result)
	if _, ok := structured["body"]; ok {
		t.Errorf("the structured payload holds a body, want metadata alone")
	}
}

// TestARefusedAddressFailsBeforeItDials covers the guard: a URL that
// names loopback is refused, and the refusal names the setting that
// lifts it. The server's handler counts the requests it saw, so the
// test tells a refusal before the dial from a refusal after it.
func TestARefusedAddressFailsBeforeItDials(t *testing.T) {
	var dials int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&dials, 1)
	}))
	defer server.Close()
	tool := New()
	result := fetchCall(t, tool, poo.ToolSettings{}, fmt.Sprintf(`{"url": %q}`, server.URL))
	if !result.IsError {
		t.Errorf("the result is %v, want a refusal", result)
	}
	if !strings.Contains(result.Text, "allow_private_hosts") {
		t.Errorf("the refusal is %q, want it to name the setting", result.Text)
	}
	if atomic.LoadInt32(&dials) != 0 {
		t.Errorf("the server saw %d requests, want none: the refusal came after the dial", dials)
	}
}

// TestARedirectToARefusedAddressIsRefused covers the redirect hop: the
// request's own host is checked at every redirect, so a page cannot
// send the fetch to an address the first URL could not name. The
// target is link-local, which no setting allows.
func TestARedirectToARefusedAddressIsRefused(t *testing.T) {
	server := fixtureServer(t, fixtureAnswer{
		Status:   http.StatusFound,
		Location: "http://169.254.169.254/latest/meta-data/",
	})
	tool := New()
	result := fetchCall(t, tool, poo.ToolSettings{FetchAllowPrivateHosts: true},
		fmt.Sprintf(`{"url": %q}`, server.URL))
	if !result.IsError {
		t.Errorf("the result is %v, want the redirect refused", result)
	}
	if !strings.Contains(result.Text, "169.254") {
		t.Errorf("the result is %q, want it to name the refused address", result.Text)
	}
}

// TestARuleMatchesTheURL covers the permission shape the tool exists
// for: a rule matches a fetch by the URL it names, so an operator can
// refuse a prefix and let the rest ride on the read-only class.
func TestARuleMatchesTheURL(t *testing.T) {
	tool := New()
	spec := tool.Spec()
	rule := []perm.Rule{{Tool: "fetch", CommandPrefix: "https://docs.example.com/", Action: perm.ActionDeny}}
	policy, err := perm.New(perm.Options{Rules: rule})
	if err != nil {
		t.Fatalf("the policy: %v", err)
	}
	for _, url := range []string{"https://docs.example.com/index.html", "https://docs.example.com/guide/intro.html"} {
		info := poo.ToolCallInfo{
			Spec:      spec,
			Arguments: []byte(fmt.Sprintf(`{"url": %q}`, url)),
		}
		decision, err := policy.BeforeTool(poo.HookContext{}, info)
		if err != nil {
			t.Fatalf("decide %s: %v", url, err)
		}
		if decision.Allowed {
			t.Errorf("the fetch of %s is allowed, want the prefix rule to refuse it", url)
		}
	}
	// A fetch the rule does not match rides on the read-only class.
	info := poo.ToolCallInfo{
		Spec:      spec,
		Arguments: []byte(`{"url": "https://other.example.com/"}`),
	}
	decision, err := policy.BeforeTool(poo.HookContext{}, info)
	if err != nil {
		t.Fatalf("decide: %v", err)
	}
	if !decision.Allowed {
		t.Errorf("the fetch of https://other.example.com/ is refused, want the read-only class to allow it")
	}
}

// TestPrepareRefusesWhatItCannotFetch covers the validation: a URL
// without a scheme, a scheme that is not HTTP(S), and an unknown
// format are failures the model corrects, and nothing runs.
func TestPrepareRefusesWhatItCannotFetch(t *testing.T) {
	tool := New()
	for _, arguments := range []string{
		`{"url": "ftp://example.com/file"}`,
		`{"url": "example.com/page"}`,
		`{"url": "https://example.com/", "format": "html"}`,
		`{"url": "", "format": "raw"}`,
		`{"url": "https://example.com/", "deep": true}`,
	} {
		if _, err := tool.Prepare([]byte(arguments), poo.PrepareEnv{}); err == nil {
			t.Errorf("prepare %s succeeded, want a failure the model corrects", arguments)
		}
	}
}
