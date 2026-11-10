package fetch

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
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
	if strings.Contains(result.Text, "PNG") || strings.Contains(result.Text, "not really") {
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

// TestTheUserAgentFollowsTheSettings covers the user agent: a custom
// setting is the header the server sees, and a session without one
// sends the tool's generic agent.
func TestTheUserAgentFollowsTheSettings(t *testing.T) {
	var seen []string
	var mu sync.Mutex
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		seen = append(seen, r.Header.Get("User-Agent"))
		mu.Unlock()
		w.Header().Set("Content-Type", "text/plain")
		w.Write([]byte("ok"))
	}))
	defer server.Close()
	tool := New()

	fetchCall(t, tool, poo.ToolSettings{
		FetchAllowPrivateHosts: true,
		FetchUserAgent:         "probe/1.0",
	}, fmt.Sprintf(`{"url": %q}`, server.URL))
	fetchCall(t, tool, poo.ToolSettings{FetchAllowPrivateHosts: true},
		fmt.Sprintf(`{"url": %q}`, server.URL))

	mu.Lock()
	defer mu.Unlock()
	if len(seen) != 2 {
		t.Fatalf("the server saw %d requests, want 2", len(seen))
	}
	if seen[0] != "probe/1.0" {
		t.Errorf("the first fetch sent %q, want the custom agent", seen[0])
	}
	if seen[1] != DefaultUserAgent {
		t.Errorf("the second fetch sent %q, want the generic agent", seen[1])
	}
}

// TestADoomLoopKeyMergesRespellingsButNotQueries covers the key the
// doom loop counts by: one host respelled is one call, and two URLs
// that differ in the query are two.
func TestADoomLoopKeyMergesRespellingsButNotQueries(t *testing.T) {
	tool := New()
	first, err := tool.Prepare([]byte(`{"url": "https://Example.com/search?q=1"}`), poo.PrepareEnv{})
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	respelled, err := tool.Prepare([]byte(`{"url": "https://example.com:443/search?q=1"}`), poo.PrepareEnv{})
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	otherQuery, err := tool.Prepare([]byte(`{"url": "https://example.com/search?q=2"}`), poo.PrepareEnv{})
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	if string(first.(poo.CanonicalKeyer).CanonicalKey()) != string(respelled.(poo.CanonicalKeyer).CanonicalKey()) {
		t.Errorf("the respelled URL has a different key: %s vs %s", first.(poo.CanonicalKeyer).CanonicalKey(), respelled.(poo.CanonicalKeyer).CanonicalKey())
	}
	if string(first.(poo.CanonicalKeyer).CanonicalKey()) == string(otherQuery.(poo.CanonicalKeyer).CanonicalKey()) {
		t.Errorf("two queries share one key: %s", first.(poo.CanonicalKeyer).CanonicalKey())
	}
}

// TestARedirectChainPastTheLimitFails covers the redirect cap: a
// chain one hop past MaxRedirects fails naming the redirects, instead
// of riding on the client's own larger default.
func TestARedirectChainPastTheLimitFails(t *testing.T) {
	var server *httptest.Server
	var hops int32
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&hops, 1) > MaxRedirects {
			w.Write([]byte("arrived"))
			return
		}
		w.Header().Set("Location", server.URL)
		w.WriteHeader(http.StatusFound)
	}))
	defer server.Close()
	tool := New()
	result := fetchCall(t, tool, poo.ToolSettings{FetchAllowPrivateHosts: true},
		fmt.Sprintf(`{"url": %q}`, server.URL))
	if !result.IsError {
		t.Errorf("the result is %v, want the chain refused", result)
	}
	if !strings.Contains(result.Text, RedirectError) {
		t.Errorf("the result is %q, want it to name the redirects", result.Text)
	}
}

// TestAReservedNetworkIsRefused covers the ranges no setting lifts:
// 0.0.0.0/8 routes to loopback on Linux, so an address like 0.1.2.3
// reaches a service on localhost even with private hosts allowed.
// The deprecated site-local IPv6 range is refused with them.
func TestAReservedNetworkIsRefused(t *testing.T) {
	tool := New()
	for _, url := range []string{
		"http://0.1.2.3:8080/",
		"http://100.64.0.1/",
		"http://198.18.0.1/",
		"http://169.254.169.254/latest/meta-data/",
		"http://[fec0::1]/",
	} {
		result := fetchCall(t, tool, poo.ToolSettings{FetchAllowPrivateHosts: true},
			fmt.Sprintf(`{"url": %q}`, url))
		if !result.IsError {
			t.Errorf("the fetch of %s succeeded, want the range refused", url)
		}
	}
}

