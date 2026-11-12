package poo

import (
	"encoding/json"
	"os"
	"regexp"
	"slices"
	"testing"

	"github.com/sdougbrown/poo/wire/protocol"
)

// TestTheProtocolLevelNamesMatchTheCore pins the two copies of the
// reasoning level vocabulary to each other: the core refuses levels by
// this list, and the wire carries it, so a copy that drifts is a
// refusal that names levels the other side does not know.
func TestTheProtocolLevelNamesMatchTheCore(t *testing.T) {
	if !slices.Equal(ReasoningLevelNames, protocol.ReasoningLevelNames) {
		t.Errorf("the core's levels are %v, the protocol's are %v",
			ReasoningLevelNames, protocol.ReasoningLevelNames)
	}
}

// TestTheSettingsSchemaTakesTheCoreLevels pins the settings schema's
// reasoning pattern to the core's list: every level the core knows the
// schema accepts, and every name the schema accepts is a level the
// core knows — a level added to one copy without the other is a
// setting that loads and is then refused, or the reverse.
func TestTheSettingsSchemaTakesTheCoreLevels(t *testing.T) {
	schema, err := os.ReadFile("config/schema/settings.umpire.json")
	if err != nil {
		t.Fatalf("reading the schema: %v", err)
	}
	var doc struct {
		Rules []struct {
			Field   string `json:"field"`
			Op      string `json:"op"`
			Pattern string `json:"pattern"`
		} `json:"rules"`
	}
	if err := json.Unmarshal(schema, &doc); err != nil {
		t.Fatalf("parsing the schema: %v", err)
	}
	var re *regexp.Regexp
	for _, rule := range doc.Rules {
		if rule.Field == "reasoning.chat" && rule.Op == "matches" {
			re, err = regexp.Compile(rule.Pattern)
			if err != nil {
				t.Fatalf("the schema's pattern does not compile: %v", err)
			}
			break
		}
	}
	if re == nil {
		t.Fatal("the schema holds no reasoning.chat matches rule")
	}
	for _, level := range ReasoningLevelNames {
		if !re.MatchString(level) {
			t.Errorf("the schema's pattern refuses the level %q", level)
		}
	}
	for _, level := range protocol.ReasoningLevelNames {
		if !re.MatchString(level) {
			t.Errorf("the schema's pattern refuses the wire level %q", level)
		}
	}
	for _, name := range []string{"minimal", "low", "medium", "high", "xhigh"} {
		if !slices.Contains(ReasoningLevelNames, name) {
			t.Errorf("the schema's pattern accepts %q, which the core does not know", name)
		}
	}
	// The budget arm takes positive numbers only: ParseReasoning refuses a
	// zero budget, and a pattern that accepted it would load a setting the
	// session then rejects.
	if !re.MatchString("4096") {
		t.Errorf("the schema's pattern refuses a token budget: %s", re.String())
	}
	if re.MatchString("0") {
		t.Errorf("the schema's pattern takes a zero budget: %s", re.String())
	}
}
