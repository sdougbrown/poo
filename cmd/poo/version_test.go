package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"runtime/debug"
	"strings"
	"testing"

	clicontract "github.com/sdougbrown/cli-contract"
)

// The version is answered from the binary's own build information, so
// these tests hand readVersionInfo the build information the toolchain
// writes: the module version of an installed binary, and the VCS stamp of a
// build from a checkout.

// TestTheUserAgentNamesTheClient covers what the binary calls itself on the
// requests it makes: its own name and the version the build carries, and
// never the name the HTTP library stamps when a client says nothing.
func TestTheUserAgentNamesTheClient(t *testing.T) {
	agent := userAgent()
	if !strings.HasPrefix(agent, "poo/") {
		t.Fatalf("the user agent is %q, want the binary's own name", agent)
	}
	if agent == "poo/" {
		t.Fatalf("the user agent names no version")
	}
}

// TestTheVersionComesFromTheBuildInformation covers what each kind of
// build reports, and the one word the version line carries.
func TestTheVersionComesFromTheBuildInformation(t *testing.T) {
	cases := []struct {
		name    string
		info    *debug.BuildInfo
		ok      bool
		want    versionInfo
		display string
	}{
		{
			name:    "a binary installed from the module",
			info:    &debug.BuildInfo{Main: debug.Module{Version: "v0.4.1"}, GoVersion: "go1.26.8"},
			ok:      true,
			want:    versionInfo{Version: "v0.4.1", GoVersion: "go1.26.8"},
			display: "v0.4.1",
		},
		{
			name: "a build from a checkout",
			info: &debug.BuildInfo{
				Main:      debug.Module{Version: "(devel)"},
				GoVersion: "go1.26.8",
				Settings: []debug.BuildSetting{
					{Key: "vcs.revision", Value: "0123456789abcdef0123456789abcdef01234567"},
					{Key: "vcs.time", Value: "2026-11-14T06:03:57Z"},
					{Key: "vcs.modified", Value: "false"},
				},
			},
			ok: true,
			want: versionInfo{
				Version:   "dev",
				Revision:  "0123456789abcdef0123456789abcdef01234567",
				Time:      "2026-11-14T06:03:57Z",
				GoVersion: "go1.26.8",
			},
			display: "dev+0123456",
		},
		{
			name: "a checkout with uncommitted changes",
			info: &debug.BuildInfo{
				Main:      debug.Module{Version: "(devel)"},
				GoVersion: "go1.26.8",
				Settings: []debug.BuildSetting{
					{Key: "vcs.revision", Value: "0123456789abcdef0123456789abcdef01234567"},
					{Key: "vcs.modified", Value: "true"},
				},
			},
			ok: true,
			want: versionInfo{
				Version:   "dev",
				Revision:  "0123456789abcdef0123456789abcdef01234567",
				Modified:  true,
				GoVersion: "go1.26.8",
			},
			display: "dev+0123456-dirty",
		},
		{
			name:    "a build that names no version",
			info:    &debug.BuildInfo{Main: debug.Module{Version: ""}, GoVersion: "go1.26.8"},
			ok:      true,
			want:    versionInfo{Version: "dev", GoVersion: "go1.26.8"},
			display: "dev",
		},
		{
			name:    "a build with no information",
			info:    nil,
			ok:      false,
			want:    versionInfo{Version: "dev"},
			display: "dev",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			info := readVersionInfo(func() (*debug.BuildInfo, bool) { return c.info, c.ok })
			if info.Version != c.want.Version {
				t.Errorf("the version is %q, want %q", info.Version, c.want.Version)
			}
			if info.Revision != c.want.Revision {
				t.Errorf("the revision is %q, want %q", info.Revision, c.want.Revision)
			}
			if info.Time != c.want.Time {
				t.Errorf("the time is %q, want %q", info.Time, c.want.Time)
			}
			if info.Modified != c.want.Modified {
				t.Errorf("the modified flag is %t, want %t", info.Modified, c.want.Modified)
			}
			if got := info.String(); got != c.display {
				t.Errorf("the version line is %q, want %q", got, c.display)
			}
		})
	}
}

