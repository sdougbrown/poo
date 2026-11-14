package read_test

import (
	"encoding/binary"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/prompt"
	"github.com/sdougbrown/poo/tool/read"
)

// wavData renders a mono 16-bit WAV of the given length.
func wavData(seconds float64) []byte {
	const rate = 16000
	frames := int(seconds * rate)
	body := make([]byte, frames*2)
	for i := 0; i < frames; i++ {
		sample := int16(8000 * math.Sin(2*math.Pi*440*float64(i)/rate))
		binary.LittleEndian.PutUint16(body[i*2:], uint16(sample))
	}
	total := 44 + len(body)
	out := make([]byte, 44, total)
	copy(out[0:4], "RIFF")
	binary.LittleEndian.PutUint32(out[4:8], uint32(total-8))
	copy(out[8:12], "WAVE")
	copy(out[12:16], "fmt ")
	binary.LittleEndian.PutUint32(out[16:20], 16)
	binary.LittleEndian.PutUint16(out[20:22], 1) // PCM
	binary.LittleEndian.PutUint16(out[22:24], 1) // mono
	binary.LittleEndian.PutUint32(out[24:28], rate)
	binary.LittleEndian.PutUint32(out[28:32], rate*2)
	binary.LittleEndian.PutUint16(out[32:34], 2)
	binary.LittleEndian.PutUint16(out[34:36], 16) // bits
	copy(out[36:40], "data")
	binary.LittleEndian.PutUint32(out[40:44], uint32(len(body)))
	return append(out, body...)
}

// writeAudio writes a recording into dir and returns its path and size.
func writeAudio(t *testing.T, dir, name string, data []byte) (string, int) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("writing the audio: %v", err)
	}
	return path, len(data)
}

// TestAudioIsReturnedAsTheSound covers the model that reads audio: the file
// comes back as an audio attachment beside a line naming it.
func TestAudioIsReturnedAsTheSound(t *testing.T) {
	dir := t.TempDir()
	data := wavData(0.5)
	path, size := writeAudio(t, dir, "clip.wav", data)

	res := call(t, read.New(read.WithInputs(poo.ModalityAudio)), map[string]any{"path": "clip.wav"}, dir)
	if res.IsError {
		t.Fatalf("reading audio failed: %s", res.Text)
	}
	if len(res.Attachments) != 1 {
		t.Fatalf("the read returned %d attachments, want one", len(res.Attachments))
	}
	attachment := res.Attachments[0]
	if attachment.Modality != poo.ModalityAudio {
		t.Errorf("the attachment's modality is %q, want audio", attachment.Modality)
	}
	if attachment.MediaType != "audio/wav" {
		t.Errorf("the media type is %q, want audio/wav", attachment.MediaType)
	}
	if len(attachment.Data) != size {
		t.Errorf("the attachment holds %d bytes, want %d", len(attachment.Data), size)
	}
	if !strings.Contains(res.Text, path) || !strings.Contains(res.Text, "audio") {
		t.Errorf("the text %q does not name the file and its modality", res.Text)
	}
}

// TestAudioIsRefusedForAModelThatCannotReadIt covers the other model.
func TestAudioIsRefusedForAModelThatCannotReadIt(t *testing.T) {
	dir := t.TempDir()
	writeAudio(t, dir, "clip.wav", wavData(0.25))

	res := call(t, read.New(read.WithInputs(poo.ModalityImage)), map[string]any{"path": "clip.wav"}, dir)
	if len(res.Attachments) != 0 {
		t.Fatalf("a text-only model was given %d attachments", len(res.Attachments))
	}
	if !strings.Contains(res.Text, "does not accept audio input") {
		t.Errorf("the refusal is %q, want it to name the modality", res.Text)
	}
	if !strings.Contains(res.Text, "bytes)") {
		t.Errorf("the refusal %q does not name the size", res.Text)
	}
	if res.IsError {
		t.Error("the refusal is flagged as an error")
	}
}

// TestAnMP3AndItsHeaderAreRecognised covers the other accepted format: a file
// that starts with an ID3 tag is audio/mpeg.
func TestAnMP3AndItsHeaderAreRecognised(t *testing.T) {
	dir := t.TempDir()
	body := append([]byte("ID3\x04\x00\x00"), make([]byte, 64)...)
	writeAudio(t, dir, "clip.mp3", body)

	res := call(t, read.New(read.WithInputs(poo.ModalityAudio)), map[string]any{"path": "clip.mp3"}, dir)
	if len(res.Attachments) != 1 {
		t.Fatalf("the read returned %d attachments, want one: %s", len(res.Attachments), res.Text)
	}
	if res.Attachments[0].MediaType != "audio/mpeg" {
		t.Errorf("the media type is %q, want audio/mpeg", res.Attachments[0].MediaType)
	}
}

