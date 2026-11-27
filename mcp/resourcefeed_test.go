package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/exec"
	"github.com/sdougbrown/poo/feed"
	"github.com/sdougbrown/poo/mcp/manifest"

	sdkjsonrpc "github.com/modelcontextprotocol/go-sdk/jsonrpc"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

// inboxURI is the drain queue the fixture's push tool appends to.
const inboxURI = "fixture://inbox"

// blobURI is the resource that answers one blob content.
const blobURI = "fixture://blob"

// errEmitRefused is the refusal a recording feed's Emit hands back, so
// a test can tell its own refusal from the transport's failures.
var errEmitRefused = errors.New("the test refused the emit")

// lockedBuffer is a log sink several goroutines may write: the feed's
// log and the connection's both reach it.
type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// recordingFeed is the feed side a test reads: every emit is kept and
// signalled, so a test waits on the delivery rather than a guess, and
// the emit may be made to refuse.
type recordingFeed struct {
	mu      sync.Mutex
	emitted []string
	// cursor is how many emits the test has already taken, so a wait
	// reads the delivery it waited for even when several landed at once.
	cursor int
	signal chan struct{}
	refuse bool
}

// newFeedIO builds the FeedIO one runner is driven with, and the feed
// that records what it emitted.
func newFeedIO(refuse bool) (*recordingFeed, poo.FeedIO, *lockedBuffer) {
	feed := &recordingFeed{signal: make(chan struct{}, 64), refuse: refuse}
	logs := &lockedBuffer{}
	io := poo.FeedIO{
		Dir:       os.TempDir(),
		SessionID: "s-42",
		Emit: func(_ string, text string, _ *poo.FeedImage, _ json.RawMessage) error {
			if feed.refuse {
				return errEmitRefused
			}
			feed.mu.Lock()
			feed.emitted = append(feed.emitted, text)
			feed.mu.Unlock()
			select {
			case feed.signal <- struct{}{}:
			default:
			}
			return nil
		},
		Log: slog.New(slog.NewTextHandler(logs, nil)),
	}
	return feed, io, logs
}

// texts returns everything the feed emitted, in order.
func (f *recordingFeed) texts() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.emitted...)
}