// TestTheVersionCommandAnswersInBothForms covers the command: the fields
// for a person, and the fields a script pins the binary by.
func TestTheVersionCommandAnswersInBothForms(t *testing.T) {
	code, _, stdout := runCLIWithStdout(t, "version")
	if code != 0 {
		t.Fatalf("poo version exited %d, want 0", code)
	}
	if !strings.HasPrefix(stdout, "poo ") {
		t.Errorf("the version output is %q, want it to start with the binary's name", stdout)
	}

	code, _, stdout = runCLIWithStdout(t, "version", "--json")
	if code != 0 {
		t.Fatalf("poo version --json exited %d, want 0", code)
	}
	var fields map[string]any
	if err := json.Unmarshal([]byte(stdout), &fields); err != nil {
		t.Fatalf("decoding the version: %v\n%s", err, stdout)
	}
	if fields["version"] == "" {
		t.Error("the version is empty")
	}
	for _, name := range []string{"version", "revision", "time", "modified", "go_version"} {
		if _, ok := fields[name]; !ok {
			t.Errorf("the JSON holds no %q field: %v", name, fields)
		}
	}
}

// TestTheVersionFlagAgreesWithTheCommand covers the alias: `--version`
// prints the one word the build information renders, and the command
// reports the same build.
func TestTheVersionFlagAgreesWithTheCommand(t *testing.T) {
	info := buildVersion()

	code, _, flagged := runCLIWithStdout(t, "--version")
	if code != 0 {
		t.Fatalf("poo --version exited %d, want 0", code)
	}
	if want := "poo " + info.String() + "\n"; flagged != want {
		t.Errorf("--version prints %q, want %q", flagged, want)
	}

	_, _, command := runCLIWithStdout(t, "version", "--json")
	var fields map[string]any
	if err := json.Unmarshal([]byte(command), &fields); err != nil {
		t.Fatalf("decoding the version: %v\n%s", err, command)
	}
	if fields["version"] != info.Version {
		t.Errorf("the command reports the version %v, want %q", fields["version"], info.Version)
	}
	if fields["go_version"] != info.GoVersion {
		t.Errorf("the command reports the Go version %v, want %q", fields["go_version"], info.GoVersion)
	}
}

// TestTheVersionTextNamesTheBuild covers the human form: the fields a
// person reads are the ones the build information carries, and a build
// with no checkout stamp prints no lines for it.
func TestTheVersionTextNamesTheBuild(t *testing.T) {
	cases := []struct {
		name     string
		info     versionInfo
		want     []string
		unwanted []string
	}{
		{
			name: "a build from a checkout",
			info: versionInfo{
				Version:   "dev",
				Revision:  "0123456789abcdef0123456789abcdef01234567",
				Time:      "2026-11-14T06:03:57Z",
				Modified:  true,
				GoVersion: "go1.26.8",
			},
			want: []string{
				"poo dev+0123456-dirty\n",
				"revision: 0123456789abcdef0123456789abcdef01234567\n",
				"time: 2026-11-14T06:03:57Z\n",
				"modified: true\n",
				"go: go1.26.8\n",
			},
		},
		{
			name:     "a binary installed from the module",
			info:     versionInfo{Version: "v0.4.1", GoVersion: "go1.26.8"},
			want:     []string{"poo v0.4.1\n", "go: go1.26.8\n"},
			unwanted: []string{"revision:", "time:", "modified:"},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var out bytes.Buffer
			if err := writeVersion(&out, c.info, false); err != nil {
				t.Fatalf("writing the version: %v", err)
			}
			for _, want := range c.want {
				if !strings.Contains(out.String(), want) {
					t.Errorf("the text holds no %q:\n%s", want, out.String())
				}
			}
			for _, unwanted := range c.unwanted {
				if strings.Contains(out.String(), unwanted) {
					t.Errorf("the text holds %q, and the build carries none:\n%s", unwanted, out.String())
				}
			}
		})
	}
}

// failingWriter refuses every write, the way a closed stdout or a full
// disk does.
type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("disk full") }

// TestTheVersionCommandReportsAWriteFailure covers the failure a script
// hits quietly otherwise: the command exits 1 and says why on stderr.
func TestTheVersionCommandReportsAWriteFailure(t *testing.T) {
	var stderr bytes.Buffer
	inv, err := clicontract.Parse(pooApp(io.Discard), []string{"version"})
	if err != nil {
		t.Fatalf("parsing the version command: %v", err)
	}
	got := handleVersion(inv, failingWriter{}, &stderr)
	if got.code != operationalExitCode {
		t.Errorf("the exit code is %d, want %d", got.code, operationalExitCode)
	}
	if !strings.Contains(stderr.String(), "poo version: disk full") {
		t.Errorf("stderr is %q, want it to name the failure", stderr.String())
	}
}
