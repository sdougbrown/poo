package poo_test

import (
	"strconv"
	"strings"
	"testing"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/prompt"
)

// The subscribe_feed tool: the core's own tool for starting one instance of
// a feed the session loads and does not start.

// TestTheSubscribeFeedSectionNamesTheInstanceCap covers the prompt rule:
// the section states the instance cap the session enforces, from the
// constant the refusal renders, so the two cannot disagree.
func TestTheSubscribeFeedSectionNamesTheInstanceCap(t *testing.T) {
	tool := &poo.SubscribeFeedTool{}
	env := prompt.Env{OptionalFeeds: []prompt.Feed{
		{Name: "git-feedback", Description: "Feedback on one repository's pull requests.",
			Scope: []prompt.FeedScope{{Name: "url", Pattern: `^https://.*$`}}},
	}}
	section := tool.PromptSection(env)
	want := "at most " + strconv.Itoa(poo.MaxFeedInstances) + " feed instances"
	if !strings.Contains(section, want) {
		t.Errorf("the section is %q, want it to name the cap: %s", section, want)
	}
	if !strings.Contains(section, "git-feedback") {
		t.Errorf("the section is %q, want it to list the feed the environment offers", section)
	}
}