// waitEmit waits for one more emit to land and returns it, in the
// order the feed delivered. A feed that never delivers fails the test
// from the wait, not from a sleep.
func (f *recordingFeed) waitEmit(t *testing.T, what string) string {
	t.Helper()
	select {
	case <-f.signal:
	case <-time.After(15 * time.Second):
		t.Fatalf("the feed never emitted %s; it emitted %q", what, f.texts())
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.cursor >= len(f.emitted) {
		t.Fatalf("an emit was signalled and none was recorded")
	}
	text := f.emitted[f.cursor]
	f.cursor++
	return text
}

// feedLaunch is one fixture server a resource feed runs against.
type feedLaunch struct {
	group *Group
	dir   string
	// protocol names the revision the fixture was forced to, for the
	// failure messages a test reads.
	protocol string
}

// launchFeed starts the fixture server forced to one protocol revision,
// the way the session's own launch starts it.
func launchFeed(t *testing.T, protocol string) *feedLaunch {
	t.Helper()
	binary := buildFixture(t, "server")
	dir := t.TempDir()
	// The templates are literals: the values a session resolves are
	// not this test's subject.
	tmpl := func(source string) manifest.Template {
		parsed, err := manifest.ParseTemplate(source)
		if err != nil {
			t.Fatalf("parsing the template %q: %v", source, err)
		}
		return parsed
	}
	env := map[string]manifest.Template{
		"POO_FIXTURE_PIDFILE": tmpl(filepath.Join(dir, "fixture.pid")),
	}
	if protocol != "" {
		env["POO_FIXTURE_PROTOCOL"] = tmpl(protocol)
	}
	g, err := Launch(context.Background(), LaunchOptions{
		SessionID: "s-42",
		Dir:       dir,
		HostEnv: func(name string) (string, bool) {
			if name == "FIXTURE" {
				return binary, true
			}
			return exec.Lookup(os.Environ())(name)
		},
		Servers: []manifest.Server{{
			Name:    "fixture",
			Command: tmpl("${FIXTURE}"),
			Env:     env,
		}},
	})
	if err != nil {
		t.Fatalf("launching the fixture: %v", err)
	}
	t.Cleanup(func() { _ = g.Close() })
	return &feedLaunch{group: g, dir: dir, protocol: protocol}
}

// push queues one message on the fixture's inbox and updates it.
func (l *feedLaunch) push(t *testing.T, text string) {
	t.Helper()
	if _, err := l.group.CallTool(context.Background(), "fixture/push", json.RawMessage(`{"text":`+quote(text)+`}`)); err != nil {
		t.Fatalf("pushing %q: %v", text, err)
	}
}

// bump advances the fixture's counter and updates its resource.
func (l *feedLaunch) bump(t *testing.T) {
	t.Helper()
	if _, err := l.group.CallTool(context.Background(), "fixture/bump", json.RawMessage(`{}`)); err != nil {
		t.Fatalf("bumping the counter: %v", err)
	}
}

// feedStats are the fixture's own counters: what the server did, which
// is what a test waits on instead of a delivery that never comes.
type feedStats struct {
	Reads        int64 `json:"reads"`
	BlobReads    int64 `json:"blob_reads"`
	Unsubscribes int64 `json:"unsubscribes"`
	Queued       int64 `json:"queued"`
}

// stats reads the fixture's counters.
func (l *feedLaunch) stats(t *testing.T) feedStats {
	t.Helper()
	res, err := l.group.CallTool(context.Background(), "fixture/stats", json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("reading the fixture's stats: %v", err)
	}
	var counters feedStats
	if err := json.Unmarshal([]byte(res.Text[0]), &counters); err != nil {
		t.Fatalf("the fixture's stats are not counters: %v\n%s", err, res.Text[0])
	}
	return counters
}

// waitStats waits for the fixture's counters to satisfy the condition.
func (l *feedLaunch) waitStats(t *testing.T, what string, cond func(feedStats) bool) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for {
		counters := l.stats(t)
		if cond(counters) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s (reads=%d unsubscribes=%d queued=%d)",
				what, counters.Reads, counters.Unsubscribes, counters.Queued)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// waitDrained waits for the fixture's queue to hold nothing: the read
// for what was pushed last answered. The read count is not the marker —
// one read answers every update queued while it ran, so how many reads
// a batch takes is the transport's business and not the test's.
func (l *feedLaunch) waitDrained(t *testing.T) {
	t.Helper()
	l.waitStats(t, "the queue to be drained", func(counted feedStats) bool { return counted.Queued == 0 })
}

// runFeed runs one attempt of a feed and hands its error back, so a
// test joins on the attempt's own end.
func runFeed(t *testing.T, run poo.FeedRunner, io poo.FeedIO) (context.CancelFunc, <-chan error) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- run(ctx, io) }()
	return cancel, done
}

// joinFeed waits for one attempt's end and returns its error.
func joinFeed(t *testing.T, done <-chan error) error {
	t.Helper()
	select {
	case err := <-done:
		return err
	case <-time.After(15 * time.Second):
		t.Fatal("the feed's attempt never ended")
		return nil
	}
}

// quote renders text as a JSON string.
func quote(text string) string {
	data, err := json.Marshal(text)
	if err != nil {
		panic(err)
	}
	return string(data)
}

