package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sdougbrown/poo/prompt"
)

func TestPromptDefaultRendersRawWirePrompt(t *testing.T) {
	got := promptFor(t, "code")
	if strings.Contains(got, "--- persona ---") || strings.Contains(got, "--- base ---") {
		t.Fatalf("default poo prompt included pretty section markers:\n%s", got)
	}
	for _, tag := range []string{"<base>", "</base>", "<profile>", "</profile>", "<tools>", "</tools>", "<environment>", "</environment>"} {
		if !strings.Contains(got, tag) {
			t.Errorf("default poo prompt missing %q:\n%s", tag, got)
		}
	}
}

func TestPromptPrettyFormatsSectionsAndSkills(t *testing.T) {
	dir := t.TempDir()
	settings := filepath.Join(dir, "settings.yaml")
	body := "version: 1\nendpoints:\n  test:\n    endpoint: http://localhost:4000/v1\nmodels:\n  test-model:\n    endpoint: test\n    model_id: test-model\n    api: chat_completions\n    tools_mode: native\n    ops: [chat]\n    context_limit: 32768\nroles:\n  chat: test-model\n"
	if err := os.WriteFile(settings, []byte(body), 0o644); err != nil {
		t.Fatalf("writing the settings: %v", err)
	}
	code, stderr, stdout := runCLIWithStdout(t, "prompt", "--pretty", "--profile", "base", "--settings", settings)
	if code != 0 {
		t.Fatalf("poo prompt --pretty exited with %d; stderr: %s", code, stderr)
	}
	for _, marker := range []string{"--- persona ---", "--- base ---", "--- environment ---"} {
		if !strings.Contains(stdout, marker) {
			t.Errorf("poo prompt --pretty missing %q:\n%s", marker, stdout)
		}
	}
	if strings.Contains(stdout, "<base>") {
		t.Errorf("poo prompt --pretty should not wrap sections in XML tags:\n%s", stdout)
	}
}

func TestWritePrettyPromptSummarizesSkillsXML(t *testing.T) {
	var buf bytes.Buffer
	writePrettyPrompt(&buf, []prompt.SectionText{
		{
			Name: "skills",
			Text: "Intro line.\n\n<available_skills>\n<skill>\n    <name>commit</name>\n    <description>Commit & PR styleguide.</description>\n    <location>/tmp/commit/SKILL.md</location>\n</skill>\n<skill>\n    <name>review</name>\n    <description>Review a diff.</description>\n    <location>/tmp/review/SKILL.md</location>\n</skill>\n</available_skills>",
		},
	})
	got := buf.String()
	if strings.Contains(got, "<available_skills>") || strings.Contains(got, "<location>") {
		t.Fatalf("pretty skills output kept raw XML:\n%s", got)
	}
	for _, want := range []string{
		"--- skills ---",
		"Intro line.",
		"Available skills (2):",
		"- commit: Commit & PR styleguide.",
		"- review: Review a diff.",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("pretty skills output missing %q:\n%s", want, got)
		}
	}
}
