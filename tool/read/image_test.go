package read_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/prompt"
	"github.com/sdougbrown/poo/tool/read"
)

// pngFile writes a small PNG into dir and returns its path and size.
func pngFile(t *testing.T, dir, name string) (string, int) {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 8, 8))
	img.Set(0, 0, color.RGBA{R: 220, G: 20, B: 20, A: 255})
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encoding the image: %v", err)
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatalf("writing the image: %v", err)
	}
	return path, buf.Len()
}

// TestAnImageIsReturnedAsTheImage covers the model that reads images: the
// file comes back as an image block beside a text line naming it, not as
// bytes in the text.
func TestAnImageIsReturnedAsTheImage(t *testing.T) {
	dir := t.TempDir()
	path, size := pngFile(t, dir, "shot.png")

	res := call(t, read.New(read.WithInputs(poo.ModalityImage)), map[string]any{"path": "shot.png"}, dir)
	if res.IsError {
		t.Fatalf("reading an image failed: %s", res.Text)
	}
	if len(res.Attachments) != 1 {
		t.Fatalf("the read returned %d attachments, want one", len(res.Attachments))
	}
	if res.Attachments[0].Modality != poo.ModalityImage {
		t.Errorf("the image's modality is %q, want image", res.Attachments[0].Modality)
	}
	if res.Attachments[0].MediaType != "image/png" {
		t.Errorf("the image's media type is %q, want image/png", res.Attachments[0].MediaType)
	}
	if len(res.Attachments[0].Data) != size {
		t.Errorf("the image holds %d bytes, want %d", len(res.Attachments[0].Data), size)
	}
	// The block the request carries follows from the attachment's modality, so
	// a read whose result reaches the model as a text line alone has lost the
	// picture.
	msg := poo.ToolResultMessage("c1", "read", res.Text, false, res.Attachments)
	if len(msg.Content) != 2 || msg.Content[1].Kind != poo.ContentImage {
		t.Fatalf("the result's message carries %+v, want the text and the picture", msg.Content)
	}
	// The text names the file and its size, so a front end that cannot draw
	// the image still says what the model saw.
	if !strings.Contains(res.Text, path) {
		t.Errorf("the text %q does not name the path", res.Text)
	}
	if !strings.Contains(res.Text, fmt.Sprintf("%d bytes", size)) {
		t.Errorf("the text %q does not name the size", res.Text)
	}
	if !strings.Contains(res.Text, "image") {
		t.Errorf("the text %q does not say it is an image", res.Text)
	}
}

// TestAnImageIsRefusedForAModelThatCannotReadIt covers the other model: the
// read says what the file is and costs a line, rather than sending a picture
// the provider would reject.
func TestAnImageIsRefusedForAModelThatCannotReadIt(t *testing.T) {
	dir := t.TempDir()
	_, size := pngFile(t, dir, "shot.png")

	res := call(t, read.New(), map[string]any{"path": "shot.png"}, dir)
	if len(res.Attachments) != 0 {
		t.Fatalf("a model that cannot read attachments was given %d", len(res.Attachments))
	}
	if !strings.Contains(res.Text, "does not accept image input") {
		t.Errorf("the refusal is %q, want it to say the model cannot read attachments", res.Text)
	}
	if !strings.Contains(res.Text, fmt.Sprintf("%d bytes", size)) {
		t.Errorf("the refusal %q does not name the size", res.Text)
	}
	// A refusal the model can act on is not an error that stops the run.
	if res.IsError {
		t.Error("the refusal is flagged as an error")
	}
}

