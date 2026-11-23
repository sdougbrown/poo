package config_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/config"
	"github.com/sdougbrown/poo/pootest"
)

// optionalRunManifest writes a manifest with one optional feed and returns
// its path.
//
// The feed declares the scope a subscription supplies, and its environment
// references one of those values, so a test can see what the subscription
// resolved.
func optionalRunManifest(t *testing.T, name, script, description string, env map[string]string) string {
	t.Helper()
	entry := map[string]any{
		"description": description,
		"command":     []string{feedScript(t, script)},
		"optional":    true,
		// The fixture prints once and exits, so its input has to wake a
		// run for the test to read it: an optional feed holds by default.
		"on_idle": "wake",
		"scope": map[string]any{
			"url": map[string]any{"type": "string", "pattern": "^https://.*$"},
		},
	}
	if env != nil {
		entry["env"] = env
	}
	encoded, err := json.Marshal(map[string]any{"version": 1, "feeds": map[string]any{name: entry}})
	if err != nil {
		t.Fatalf("encoding the manifest: %v", err)
	}
	path := filepath.Join(t.TempDir(), "feeds.json")
	if err := os.WriteFile(path, encoded, 0o600); err != nil {
		t.Fatalf("writing the manifest: %v", err)
	}
	return path
}

// subscribeFeedTurn is one scripted turn that subscribes to a feed.
func subscribeFeedTurn(name, url string) pootest.Turn {
	args, _ := json.Marshal(map[string]any{"feed": name, "scope": map[string]string{"url": url}})
	return pootest.Turn{Calls: []pootest.Call{{Name: poo.SubscribeFeedToolName, Arguments: args, ID: "c1"}}}
}

// TestAnOptionalFeedResolvesItsScopeWhenTheModelSubscribes covers the
// build's seam end to end: the manifest's `scope:url` reference resolves to
// the value the model's subscription supplied, so the feed's command runs
// with it in its environment.
func TestAnOptionalFeedResolvesItsScopeWhenTheModelSubscribes(t *testing.T) {
	const url = "https://example.test/repo"
	manifest := optionalRunManifest(t, "git-feedback", "echoenv", "Feedback on one repository.", map[string]string{
		"FIXTURE_ECHO": "scope:url",
	})
	settings, err := config.Parse([]byte(theFeedsSettings(manifest)+"session:\n  mode: logged\n  dir: "+t.TempDir()+"\n"), config.FormatYAML)
	if err != nil {
		t.Fatalf("decoding the settings: %v", err)
	}
	runtime := build(t, settings, config.Overrides{})
	defer runtime.Store.Close()

	// The build hands the core the feed's schema and the scope reference,
	// and the profile offers the tool that starts one.
	if len(runtime.Options.Feeds) != 1 || !runtime.Options.Feeds[0].Optional {
		t.Fatalf("the build's feeds are %+v, want the one optional feed", runtime.Options.Feeds)
	}
	if scope := runtime.Options.Feeds[0].Scope; len(scope) != 1 || scope[0].Name != "url" || scope[0].Pattern != "^https://.*$" {
		t.Errorf("the feed's scope is %+v, want one url field", scope)
	}
	if !slices.ContainsFunc(runtime.Profile.Tools, func(tool poo.ToolInfo) bool {
		return tool.Name == poo.SubscribeFeedToolName && tool.Class == poo.ClassReadOnly
	}) {
		t.Errorf("the profile does not offer %s: %+v", poo.SubscribeFeedToolName, runtime.Profile.Tools)
	}

	transport := pootest.NewTransport(
		subscribeFeedTurn("git-feedback", url),
		pootest.Turn{Text: "the answer"},
		pootest.Turn{Text: "the answer"},
	)
	runtime.Options.Transport = transport
	sess, err := runtime.CreateSession(context.Background(), runtime.Store, &poo.SessionCreatedRecord{
		RecordMeta: poo.NewRecordMeta("session.created"), SessionMetadata: poo.SessionMetadata{Format: poo.FormatVersion,
			SessionID:  "s1",
			Mode:       "logged",
			CreatedAt:  "2026-11-04T12:00:00Z",
			WorkingDir: t.TempDir(),

			Profile: "code",
			Tools:   runtime.Profile.Tools, Model: poo.SessionModel{ModelID: "test-model"},
			ContextLimit: 32768},
	})
	if err != nil {
		t.Fatalf("creating the session: %v", err)
	}
	defer sess.Close()

	if _, err := sess.Prompt(context.Background(), "watch the repository"); err != nil {
		t.Fatalf("starting the run: %v", err)
	}
	if _, err := sess.Wait(context.Background()); err != nil {
		t.Fatalf("waiting for the run: %v", err)
	}
	// The fixture prints the value it was given, and the instance's origin
	// names the feed and the scope the subscription supplied.
	waitUntil(t, "the feed's input carrying the subscribed value", func() bool {
		for _, request := range transport.Requests() {
			for _, message := range request.Messages {
				if !poo.IsFeedOrigin(message.Origin) || !strings.Contains(message.Text(), url+" and  and []") {
					continue
				}
				if name, ok := poo.FeedName(message.Origin); ok && name == "git-feedback" {
					return true
				}
			}
		}
		return false
	})
}