// TestResourceFeedDeliversAQueuedItemAndEveryUpdateOnce covers the
// first-wake contract on both protocol revisions: what the resource
// queued before the subscription is read once; an update that races the
// initial read is delivered once, whichever side of the read it lands
// on; a later update is read again; a content with no text delivers
// nothing; and the attempt ends with its subscription cleaned up.
func TestResourceFeedDeliversAQueuedItemAndEveryUpdateOnce(t *testing.T) {
	for _, protocol := range []string{"", "legacy"} {
		t.Run(protocolName(protocol), func(t *testing.T) {
			l := launchFeed(t, protocol)
			if !l.group.Subscribes("fixture") {
				t.Fatal("the fixture advertised no resource subscriptions")
			}
			// The queued item is the first read's own: it sits in the
			// resource before the subscription exists.
			l.push(t, "queued")
			run := ResourceFeed(l.group, "fixture", inboxURI)
			feed, io, _ := newFeedIO(false)
			cancel, done := runFeed(t, run, io)

			// The racing update is pushed the moment the attempt starts,
			// so its notification may land before, during or after the
			// initial read: whichever it is, the message is delivered
			// once, for the read drains and the update's own read finds
			// nothing left behind.
			l.push(t, "racing")

			if got := feed.waitEmit(t, "the queued item"); got != "queued" {
				t.Errorf("the first delivery is %q, want the queued item", got)
			}
			if got := feed.waitEmit(t, "the racing update"); got != "racing" {
				t.Errorf("the second delivery is %q, want the racing update", got)
			}

			// A later update is read again, and a content with no text
			// delivers nothing: the read happens, and nothing queues.
			l.push(t, "later")
			if got := feed.waitEmit(t, "the later update"); got != "later" {
				t.Errorf("the third delivery is %q, want the later update", got)
			}
			l.push(t, "")
			// The empty content is read before the assertion: the queue is
			// what the read drains, so an empty queue is the read's answer
			// having come back. A count of reads would not say it — one
			// read answers every update queued while it ran.
			l.waitDrained(t)

			cancel()
			if err := joinFeed(t, done); !errors.Is(err, context.Canceled) {
				t.Errorf("the cancelled attempt returned %v, want its context's error", err)
			}
			if got := feed.texts(); len(got) != 3 {
				t.Errorf("the feed delivered %q, want each message once", got)
			}
			// The subscription ended with the attempt: the legacy
			// revision says so with a request the server counts, and the
			// modern one cancels the listen the client opened.
			if protocol == "legacy" {
				l.waitStats(t, "the unsubscribe", func(counted feedStats) bool { return counted.Unsubscribes >= 1 })
			}
		})
	}
}

// TestResourceFeedTwoFeedsOnOneServerBothDrain covers the reach of an
// acknowledgement: under the new revision every subscription is its own
// listen stream, and the acknowledgement it carries names only that
// stream's URI, so two feeds on one server are granted by two
// acknowledgements. A grant read as another feed's refusal would end
// that attempt at every session start.
func TestResourceFeedTwoFeedsOnOneServerBothDrain(t *testing.T) {
	l := launchFeed(t, "")
	l.push(t, "the inbox item")
	inboxFeed, inboxIO, _ := newFeedIO(false)
	counterFeed, counterIO, _ := newFeedIO(false)
	cancelInbox, inboxDone := runFeed(t, ResourceFeed(l.group, "fixture", inboxURI), inboxIO)
	defer cancelInbox()
	cancelCounter, counterDone := runFeed(t, ResourceFeed(l.group, "fixture", counterURI), counterIO)
	defer cancelCounter()

	// Both attempts get past their acknowledgements and read once: the
	// inbox drains the item it queued before the subscription, and the
	// counter answers the count it holds.
	if got := inboxFeed.waitEmit(t, "the queued inbox item"); got != "the inbox item" {
		t.Errorf("the inbox feed's first delivery is %q, want the queued item", got)
	}
	if got := counterFeed.waitEmit(t, "the counter's first read"); got != "0" {
		t.Errorf("the counter feed's first delivery is %q, want the count", got)
	}

	// An update reaches the feed whose URI it names: the bump moves the
	// counter feed, and the inbox feed reads no further.
	l.bump(t)
	if got := counterFeed.waitEmit(t, "the bumped counter"); got != "1" {
		t.Errorf("the counter feed's update delivered %q, want the bumped count", got)
	}
	if got := inboxFeed.texts(); len(got) != 1 {
		t.Errorf("the inbox feed delivered %q, want its queued item alone", got)
	}

	// Neither attempt ended: the other feed's acknowledgement was never
	// its refusal.
	for _, attempt := range []struct {
		name string
		done <-chan error
	}{{"inbox", inboxDone}, {"counter", counterDone}} {
		select {
		case err := <-attempt.done:
			t.Errorf("the %s feed's attempt ended with %v, want it running", attempt.name, err)
		default:
		}
	}
}

