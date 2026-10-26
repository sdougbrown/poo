package poo_test

import (
	"testing"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/tool/bash"
)

// TestTheHeaderTheRunCommandWritesCanBeRestored builds the
// session header the way cmd/poo/main.go builds it (a tool's name and
// class, and no background mode) and restores it with the tool that
// binary links.
func TestTheHeaderTheRunCommandWritesCanBeRestored(t *testing.T) {
	tool := bash.New()
	spec := tool.Spec()
	if spec.Background != poo.BackgroundAllowed {
		t.Fatalf("the bash tool's background mode is %q, want %q", spec.Background, poo.BackgroundAllowed)
	}
	// cmd/poo/main.go: sessionTools = append(sessionTools,
	// poo.ToolInfo{Name: spec.Name, Class: spec.Class})
	recorded := []poo.ToolInfo{{Name: spec.Name, Class: spec.Class}}
	// the header the run command writes for this tool
	created := &poo.SessionCreatedRecord{
		RecordMeta: poo.NewRecordMeta("session.created"), SessionMetadata: poo.SessionMetadata{Format: poo.FormatVersion,
			SessionID:  "s1",
			Mode:       "logged",
			WorkingDir: "/tmp",
			Tools:      recorded, Model: poo.SessionModel{ModelID: "m"},
			ContextLimit: 32768,
			ToolsMode:    "native"},
	}
	records := []poo.Record{created}

	opts := poo.Options{
		Transport: scripted(nil),
		Tools:     map[string]poo.Tool{spec.Name: tool},
		ToolOrder: []string{spec.Name},
		Model:     poo.ModelInfo{Name: "m", ModelID: "m", ContextLimit: 32768},
		Models:    map[string]poo.ModelInfo{"m": {Name: "m", ModelID: "m", ContextLimit: 32768}},
	}
	_, _, err := poo.Restore(opts, records)
	if err != nil {
		t.Fatalf("the restore of a session this binary wrote failed: %v", err)
	}
}
