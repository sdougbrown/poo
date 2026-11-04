// Package feed holds the manifests of long-lived sources that feed
// messages into a session.
//
// A feed is a process that runs beside a session and emits messages.
// Its manifest names the command, the environment, and what the
// feed's inputs do when the session is idle.
//
// The package loads the manifests the settings name and holds the
// validated specs. The session owns the lifecycle that starts and stops
// the processes. The config layer turns a spec into a real process.
//
// A feed's environment follows a registered CLI's rules. An entry is a
// literal, or an `env:NAME` reference. The host resolves a reference
// against its own scrubbed environment.
package feed

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"

	"github.com/sdougbrown/cli-registry"
	"github.com/sdougbrown/cli-registry/manifest"
)

// The idle policies a feed's manifest may name. An entry without one
// inherits the session's `jobs.on_idle`.
const (
	// OnIdleWake delivers a feed input even when the session is idle,
	// waking a run for it.
	OnIdleWake = "wake"
	// OnIdleHold queues a feed input and leaves it held until a run is
	// already going.
	OnIdleHold = "hold"
)

// manifestVersion is the feed manifest version this package loads.
const manifestVersion = 1

// namePattern is the shape of a feed's name. A name appears in origins,
// job ids and records, so it stays lowercase and short.
var namePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]*$`)

// Spec is one validated feed manifest entry.
type Spec struct {
	// Name is the name the feed answers to.
	Name string
	// Description is what the feed is for, as its manifest says.
	Description string
	// Command is the argv the feed's process runs.
	Command []string
	// Env are the environment entries the manifest declares, in name
	// order, unresolved.
	Env []cliregistry.EnvBinding
	// Restart says the feed's process is restarted when it exits.
	Restart bool
	// OnIdle is the feed's idle policy, or the empty string to inherit
	// the session's.
	OnIdle string
}

// ResolveEnv turns the spec's env bindings into "NAME=value" entries for
// an executor, using lookup to look host variables up.
//
// The library's benign names are copied first, then the manifest's
// entries. A manifest reference lookup does not resolve is an error that
// names the variable: a feed whose environment cannot be assembled
// whole is not started.
func (s Spec) ResolveEnv(lookup func(string) (string, bool)) ([]string, error) {
	// The library's Plan resolves exactly these bindings, over the
	// benign names, so the resolution stays the library's.
	return (&cliregistry.Plan{Env: s.Env}).ResolveEnv(lookup)
}

// Set is the set of feed manifests the settings name.
//
// The set is immutable after Load, so every reader may read it
// concurrently.
type Set struct {
	// specs are the loaded feeds, in name order.
	specs []Spec
}

// Load reads the feed manifests reachable from paths and validates them.
//
// A path is a manifest file, a directory of manifests, or several of
// them, as the cli-registry manifests take them. A feed name defined by
// two manifests fails the load, and the failure names both files: an
// origin has to mean one feed.
//
// A bad version, command, name, on_idle or unknown field fails the
// load. The failure names the file and the feed.
func Load(paths ...string) (*Set, error) {
	files, err := manifest.Discover(paths...)
	if err != nil {
		return nil, fmt.Errorf("feed manifests: %w", err)
	}
	defined := map[string]string{}
	var specs []Spec
	for _, file := range files {
		fileSpecs, err := readFile(file)
		if err != nil {
			return nil, err
		}
		for _, spec := range fileSpecs {
			if first, dup := defined[spec.Name]; dup {
				return nil, fmt.Errorf("feed %q is defined in both %s and %s", spec.Name, first, file)
			}
			defined[spec.Name] = file
			specs = append(specs, spec)
		}
	}
	sort.Slice(specs, func(i, j int) bool { return specs[i].Name < specs[j].Name })
	return &Set{specs: specs}, nil
}

// Len is the number of loaded feeds.
func (s *Set) Len() int {
	if s == nil {
		return 0
	}
	return len(s.specs)
}

// Specs returns the loaded feeds, in name order.
//
// The list is a copy, so a caller may reorder it. The env bindings are
// the set's own and are not to be changed.
func (s *Set) Specs() []Spec {
	if s == nil {
		return nil
	}
	return append([]Spec(nil), s.specs...)
}

// envelope is the manifest's top-level shape, decoded with unknown
// fields rejected and duplicate keys detected.
type envelope struct {
	Version int                        `json:"version"`
	Feeds   map[string]json.RawMessage `json:"feeds"`
}

// body is one feed's manifest entry, decoded with unknown fields
// rejected.
type body struct {
	Description string            `json:"description"`
	Command     []string          `json:"command"`
	Env         map[string]string `json:"env"`
	Restart     bool              `json:"restart"`
	OnIdle      string            `json:"on_idle"`
}

// readFile reads one manifest and returns its validated specs, in name
// order.
func readFile(path string) ([]Spec, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("feed manifest %s: %w", path, err)
	}
	var env envelope
	// Decode rejects unknown fields and duplicate keys, and names the
	// file in its failures.
	if err := manifest.Decode(path, data, &env); err != nil {
		return nil, fmt.Errorf("feed manifest: %w", err)
	}
	if env.Version != manifestVersion {
		return nil, fmt.Errorf("feed manifest %s: unsupported version %d, want %d", path, env.Version, manifestVersion)
	}
	names := make([]string, 0, len(env.Feeds))
	for name := range env.Feeds {
		names = append(names, name)
	}
	sort.Strings(names)
	specs := make([]Spec, 0, len(names))
	for _, name := range names {
		spec, err := readFeed(path, name, env.Feeds[name])
		if err != nil {
			return nil, err
		}
		specs = append(specs, spec)
	}
	return specs, nil
}

// readFeed validates one feed's manifest entry.
func readFeed(path, name string, raw json.RawMessage) (Spec, error) {
	if !namePattern.MatchString(name) {
		return Spec{}, fmt.Errorf("feed manifest %s: feed name %q is not a valid feed name", path, name)
	}
	// The entry is already JSON: Decode normalized the whole file, in
	// whatever format it was written, and checked it for duplicate keys.
	var b body
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&b); err != nil {
		return Spec{}, fmt.Errorf("feed manifest %s: feed %q: %w", path, name, err)
	}
	if len(b.Command) == 0 {
		return Spec{}, fmt.Errorf("feed manifest %s: feed %q has an empty command", path, name)
	}
	switch b.OnIdle {
	case "", OnIdleWake, OnIdleHold:
	default:
		return Spec{}, fmt.Errorf("feed manifest %s: feed %q has an on_idle %q, want %q or %q", path, name, b.OnIdle, OnIdleWake, OnIdleHold)
	}
	names := make([]string, 0, len(b.Env))
	for entry := range b.Env {
		names = append(names, entry)
	}
	sort.Strings(names)
	envs := make([]cliregistry.EnvBinding, 0, len(names))
	for _, entry := range names {
		value := b.Env[entry]
		if from, ok := strings.CutPrefix(value, "env:"); ok {
			envs = append(envs, cliregistry.EnvBinding{Name: entry, From: from, Reference: true})
			continue
		}
		envs = append(envs, cliregistry.EnvBinding{Name: entry, Value: value})
	}
	return Spec{
		Name:        name,
		Description: b.Description,
		Command:     append([]string(nil), b.Command...),
		Env:         envs,
		Restart:     b.Restart,
		OnIdle:      b.OnIdle,
	}, nil
}