// TestResourceFeedEndsWhenTheReadFails covers the failure semantics: a
// read that the server refuses ends the attempt with it, and nothing is
// emitted.
func TestResourceFeedEndsWhenTheReadFails(t *testing.T) {
	for _, protocol := range []string{"", "legacy"} {
		t.Run(protocolName(protocol), func(t *testing.T) {
			l := launchFeed(t, protocol)
			run := ResourceFeed(l.group, "fixture", "fixture://missing")
			feed, io, _ := newFeedIO(false)
			cancel, done := runFeed(t, run, io)
			defer cancel()

			err := joinFeed(t, done)
			if err == nil {
				t.Fatal("the attempt of an unreadable resource succeeded")
			}
			for _, want := range []string{"fixture", "fixture://missing", "reading"} {
				if !bytes.Contains([]byte(err.Error()), []byte(want)) {
					t.Errorf("the failure %q does not name %q", err, want)
				}
			}
			if got := feed.texts(); len(got) != 0 {
				t.Errorf("the failed read emitted %q, want nothing", got)
			}
		})
	}
}

// TestResourceFeedEndsWhenAnEmitFails covers the other failure: an emit
// the session refuses ends the attempt, and the records before it stand.
func TestResourceFeedEndsWhenAnEmitFails(t *testing.T) {
	l := launchFeed(t, "")
	l.push(t, "doomed")
	run := ResourceFeed(l.group, "fixture", inboxURI)
	feed, io, _ := newFeedIO(true)
	cancel, done := runFeed(t, run, io)
	defer cancel()

	if err := joinFeed(t, done); !errors.Is(err, errEmitRefused) {
		t.Errorf("the attempt ended with %v, want the refused emit", err)
	}
	if got := feed.texts(); len(got) != 0 {
		t.Errorf("the refused emit still recorded %q", got)
	}
}

// TestResourceFeedSkipsABlob covers the content the feed does not read:
// a blob is skipped with a line in the feed's log — no bytes of it —
// and the feed keeps running.
func TestResourceFeedSkipsABlob(t *testing.T) {
	l := launchFeed(t, "")
	run := ResourceFeed(l.group, "fixture", blobURI)
	feed, io, logs := newFeedIO(false)
	cancel, done := runFeed(t, run, io)

	l.waitStats(t, "the blob's read", func(counted feedStats) bool { return counted.BlobReads >= 1 })
	if got := logs.String(); !bytes.Contains([]byte(got), []byte("blob")) {
		t.Errorf("the feed's log says %q, want the blob's skip", got)
	}
	if got := feed.texts(); len(got) != 0 {
		t.Errorf("the blob delivered %q, want nothing", got)
	}

	cancel()
	if err := joinFeed(t, done); !errors.Is(err, context.Canceled) {
		t.Errorf("the cancelled attempt returned %v, want its context's error", err)
	}
}

// TestResourceFeedRefusesAnOversizedRecord covers the limit the
// resource feed shares with every other feed path: a text past the
// record a feed holds ends the attempt naming the read and the limit,
// rather than becoming a record that nothing else would carry.
func TestResourceFeedRefusesAnOversizedRecord(t *testing.T) {
	const uri = "test://huge"
	huge := strings.Repeat("x", feed.MaxLineBytes+1)
	server := sdkmcp.NewServer(&sdkmcp.Implementation{Name: "inline", Version: "test"}, &sdkmcp.ServerOptions{
		SubscribeHandler:   func(context.Context, *sdkmcp.SubscribeRequest) error { return nil },
		UnsubscribeHandler: func(context.Context, *sdkmcp.UnsubscribeRequest) error { return nil },
	})
	server.AddResource(&sdkmcp.Resource{URI: uri, Name: "huge", MIMEType: "text/plain"},
		func(context.Context, *sdkmcp.ReadResourceRequest) (*sdkmcp.ReadResourceResult, error) {
			return &sdkmcp.ReadResourceResult{
				Contents: []*sdkmcp.ResourceContents{{URI: uri, MIMEType: "text/plain", Text: huge}},
			}, nil
		})
	clientTransport, serverTransport := sdkmcp.NewInMemoryTransports()
	go func() { _ = server.Run(context.Background(), serverTransport) }()
	g, err := Connect(context.Background(), "inline", clientTransport)
	if err != nil {
		t.Fatalf("connecting the inline server: %v", err)
	}
	defer g.Close()

	run := ResourceFeed(g, "inline", uri)
	records, io, _ := newFeedIO(false)
	cancel, done := runFeed(t, run, io)
	defer cancel()

	got := joinFeed(t, done)
	if got == nil {
		t.Fatal("the attempt of an oversized record succeeded")
	}
	for _, want := range []string{"inline", uri, fmt.Sprint(feed.MaxLineBytes)} {
		if !bytes.Contains([]byte(got.Error()), []byte(want)) {
			t.Errorf("the failure %q does not name %q", got, want)
		}
	}
	if texts := records.texts(); len(texts) != 0 {
		t.Errorf("the oversized record emitted %d records, want nothing", len(texts))
	}
}

