package config_test

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/config"
	"github.com/sdougbrown/poo/pootest"
)

// The command filter: a manifest's `filter.command` is one process per
// record, and the record that process writes is the one the session
// delivers.

// filterScript returns the absolute path of one fixture filter script.
//
// The filters are the feed package's fixtures beside the feeds: the
// runtime runs the same scripts the runner's tests run, so a change to the
// record a filter reads is a change both packages see.
func filterScript(t *testing.T, name string) string {
	t.Helper()
	path, err := filepath.Abs(filepath.Join("..", "feed", "testdata", "bin", name))
	if err != nil {
		t.Fatalf("resolving the fixture filter %q: %v", name, err)
	}
	return path
}

// filterFeedSession builds a session whose one feed is the manifest entry
// given, and returns it with its transport, its sink and the directory its
// own files are in.
func filterFeedSession(t *testing.T, name string, entry map[string]any) (*poo.Session, *pootest.Transport, *pootest.Sink, string, *config.Runtime) {
	t.Helper()
	encoded, err := json.Marshal(map[string]any{"version": 1, "feeds": map[string]any{name: entry}})
	if err != nil {
		t.Fatalf("encoding the manifest: %v", err)
	}
	path := filepath.Join(t.TempDir(), "feeds.json")
	if err := os.WriteFile(path, encoded, 0o600); err != nil {
		t.Fatalf("writing the manifest: %v", err)
	}
	settings, err := config.Parse([]byte(theFeedsSettings(path)), config.FormatYAML)
	if err != nil {
		t.Fatal(err)
	}
	runtime := build(t, settings, config.Overrides{})
	turns := make([]pootest.Turn, 0, 16)
	for range 16 {
		turns = append(turns, pootest.Turn{Text: "ack"})
	}
	transport := pootest.NewTransport(turns...)
	sink := pootest.NewSink()
	dir := t.TempDir()
	runtime.Options.Transport = transport
	runtime.Options.Sink = sink
	runtime.Options.SessionDir = dir
	sess, err := poo.New(runtime.Options)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sess.Close() })
	return sess, transport, sink, dir, runtime
}

// queuedFeedInputs returns what the session accepted as a feed's input, in
// acceptance order.
func queuedFeedInputs(sink *pootest.Sink) []*poo.InputQueuedRecord {
	var out []*poo.InputQueuedRecord
	for _, rec := range sink.Records() {
		if r, ok := rec.(*poo.InputQueuedRecord); ok && r.Mode == poo.InputModeFeed {
			out = append(out, r)
		}
	}
	return out
}

// deliveredFeedMessage returns the message a feed's record was delivered
// as, or nil when none was.
func deliveredFeedMessage(sink *pootest.Sink) *poo.MessageRecord {
	for _, rec := range sink.Records() {
		if r, ok := rec.(*poo.MessageRecord); ok && poo.IsFeedOrigin(r.Origin) {
			return r
		}
	}
	return nil
}

// TestAScriptFilterPassesTheRecordItReads covers the contract's round
// trip: the record the feed wrote arrives on the filter's standard input,
// and the record the filter writes is the one the session delivers.
func TestAScriptFilterPassesTheRecordItReads(t *testing.T) {
	seen := filepath.Join(t.TempDir(), "filter.stdin")
	_, transport, sink, _, _ := filterFeedSession(t, "chat", map[string]any{
		"command": []string{feedScript(t, "chat")},
		"on_idle": "wake",
		"filter":  map[string]any{"command": []string{filterScript(t, "filter-echo")}},
		"env":     map[string]string{"FIXTURE_FILTER_LOG": seen},
	})

	waitUntil(t, "both records to reach the chat model", func() bool {
		return theModelRead(transport, `<feed name="chat" from="alice">the first message</feed>`, poo.FeedOrigin("chat")) &&
			theModelRead(transport, `<feed name="chat">the second message</feed>`, poo.FeedOrigin("chat"))
	})
	// The filter saw one record per line, exactly as the feed wrote it, and
	// the sender rode the record in.
	logged, err := os.ReadFile(seen)
	if err != nil {
		t.Fatalf("reading the filter's log: %v", err)
	}
	lines := strings.Split(strings.TrimRight(string(logged), "\n"), "\n")
	want := []string{
		`{"text":"the first message","from":"alice"}`,
		`{"text":"the second message","from":""}`,
	}
	if len(lines) != len(want) {
		t.Fatalf("the filter read %d records (%v), want %d", len(lines), lines, len(want))
	}
	for i := range want {
		if lines[i] != want[i] {
			t.Errorf("the filter read %q, want %q", lines[i], want[i])
		}
	}
	// The session delivered what the filter wrote, sender and all.
	queued := queuedFeedInputs(sink)
	if len(queued) != 2 {
		t.Fatalf("the session queued %d feed inputs, want 2", len(queued))
	}
	if queued[0].Text != `<feed name="chat" from="alice">the first message</feed>` {
		t.Errorf("the first input is %q, want the filter's first record", queued[0].Text)
	}
}

