package poo

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"sync"
	"testing"
)

// largePNG renders a PNG of the given size.
func largePNG(t *testing.T, width, height int) []byte {
	t.Helper()
	img := image.NewGray(image.Rect(0, 0, width, height))
	img.Set(0, 0, color.Gray{Y: 255})
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encoding the image: %v", err)
	}
	return buf.Bytes()
}

// TestAnImageCostsItsPixels covers the estimate: a picture is charged by its
// pixels, not its bytes, so a large image is not invisible to the budget and a
// small one is not charged megabytes.
func TestAnImageCostsItsPixels(t *testing.T) {
	smallPNG := largePNG(t, 32, 32)
	largePNGData := largePNG(t, 1024, 1024)
	small := estimateMessageChars(ToolResultMessage("c1", "read", "read: small.png", false,
		[]Image{{MediaType: "image/png", Data: smallPNG}}))
	large := estimateMessageChars(ToolResultMessage("c2", "read", "read: large.png", false,
		[]Image{{MediaType: "image/png", Data: largePNGData}}))
	if large <= small {
		t.Fatalf("a 1024x1024 image costs %d chars, and a 32x32 one %d", large, small)
	}
	// The picture's contribution is its pixels at the shared rule, not its
	// compressed size.
	plain := estimateMessageChars(ToolResultMessage("c2", "read", "read: large.png", false, nil))
	if got, want := large-plain, imageChars(largePNGData); got != want {
		t.Errorf("the picture costs %d chars, want %d", got, want)
	}
	// An image whose header cannot be read is never free.
	unknown := estimateMessageChars(ToolResultMessage("c3", "read", "read: x.webp", false,
		[]Image{{MediaType: "image/webp", Data: []byte("RIFF....WEBP")}}))
	if unknown <= small {
		t.Errorf("an unreadable header costs %d chars, and a 32x32 image %d", unknown, small)
	}
}

// TestCloningAMessageKeepsItsOwnImageBytes covers the copy: a clone owns its
// bytes, so a consumer mutating them cannot corrupt the session's history.
func TestCloningAMessageKeepsItsOwnImageBytes(t *testing.T) {
	original := ToolResultMessage("c1", "read", "read: shot.png", false,
		[]Image{{MediaType: "image/png", Data: []byte{1, 2, 3}}})
	cloned := CloneMessages([]Message{original})
	cloned[0].Content[1].Data[0] = 9
	if original.Content[1].Data[0] != 1 {
		t.Error("the clone shares the original's image bytes")
	}
}

// TestACopyOfAResultKeepsItsOwnImageBytes covers the other copy.
func TestACopyOfAResultKeepsItsOwnImageBytes(t *testing.T) {
	result := ToolResult{Text: "read", Images: []Image{{MediaType: "image/png", Data: []byte{1, 2, 3}}}}
	cloned := copyResult(result)
	cloned.Images[0].Data[0] = 9
	if result.Images[0].Data[0] != 1 {
		t.Error("the copied result shares the original's image bytes")
	}
}

// TestAModelThatCannotReadImagesIsSentNone covers the switch: the history
// keeps a picture, and a model that does not read images is not sent it.
func TestAModelThatCannotReadImagesIsSentNone(t *testing.T) {
	messages := []Message{ToolResultMessage("c1", "read", "read: shot.png", false,
		[]Image{{MediaType: "image/png", Data: []byte{1}}})}

	kept := stripUnreadableImages(messages, ModelInfo{Inputs: []Modality{ModalityText, ModalityImage}})
	if len(kept[0].Content) != 2 {
		t.Fatalf("a model that reads images lost the picture: %+v", kept[0].Content)
	}
	stripped := stripUnreadableImages(messages, ModelInfo{Inputs: []Modality{ModalityText}})
	if len(stripped[0].Content) != 1 {
		t.Fatalf("a model that reads text kept %d blocks, want the result alone", len(stripped[0].Content))
	}
	if stripped[0].Content[0].Kind != ContentToolResult {
		t.Errorf("the remaining block is %q, want the tool result", stripped[0].Content[0].Kind)
	}
	// The original message still holds its picture.
	if len(messages[0].Content) != 2 {
		t.Error("stripping mutated the history")
	}
}

// TestAnImageOfAnUnknownTypeIsLeftOut covers the allowlist: a block whose
// media type no provider accepts is not carried, so a mislabelled picture
// cannot fail every request the history holds.
func TestAnImageOfAnUnknownTypeIsLeftOut(t *testing.T) {
	msg := ToolResultMessage("c1", "read", "read: x.bmp", false, []Image{
		{MediaType: "image/bmp", Data: []byte{1}},
		{MediaType: "image/png", Data: []byte{2}},
	})
	if len(msg.Content) != 2 {
		t.Fatalf("the message holds %d blocks, want the result and one picture", len(msg.Content))
	}
	if msg.Content[1].MediaType != "image/png" {
		t.Errorf("the kept picture is %q, want image/png", msg.Content[1].MediaType)
	}
}

// TestAModelSwitchRacesTheCallEnvironment covers the snapshot: a background
// job builds its environment while a switch may be writing the model, so the
// read is taken under the same lock. Run under -race, an unsynchronized read
// fails here.
func TestAModelSwitchRacesTheCallEnvironment(t *testing.T) {
	s, err := New(Options{
		SessionID: "s1",
		Transport: stubTransport{},
		Model:     ModelInfo{Name: "m", ModelID: "m", Endpoint: "test", API: APIChatCompletions, Ops: []Op{OpChat}, ContextLimit: 8192},
	})
	if err != nil {
		t.Fatalf("building the session: %v", err)
	}
	t.Cleanup(func() { s.Close() })

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < 200; i++ {
			_ = s.toolEnvFor("r1", "r1")
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < 200; i++ {
			_, _ = s.SetModel(ModelInfo{
				Name:   "m2",
				API:    APIChatCompletions,
				Ops:    []Op{OpChat},
				Inputs: []Modality{ModalityText, ModalityImage},
			})
		}
	}()
	wg.Wait()
}