// TestResourceFeedEndsWithoutAnUnsubscribeWhenTheSubscribeFails covers
// the teardown's condition: an attempt whose subscribe failed holds no
// subscription, so it sends no unsubscribe for one. The legacy revision
// answers the subscribe call itself, so the refusal is its error rather
// than an acknowledgement that grants nothing.
func TestResourceFeedEndsWithoutAnUnsubscribeWhenTheSubscribeFails(t *testing.T) {
	const uri = "test://inbox"
	var unsubscribes atomic.Int64
	server := sdkmcp.NewServer(&sdkmcp.Implementation{Name: "inline", Version: "test"}, &sdkmcp.ServerOptions{
		SupportedProtocolVersions: []string{"2025-11-25"},
		SubscribeHandler: func(context.Context, *sdkmcp.SubscribeRequest) error {
			return errors.New("the resource is not subscribable")
		},
		UnsubscribeHandler: func(context.Context, *sdkmcp.UnsubscribeRequest) error {
			unsubscribes.Add(1)
			return nil
		},
	})
	server.AddResource(&sdkmcp.Resource{URI: uri, Name: "inbox", MIMEType: "text/plain"},
		func(context.Context, *sdkmcp.ReadResourceRequest) (*sdkmcp.ReadResourceResult, error) {
			return &sdkmcp.ReadResourceResult{}, nil
		})
	clientTransport, serverTransport := sdkmcp.NewInMemoryTransports()
	go func() { _ = server.Run(context.Background(), serverTransport) }()
	g, err := Connect(context.Background(), "inline", clientTransport)
	if err != nil {
		t.Fatalf("connecting the inline server: %v", err)
	}
	defer g.Close()

	run := ResourceFeed(g, "inline", uri)
	_, io, _ := newFeedIO(false)
	cancel, done := runFeed(t, run, io)
	defer cancel()

	got := joinFeed(t, done)
	if got == nil || !bytes.Contains([]byte(got.Error()), []byte("subscribing")) {
		t.Fatalf("the attempt ended with %v, want the subscribe's failure", got)
	}
	if n := unsubscribes.Load(); n != 0 {
		t.Errorf("the refused subscribe sent %d unsubscribes, want none", n)
	}
}

// TestResourceFeedEndsWhenTheServerIsUnknown covers the runner's own
// guard: a group that holds no server by that name fails the attempt
// naming it, rather than reaching for a session that is not there.
func TestResourceFeedEndsWhenTheServerIsUnknown(t *testing.T) {
	l := launchFeed(t, "")
	run := ResourceFeed(l.group, "nope", inboxURI)
	_, io, _ := newFeedIO(false)
	cancel, done := runFeed(t, run, io)
	defer cancel()

	err := joinFeed(t, done)
	if err == nil || !bytes.Contains([]byte(err.Error()), []byte("nope")) {
		t.Errorf("the attempt ended with %v, want the unknown server named", err)
	}
}