// TestAScriptFilterPassesAnImageThrough covers a record that names an image:
// the picture is the record's, the filter passes it back with the rest, and
// the session delivers it beside the text. The path the filter wrote back is
// the file the bytes are read from, and the media type it wrote is the one the
// delivery holds.
func TestAScriptFilterPassesAnImageThrough(t *testing.T) {
	png := append([]byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}, 1, 2, 3, 4)
	dir := t.TempDir()
	imagePath := filepath.Join(dir, "shot.png")
	if err := os.WriteFile(imagePath, png, 0o600); err != nil {
		t.Fatal(err)
	}
	record := `{"text":"the look","from":"alice","image":{"path":"` + imagePath + `","media":"image/png"}}`
	records := filepath.Join(dir, "records.jsonl")
	if err := os.WriteFile(records, []byte(record+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	seen := filepath.Join(dir, "filter.stdin")
	manifest := `{
  "version": 1,
  "feeds": {
    "inbox": {
      "command": ["sh", "-c", "cat \"$1\"; sleep 60", "feed", "` + records + `"],
      "on_idle": "wake",
      "filter": {"command": ["` + filterScript(t, "filter-echo") + `"]},
      "env": {"FIXTURE_FILTER_LOG": "` + seen + `"}
    }
  }
}`
	path := filepath.Join(dir, "feeds.json")
	if err := os.WriteFile(path, []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}
	settings, err := config.Parse([]byte(theFeedsSettings(path)), config.FormatYAML)
	if err != nil {
		t.Fatal(err)
	}
	runtime := build(t, settings, config.Overrides{})
	// The session's model reads images, so the feed's picture is one the
	// admission takes.
	runtime.Options.Model.Inputs = []poo.Modality{poo.ModalityText, poo.ModalityImage}
	sink := pootest.NewSink()
	runtime.Options.Transport = pootest.NewTransport(pootest.Turn{Text: "ack"}, pootest.Turn{Text: "ack"})
	runtime.Options.Sink = sink
	sess, err := poo.New(runtime.Options)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sess.Close() })

	waitUntil(t, "the message with its picture to be delivered", func() bool {
		return deliveredFeedMessage(sink) != nil
	})
	// The record the filter read named the picture, path and media type
	// included, so the image branch of the record a filter is handed is the
	// one the feed's own input carried.
	logged, err := os.ReadFile(seen)
	if err != nil {
		t.Fatalf("reading the filter's log: %v", err)
	}
	if got := strings.TrimSpace(string(logged)); got != record {
		t.Errorf("the filter read %q, want %q", got, record)
	}
	// What the filter wrote back is the message: the picture is read again
	// from the path it named, and the media type it named is the one the
	// message carries.
	queued := queuedFeedInputs(sink)
	if len(queued) != 1 {
		t.Fatalf("the session queued %d feed inputs, want the one message", len(queued))
	}
	if queued[0].Text != `<feed name="inbox" from="alice">the look</feed>` {
		t.Errorf("the input is %q, want the filter's record beside the picture", queued[0].Text)
	}
	if len(queued[0].Attachments) != 1 || queued[0].Attachments[0].MediaType != "image/png" || !bytes.Equal(queued[0].Attachments[0].Data, png) {
		t.Errorf("the queued input holds %+v, want the png the filter's record names", queued[0].Attachments)
	}
	message := deliveredFeedMessage(sink)
	if len(message.Content) != 2 || message.Content[1].Kind != poo.ContentImage ||
		message.Content[1].MediaType != "image/png" || !bytes.Equal(message.Content[1].Data, png) {
		t.Errorf("the delivered message holds %+v, want the text and its png", message.Content)
	}
}

