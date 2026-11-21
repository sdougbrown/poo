package poo_test

// A feed's input that carries an image: the session reads the file at
// the acceptance, the record and the delivery carry the bytes, and a
// restore delivers the same picture. An image that does not read, an
// image the head does not name, a declared type the head does not
// confirm, an image past the model's own cap, and an image a model
// cannot read are each the input's failure.

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sdougbrown/poo"
)

// pngBytes are the bytes of a file whose head names a PNG.
var pngBytes = append([]byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}, 1, 2, 3, 4)

// jpegBytes are the bytes of a file whose head names a JPEG.
var jpegBytes = append([]byte{0xff, 0xd8, 0xff, 0xe0}, 5, 6, 7, 8)

// visionOptions returns the options of a session whose model reads
// images, so a feed's picture is one the request keeps.
func visionOptions(t *testing.T, tr poo.ChatTransport, sink poo.RecordSink) poo.Options {
	t.Helper()
	opts := testOptions(t, tr, &echoTool{}, sink)
	opts.Model.Inputs = []poo.Modality{poo.ModalityText, poo.ModalityImage}
	return opts
}

// imageFeedSession returns a session that runs one feed whose single
// emit carries an image, and the channel that releases it.
func imageFeedSession(t *testing.T, opts poo.Options, path, media, from, text string) (*poo.Session, chan struct{}) {
	t.Helper()
	emit := make(chan struct{})
	s := newTestSession(t, opts, poo.WithFeeds(poo.FeedSpec{
		Name:   "inbox",
		OnIdle: poo.OnIdleWake,
		Run: func(ctx context.Context, io poo.FeedIO) error {
			<-emit
			if err := io.Emit(from, text, &poo.FeedImage{Path: path, MediaType: media}); err != nil {
				return err
			}
			<-ctx.Done()
			return ctx.Err()
		},
	}))
	t.Cleanup(func() { s.Close() })
	return s, emit
}

// deliveredFeedImage returns the image block of the feed's delivered
// message, and whether the delivery happened.
func deliveredFeedImage(sink *recordingSink) (poo.ContentBlock, bool) {
	for _, m := range sink.messages() {
		if !poo.IsFeedOrigin(m.Origin) {
			continue
		}
		for _, block := range m.Content {
			if block.Kind == poo.ContentImage {
				return block, true
			}
		}
	}
	return poo.ContentBlock{}, false
}

// TestAFeedImageReachesTheModel covers the delivery: the session reads
// the image the feed names, the acceptance record carries its bytes,
// and the delivered message carries the picture beside the text.
func TestAFeedImageReachesTheModel(t *testing.T) {
	tr := scripted([]fakeResponse{{text: "the answer"}})
	sink := &recordingSink{}
	opts := visionOptions(t, tr, sink)
	// The feed's image answers to the session's own working directory.
	if err := os.WriteFile(filepath.Join(opts.WorkingDir, "shot.png"), pngBytes, 0o600); err != nil {
		t.Fatalf("writing the image: %v", err)
	}
	_, emit := imageFeedSession(t, opts, "shot.png", "image/png", "alice", "the look")
	close(emit)
	waitFor(t, "the woken run to settle", func() bool { return sink.Settled() != nil })

	// The acceptance record carries the picture, so a held input is
	// still one after a restart.
	queued := feedInput(sink.allRecords())
	if queued == nil {
		t.Fatal("the log holds no feed input record")
	}
	if len(queued.Attachments) != 1 ||
		queued.Attachments[0].Modality != poo.ModalityImage ||
		queued.Attachments[0].MediaType != "image/png" ||
		string(queued.Attachments[0].Data) != string(pngBytes) {
		t.Errorf("the acceptance holds %+v, want the png", queued.Attachments)
	}

	// The delivered message carries the picture beside the text.
	block, ok := deliveredFeedImage(sink)
	if !ok {
		t.Fatal("the feed's input did not reach the model's view")
	}
	if block.MediaType != "image/png" || string(block.Data) != string(pngBytes) {
		t.Errorf("the image block is %+v, want the png", block)
	}
}

// TestAnImageOnlyFeedInputDeliversThePicture covers the envelope's own
// case: a message that names an image may leave its text empty, and
// the delivery still carries the picture beside the empty element.
func TestAnImageOnlyFeedInputDeliversThePicture(t *testing.T) {
	tr := scripted([]fakeResponse{{text: "the answer"}})
	sink := &recordingSink{}
	opts := visionOptions(t, tr, sink)
	if err := os.WriteFile(filepath.Join(opts.WorkingDir, "shot.png"), pngBytes, 0o600); err != nil {
		t.Fatalf("writing the image: %v", err)
	}
	_, emit := imageFeedSession(t, opts, "shot.png", "image/png", "", "")
	close(emit)
	waitFor(t, "the woken run to settle", func() bool { return sink.Settled() != nil })

	queued := feedInput(sink.allRecords())
	if queued == nil {
		t.Fatal("the log holds no feed input record")
	}
	if len(queued.Attachments) != 1 {
		t.Fatalf("the acceptance holds %+v, want the picture alone", queued.Attachments)
	}
	block, ok := deliveredFeedImage(sink)
	if !ok {
		t.Fatal("the picture did not reach the model's view")
	}
	if block.MediaType != "image/png" || string(block.Data) != string(pngBytes) {
		t.Errorf("the image block is %+v, want the png", block)
	}
}