// TestResourceFeedRefusesAnAckWithoutTheURI covers the modern revision's
// refusal: a server that acknowledges the listen without granting the
// URI ends the attempt naming both, and nothing is delivered.
func TestResourceFeedRefusesAnAckWithoutTheURI(t *testing.T) {
	// The inline server holds a resource but no subscribe capability, so
	// its acknowledgement grants nothing.
	server := sdkmcp.NewServer(&sdkmcp.Implementation{Name: "inline", Version: "test"}, nil)
	server.AddResource(&sdkmcp.Resource{URI: "test://inbox", Name: "inbox", MIMEType: "text/plain"},
		func(context.Context, *sdkmcp.ReadResourceRequest) (*sdkmcp.ReadResourceResult, error) {
			return &sdkmcp.ReadResourceResult{
				Contents: []*sdkmcp.ResourceContents{{URI: "test://inbox", MIMEType: "text/plain", Text: "the item"}},
			}, nil
		})
	clientTransport, serverTransport := sdkmcp.NewInMemoryTransports()
	go func() { _ = server.Run(context.Background(), serverTransport) }()
	g, err := Connect(context.Background(), "inline", clientTransport)
	if err != nil {
		t.Fatalf("connecting the inline server: %v", err)
	}
	defer g.Close()
	if g.Subscribes("inline") {
		t.Fatal("the inline server advertised resource subscriptions it does not hold")
	}

	run := ResourceFeed(g, "inline", "test://inbox")
	feed, io, _ := newFeedIO(false)
	cancel, done := runFeed(t, run, io)
	defer cancel()

	got := joinFeed(t, done)
	if got == nil || !bytes.Contains([]byte(got.Error()), []byte("without granting it")) {
		t.Errorf("the attempt ended with %v, want the acknowledgement's refusal", got)
	}
	if texts := feed.texts(); len(texts) != 0 {
		t.Errorf("the refused subscription delivered %q, want nothing", texts)
	}
}

// TestResourceFeedALateAckNeverSatisfiesALaterAttempt covers the
// per-attempt acknowledgement contract: an attempt that retires before
// its acknowledgement is answered takes its waiter with it, and an
// acknowledgement that arrives after it — even a grant for the very URI
// — answers no one, so the next attempt waits for one of its own.
func TestResourceFeedALateAckNeverSatisfiesALaterAttempt(t *testing.T) {
	// The acknowledgement wait is the attempt's own bound, and the test
	// shortens it so the second attempt's missing answer fails fast.
	original := resourceAckWait
	resourceAckWait = 400 * time.Millisecond
	t.Cleanup(func() { resourceAckWait = original })

	// The server grants everything it is asked to listen for, and the
	// transport drops every acknowledgement it sends, so the only
	// acknowledgement an attempt could see is the one the test routes by
	// hand — the middleware's own call, fired the same way the routing
	// fires it.
	server := sdkmcp.NewServer(&sdkmcp.Implementation{Name: "inline", Version: "test"}, &sdkmcp.ServerOptions{
		SubscribeHandler:   func(context.Context, *sdkmcp.SubscribeRequest) error { return nil },
		UnsubscribeHandler: func(context.Context, *sdkmcp.UnsubscribeRequest) error { return nil },
	})
	server.AddResource(&sdkmcp.Resource{URI: "test://inbox", Name: "inbox", MIMEType: "text/plain"},
		func(context.Context, *sdkmcp.ReadResourceRequest) (*sdkmcp.ReadResourceResult, error) {
			return &sdkmcp.ReadResourceResult{
				Contents: []*sdkmcp.ResourceContents{{URI: "test://inbox", MIMEType: "text/plain", Text: "the item"}},
			}, nil
		})
	clientTransport, serverTransport := sdkmcp.NewInMemoryTransports()
	go func() { _ = server.Run(context.Background(), serverTransport) }()
	dropped := newDroppedAcks(clientTransport)
	g, err := Connect(context.Background(), "inline", dropped)
	if err != nil {
		t.Fatalf("connecting the inline server: %v", err)
	}
	defer g.Close()

	const uri = "test://inbox"
	run := ResourceFeed(g, "inline", uri)
	feed, io, _ := newFeedIO(false)

	// The first attempt subscribes and waits: its acknowledgement is
	// dropped, and cancelling it retires the attempt with its waiter
	// stopped — the cancellation the first-wake contract ends on.
	cancel1, done1 := runFeed(t, run, io)
	dropped.waitForListens(t, 1)
	cancel1()
	if err := joinFeed(t, done1); !errors.Is(err, context.Canceled) {
		t.Errorf("the retired attempt returned %v, want its context's error", err)
	}

	// The late acknowledgement arrives now, after the attempt that asked
	// for it is gone: it is routed exactly the way the middleware routes
	// one, and it reaches no one.
	g.state("inline").waiters.acknowledged([]string{uri})

	// The next attempt registers a waiter of its own and subscribes
	// again. A design that latched the late grant would satisfy it, and
	// the item would deliver; the per-attempt waiter leaves the attempt
	// waiting for an acknowledgement of its own, which never comes, and
	// the bound ends it.
	cancel2, done2 := runFeed(t, run, io)
	defer cancel2()
	dropped.waitForListens(t, 2)
	err = joinFeed(t, done2)
	if err == nil || !bytes.Contains([]byte(err.Error()), []byte("never acknowledged")) {
		t.Fatalf("the second attempt ended with %v, want the missing acknowledgement", err)
	}
	if texts := feed.texts(); len(texts) != 0 {
		t.Errorf("the late acknowledgement delivered %q, want nothing", texts)
	}
}