// TestAScriptFilterDropsWhatItDoesNotWrite covers the drop: a filter that
// writes nothing delivers nothing, and the feed carries on.
func TestAScriptFilterDropsWhatItDoesNotWrite(t *testing.T) {
	seen := filepath.Join(t.TempDir(), "filter.stdin")
	_, _, sink, _, _ := filterFeedSession(t, "chat", map[string]any{
		"command": []string{feedScript(t, "chat")},
		"on_idle": "wake",
		"filter":  map[string]any{"command": []string{filterScript(t, "filter-drop")}},
		"env":     map[string]string{"FIXTURE_FILTER_LOG": seen},
	})
	// Both records reach the filter, and none of them becomes an input.
	waitUntil(t, "the filter to read both records", func() bool {
		logged, err := os.ReadFile(seen)
		return err == nil && strings.Count(string(logged), "\n") == 2
	})
	if queued := queuedFeedInputs(sink); len(queued) != 0 {
		t.Fatalf("the session queued %d feed inputs, want none: the filter dropped them", len(queued))
	}
	if ends := feedEndsOf(sink); len(ends) != 0 {
		t.Fatalf("the feed ended %+v, want it still running: a drop is not a failure", ends)
	}
}

// TestAScriptFilterAddsStructuredData covers the object a filter may add
// beside its text: the delivered message carries it, and the model reads
// the text alone.
func TestAScriptFilterAddsStructuredData(t *testing.T) {
	_, transport, sink, _, _ := filterFeedSession(t, "chat", map[string]any{
		"command": []string{feedScript(t, "chat")},
		"on_idle": "wake",
		"filter":  map[string]any{"command": []string{filterScript(t, "filter-data")}},
	})
	waitUntil(t, "the structured record to reach the chat model", func() bool {
		return theModelRead(transport, `<feed name="chat" from="alice">a structured record</feed>`, poo.FeedOrigin("chat"))
	})
	queued := queuedFeedInputs(sink)
	if len(queued) == 0 {
		t.Fatal("the session queued no feed input")
	}
	if kind, n := dataOf(t, queued[0].Data); kind != "note" || n != 3 {
		t.Errorf("the queued input's data is %q, want the filter's object", queued[0].Data)
	}
	// The delivered message carries the object, and its text does not: the
	// model reads the text, and the object is the record's side channel.
	msg := deliveredFeedMessage(sink)
	if msg == nil {
		t.Fatal("the feed's record never reached the model")
	}
	if kind, _ := dataOf(t, msg.Data); kind != "note" {
		t.Errorf("the delivered message's data is %q, want the filter's object", msg.Data)
	}
	if strings.Contains(msg.Content[0].Text, "kind") {
		t.Errorf("the delivered text %q carries the data", msg.Content[0].Text)
	}
}

// dataOf decodes a record's data object and returns its kind and count.
func dataOf(t *testing.T, raw json.RawMessage) (string, int) {
	t.Helper()
	if raw == nil {
		return "", 0
	}
	var data struct {
		Kind string `json:"kind"`
		N    int    `json:"n"`
	}
	if err := json.Unmarshal(raw, &data); err != nil {
		t.Fatalf("the record's data is %q: %v", raw, err)
	}
	return data.Kind, data.N
}

