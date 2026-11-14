package poo

import (
	"bytes"
	"encoding/binary"
	"hash/crc32"
	"image"
	"image/color"
	"image/png"
	"math"
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
		[]Attachment{{Modality: ModalityImage, MediaType: "image/png", Data: smallPNG}}))
	large := estimateMessageChars(ToolResultMessage("c2", "read", "read: large.png", false,
		[]Attachment{{Modality: ModalityImage, MediaType: "image/png", Data: largePNGData}}))
	if large <= small {
		t.Fatalf("a 1024x1024 image costs %d chars, and a 32x32 one %d", large, small)
	}
	// An image whose header cannot be read is never free.
	unknown := estimateMessageChars(ToolResultMessage("c3", "read", "read: x.webp", false,
		[]Attachment{{Modality: ModalityImage, MediaType: "image/webp", Data: []byte("RIFF....WEBP")}}))
	if unknown <= small {
		t.Errorf("an unreadable header costs %d chars, and a 32x32 image %d", unknown, small)
	}
}

// TestCloningAMessageKeepsItsOwnImageBytes covers the copy: a clone owns its
// bytes, so a consumer mutating them cannot corrupt the session's history.
func TestCloningAMessageKeepsItsOwnImageBytes(t *testing.T) {
	original := ToolResultMessage("c1", "read", "read: shot.png", false,
		[]Attachment{{Modality: ModalityImage, MediaType: "image/png", Data: []byte{1, 2, 3}}})
	cloned := CloneMessages([]Message{original})
	cloned[0].Content[1].Data[0] = 9
	if original.Content[1].Data[0] != 1 {
		t.Error("the clone shares the original's image bytes")
	}
}

// TestACopyOfAResultKeepsItsOwnImageBytes covers the other copy.
func TestACopyOfAResultKeepsItsOwnImageBytes(t *testing.T) {
	result := ToolResult{Text: "read", Attachments: []Attachment{{Modality: ModalityImage, MediaType: "image/png", Data: []byte{1, 2, 3}}}}
	cloned := copyResult(result)
	cloned.Attachments[0].Data[0] = 9
	if result.Attachments[0].Data[0] != 1 {
		t.Error("the copied result shares the original's image bytes")
	}
}