// droppedAcks is a client-side transport that drops every subscription
// acknowledgement the server sends — the middleware never sees one, so
// the acknowledgements a test routes by hand are the only ones — and
// signals every listen the client opens, so a test can wait for the
// subscribe round trip itself.
//
// An acknowledgement is dropped, not held: the in-memory transport is a
// pipe with no buffer of its own, so a read that holds a message back
// blocks the writes behind it on the other end, and the flow the test
// choreographs would wedge itself. A dropped acknowledgement lets the
// conversation keep moving.
type droppedAcks struct {
	transport sdkmcp.Transport

	// listens receives one signal per subscriptions/listen the client
	// opens, and seen counts how many have opened in all.
	listens chan struct{}
	mu      sync.Mutex
	seen    int
}

func newDroppedAcks(transport sdkmcp.Transport) *droppedAcks {
	return &droppedAcks{transport: transport, listens: make(chan struct{}, 16)}
}

func (d *droppedAcks) Connect(ctx context.Context) (sdkmcp.Connection, error) {
	conn, err := d.transport.Connect(ctx)
	if err != nil {
		return nil, err
	}
	return &droppedConnection{Connection: conn, acks: d}, nil
}

// waitForListens waits for count listen streams to have opened in all.
// The count is the conversation's total, so a call that follows an
// earlier one waits for the listens after them, not for count more.
func (d *droppedAcks) waitForListens(t *testing.T, count int) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for {
		d.mu.Lock()
		seen := d.seen
		d.mu.Unlock()
		if seen >= count {
			return
		}
		select {
		case <-d.listens:
			d.mu.Lock()
			d.seen++
			d.mu.Unlock()
		case <-time.After(time.Until(deadline)):
			t.Fatalf("the client opened %d listen streams, want %d", seen, count)
		}
	}
}

// droppedConnection is the connection side of the dropping transport.
type droppedConnection struct {
	sdkmcp.Connection
	acks *droppedAcks
}

func (c *droppedConnection) Read(ctx context.Context) (sdkjsonrpc.Message, error) {
	for {
		msg, err := c.Connection.Read(ctx)
		if err != nil {
			return nil, err
		}
		if !isAcknowledgement(msg) {
			return msg, nil
		}
	}
}

func (c *droppedConnection) Write(ctx context.Context, msg sdkjsonrpc.Message) error {
	if req, ok := msg.(*sdkjsonrpc.Request); ok && req.Method == "subscriptions/listen" {
		select {
		case c.acks.listens <- struct{}{}:
		default:
		}
	}
	return c.Connection.Write(ctx, msg)
}

// isAcknowledgement reports whether a message is the subscription
// acknowledgement the waiters route.
func isAcknowledgement(msg sdkjsonrpc.Message) bool {
	req, ok := msg.(*sdkjsonrpc.Request)
	return ok && req.Method == acknowledgedMethod
}

// protocolName names one revision the fixture was forced to.
func protocolName(protocol string) string {
	if protocol == "" {
		return "modern"
	}
	return protocol
}
