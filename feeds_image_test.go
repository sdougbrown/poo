package poo_test

// A feed's input that carries an image: the session reads the file at
// the acceptance, the record and the delivery carry the bytes, and a
// restore delivers the same picture. An image that does not read, an
// image the head does not name, a declared type the head does not
// confirm, and an image past the model's own cap are each the input's
// failure.

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

// TestAFeedImageReachesTheModel covers the delivery: the session reads
// the image the feed names, the acceptance record carries its bytes,
// and the delivered message carries the picture beside the text.
func TestAFeedImageReachesTheModel(t *testing.T) {
	tr := scripted([]fakeResponse{{text: "the answer"}})
	sink := &recordingSink{}
	opts := testOptions(t, tr, &echoTool{}, sink)
	// The feed's image answers to the session's own working directory.
	if err := os.WriteFile(filepath.Join(opts.WorkingDir, "shot.png"), pngBytes, 0o600); err != nil {
		t.Fatalf("writing the image: %v", err)
	}
	// The model reads images, so the request keeps the picture.
	opts.Model.Inputs = []poo.Modality{poo.ModalityText, poo.ModalityImage}
	emit := make(chan struct{})
	s := newTestSession(t, opts, poo.WithFeeds(poo.FeedSpec{
		Name:   "inbox",
		OnIdle: poo.OnIdleWake,
		Run: func(ctx context.Context, io poo.FeedIO) error {
			<-emit
			if err := io.Emit("alice", "the look", &poo.FeedImage{Path: "shot.png", MediaType: "image/png"}); err != nil {
				return err
			}
			<-ctx.Done()
			return ctx.Err()
		},
	}))
	t.Cleanup(func() { s.Close() })
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
	found := false
	for _, m := range sink.messages() {
		if !poo.IsFeedOrigin(m.Origin) {
			continue
		}
		found = true
		if len(m.Content) != 2 {
			t.Fatalf("the delivered message holds %d blocks, want the text and the image", len(m.Content))
		}
		if m.Content[0].Kind != poo.ContentText || !strings.Contains(m.Content[0].Text, "the look") {
			t.Errorf("the first block is %+v, want the feed's text", m.Content[0])
		}
		block := m.Content[1]
		if block.Kind != poo.ContentImage || block.MediaType != "image/png" || string(block.Data) != string(pngBytes) {
			t.Errorf("the image block is %+v, want the png", block)
		}
	}
	if !found {
		t.Fatal("the feed's input did not reach the model's view")
	}
}

// TestARestoredFeedImageIsStillOne covers the restore: a feed's
// accepted input carries its attachments, so a session that loads again
// delivers the picture it was holding.
func TestARestoredFeedImageIsStillOne(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "shot.png")
	if err := os.WriteFile(path, pngBytes, 0o600); err != nil {
		t.Fatalf("writing the image: %v", err)
	}
	tr := scripted(nil)
	sink := &recordingSink{}
	opts := testOptions(t, tr, &echoTool{}, sink)
	// The input is held: no run takes it, so the records hold it.
	opts.Jobs = poo.JobOptions{OnIdle: poo.OnIdleHold}
	opts.WorkingDir = dir
	s := newTestSession(t, opts)
	if err := poo.QueueFeedImageInput(s, "inbox", "alice", "the look", &poo.FeedImage{Path: path}); err != nil {
		t.Fatalf("queueing the image input: %v", err)
	}
	s.Close()

	pending := poo.PendingJobResults(sink.allRecords())
	if len(pending) != 1 {
		t.Fatalf("the records hold %d pending results, want the held input", len(pending))
	}
	if len(pending[0].Attachments) != 1 ||
		pending[0].Attachments[0].MediaType != "image/png" ||
		string(pending[0].Attachments[0].Data) != string(pngBytes) {
		t.Errorf("the held input holds %+v, want the png", pending[0].Attachments)
	}
}

// TestAFeedImageThatDoesNotReadIsRefused covers the read: an image the
// feed names that is not there is the input's failure.
func TestAFeedImageThatDoesNotReadIsRefused(t *testing.T) {
	s := newTestSession(t, testOptions(t, scripted(nil), &echoTool{}, &recordingSink{}))
	err := poo.QueueFeedImageInput(s, "inbox", "", "the look", &poo.FeedImage{Path: filepath.Join(t.TempDir(), "nope.png")})
	if err == nil {
		t.Fatal("the session accepted an image that does not read")
	}
	if !strings.Contains(err.Error(), "does not read") {
		t.Errorf("the refusal is %q, want it to name the read", err)
	}
}

// TestAFeedImageThatIsNotOneIsRefused covers the sniff: a file whose
// head names no image the session reads is the input's failure.
func TestAFeedImageThatIsNotOneIsRefused(t *testing.T) {
	path := filepath.Join(t.TempDir(), "shot.png")
	if err := os.WriteFile(path, []byte("this is text, not a picture"), 0o600); err != nil {
		t.Fatalf("writing the file: %v", err)
	}
	s := newTestSession(t, testOptions(t, scripted(nil), &echoTool{}, &recordingSink{}))
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
	s := newTestSession(t, testOptions(t, scripted(nil), &echoTool{}, &recordingSink{}))
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
	s := newTestSession(t, testOptions(t, scripted(nil), &echoTool{}, &recordingSink{}))
	err := poo.QueueFeedImageInput(s, "inbox", "", "the look", &poo.FeedImage{Path: path})
	if err == nil {
		t.Fatal("the session accepted an image past the cap")
	}
	if !strings.Contains(err.Error(), "larger than") {
		t.Errorf("the refusal is %q, want it to name the cap", err)
	}
}

// TestTheSniffedTypeIsTheOneDelivered covers the declaration's quiet
// side: a feed that names no media type is answered with the type the
// head names, so the picture arrives with its own type.
func TestTheSniffedTypeIsTheOneDelivered(t *testing.T) {
	path := filepath.Join(t.TempDir(), "shot.jpg")
	if err := os.WriteFile(path, jpegBytes, 0o600); err != nil {
		t.Fatalf("writing the image: %v", err)
	}
	tr := scripted([]fakeResponse{{text: "the answer"}})
	sink := &recordingSink{}
	opts := testOptions(t, tr, &echoTool{}, sink)
	opts.WorkingDir = t.TempDir()
	opts.Model.Inputs = []poo.Modality{poo.ModalityText, poo.ModalityImage}
	s := newTestSession(t, opts)
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
