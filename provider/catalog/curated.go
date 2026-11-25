package catalog

import (
	"strings"

	"github.com/sdougbrown/poo"
)

// curated is the curated layer: the family rules.
//
// The layer describes families of models — a local model a llama.cpp server
// serves, a gateway's GLM — and nothing else. What a hosted provider serves is
// the provider's own report, read by the probes, because a snapshot in the
// binary goes stale the day the provider adds or retires a model, and the
// endpoint that serves it is the one thing poo does not have to guess.
type curated struct{}

// newCurated returns the curated layer.
func newCurated() *curated {
	return &curated{}
}

// forModel returns the curated fields and the notes for a model id.
//
// A family rule describes a family of models, so a model id no rule knows has no
// curated fields.
func (c *curated) forModel(modelID string) (poo.ModelInfo, []string) {
	info := poo.ModelInfo{}
	var notes []string
	for _, family := range families {
		if !family.match(modelID) {
			continue
		}
		family.describe(&info)
		notes = append(notes, family.note)
	}
	return info, notes
}

// modalities maps the modality names a layer reads to the modalities poo
// names: a bare "text" or a MIME type, in either letter case.
//
// Every layer that reads modality names — the models file, the settings and the
// discovery probe's model list — maps them here, so one reported name means one
// modality whichever layer read it.
func modalities(names []string) []poo.Modality {
	var out []poo.Modality
	for _, name := range names {
		name := strings.ToLower(name)
		switch {
		case name == "text":
			out = append(out, poo.ModalityText)
		case name == "image", strings.HasPrefix(name, "image/"):
			out = append(out, poo.ModalityImage)
		case name == "audio", strings.HasPrefix(name, "audio/"):
			out = append(out, poo.ModalityAudio)
		}
	}
	return out
}