// TestAJSONBodyComesBackWhole covers the JSON family: an application
// json response is text the model reads, and its body comes back.
func TestAJSONBodyComesBackWhole(t *testing.T) {
	server := fixtureServer(t, fixtureAnswer{
		Status:      http.StatusOK,
		ContentType: "application/json",
		Body:        `{"answer": 42}`,
	})
	tool := New()
	result := fetchCall(t, tool, poo.ToolSettings{FetchAllowPrivateHosts: true},
		fmt.Sprintf(`{"url": %q}`, server.URL))
	if !strings.Contains(result.Text, `"answer": 42`) {
		t.Errorf("the result is %q, want the JSON body", result.Text)
	}
}

// TestABodyWithNoContentTypeIsTextWhenItsBytesAre covers the response
// that named no type: the bytes decide, and a valid UTF-8 body comes
// back.
func TestABodyWithNoContentTypeIsTextWhenItsBytesAre(t *testing.T) {
	server := fixtureServer(t, fixtureAnswer{
		Status: http.StatusOK,
		Body:   "plain words, no header",
	})
	tool := New()
	result := fetchCall(t, tool, poo.ToolSettings{FetchAllowPrivateHosts: true},
		fmt.Sprintf(`{"url": %q}`, server.URL))
	if !strings.Contains(result.Text, "plain words, no header") {
		t.Errorf("the result is %q, want the untyped body", result.Text)
	}
}

// TestABadCharsetFailsTheCallAsData covers the conversion's refusal:
// a charset the page named and the reader does not know is an error
// the model reads, not a body of mojibake.
func TestABadCharsetFailsTheCallAsData(t *testing.T) {
	server := fixtureServer(t, fixtureAnswer{
		Status:      http.StatusOK,
		ContentType: "text/html; charset=not-a-charset",
		Body:        "<p>words</p>",
	})
	tool := New()
	result := fetchCall(t, tool, poo.ToolSettings{FetchAllowPrivateHosts: true},
		fmt.Sprintf(`{"url": %q}`, server.URL))
	if !result.IsError {
		t.Errorf("the result is %v, want the conversion refused", result)
	}
	if !strings.Contains(result.Text, "not-a-charset") {
		t.Errorf("the result is %q, want it to name the charset", result.Text)
	}
}

// TestAMultiHomedHostIsRefusedOnOneBadRecord covers the fail-closed
// rule for a name with several addresses: one refused record refuses
// the host, so a domain with a private record cannot ride a public
// one past the guard.
func TestAMultiHomedHostIsRefusedOnOneBadRecord(t *testing.T) {
	restore := resolve
	resolve = func(ctx context.Context, host string) ([]net.IPAddr, error) {
		return []net.IPAddr{
			{IP: net.ParseIP("203.0.113.10")},
			{IP: net.ParseIP("10.0.0.7")},
		}, nil
	}
	defer func() { resolve = restore }()

	tool := New()
	result := fetchCall(t, tool, poo.ToolSettings{}, `{"url": "http://multi.example.com/"}`)
	if !result.IsError {
		t.Errorf("the result is %v, want the host refused on its private record", result)
	}
}

// TestAResolverFailureFailsClosed covers the resolver seam: a name the
// resolver cannot answer has no address to approve, and the fetch
// fails instead of dialing the name unguarded.
func TestAResolverFailureFailsClosed(t *testing.T) {
	restore := resolve
	resolve = func(ctx context.Context, host string) ([]net.IPAddr, error) {
		return nil, fmt.Errorf("the resolver is down")
	}
	defer func() { resolve = restore }()

	tool := New()
	result := fetchCall(t, tool, poo.ToolSettings{FetchAllowPrivateHosts: true},
		`{"url": "http://unreachable.example.com/"}`)
	if !result.IsError {
		t.Errorf("the result is %v, want the resolution failure as the error", result)
	}
	if !strings.Contains(result.Text, "resolving") {
		t.Errorf("the result is %q, want it to name the resolution", result.Text)
	}
}

// TestADialThroughAProxiedContextIsExempt covers the proxy exemption:
// a request whose context marks the proxy dials the proxy's private
// address without a refusal, while an unmarked request is refused —
// the proxy is the operator's infrastructure, and the target's host
// keeps its own check.
func TestADialThroughAProxiedContextIsExempt(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("through the proxy"))
	}))
	defer server.Close()
	dialer := &net.Dialer{Timeout: time.Second}

	guarded := guardedDial(dialer, false)
	host, port, err := net.SplitHostPort(server.Listener.Addr().String())
	if err != nil {
		t.Fatalf("splitting the address: %v", err)
	}
	_, err = guarded(context.Background(), "tcp", net.JoinHostPort(host, port))
	var refused *refusedError
	if !errors.As(err, &refused) {
		t.Errorf("the unmarked dial is %v, want the loopback refused", err)
	}

	markedConn, err := guarded(context.WithValue(context.Background(), proxiedKey{}, true), "tcp", net.JoinHostPort(host, port))
	if err != nil {
		t.Fatalf("the marked dial failed: %v", err)
	}
	markedConn.Close()
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