// TestAScriptFilterFailureSuspendsTheFeed covers the failure: a filter that
// exits nonzero suspends the feed whatever its restart policy says, and the
// session wakes with a report that names the fault and the file holding the
// filter's own diagnostics.
func TestAScriptFilterFailureSuspendsTheFeed(t *testing.T) {
	sess, _, sink, dir, _ := filterFeedSession(t, "chat", map[string]any{
		"command": []string{feedScript(t, "chat")},
		"on_idle": "wake",
		"restart": true,
		"filter":  map[string]any{"command": []string{filterScript(t, "filter-fail")}},
	})
	waitUntil(t, "the fault report to reach the chat model", func() bool {
		return deliveredFeedMessage(sink) != nil
	})
	text := deliveredFeedMessage(sink).Content[0].Text
	for _, want := range []string{
		`name="chat"`,
		`fault="` + poo.FeedFaultExit + `"`,
		"the filter exited with status 4",
		// The remedy: this feed is one the session starts itself, so this
		// stage offers no call that starts it again.
		"stays suspended until the session closes",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("the fault report %q does not say %q", text, want)
		}
	}
	// The report names the file the filter's stderr went to, and the file
	// holds exactly that: the diagnostics never ride the input.
	path := attributeOf(t, text, "log")
	if filepath.Dir(path) != dir {
		t.Errorf("the fault log is %q, want it beside the session's files in %q", path, dir)
	}
	stderr, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading the fault log: %v", err)
	}
	if string(stderr) != "the filter could not read the record\n" {
		t.Errorf("the fault log holds %q, want the filter's stderr", stderr)
	}
	if strings.Contains(text, "the filter could not read the record") {
		t.Errorf("the fault report %q carries the filter's stderr", text)
	}
	// The attempt ended suspended, and the restart policy did not apply.
	ends := feedEndsOf(sink)
	if len(ends) != 1 {
		t.Fatalf("the log holds %d feed.ended records, want the one attempt", len(ends))
	}
	if ends[0].Outcome != poo.FeedSuspended {
		t.Errorf("the attempt ended %q, want %q", ends[0].Outcome, poo.FeedSuspended)
	}
	if ends[0].RestartInMS != 0 {
		t.Errorf("the attempt scheduled a restart in %dms, want none", ends[0].RestartInMS)
	}
	if n := feedStartsOf(sink); n != 1 {
		t.Errorf("the feed started %d attempts, want 1", n)
	}
	// The jobs listing says the feed is suspended.
	suspended := false
	for _, job := range sess.Jobs() {
		if job.Tool == poo.FeedToolName && job.Status == poo.JobSuspended {
			suspended = true
		}
	}
	if !suspended {
		t.Errorf("the jobs listing does not show the feed suspended: %+v", sess.Jobs())
	}
}

// TestAScriptFilterRefusesOutputThatIsNotARecord covers the other failure:
// a filter that exits cleanly and writes something else is the same
// suspension, with the code that names it.
func TestAScriptFilterRefusesOutputThatIsNotARecord(t *testing.T) {
	_, _, sink, _, _ := filterFeedSession(t, "chat", map[string]any{
		"command": []string{feedScript(t, "chat")},
		"on_idle": "wake",
		"filter":  map[string]any{"command": []string{filterScript(t, "filter-bad")}},
	})
	waitUntil(t, "the fault report to reach the chat model", func() bool {
		return deliveredFeedMessage(sink) != nil
	})
	text := deliveredFeedMessage(sink).Content[0].Text
	if !strings.Contains(text, `fault="`+poo.FeedFaultOutput+`"`) {
		t.Errorf("the fault report %q does not name the output fault", text)
	}
	if ends := feedEndsOf(sink); len(ends) != 1 || ends[0].Outcome != poo.FeedSuspended {
		t.Fatalf("the attempts ended %+v, want one suspended attempt", ends)
	}
}

// TestAScriptFilterRefusesOutputPastTheRecordCap covers the output cap:
// a filter that writes more than a record may be suspends the feed, with
// the code that names the cap. The bytes past the cap are dropped rather
// than held, and the fault is the one that says so.
func TestAScriptFilterRefusesOutputPastTheRecordCap(t *testing.T) {
	_, _, sink, _, _ := filterFeedSession(t, "chat", map[string]any{
		"command": []string{feedScript(t, "chat")},
		"on_idle": "wake",
		"filter":  map[string]any{"command": []string{filterScript(t, "filter-oversize")}},
	})
	waitUntil(t, "the fault report to reach the chat model", func() bool {
		return deliveredFeedMessage(sink) != nil
	})
	text := deliveredFeedMessage(sink).Content[0].Text
	if !strings.Contains(text, `fault="`+poo.FeedFaultRecord+`"`) {
		t.Errorf("the fault report %q does not name the record fault", text)
	}
	if ends := feedEndsOf(sink); len(ends) != 1 || ends[0].Outcome != poo.FeedSuspended {
		t.Fatalf("the attempts ended %+v, want one suspended attempt", ends)
	}
}