// TestARestoredFeedImageIsStillOne covers the restore: a held feed
// input carries its attachment, and a session that loads again delivers
// the picture it was holding.
func TestARestoredFeedImageIsStillOne(t *testing.T) {
	created := header("s1")
	// The recorded model reads images, so the restored request keeps
	// the picture.
	if created.Roles.Chat != nil {
		created.Roles.Chat.Inputs = []poo.Modality{poo.ModalityText, poo.ModalityImage}
	}
	queued := &poo.InputQueuedRecord{
		RecordMeta:  poo.NewRecordMetaFrom("input.queued", poo.FeedOrigin("inbox")),
		SessionID:  "s1",
		InputID:    "i1",
		Mode:       poo.InputModeFeed,
		Text:       `<feed name="inbox">the look</feed>`,
		// The acceptance carried the picture, so the held input is
		// still one.
		Attachments: []poo.Attachment{{Modality: poo.ModalityImage, MediaType: "image/png", Data: pngBytes}},
		QueuedAt:    "2026-01-01T00:00:00Z",
	}
	delivered := &recordingSink{}
	opts := restoreOptions(t, scripted([]fakeResponse{{text: "the answer"}}), delivered)
	// The held input stays held until the prompt takes it, so the
	// restored run is the test's own.
	opts.Jobs = poo.JobOptions{OnIdle: poo.OnIdleHold}
	restored, _, err := poo.Restore(opts, []poo.Record{created, queued})
	if err != nil {
		t.Fatalf("restoring the session: %v", err)
	}
	t.Cleanup(func() { restored.Close() })
	if _, err := restored.Prompt(context.Background(), "go"); err != nil {
		t.Fatalf("starting the restored run: %v", err)
	}
	if _, err := restored.Wait(context.Background()); err != nil {
		t.Fatalf("waiting for the restored run: %v", err)
	}
	block, ok := deliveredFeedImage(delivered)
	if !ok {
		t.Fatal("the restored session delivered no picture")
	}
	if block.MediaType != "image/png" || string(block.Data) != string(pngBytes) {
		t.Errorf("the restored delivery is %+v, want the png", block)
	}
}

// TestAFeedImageAModelCannotReadFailsTheAttempt covers the admission: a
// model that reads no images would drop the whole input at the request,
// so the emit is refused and the attempt ends in failure, its error
// naming the model's eyes.
func TestAFeedImageAModelCannotReadFailsTheAttempt(t *testing.T) {
	tr := scripted(nil)
	sink := &recordingSink{}
	opts := testOptions(t, tr, &echoTool{}, sink)
	_, emit := imageFeedSession(t, opts, "shot.png", "image/png", "", "the look")
	close(emit)
	waitFor(t, "the failed attempt to end", func() bool {
		return feedEnd(sink.allRecords(), 1) != nil
	})
	ended := feedEnd(sink.allRecords(), 1)
	if ended.Outcome != poo.FeedFailed {
		t.Errorf("the attempt ended %q, want %q", ended.Outcome, poo.FeedFailed)
	}
	if !strings.Contains(ended.Error, "does not read images") {
		t.Errorf("the attempt's error is %q, want it to name the model's eyes", ended.Error)
	}
	if feedInput(sink.allRecords()) != nil {
		t.Error("the image was queued, want the input refused")
	}
}

// TestAFeedImageThatDoesNotReadFailsTheAttempt covers the wiring: an
// image the feed names that is not there fails the attempt, and nothing
// is queued for the model.
func TestAFeedImageThatDoesNotReadFailsTheAttempt(t *testing.T) {
	tr := scripted(nil)
	sink := &recordingSink{}
	opts := visionOptions(t, tr, sink)
	_, emit := imageFeedSession(t, opts, filepath.Join(t.TempDir(), "nope.png"), "", "", "the look")
	close(emit)
	waitFor(t, "the failed attempt to end", func() bool {
		return feedEnd(sink.allRecords(), 1) != nil
	})
	ended := feedEnd(sink.allRecords(), 1)
	if ended.Outcome != poo.FeedFailed {
		t.Errorf("the attempt ended %q, want %q", ended.Outcome, poo.FeedFailed)
	}
	if !strings.Contains(ended.Error, "nope.png") {
		t.Errorf("the attempt's error is %q, want it to name the image", ended.Error)
	}
	if feedInput(sink.allRecords()) != nil {
		t.Error("the image was queued, want the input refused")
	}
}