// TestAnOversizedImageIsRefused covers the cap: an image past the provider's
// own limit is named rather than sent to fail there.
func TestAnOversizedImageIsRefused(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "big.png")
	// A PNG header plus padding: the sniff sees an image, the size is past
	// the cap.
	body := append([]byte("\x89PNG\r\n\x1a\n"), bytes.Repeat([]byte{0}, read.MaxImageBytes)...)
	if err := os.WriteFile(path, body, 0o644); err != nil {
		t.Fatalf("writing the image: %v", err)
	}

	res := call(t, read.New(read.WithInputs(poo.ModalityImage)), map[string]any{"path": "big.png"}, dir)
	if len(res.Attachments) != 0 {
		t.Fatalf("an oversized image was returned")
	}
	if !strings.Contains(res.Text, "limit") {
		t.Errorf("the refusal is %q, want it to name the limit", res.Text)
	}
}

// TestTheImageDescriptionIsOnlyOfferedToAModelThatReadsThem covers the
// description: a tool a model cannot use must not advertise the use.
func TestTheImageDescriptionIsOnlyOfferedToAModelThatReadsThem(t *testing.T) {
	without := read.New()
	with := read.New(read.WithInputs(poo.ModalityImage))
	if strings.Contains(without.Spec().Description, "image") {
		t.Errorf("a model that cannot read attachments is offered them: %q", without.Spec().Description)
	}
	if !strings.Contains(with.Spec().Description, "image") {
		t.Errorf("a model that reads attachments is not told so: %q", with.Spec().Description)
	}
	// The guidance the tool brings says the same thing as its description.
	if strings.Contains(without.PromptSection(prompt.Env{}), "image file") {
		t.Error("a model that cannot read attachments is told how to read one")
	}
	if !strings.Contains(with.PromptSection(prompt.Env{}), "image file") {
		t.Error("a model that reads attachments is not told how")
	}
}

// TestAGifAndAWebPAreImages covers the other media types by their headers.
func TestAGifAndAWebPAreImages(t *testing.T) {
	tests := []struct {
		name  string
		head  string
		media string
	}{
		{name: "a.gif", head: "GIF89a", media: "image/gif"},
		{name: "b.webp", head: "RIFF????WEBP", media: "image/webp"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			body := append([]byte(tt.head), bytes.Repeat([]byte{0}, 16)...)
			if err := os.WriteFile(filepath.Join(dir, tt.name), body, 0o644); err != nil {
				t.Fatalf("writing the file: %v", err)
			}
			res := call(t, read.New(read.WithInputs(poo.ModalityImage)), map[string]any{"path": tt.name}, dir)
			if len(res.Attachments) != 1 {
				t.Fatalf("the read returned %d attachments, want one: %s", len(res.Attachments), res.Text)
			}
			if res.Attachments[0].MediaType != tt.media {
				t.Errorf("the media type is %q, want %q", res.Attachments[0].MediaType, tt.media)
			}
		})
	}
}

// TestAJPEGAndItsExactLimit covers the other common header and the cap's
// edge: a file of exactly the limit is returned, one byte more is refused.
func TestAJPEGAndItsExactLimit(t *testing.T) {
	dir := t.TempDir()
	head := []byte("\xff\xd8\xff\xe0")
	exact := append(append([]byte(nil), head...), bytes.Repeat([]byte{0}, read.MaxImageBytes-len(head))...)
	if err := os.WriteFile(filepath.Join(dir, "exact.jpg"), exact, 0o644); err != nil {
		t.Fatalf("writing the image: %v", err)
	}
	res := call(t, read.New(read.WithInputs(poo.ModalityImage)), map[string]any{"path": "exact.jpg"}, dir)
	if len(res.Attachments) != 1 {
		t.Fatalf("an image of exactly the limit was refused: %s", res.Text)
	}
	if res.Attachments[0].MediaType != "image/jpeg" {
		t.Errorf("the media type is %q, want image/jpeg", res.Attachments[0].MediaType)
	}

	over := append(append([]byte(nil), head...), bytes.Repeat([]byte{0}, read.MaxImageBytes-len(head)+1)...)
	if err := os.WriteFile(filepath.Join(dir, "over.jpg"), over, 0o644); err != nil {
		t.Fatalf("writing the image: %v", err)
	}
	if res := call(t, read.New(read.WithInputs(poo.ModalityImage)), map[string]any{"path": "over.jpg"}, dir); len(res.Attachments) != 0 {
		t.Error("an image one byte past the limit was returned")
	}
}

