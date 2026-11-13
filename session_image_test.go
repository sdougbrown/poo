package poo_test

import (
	"context"
	"slices"
	"testing"

	"github.com/sdougbrown/poo"
)

// TestATextOnlyModelIsSentNoImage covers the request's gate: a tool may hand
// back a picture, and a model whose inputs do not declare images is not sent
// it.
func TestATextOnlyModelIsSentNoImage(t *testing.T) {
	tool := &echoTool{call: func(context.Context) (poo.ToolResult, error) {
		return poo.ToolResult{
			Text:   "read: shot.png (image, 4 bytes)",
			Images: []poo.Image{{MediaType: "image/png", Data: []byte{1, 2, 3, 4}}},
		}, nil
	}}
	tr := scripted([]fakeResponse{toolCallTurn(), {text: "done"}})
	opts := testOptions(t, tr, tool, &recordingSink{})
	if len(opts.Model.Inputs) != 0 {
		t.Fatalf("the case's model declares inputs %v, want none", opts.Model.Inputs)
	}
	s := newTestSession(t, opts)
	runSession(t, s, "start")

	for _, req := range tr.Requests() {
		for _, m := range req.Messages {
			for _, b := range m.Content {
				if b.Kind == poo.ContentImage {
					t.Fatal("a model that reads no images was sent one")
				}
			}
		}
	}
}

// TestAModelThatReadsImagesIsSentOne covers the other half: the same session
// with the modality declared sends the picture.
func TestAModelThatReadsImagesIsSentOne(t *testing.T) {
	tool := &echoTool{call: func(context.Context) (poo.ToolResult, error) {
		return poo.ToolResult{
			Text:   "read: shot.png (image, 4 bytes)",
			Images: []poo.Image{{MediaType: "image/png", Data: []byte{1, 2, 3, 4}}},
		}, nil
	}}
	tr := scripted([]fakeResponse{toolCallTurn(), {text: "done"}})
	opts := testOptions(t, tr, tool, &recordingSink{})
	opts.Model.Inputs = []poo.Modality{poo.ModalityText, poo.ModalityImage}
	s := newTestSession(t, opts)
	runSession(t, s, "start")

	sent := false
	for _, req := range tr.Requests() {
		for _, m := range req.Messages {
			for _, b := range m.Content {
				if b.Kind == poo.ContentImage {
					sent = true
				}
			}
		}
	}
	if !sent {
		t.Error("a model that reads images was sent none")
	}
}

// TestACallSeesTheRunsModel covers the seam the read tool gates on: the
// environment a tool runs in names the model the run speaks to, so a session
// that switched models answers for the one it has now.
func TestACallSeesTheRunsModel(t *testing.T) {
	tool := &echoTool{result: "ok"}
	tr := scripted([]fakeResponse{toolCallTurn(), {text: "done"}})
	opts := testOptions(t, tr, tool, &recordingSink{})
	opts.Model.Inputs = []poo.Modality{poo.ModalityText, poo.ModalityImage}
	s := newTestSession(t, opts)
	runSession(t, s, "start")

	envs := tool.Envs()
	if len(envs) == 0 {
		t.Fatal("the tool ran with no environment")
	}
	if !slices.Contains(envs[0].Model.Inputs, poo.ModalityImage) {
		t.Errorf("the call's model inputs are %v, want the image modality", envs[0].Model.Inputs)
	}
}
