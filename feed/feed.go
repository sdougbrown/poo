// Package feed holds the manifests of long-lived sources that feed
// messages into a session.
//
// A feed is a process that runs beside a session and emits messages.
// Its manifest names the command, the environment, and what the
// feed's inputs do when the session is idle.
//
// The package loads the manifests the settings name and holds the
// validated specs. It also holds the runner that starts a feed's
// process, reads its output as messages, and reaps it before the attempt
// returns. The session owns the lifecycle that starts and stops attempts,
// and the config layer wires the runner to the session.
//
// A feed is either one the session runs from its own start or one it
// starts on demand. An `optional` feed declares the scope a subscription
// supplies — the fields, and the shape each field's value takes — and
// never runs until something subscribes to it.
//
// A feed's environment follows a registered tool's rules. An entry is a
// literal, an `env:NAME` reference to the session's scrubbed
// environment, a `session:id` reference to the session's own ID, or a
// `scope:NAME` reference to a value the subscription named. The
// references resolve when the feed starts, not when the settings load. A
// reference to a name the feed cannot resolve fails the load: the feed
// would never resolve it, and a feed that starts and fails every attempt
// is worse than a manifest that does not load. The host resolves an
// environment reference against its own scrubbed environment, the
// session for the session's own values, and the subscription for the
// scope's.
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
	"github.com/sdougbrown/poo/exec"
	"github.com/sdougbrown/poo/scopeschema"
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

// scopeNamePattern is scopeschema.ScopeNamePattern, compiled.
var scopeNamePattern = regexp.MustCompile(scopeschema.ScopeNamePattern)

// ScopeField is one field of a feed's scope: a value a subscription
// supplies, and the shape the value takes.
type ScopeField struct {
	// Name is the field's name. A `scope:NAME` environment binding names
	// it.
	Name string
	// Type is the field's type, which is "string".
	Type string
	// Pattern is the expression the whole value must match, or "" when
	// any string is accepted.
	Pattern string
}

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
	// MaxPending caps the feed's undelivered inputs. Zero means the
	// core's default backlog.
	MaxPending int
	// Optional says the feed never starts with the session: something
	// subscribes to it, and the scope below is what it may supply.
	Optional bool
	// Scope is the fields a subscription must supply, in name order, and
	// empty for a feed that takes none.
	Scope []ScopeField
	// Filter is the manifest's optional delivery screen. The config
	// layer applies it to each emitted record; the runner ignores it.
	Filter *Filter
}

// Filter is a feed's delivery screen: the one place a feed's records are
// dropped or let through. A manifest names one of its two kinds.
//
// A command filter is one process per record. The record arrives on the
// filter's standard input; the record it writes to its standard output is
// the one delivered, and it may add a `data` object beside its text; an
// empty standard output drops the record; and a nonzero exit, or output
// that is not a record, fails the attempt. It fails closed: the feed
// suspends rather than delivering what the filter did not pass.
//
// A question filter asks the session's decision model the question about
// a message's text, and drops the message when the answer is no with at
// least MinNoProbability — a picture rides the answer of its own text, and
// a message whose text is empty is delivered unasked, for the decider
// never sees a picture. It fails open: a message it cannot decide is
// delivered.
//
// A consumer that wires the runner itself applies no filter.
type Filter struct {
	// Command is the argv of the filter's own process, and is empty on a
	// question filter.
	Command []string `json:"command"`
	// Question is asked about each message the filter sees. Phrase it
	// so yes means deliver. It is empty on a command filter.
	Question string `json:"question"`
	// MinNoProbability is the probability of no at which the message is
	// dropped. It is greater than 0.5 and at most 1, and it belongs to a
	// question filter.
	MinNoProbability float64 `json:"min_no_probability"`
}

// Script reports whether the filter is the command kind.
func (f *Filter) Script() bool {
	return f != nil && len(f.Command) > 0
}

