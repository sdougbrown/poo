package main

import (
	"encoding/json"
	"fmt"
	"io"
	"runtime"
	"runtime/debug"
	"strings"

	clicontract "github.com/sdougbrown/cli-contract"
)

// versionInfo is the identity a binary reports for itself.
//
// The values come from the build information the Go toolchain embeds: the
// module version for a binary installed with `go install module@version`,
// and the version control stamp for a build from a checkout. Nothing here is a
// constant or a linker flag, so it cannot go stale.
type versionInfo struct {
	Version   string `json:"version"`
	Revision  string `json:"revision"`
	Time      string `json:"time"`
	Modified  bool   `json:"modified"`
	GoVersion string `json:"go_version"`
}

// devVersion is what a build carrying no version of its own reports.
const devVersion = "dev"

// shortRevision is how much of the revision a version line carries:
// enough to identify a build, short enough to read.
const shortRevision = 7

// buildVersion returns the running binary's own build information.
func buildVersion() versionInfo {
	return readVersionInfo(debug.ReadBuildInfo)
}

// readVersionInfo reads the build information through read, which tests
// replace with a fabricated value.
func readVersionInfo(read func() (*debug.BuildInfo, bool)) versionInfo {
	info := versionInfo{Version: devVersion, GoVersion: runtime.Version()}
	bi, ok := read()
	if !ok || bi == nil {
		return info
	}
	info.Version = moduleVersion(bi)
	if bi.GoVersion != "" {
		info.GoVersion = bi.GoVersion
	}
	for _, setting := range bi.Settings {
		switch setting.Key {
		case "vcs.revision":
			info.Revision = setting.Value
		case "vcs.time":
			info.Time = setting.Value
		case "vcs.modified":
			info.Modified = setting.Value == "true"
		}
	}
	return info
}

// moduleVersion is the version the build information carries.
//
// The toolchain stamps the module version on a binary installed with
// `go install module@version`, and "(devel)" on a build from source.
func moduleVersion(bi *debug.BuildInfo) string {
	version := bi.Main.Version
	if version == "" || version == "(devel)" {
		return devVersion
	}
	return version
}

// String renders the identity in one word, for --version and for the
// session records: the release, or the checkout a development build came
// from.
func (v versionInfo) String() string {
	if v.Version != devVersion || v.Revision == "" {
		return v.Version
	}
	revision := v.Revision
	if len(revision) > shortRevision {
		revision = revision[:shortRevision]
	}
	if v.Modified {
		return devVersion + "+" + revision + "-dirty"
	}
	return devVersion + "+" + revision
}

// writeVersion prints the identity: the fields a script pins a binary by
// as JSON, or one line and the fields behind it for a person.
func writeVersion(out io.Writer, info versionInfo, asJSON bool) error {
	if asJSON {
		encoded, err := json.MarshalIndent(info, "", "  ")
		if err != nil {
			return err
		}
		_, err = fmt.Fprintln(out, string(encoded))
		return err
	}
	var b strings.Builder
	fmt.Fprintf(&b, "poo %s\n", info)
	if info.Revision != "" {
		fmt.Fprintf(&b, "revision: %s\n", info.Revision)
	}
	if info.Time != "" {
		fmt.Fprintf(&b, "time: %s\n", info.Time)
	}
	if info.Revision != "" {
		fmt.Fprintf(&b, "modified: %t\n", info.Modified)
	}
	fmt.Fprintf(&b, "go: %s\n", info.GoVersion)
	_, err := io.WriteString(out, b.String())
	return err
}

// handleVersion executes `poo version`: the build information the binary
// carries, for a person or for a script.
func handleVersion(inv clicontract.Invocation, stdout, stderr io.Writer) exit {
	if err := writeVersion(stdout, buildVersion(), inv.Bools["json"]); err != nil {
		fmt.Fprintf(stderr, "poo version: %v\n", err)
		return exit{operationalExitCode}
	}
	return exit{}
}