// TestAFeedImageThatIsNotOneIsRefused covers the sniff: a file whose
// head names no image the session reads is the input's failure.
func TestAFeedImageThatIsNotOneIsRefused(t *testing.T) {
	path := filepath.Join(t.TempDir(), "shot.png")
	if err := os.WriteFile(path, []byte("this is text, not a picture"), 0o600); err != nil {
		t.Fatalf("writing the file: %v", err)
	}
	s := newTestSession(t, visionOptions(t, scripted(nil), &recordingSink{}))
	err := poo.QueueFeedImageInput(s, "inbox", "", "the look", &poo.FeedImage{Path: path})
	if err == nil {
		t.Fatal("the session accepted a file that is not an image")
	}
	if !strings.Contains(err.Error(), "not an image") {
		t.Errorf("the refusal is %q, want it to name the image", err)
	}
}

// TestAFeedImageMislabeledIsRefused covers the declaration: a media
// type the feed declared that the head does not confirm is the input's
// failure, and the refusal names both.
func TestAFeedImageMislabeledIsRefused(t *testing.T) {
	path := filepath.Join(t.TempDir(), "shot.png")
	if err := os.WriteFile(path, pngBytes, 0o600); err != nil {
		t.Fatalf("writing the image: %v", err)
	}
	s := newTestSession(t, visionOptions(t, scripted(nil), &recordingSink{}))
	err := poo.QueueFeedImageInput(s, "inbox", "", "the look", &poo.FeedImage{Path: path, MediaType: "image/jpeg"})
	if err == nil {
		t.Fatal("the session accepted an image the head contradicts")
	}
	if !strings.Contains(err.Error(), "image/jpeg") || !strings.Contains(err.Error(), "image/png") {
		t.Errorf("the refusal is %q, want it to name the declared type and the head's", err)
	}
}

// TestAFeedImagePastTheCapIsRefused covers the cap: an image larger
// than the bytes a model takes is the input's failure.
func TestAFeedImagePastTheCapIsRefused(t *testing.T) {
	path := filepath.Join(t.TempDir(), "shot.png")
	if err := os.WriteFile(path, append(append([]byte{}, pngBytes...), make([]byte, poo.MaxImageBytes)...), 0o600); err != nil {
		t.Fatalf("writing the image: %v", err)
	}
	s := newTestSession(t, visionOptions(t, scripted(nil), &recordingSink{}))
	err := poo.QueueFeedImageInput(s, "inbox", "", "the look", &poo.FeedImage{Path: path})
	if err == nil {
		t.Fatal("the session accepted an image past the cap")
	}
	if !strings.Contains(err.Error(), "larger than") {
		t.Errorf("the refusal is %q, want it to name the cap", err)
	}
}

// TestAFeedImageAModelWithoutEyesIsRefused covers the seam: the test
// helper admits the image the way a feed's emit does, so a model that
// reads no images refuses it here too.
func TestAFeedImageAModelWithoutEyesIsRefused(t *testing.T) {
	s := newTestSession(t, testOptions(t, scripted(nil), &echoTool{}, &recordingSink{}))
	err := poo.QueueFeedImageInput(s, "inbox", "", "the look", &poo.FeedImage{Path: filepath.Join(t.TempDir(), "shot.png")})
	if err == nil {
		t.Fatal("the session accepted an image the model cannot read")
	}
	if !strings.Contains(err.Error(), "does not read images") {
		t.Errorf("the refusal is %q, want it to name the model's eyes", err)
	}
}

// TestTheSniffedTypeIsTheOneDelivered covers the declaration's quiet
// side: a feed that names no media type is answered with the type the
// head names, so the picture arrives with its own type.
func TestTheSniffedTypeIsTheOneDelivered(t *testing.T) {
	path := jpegFixture(t)
	tr := scripted([]fakeResponse{{text: "the answer"}})
	sink := &recordingSink{}
	s := newTestSession(t, visionOptions(t, tr, sink))
	if err := poo.QueueFeedImageInput(s, "inbox", "alice", "the look", &poo.FeedImage{Path: path}); err != nil {
		t.Fatalf("queueing the image input: %v", err)
	}
	waitFor(t, "the woken run to settle", func() bool { return sink.Settled() != nil })

	queued := feedInput(sink.allRecords())
	if queued == nil {
		t.Fatal("the log holds no feed input record")
	}
	if len(queued.Attachments) != 1 || queued.Attachments[0].MediaType != "image/jpeg" {
		t.Errorf("the acceptance holds %+v, want the head's own type", queued.Attachments)
	}
}

// jpegFixture writes a small JPEG-shaped file and returns its path.
func jpegFixture(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "shot.jpg")
	if err := os.WriteFile(path, jpegBytes, 0o600); err != nil {
		t.Fatalf("writing the image: %v", err)
	}
	return path
}