// TestAnOversizedRecordingIsRefused covers the cap: a recording of exactly the
// limit is returned, and one byte more is refused.
func TestAnOversizedRecordingIsRefused(t *testing.T) {
	dir := t.TempDir()
	head := []byte("ID3\x04\x00\x00")
	exact := append(append([]byte(nil), head...), make([]byte, read.MaxAudioBytes-len(head))...)
	writeAudio(t, dir, "exact.mp3", exact)
	if res := call(t, read.New(read.WithInputs(poo.ModalityAudio)), map[string]any{"path": "exact.mp3"}, dir); len(res.Attachments) != 1 {
		t.Fatalf("a recording of exactly the limit was refused: %s", res.Text)
	}

	body := append(append([]byte(nil), head...), make([]byte, read.MaxAudioBytes-len(head)+1)...)
	writeAudio(t, dir, "big.mp3", body)

	res := call(t, read.New(read.WithInputs(poo.ModalityAudio)), map[string]any{"path": "big.mp3"}, dir)
	if len(res.Attachments) != 0 {
		t.Fatal("an oversized recording was returned")
	}
	if !strings.Contains(res.Text, "limit") {
		t.Errorf("the refusal is %q, want it to name the limit", res.Text)
	}
}

// TestAMp3WithoutATagIsRecognised covers the other MP3 shape: a file that starts
// straight at a frame sync, with no ID3 tag in front of it.
func TestAMp3WithoutATagIsRecognised(t *testing.T) {
	dir := t.TempDir()
	// A frame sync: eleven set bits, then the rest of the header.
	body := append([]byte{0xff, 0xfb, 0x90, 0x00}, make([]byte, 400)...)
	writeAudio(t, dir, "clip.mp3", body)

	res := call(t, read.New(read.WithInputs(poo.ModalityAudio)), map[string]any{"path": "clip.mp3"}, dir)
	if len(res.Attachments) != 1 {
		t.Fatalf("the read returned %d attachments, want one: %s", len(res.Attachments), res.Text)
	}
	if res.Attachments[0].MediaType != "audio/mpeg" {
		t.Errorf("the media type is %q, want audio/mpeg", res.Attachments[0].MediaType)
	}
}

// TestTheAudioDescriptionIsOnlyOfferedToAModelThatReadsIt covers the
// description and the guidance: a tool a model cannot use must not advertise
// the use.
func TestTheAudioDescriptionIsOnlyOfferedToAModelThatReadsIt(t *testing.T) {
	without := read.New()
	with := read.New(read.WithInputs(poo.ModalityAudio))
	if strings.Contains(without.Spec().Description, "audio") {
		t.Errorf("a model that cannot read audio is offered it: %q", without.Spec().Description)
	}
	if !strings.Contains(with.Spec().Description, "audio") {
		t.Errorf("a model that reads audio is not told so: %q", with.Spec().Description)
	}
	// The guidance the tool brings says the same thing as its description.
	if strings.Contains(without.PromptSection(prompt.Env{}), "audio file") {
		t.Error("a model that cannot read audio is told how to read one")
	}
	if !strings.Contains(with.PromptSection(prompt.Env{}), "audio file") {
		t.Error("a model that reads audio is not told how")
	}
}

// TestAnAdtsStreamIsNotAnMp3 covers the stream that shares the syncword: a raw
// AAC file starts 0xFF 0xF9, and the two bits MP3 reserves for a layer are what
// tells the two apart.
func TestAnAdtsStreamIsNotAnMp3(t *testing.T) {
	dir := t.TempDir()
	// ADTS: the syncword, then the reserved layer bits MP3 never sets.
	body := append([]byte{0xff, 0xf9, 0x50, 0x80, 0x00}, make([]byte, 400)...)
	writeAudio(t, dir, "clip.aac", body)

	res := call(t, read.New(read.WithInputs(poo.ModalityAudio)), map[string]any{"path": "clip.aac"}, dir)
	if len(res.Attachments) != 0 {
		t.Fatalf("an ADTS stream was returned as %+v", res.Attachments[0])
	}
}

// TestAModelThatReadsBothModalitiesIsToldBoth covers the configuration this
// change enables: one line for each modality, in the description and in the
// guidance.
func TestAModelThatReadsBothModalitiesIsToldBoth(t *testing.T) {
	both := read.New(read.WithInputs(poo.ModalityImage, poo.ModalityAudio))
	description := both.Spec().Description
	if !strings.Contains(description, "image") || !strings.Contains(description, "audio") {
		t.Errorf("the description offers one modality only: %q", description)
	}
	guidance := both.PromptSection(prompt.Env{})
	if !strings.Contains(guidance, "image file") || !strings.Contains(guidance, "audio file") {
		t.Errorf("the guidance offers one modality only: %q", guidance)
	}
}