// TestACancelledImageReadStops covers the context: a cancelled run does not
// pay to read five megabytes.
func TestACancelledImageReadStops(t *testing.T) {
	dir := t.TempDir()
	pngFile(t, dir, "shot.png")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	tool := read.New(read.WithInputs(poo.ModalityImage))
	raw, err := json.Marshal(map[string]any{"path": "shot.png"})
	if err != nil {
		t.Fatalf("marshalling args: %v", err)
	}
	prepared, err := tool.Prepare(raw, poo.PrepareEnv{WorkingDir: dir})
	if err != nil {
		t.Fatalf("preparing: %v", err)
	}
	res, err := tool.Call(ctx, prepared, poo.ToolEnv{PrepareEnv: poo.PrepareEnv{WorkingDir: dir}})
	if err != nil {
		t.Fatalf("calling: %v", err)
	}
	if len(res.Attachments) != 0 {
		t.Error("a cancelled run read the image")
	}
	if !res.IsError {
		t.Errorf("the cancellation is not an error: %q", res.Text)
	}
}

// TestTheRunsModelDecidesAtCallTime covers the switch: a session that changed
// models answers for the model it has now, not the one the tool was built for.
func TestTheRunsModelDecidesAtCallTime(t *testing.T) {
	dir := t.TempDir()
	pngFile(t, dir, "shot.png")

	callWith := func(t *testing.T, tool *read.Tool, inputs []poo.Modality) poo.ToolResult {
		t.Helper()
		raw, err := json.Marshal(map[string]any{"path": "shot.png"})
		if err != nil {
			t.Fatalf("marshalling args: %v", err)
		}
		prepared, err := tool.Prepare(raw, poo.PrepareEnv{WorkingDir: dir})
		if err != nil {
			t.Fatalf("preparing: %v", err)
		}
		res, err := tool.Call(context.Background(), prepared, poo.ToolEnv{
			PrepareEnv: poo.PrepareEnv{WorkingDir: dir},
			Model:      poo.ModelInfo{Inputs: inputs},
		})
		if err != nil {
			t.Fatalf("calling: %v", err)
		}
		return res
	}

	// Built for a text-only model, run for one that reads attachments.
	res := callWith(t, read.New(), []poo.Modality{poo.ModalityText, poo.ModalityImage})
	if len(res.Attachments) != 1 {
		t.Errorf("a model that reads attachments was given %d: %s", len(res.Attachments), res.Text)
	}

	// Built for a model that reads attachments, run for one that does not.
	res = callWith(t, read.New(read.WithInputs(poo.ModalityImage)), []poo.Modality{poo.ModalityText})
	if len(res.Attachments) != 0 {
		t.Error("a text-only model was given an image")
	}
	if !strings.Contains(res.Text, "does not accept image input") {
		t.Errorf("the refusal is %q, want it to name the reason", res.Text)
	}

	// A caller that names no model falls back to what the tool was built for.
	res = callWith(t, read.New(read.WithInputs(poo.ModalityImage)), nil)
	if len(res.Attachments) != 1 {
		t.Errorf("a caller with no model lost the tool's own answer: %s", res.Text)
	}
}

// TestEverySniffedTypeIsOneABlockCarries covers the coupling: a type the
// sniffer produces is a type the block layer accepts, so a new magic cannot
// be dropped on the way out.
func TestEverySniffedTypeIsOneABlockCarries(t *testing.T) {
	for _, media := range []string{"image/png", "image/jpeg", "image/gif", "image/webp"} {
		if !poo.IsMediaType(poo.ModalityImage, media) {
			t.Errorf("the sniffer produces %q, and a block would drop it", media)
		}
	}
}