// TestAModelThatCannotReadImagesIsSentNone covers the switch: the history
// keeps a picture, and a model that does not read attachments is not sent it.
func TestAModelThatCannotReadImagesIsSentNone(t *testing.T) {
	messages := []Message{ToolResultMessage("c1", "read", "read: shot.png", false,
		[]Attachment{{Modality: ModalityImage, MediaType: "image/png", Data: []byte{1}}})}

	kept := stripUnreadableAttachments(messages, ModelInfo{Inputs: []Modality{ModalityText, ModalityImage}})
	if len(kept[0].Content) != 2 {
		t.Fatalf("a model that reads attachments lost the picture: %+v", kept[0].Content)
	}
	stripped := stripUnreadableAttachments(messages, ModelInfo{Inputs: []Modality{ModalityText}})
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
	msg := ToolResultMessage("c1", "read", "read: x.bmp", false, []Attachment{
		{Modality: ModalityImage, MediaType: "image/bmp", Data: []byte{1}},
		{Modality: ModalityImage, MediaType: "image/png", Data: []byte{2}},
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

// TestAnOversizedHeaderIsChargedTheCap covers the clamp: a header may name
// any size, and a declared one is not a reason to bill the window for it. The
// header is real, so the decoder reads it; the pixels it names are not.
func TestAnOversizedHeaderIsChargedTheCap(t *testing.T) {
	ihdr := make([]byte, 13)
	binary.BigEndian.PutUint32(ihdr[0:], 20000)
	binary.BigEndian.PutUint32(ihdr[4:], 20000)
	ihdr[8] = 8
	ihdr[9] = 2
	chunk := func(kind string, data []byte) []byte {
		out := binary.BigEndian.AppendUint32(nil, uint32(len(data)))
		out = append(out, kind...)
		out = append(out, data...)
		return binary.BigEndian.AppendUint32(out, crc32.ChecksumIEEE(append([]byte(kind), data...)))
	}
	head := append([]byte("\x89PNG\r\n\x1a\n"), chunk("IHDR", ihdr)...)
	head = append(head, chunk("IEND", nil)...)
	config, _, err := image.DecodeConfig(bytes.NewReader(head))
	if err != nil {
		t.Fatalf("the header did not decode: %v", err)
	}
	if got := config.Width * config.Height; got <= imageMaxPixels {
		t.Fatalf("the header names %d pixels, want more than the %d cap", got, imageMaxPixels)
	}

	pixels := float64(imageMaxPixels)
	want := int(pixels / imagePixelsPerToken * CharsPerToken)
	if got := attachmentChars(ContentBlock{Kind: ContentImage, Data: head}); got != want {
		t.Errorf("an image of declared %d pixels costs %d chars, want the cap's %d", config.Width*config.Height, got, want)
	}
}

// TestAudioCostsItsSeconds covers the estimator's audio rule: a recording is
// charged by its length, and one whose header does not parse is charged a
// default rather than nothing.
func TestAudioCostsItsSeconds(t *testing.T) {
	short := wavForTest(t, 1)
	long := wavForTest(t, 10)
	shortChars := attachmentChars(ContentBlock{Kind: ContentAudio, Data: short})
	longChars := attachmentChars(ContentBlock{Kind: ContentAudio, Data: long})
	if longChars <= shortChars {
		t.Fatalf("ten seconds cost %d chars and one second %d", longChars, shortChars)
	}
	// One second at the shared rule, within rounding.
	wantShort := int(math.Round(float64(audioTokensPerSecond) * CharsPerToken))
	if diff := shortChars - wantShort; diff > 100 || diff < -100 {
		t.Errorf("one second costs %d chars, want about %d", shortChars, wantShort)
	}
	unknown := attachmentChars(ContentBlock{Kind: ContentAudio, MediaType: "audio/mpeg", Data: []byte("ID3nope")})
	if unknown != int(float64(audioDefaultSeconds)*audioTokensPerSecond*CharsPerToken) {
		t.Errorf("an unknown clip costs %d chars, want the default's", unknown)
	}
	// A header that declares a rate as low as it likes is capped, the way a
	// picture whose header names an enormous size is.
	slow := attachmentChars(ContentBlock{Kind: ContentAudio, Data: wavWithRate(t, 1, 4096)})
	if want := int(float64(audioMaxSeconds) * audioTokensPerSecond * CharsPerToken); slow != want {
		t.Errorf("a clip declared at 1 Hz costs %d chars, want the cap's %d", slow, want)
	}
	// A header that does not parse is charged the default, so a corrupt file
	// is never free: a format chunk cut short of its fields, and one that
	// names no sample rate.
	defaultChars := int(float64(audioDefaultSeconds) * audioTokensPerSecond * CharsPerToken)
	truncated := []byte("RIFF\x00\x00\x00\x00WAVEfmt \x10\x00\x00\x00")
	if got := attachmentChars(ContentBlock{Kind: ContentAudio, Data: truncated}); got != defaultChars {
		t.Errorf("a clip cut short inside its format chunk costs %d chars, want the default's %d", got, defaultChars)
	}
	unsigned := wavWithRate(t, 0, 100)
	if got := attachmentChars(ContentBlock{Kind: ContentAudio, Data: unsigned}); got != defaultChars {
		t.Errorf("a clip that names no sample rate costs %d chars, want the default's %d", got, defaultChars)
	}
	// A file cut short of the size its header declares is as long as the
	// bytes that are left, not as long as it claimed.
	cut := wavForTest(t, 2)
	cut = cut[:len(cut)/2]
	// The same sound, with a header that declares what the file holds.
	honest := wavWithRate(t, 8000, (len(cut)-44)/2)
	if got, want := attachmentChars(ContentBlock{Kind: ContentAudio, Data: cut}), attachmentChars(ContentBlock{Kind: ContentAudio, Data: honest}); got != want {
		t.Errorf("a truncated clip costs %d chars, want the %d of the bytes it holds", got, want)
	}
	// A metadata chunk before the sound is a chunk, not frames: a recording
	// with one is as long as the same recording without.
	// The chunk is half a second of sound at this rate, so counting it as
	// frames would charge half again as much.
	listing := wavWithMetadata(t, short, 8000)
	if got := attachmentChars(ContentBlock{Kind: ContentAudio, Data: listing}); got != shortChars {
		t.Errorf("a clip with a metadata chunk costs %d chars, want the %d of the same clip without one", got, shortChars)
	}
}

// TestAModelThatCannotReadAudioIsSentNone covers the strip: the block goes out
// of the request and stays in the history.
func TestAModelThatCannotReadAudioIsSentNone(t *testing.T) {
	audio := ToolResultMessage("c1", "read", "read: clip.wav", false,
		[]Attachment{{Modality: ModalityAudio, MediaType: "audio/wav", Data: []byte{1}}})
	kept := stripUnreadableAttachments([]Message{audio}, ModelInfo{Inputs: []Modality{ModalityText, ModalityAudio}})
	if len(kept[0].Content) != 2 {
		t.Fatalf("a model that reads audio lost the recording: %+v", kept[0].Content)
	}
	history := []Message{audio}
	stripped := stripUnreadableAttachments(history, ModelInfo{Inputs: []Modality{ModalityText, ModalityImage}})
	if len(stripped[0].Content) != 1 {
		t.Errorf("a model that reads no audio kept %d blocks", len(stripped[0].Content))
	}
	// The history keeps the recording: a model swapped back sends it again.
	if len(history[0].Content) != 2 {
		t.Errorf("stripping left %d blocks in the history, want the result and the recording", len(history[0].Content))
	}
}

// wavForTest renders a mono 16-bit WAV of the given length.
func wavForTest(t *testing.T, seconds int) []byte {
	t.Helper()
	return wavWithRate(t, 8000, seconds*8000)
}

// wavWithRate renders a mono 16-bit WAV of the given frame rate and length.
func wavWithRate(t *testing.T, rate, frames int) []byte {
	t.Helper()
	body := make([]byte, frames*2)
	out := make([]byte, 44, 44+len(body))
	copy(out[0:4], "RIFF")
	binary.LittleEndian.PutUint32(out[4:8], uint32(len(body)+36))
	copy(out[8:12], "WAVE")
	copy(out[12:16], "fmt ")
	binary.LittleEndian.PutUint32(out[16:20], 16)
	binary.LittleEndian.PutUint16(out[20:22], 1)
	binary.LittleEndian.PutUint16(out[22:24], 1)
	binary.LittleEndian.PutUint32(out[24:28], uint32(rate))
	binary.LittleEndian.PutUint32(out[28:32], uint32(rate*2))
	binary.LittleEndian.PutUint16(out[32:34], 2)
	binary.LittleEndian.PutUint16(out[34:36], 16)
	copy(out[36:40], "data")
	binary.LittleEndian.PutUint32(out[40:44], uint32(len(body)))
	return append(out, body...)
}

// wavWithMetadata inserts a LIST chunk of the given size before a WAV's sound.
func wavWithMetadata(t *testing.T, wav []byte, size int) []byte {
	t.Helper()
	// The header is the RIFF tag and the format chunk, up to the sound.
	const header = 36
	out := append([]byte(nil), wav[:header]...)
	chunk := make([]byte, 8+size+size%2)
	copy(chunk[0:4], "LIST")
	binary.LittleEndian.PutUint32(chunk[4:8], uint32(size))
	out = append(out, chunk...)
	out = append(out, wav[header:]...)
	binary.LittleEndian.PutUint32(out[4:8], uint32(len(out)-8))
	return out
}