// TestAProfileOffersNoSubscribeToolWithoutAnOptionalFeed covers the other
// side of the offer: a build that loads only feeds the session starts
// itself gives the model no tool for starting one.
func TestAProfileOffersNoSubscribeToolWithoutAnOptionalFeed(t *testing.T) {
	manifest := feedRunManifest(t, "chat", "chat", "Messages from the fixture feed.", nil)
	settings, err := config.Parse([]byte(theFeedsSettings(manifest)), config.FormatYAML)
	if err != nil {
		t.Fatalf("decoding the settings: %v", err)
	}
	runtime := build(t, settings, config.Overrides{})
	defer runtime.Store.Close()
	if slices.ContainsFunc(runtime.Profile.Tools, func(tool poo.ToolInfo) bool {
		return tool.Name == poo.SubscribeFeedToolName
	}) {
		t.Errorf("the profile offers %s with no optional feed: %+v", poo.SubscribeFeedToolName, runtime.Profile.Tools)
	}
	// The job tools are still offered: the feed is listed and stopped
	// through them.
	for _, name := range []string{"jobs", "cancel_job"} {
		if !slices.ContainsFunc(runtime.Profile.Tools, func(tool poo.ToolInfo) bool { return tool.Name == name }) {
			t.Errorf("the profile does not offer %q for a feed", name)
		}
	}
	// The manifest's own on_idle still reaches the core: only an optional
	// feed's absent policy means hold.
	if got := runtime.Options.Feeds[0].OnIdle; got != poo.OnIdleWake {
		t.Errorf("the feed's idle policy is %q, want the manifest's wake", got)
	}
}

// TestAnOptionalFeedKeepsTheManifestsOnIdle covers the mapping: an
// optional feed whose manifest names no policy arrives at the core with
// none, and the core holds its inputs; the manifest may still ask for a
// wake.
func TestAnOptionalFeedKeepsTheManifestsOnIdle(t *testing.T) {
	for _, tc := range []struct {
		name   string
		onIdle string
		want   poo.OnIdle
	}{
		{name: "the manifest names none", onIdle: "", want: ""},
		{name: "the manifest asks for a wake", onIdle: `"on_idle": "wake",`, want: poo.OnIdleWake},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "feeds.json")
			body := `{"version": 1, "feeds": {"git-feedback": {"command": ["true"], "optional": true, ` + tc.onIdle + ` "scope": {"url": {"type": "string"}}}}}`
			if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
				t.Fatalf("writing the manifest: %v", err)
			}
			settings, err := config.Parse([]byte(theFeedsSettings(path)), config.FormatYAML)
			if err != nil {
				t.Fatalf("decoding the settings: %v", err)
			}
			runtime := build(t, settings, config.Overrides{})
			defer runtime.Store.Close()
			if got := runtime.Options.Feeds[0].OnIdle; got != tc.want {
				t.Errorf("the feed's idle policy is %q, want %q", got, tc.want)
			}
			// The spec is the loader's, so a feed the manifest did not
			// mark optional stays one the session starts itself.
			if !runtime.Options.Feeds[0].Optional {
				t.Error("the build dropped the feed's optional flag")
			}
		})
	}
}