// ResolveEnvScoped resolves the spec's env bindings into "NAME=value"
// entries for an executor, using lookup to look host variables up, and
// resolves the manifest's session references through scoped: scope
// "session", name "id" resolves to the session's ID, and the resolver
// a caller passes is exec.SessionScope over that ID.
//
// The library's benign names are copied first, then the manifest's
// entries. A manifest reference lookup does not resolve is an error that
// names the variable: a feed whose environment cannot be assembled
// whole is not started.
func (s Spec) ResolveEnvScoped(lookup func(string) (string, bool), scoped func(scope, name string) (string, error)) ([]string, error) {
	// The library's Plan resolves exactly these bindings, over the
	// benign names, so the resolution stays the library's.
	return (&cliregistry.Plan{Env: s.Env}).ResolveEnvScoped(lookup, scoped)
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
// A bad version, command, name, on_idle, filter or unknown field fails the
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

// Has reports whether a loaded feed answers to the name.
func (s *Set) Has(name string) bool {
	if s == nil {
		return false
	}
	for _, spec := range s.specs {
		if spec.Name == name {
			return true
		}
	}
	return false
}

// Specs returns the loaded feeds, in name order.
//
// The list and its specs are copies, including each command, env and filter.
func (s *Set) Specs() []Spec {
	if s == nil || len(s.specs) == 0 {
		return nil
	}
	out := make([]Spec, len(s.specs))
	for i, spec := range s.specs {
		spec.Command = append([]string(nil), spec.Command...)
		spec.Env = append([]cliregistry.EnvBinding(nil), spec.Env...)
		spec.Scope = append([]ScopeField(nil), spec.Scope...)
		if spec.Filter != nil {
			filter := *spec.Filter
			filter.Command = append([]string(nil), spec.Filter.Command...)
			spec.Filter = &filter
		}
		out[i] = spec
	}
	return out
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
	Description string                     `json:"description"`
	Command     []string                   `json:"command"`
	Env         map[string]string          `json:"env"`
	Restart     bool                       `json:"restart"`
	OnIdle      string                     `json:"on_idle"`
	MaxPending  *int                       `json:"max_pending"`
	Filter      *Filter                    `json:"filter"`
	Optional    bool                       `json:"optional"`
	Scope       map[string]scopeFieldEntry `json:"scope"`
}

// scopeFieldEntry is one field of a feed's scope schema, as the manifest
// declares it.
type scopeFieldEntry struct {
	Type    string `json:"type"`
	Pattern string `json:"pattern"`
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
	maxPending := 0
	if b.MaxPending != nil {
		if *b.MaxPending < 0 {
			return Spec{}, fmt.Errorf("feed manifest %s: feed %q has a negative max_pending %d", path, name, *b.MaxPending)
		}
		maxPending = *b.MaxPending
	}
	if err := readFilter(path, name, b.Filter); err != nil {
		return Spec{}, err
	}
	scope, err := readScope(path, name, b)
	if err != nil {
		return Spec{}, err
	}
	names := make([]string, 0, len(b.Env))
	for entry := range b.Env {
		names = append(names, entry)
	}
	sort.Strings(names)
	envs := make([]cliregistry.EnvBinding, 0, len(names))
	for _, entry := range names {
		envs = append(envs, cliregistry.ParseEnvBinding(entry, b.Env[entry]))
	}
	for _, binding := range envs {
		if !binding.Reference || binding.Scope == "" {
			continue
		}
		if binding.Scope == cliregistry.ScopeScope {
			// The feed's own schema is what a scope reference may name:
			// the subscription supplies exactly the values the manifest
			// declares, so a reference to any other name would fail
			// every attempt.
			if err := declaresScopeField(scope, binding.From); err != nil {
				return Spec{}, fmt.Errorf("feed manifest %s: feed %q: the env %q names %v", path, name, binding.Name, err)
			}
			continue
		}
		if err := exec.ValidSessionReference(binding.Scope, binding.From); err != nil {
			return Spec{}, fmt.Errorf("feed manifest %s: feed %q: %w", path, name, err)
		}
	}
	return Spec{
		Name:        name,
		Description: b.Description,
		Command:     append([]string(nil), b.Command...),
		Env:         envs,
		Restart:     b.Restart,
		OnIdle:      b.OnIdle,
		MaxPending:  maxPending,
		Optional:    b.Optional,
		Scope:       scope,
		Filter:      b.Filter,
	}, nil
}

// readFilter validates a feed's filter.
//
// A feed's filter is one of the two kinds, so a manifest that names both a
// command and a question fails the load: the record would either go to the
// filter's process or be put to the decision model, and a feed that means
// both means neither.
func readFilter(path, name string, f *Filter) error {
	if f == nil {
		return nil
	}
	f.Question = strings.TrimSpace(f.Question)
	if len(f.Command) > 0 {
		if f.Question != "" {
			return fmt.Errorf("feed manifest %s: feed %q names both a filter.command and a filter.question, and a filter is one of the two", path, name)
		}
		if f.MinNoProbability != 0 {
			return fmt.Errorf("feed manifest %s: feed %q names a filter.min_no_probability, and that belongs to a filter.question", path, name)
		}
		if f.Command[0] == "" {
			return fmt.Errorf("feed manifest %s: feed %q has an empty filter command", path, name)
		}
		return nil
	}
	if f.Question == "" {
		return fmt.Errorf("feed manifest %s: feed %q needs a filter.command or a filter.question", path, name)
	}
	if !(f.MinNoProbability > 0.5 && f.MinNoProbability <= 1) {
		return fmt.Errorf("feed manifest %s: feed %q needs a filter.min_no_probability greater than 0.5 and at most 1", path, name)
	}
	return nil
}

// readScope validates a feed's scope schema and returns its fields, in
// name order.
//
// A feed declares a scope because a subscription supplies the values it
// watches, and only a feed that starts on demand has a subscription: a
// scope on a feed the session starts itself is a manifest that would fail
// every attempt, so it fails the load instead.
func readScope(path, name string, b body) ([]ScopeField, error) {
	if len(b.Scope) > 0 && !b.Optional {
		return nil, fmt.Errorf("feed manifest %s: feed %q declares a scope and is not optional", path, name)
	}
	fieldNames := make([]string, 0, len(b.Scope))
	for field := range b.Scope {
		fieldNames = append(fieldNames, field)
	}
	sort.Strings(fieldNames)
	fields := make([]ScopeField, 0, len(fieldNames))
	for _, field := range fieldNames {
		entry := b.Scope[field]
		if !scopeNamePattern.MatchString(field) {
			return nil, fmt.Errorf("feed manifest %s: feed %q: scope field %q is not a valid field name", path, name, field)
		}
		if entry.Type != scopeschema.ScopeStringType {
			return nil, fmt.Errorf("feed manifest %s: feed %q: scope field %q has the type %q, want %q", path, name, field, entry.Type, scopeschema.ScopeStringType)
		}
		if entry.Pattern != "" {
			if _, err := regexp.Compile(entry.Pattern); err != nil {
				return nil, fmt.Errorf("feed manifest %s: feed %q: scope field %q has a pattern that does not compile: %v", path, name, field, err)
			}
		}
		fields = append(fields, ScopeField{Name: field, Type: entry.Type, Pattern: entry.Pattern})
	}
	return fields, nil
}

// declaresScopeField reports whether the feed's scope declares the field,
// naming the field it could not find.
func declaresScopeField(scope []ScopeField, field string) error {
	if field == "" {
		return fmt.Errorf("no scope value")
	}
	for _, declared := range scope {
		if declared.Name == field {
			return nil
		}
	}
	return fmt.Errorf("the scope value %q, and the feed declares no such field", cliregistry.ScopeScope+":"+field)
}