// TestAFeedWithoutAFilterIgnoresData covers the rule that a record's data is
// the filter's to produce: a feed that runs no filter delivers its text, and
// the data field it writes is a field the session does not know.
func TestAFeedWithoutAFilterIgnoresData(t *testing.T) {
	_, transport, sink, _, _ := filterFeedSession(t, "chat", map[string]any{
		"command": []string{feedScript(t, "filterless-data")},
		"on_idle": "wake",
	})
	waitUntil(t, "the message to reach the chat model", func() bool {
		return theModelRead(transport, `<feed name="chat" from="alice">the message</feed>`, poo.FeedOrigin("chat"))
	})
	queued := queuedFeedInputs(sink)
	if len(queued) != 1 {
		t.Fatalf("the session queued %d inputs, want the one message", len(queued))
	}
	if queued[0].Data != nil {
		t.Errorf("the input carries the data %q, and a feed without a filter has none", queued[0].Data)
	}
	if msg := deliveredFeedMessage(sink); msg == nil || msg.Data != nil {
		t.Errorf("the delivered message carries data %q, and a feed without a filter has none", msg.Data)
	}
}

// TestAFilterDataSurvivesASessionRestart covers the restore's data: a
// held input that carries a filter's data object is still one after a
// restart, and the delivered message carries the object.
func TestAFilterDataSurvivesASessionRestart(t *testing.T) {
	entry := map[string]any{
		"command": []string{feedScript(t, "chat")},
		"on_idle": "hold",
		"filter":  map[string]any{"command": []string{filterScript(t, "filter-data")}},
	}
	encoded, err := json.Marshal(map[string]any{"version": 1, "feeds": map[string]any{"chat": entry}})
	if err != nil {
		t.Fatalf("encoding the manifest: %v", err)
	}
	manifestPath := filepath.Join(t.TempDir(), "feeds.json")
	if err := os.WriteFile(manifestPath, encoded, 0o600); err != nil {
		t.Fatalf("writing the manifest: %v", err)
	}
	settings, err := config.Parse([]byte(theFeedsSettings(manifestPath)+"session:\n  mode: logged\n  dir: "+t.TempDir()+"\n"), config.FormatYAML)
	if err != nil {
		t.Fatalf("decoding the settings: %v", err)
	}
	runtime := build(t, settings, config.Overrides{})
	defer runtime.Store.Close()
	transport := pootest.NewTransport(pootest.Turn{Text: "ack"}, pootest.Turn{Text: "ack"}, pootest.Turn{Text: "ack"})
	runtime.Options.Transport = transport

	created := &poo.SessionCreatedRecord{
		RecordMeta: poo.NewRecordMeta("session.created"), SessionMetadata: poo.SessionMetadata{Format: poo.FormatVersion,
			SessionID:  "s1",
			Mode:       "logged",
			CreatedAt:  "2026-11-04T12:00:00Z",
			WorkingDir: t.TempDir(),
			Profile:    "code",
			Tools:      runtime.Profile.Tools, Model: poo.SessionModel{ModelID: "test-model"},
			ContextLimit: 32768},
	}
	sess, err := runtime.CreateSession(context.Background(), runtime.Store, created)
	if err != nil {
		t.Fatalf("creating the session: %v", err)
	}
	// The structured input is held: the feed's policy is hold, so it
	// waits for the next prompt. The records go to the store, so the
	// check loads them.
	waitUntil(t, "the structured input to be held", func() bool {
		loaded, err := runtime.Store.Load(context.Background(), "s1")
		if err != nil {
			return false
		}
		for _, rec := range loaded.Records {
			if _, ok := rec.(*poo.InputHeldRecord); ok {
				return true
			}
		}
		return false
	})
	if err := sess.Close(); err != nil {
		t.Fatalf("closing the session: %v", err)
	}
	// The restore builds from the session's own records, so the held
	// input's data is the one the records carry.
	loaded, err := runtime.Store.Load(context.Background(), "s1")
	if err != nil {
		t.Fatalf("loading the session's records: %v", err)
	}
	restoredTransport := pootest.NewTransport(pootest.Turn{Text: "ack"}, pootest.Turn{Text: "ack"})
	restoredOptions := runtime.Options
	restoredOptions.Transport = restoredTransport
	restoredSink := pootest.NewSink()
	restoredOptions.Sink = restoredSink
	restored, _, err := poo.Restore(restoredOptions, loaded.Records)
	if err != nil {
		t.Fatalf("restoring the session: %v", err)
	}
	defer restored.Close()
	// The restore re-holds the input: the session is busy until it does,
	// so the prompt waits for the re-hold.
	waitUntil(t, "the restored input to be held", func() bool {
		for _, rec := range restoredSink.Records() {
			if _, ok := rec.(*poo.InputHeldRecord); ok {
				return true
			}
		}
		return false
	})
	// The next prompt delivers the held input, and the delivery carries
	// the filter's data object.
	if _, err := restored.Prompt(context.Background(), "go"); err != nil {
		t.Fatalf("starting the run: %v", err)
	}
	waitUntil(t, "the held input to be delivered", func() bool {
		return deliveredFeedMessage(restoredSink) != nil
	})
	msg := deliveredFeedMessage(restoredSink)
	if kind, _ := dataOf(t, msg.Data); kind != "note" {
		t.Errorf("the delivered message's data is %q, want the filter's object", msg.Data)
	}
}

