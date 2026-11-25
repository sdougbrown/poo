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
	skillDir := filepath.Join(dir, "skills", "demo")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatalf("creating the skill dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("---\ndescription: Demo skill guidance.\n---\n\nBody.\n"), 0o644); err != nil {
		t.Fatalf("writing the skill: %v", err)
	}
	settings := filepath.Join(dir, "settings.yaml")
	body := "version: 1\nskills:\n  paths:\n    - " + filepath.Join(dir, "skills") + "\nendpoints:\n  test:\n    endpoint: http://localhost:4000/v1\nmodels:\n  test-model:\n    endpoint: test\n    model_id: test-model\n    api: chat_completions\n    tools_mode: native\n    ops: [chat]\n    context_limit: 32768\nroles:\n  chat: test-model\n"
	if err := os.WriteFile(settings, []byte(body), 0o644); err != nil {
		t.Fatalf("writing the settings: %v", err)
	}
	code, stderr, stdout := runCLIWithStdout(t, "prompt", "--pretty", "--profile", "base", "--settings", settings)
	if code != 0 {
		t.Fatalf("poo prompt --pretty exited with %d; stderr: %s", code, stderr)
	}
	for _, want := range []string{
		"--- persona ---",
		"--- base ---",
		"--- skills ---",
		"use that absolute path in tool calls.",
		"Available skills (1):",
		"- demo: Demo skill guidance.",
		"--- environment ---",
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("poo prompt --pretty missing %q:\n%s", want, stdout)
		}
	}
	if strings.Contains(stdout, "<base>") || strings.Contains(stdout, "<available_skills>") {
		t.Errorf("poo prompt --pretty should not emit XML wrappers:\n%s", stdout)
	}
}

func TestWritePrettyPromptSummarizesSkillsXML(t *testing.T) {
	t.Run("multiple skills and trailing text", func(t *testing.T) {
		var buf bytes.Buffer
		writePrettyPrompt(&buf, []prompt.SectionText{
			{
				Name: "skills",
				Text: "Intro line.\n\n<available_skills>\n<skill>\n    <name>commit</name>\n    <description>Mentions </available_skills> & PR styleguide.</description>\n    <location>/tmp/commit/SKILL.md</location>\n</skill>\n<skill>\n    <name>bare</name>\n</skill>\n</available_skills>\n\nTrailing note.",
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
			"- commit: Mentions </available_skills> & PR styleguide.",
			"- bare",
			"Trailing note.",
		} {
			if !strings.Contains(got, want) {
				t.Errorf("pretty skills output missing %q:\n%s", want, got)
			}
		}
	})

	t.Run("non-skills section untouched", func(t *testing.T) {
		raw := "Keep <available_skills><skill><name>x</name></skill></available_skills> intact."
		var buf bytes.Buffer
		writePrettyPrompt(&buf, []prompt.SectionText{{Name: "user", Text: raw}})
		if !strings.Contains(buf.String(), raw) {
			t.Errorf("non-skills section was altered:\n%s", buf.String())
		}
	})

	t.Run("fallback on missing, unclosed, or empty catalog", func(t *testing.T) {
		cases := []string{
			"No catalog tags here.",
			"Intro\n<available_skills>\n<skill><name>x</name></skill>",
			"Intro\n<available_skills></available_skills>",
		}
		for _, tc := range cases {
			if got := prettySkillsSection(tc); got != tc {
				t.Errorf("prettySkillsSection(%q) = %q, want unchanged", tc, got)
			}
		}
	})
}