// attributeOf returns the value of one attribute of the element a feed's
// input renders.
func attributeOf(t *testing.T, text, name string) string {
	t.Helper()
	open := ` ` + name + `="`
	start := strings.Index(text, open)
	if start < 0 {
		t.Fatalf("the text %q names no %s attribute", text, name)
	}
	rest := text[start+len(open):]
	end := strings.IndexByte(rest, '"')
	if end < 0 {
		t.Fatalf("the text %q has an unterminated %s attribute", text, name)
	}
	return rest[:end]
}

// feedEndsOf returns the feed.ended records a sink holds.
func feedEndsOf(sink *pootest.Sink) []*poo.FeedEndedRecord {
	var out []*poo.FeedEndedRecord
	for _, rec := range sink.Records() {
		if r, ok := rec.(*poo.FeedEndedRecord); ok {
			out = append(out, r)
		}
	}
	return out
}

// feedStartsOf returns how many attempts a sink's feed.started records
// name.
func feedStartsOf(sink *pootest.Sink) int {
	n := 0
	for _, rec := range sink.Records() {
		if _, ok := rec.(*poo.FeedStartedRecord); ok {
			n++
		}
	}
	return n
}

// TestAFaultLogOfASessionTheRuntimeCreatedLandsBesideItsRecords covers the
// session directory the runtime hands a session it creates: a feed's fault
// log belongs beside the session's records, not in the directory the
// session works in.
func TestAFaultLogOfASessionTheRuntimeCreatedLandsBesideItsRecords(t *testing.T) {
	encoded, err := json.Marshal(map[string]any{"version": 1, "feeds": map[string]any{"chat": map[string]any{
		"command": []string{feedScript(t, "chat")},
		"on_idle": "wake",
		"filter":  map[string]any{"command": []string{filterScript(t, "filter-fail")}},
	}}})
	if err != nil {
		t.Fatalf("encoding the manifest: %v", err)
	}
	manifestPath := filepath.Join(t.TempDir(), "feeds.json")
	if err := os.WriteFile(manifestPath, encoded, 0o600); err != nil {
		t.Fatalf("writing the manifest: %v", err)
	}
	settings, err := config.Parse([]byte(theFeedsSettings(manifestPath)+"session:\n  mode: logged\n  dir: "+t.TempDir()+"\n"), config.FormatYAML)
	if err != nil {
		t.Fatalf("decoding the settings: %v", err)
	}
	runtime := build(t, settings, config.Overrides{})
	defer runtime.Store.Close()
	runtime.Options.Transport = pootest.NewTransport(pootest.Turn{Text: "ack"}, pootest.Turn{Text: "ack"})
	created := &poo.SessionCreatedRecord{
		RecordMeta: poo.NewRecordMeta("session.created"), SessionMetadata: poo.SessionMetadata{Format: poo.FormatVersion,
			SessionID:  "s1",
			Mode:       "logged",
			CreatedAt:  "2026-11-04T12:00:00Z",
			WorkingDir: t.TempDir(),
			Profile:    "code",
			Tools:      runtime.Profile.Tools, Model: poo.SessionModel{ModelID: "test-model"},
			ContextLimit: 32768},
	}
	sess, err := runtime.CreateSession(context.Background(), runtime.Store, created)
	if err != nil {
		t.Fatalf("creating the session: %v", err)
	}
	defer sess.Close()
	waitUntil(t, "the fault log beside the session's records", func() bool {
		matches, err := filepath.Glob(filepath.Join(runtime.Store.Dir(), "*-fault-*.log"))
		return err == nil && len(matches) > 0
	})
	stray, err := filepath.Glob(filepath.Join(created.WorkingDir, "*-fault-*.log"))
	if err != nil {
		t.Fatalf("looking for a fault log in the working directory: %v", err)
	}
	if len(stray) != 0 {
		t.Errorf("the fault log is in the working directory: %v", stray)
	}
}
