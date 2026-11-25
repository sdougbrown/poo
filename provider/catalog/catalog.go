// Package catalog resolves the models the harness can use.
//
// The catalog has four layers, and the later ones win, field by field.
//
//  1. The curated layer is embedded in the binary: the family rules in
//     families.go describe families of local models. What a hosted provider
//     serves is not here — a snapshot of it goes stale the day the provider
//     adds or retires a model, and the provider's own list is one probe away.
//  2. The discovery layer holds what the endpoints report about the
//     models they serve.
//  3. The models file layer is what the operator's models file declares.
//  4. The settings layer is the user's own configuration.
//
// Every model in the catalog is a poo.ModelInfo whose Source map names
// the layer that set each field, so `poo models --explain` can say
// where a value came from.
package catalog

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"sort"
	"sync"
	"time"

	"github.com/sdougbrown/poo"
)

// The names the Source map uses for a model's fields.
//
// They are the JSON names of poo.ModelInfo's fields, so a reader can
// match them up, and the fields that hold a group of values name their
// parts: "sampling.temperature", "tools.mode" and "reasoning.mode".
const (
	fieldName            = "name"
	fieldProvider        = "provider"
	fieldEndpoint        = "endpoint"
	fieldModelID         = "model_id"
	fieldAPI             = "api"
	fieldOps             = "ops"
	fieldInputs          = "inputs"
	fieldContextLimit    = "context_limit"
	fieldInputLimit      = "input_limit"
	fieldMaxOutput       = "max_output"
	fieldSampling        = "sampling"
	fieldPrefixCache     = "prefix_cache"
	fieldTools           = "tools"
	fieldReasoning       = "reasoning"
	fieldReasoningLevels = "reasoning.levels"
	fieldTemperature     = "temperature"
	fieldPromptCache     = "prompt_cache"
	fieldPromptVariant   = "prompt_variant"
	fieldQuirks          = "quirks"
)

// The layers the catalog merges, in order.
const (
	// LayerCurated is the catalog that ships with the binary.
	LayerCurated = "curated"
	// LayerDiscovery is what the endpoints reported.
	LayerDiscovery = "discovery"
	// LayerModelsFile is what the operator's models file declares.
	LayerModelsFile = "models_file"
	// LayerSettings is the user's own configuration.
	LayerSettings = poo.LayerSettings
	// LayerDefault is a value no layer set and the catalog filled in.
	LayerDefault = "default"
)

// Options configure a catalog.
type Options struct {
	// Settings are the user's settings. The catalog reads the endpoints, the
	// models and the roles from them, and each model's sampling with it.
	Settings Settings
	// File is the models file the settings name or the one beside them.
	// Its endpoints and models are the layer the settings win over.
	File FileSettings
	// Logger receives the catalog's warnings. A nil logger drops them.
	Logger *slog.Logger
	// StateDir is the state root the poo directory lives under. The
	// discovery cache is <StateDir>/poo/discovery.json. Empty means the
	// user's state directory.
	StateDir string
	// DiscoveryTTL is how long a cached discovery result stays fresh. It decides when
	// Refresh probes an endpoint again instead of reading the cache; it does not bound the
	// build's cached layer, which applies what the endpoints last reported however old it
	// is — a build never probes, and a model that vanishes with age is not a world a
	// session can hold. Zero means DefaultDiscoveryTTL.
	DiscoveryTTL time.Duration
	// HTTPClient is the client the probes use. A nil client means a
	// default one.
	HTTPClient *http.Client
	// ProbeTimeout bounds one probe. Zero means DefaultProbeTimeout.
	ProbeTimeout time.Duration
	// UserAgent is what the client calls itself on the probes. A gateway
	// that watches its traffic asks each client to name itself, and a
	// client that says nothing is named by the HTTP library alone. Empty
	// sends none.
	UserAgent string
	// EndpointKey resolves the key an endpoint's credential reads, and an
	// endpoint whose key is not in reach resolves to the empty string. The
	// probes ask with it: a hosted provider's model list is served to a key,
	// and a probe that asked anonymously would be refused and would leave the
	// endpoint offering nothing. Nil sends no credential, which is what a
	// binary that links no credential store can do.
	EndpointKey func(name string, credential Credential) string
	// Cached says the catalog holds what the endpoints last reported, read from the
	// discovery cache when it is built. It is how a model an endpoint served is a model a
	// session can name without a probe: the build never reaches the network, and the cache
	// is local. The layer is read through the same rule the refresh's stale fallback uses
	// (cache.stale), so the two readers cannot disagree about what the cache holds.
	//
	// A cached result is the endpoint's own report, so it sits above the curated layer and
	// below the models file: what a server says is a fact about the server, and what the
	// operator writes is the operator's word on it.
	Cached bool
}

// Catalog is the effective catalog: the merged models, the endpoints
// that serve them and the role assignments.
//
// The entries and the model index move when a probe reports what an endpoint
// serves, and a probe can land while another goroutine reads the catalog: a wire
// request reprobes, and a session's switch, decider and statusline read. The
// mutex is what keeps a reprobe from writing the maps a reader is walking, which
// in Go is not an error but a panic. The endpoints and the roles are set once,
// when the catalog is built, and are read without it.
type Catalog struct {
	opts Options
	// mu guards the entries and the model index.
	mu         sync.RWMutex
	curated    *curated
	entries    map[string]*entry
	modelIndex map[modelRef]*entry
	endpoints  map[string]Endpoint
	roles      map[Role]string
}

// modelRef names the model a model info serves: an endpoint and the id
// the endpoint calls it.
type modelRef struct {
	endpoint string
	modelID  string
}

// entry is one model in the catalog, with the layers that built it.
type entry struct {
	// name is the model's name: the settings' name, the model's id for a curated
	// model no layer named, or endpoint/id for a discovered one.
	name string
	// endpoint is the endpoint's name, empty when no layer named one.
	endpoint string
	// modelID is the id the endpoint calls the model.
	modelID string
	// notes are the curated notes about the model's family.
	notes []string
	// curated, discovery, file and settings hold the fields each layer set.
	curated   poo.ModelInfo
	discovery poo.ModelInfo
	file      poo.ModelInfo
	settings  poo.ModelInfo
}

// New returns a catalog with the curated and the settings layers
// merged.
//
// With Cached, the catalog also holds what the endpoints last reported, read from the
// discovery cache: the endpoints the catalog still declares, and nothing else.
//
// Without it, the catalog holds no discovery results yet: call Refresh to probe the
// endpoints, or Build to do both and to check the roles.
func New(opts Options) *Catalog {
	c := &Catalog{
		opts:       opts,
		curated:    newCurated(),
		entries:    map[string]*entry{},
		modelIndex: map[modelRef]*entry{},
		endpoints:  map[string]Endpoint{},
		roles:      map[Role]string{},
	}
	for name, endpoint := range opts.File.Endpoints {
		c.endpoints[name] = mergeEndpoint(c.endpoints[name], endpoint.endpoint(name))
	}
	for name, endpoint := range opts.Settings.Endpoints {
		c.endpoints[name] = mergeEndpoint(c.endpoints[name], endpoint.endpoint(name))
	}
	c.addFileModels()
	c.addSettingsModels()
	if opts.Cached {
		c.applyCached()
	}
	c.addRoles()
	return c
}

// applyCached merges what the endpoints last reported, without asking them again.
//
// The cache is the one place a session learns what an endpoint serves, because a build does
// not probe. Entries apply however old they are — an entry that went stale still describes
// what a session may be using, and a model disappearing because the cache aged out would be
// a failure nobody asked for; freshness is Refresh's decision, not the build's. The layer is
// read through the same per-endpoint rule Refresh's stale fallback uses (cache.stale), so
// the two readers cannot disagree; only the endpoints the catalog still declares are asked.
// A cache that cannot be read is a catalog without this layer rather than a failed build.
func (c *Catalog) applyCached() {
	cache := c.cache()
	cached, err := cache.load()
	if err != nil {
		c.warn("the discovery cache could not be read", slog.Any("error", err))
		return
	}
	if cached == nil {
		return
	}
	c.applyDiscovery(cache.layer(cached, c.endpointNames()))
}

// addFileModels adds the models the models file declares.
//
// A model the settings name too is one entry with both layers: the file's
// fields are what the settings overrule, one field at a time.
func (c *Catalog) addFileModels() {
	names := make([]string, 0, len(c.opts.File.Models))
	for name := range c.opts.File.Models {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		model := c.opts.File.Models[name]
		if model.ModelID == "" {
			c.warn("the models file does not name the model the endpoint calls it",
				slog.String("model", name))
			continue
		}
		endpoint, ok := c.endpoints[model.Endpoint]
		if !ok {
			c.warn("the models file names an endpoint it does not declare",
				slog.String("model", name),
				slog.String("endpoint", model.Endpoint))
			continue
		}
		e := &entry{
			name:     name,
			endpoint: model.Endpoint,
			modelID:  model.ModelID,
		}
		e.file = model.layer(name, endpoint.Endpoint)
		e.curated, e.notes = c.curated.forModel(model.ModelID)
		c.entries[name] = e
		c.modelIndex[modelRef{endpoint: model.Endpoint, modelID: model.ModelID}] = e
	}
}

// Build resolves the catalog and checks the roles.
//
// The endpoints are probed, the layers are merged and the role
// assignments are checked. The chat model is the one a session cannot
// run without, so a chat model the catalog cannot resolve is an error.
// Every other model the catalog cannot resolve is a warning.
func Build(ctx context.Context, opts Options) (*Catalog, error) {
	// Build probes, so the discovery layer comes from Refresh, which reads the cache
	// itself. Building the cached layer too would apply it twice — and warn twice when
	// the cache is unreadable.
	opts.Cached = false
	c := New(opts)
	if err := c.Refresh(ctx); err != nil {
		return nil, err
	}
	if err := c.CheckRoles(); err != nil {
		return nil, err
	}
	return c, nil
}

// addSettingsModels adds the models the settings configure.
func (c *Catalog) addSettingsModels() {
	names := make([]string, 0, len(c.opts.Settings.Models))
	for name := range c.opts.Settings.Models {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		model := c.opts.Settings.Models[name]
		if model.ModelID == "" {
			c.warn("the model does not name the model the endpoint calls it",
				slog.String("model", name))
			continue
		}
		endpoint, ok := c.endpoints[model.Endpoint]
		if !ok {
			c.warn("the model names an endpoint that is not configured",
				slog.String("model", name),
				slog.String("endpoint", model.Endpoint))
			continue
		}
		// A model the models file declared is one entry with both layers, so
		// the settings' fields are merged over the file's rather than added
		// beside them.
		e, ok := c.entries[name]
		if !ok {
			e = &entry{name: name}
			c.entries[name] = e
		} else if e.endpoint != model.Endpoint || e.modelID != model.ModelID {
			// The settings serve the model the file named from somewhere else,
			// or under another id: the entry moves with them, so the discovery
			// layer still finds it under the id it reports.
			delete(c.modelIndex, modelRef{endpoint: e.endpoint, modelID: e.modelID})
		}
		e.endpoint = model.Endpoint
		e.modelID = model.ModelID
		e.settings = model.layer(name, endpoint.Endpoint)
		e.curated, e.notes = c.curated.forModel(model.ModelID)
		c.modelIndex[modelRef{endpoint: model.Endpoint, modelID: model.ModelID}] = e
	}
}

// addRoles records the role assignments.
func (c *Catalog) addRoles() {
	for name, model := range c.opts.Settings.Roles {
		c.roles[Role(name)] = model
	}
}

// entryForModel returns the entry that serves a model id at an
// endpoint, or nil when there is none.
func (c *Catalog) entryForModel(endpoint, modelID string) *entry {
	return c.modelIndex[modelRef{endpoint: endpoint, modelID: modelID}]
}

// List returns the effective models, sorted by name.
func (c *Catalog) List() []poo.ModelInfo {
	c.mu.RLock()
	defer c.mu.RUnlock()
	names := make([]string, 0, len(c.entries))
	for name := range c.entries {
		names = append(names, name)
	}
	sort.Strings(names)
	out := make([]poo.ModelInfo, 0, len(names))
	for _, name := range names {
		out = append(out, c.effective(name))
	}
	return out
}

// Resolve returns the effective model with the given name.
//
// A name the catalog does not hold is an error: the caller asked for a
// model that does not exist.
func (c *Catalog) Resolve(name string) (poo.ModelInfo, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if _, ok := c.entries[name]; !ok {
		return poo.ModelInfo{}, &UnresolvedError{Name: name}
	}
	return c.effective(name), nil
}

// ReasoningFields returns the request fields that carry the reasoning
// setting for a model.
//
// The setting is the settings' reasoning.chat value and the model's own
// control says how it is carried. A model that cannot do what the
// setting asks for is a configuration error, and the error names the
// setting.
func (c *Catalog) ReasoningFields(name string) ([]poo.ReasoningField, error) {
	info, err := c.Resolve(name)
	if err != nil {
		return nil, err
	}
	return info.ReasoningFields(c.opts.Settings.Reasoning)
}

// Explanation is one field of a model and the layer that set it.
type Explanation struct {
	// Field is the field's name, as the Source map names it.
	Field string
	// Value is the field's value.
	Value string
	// Layer is the layer that set the field: curated, discovery,
	// settings or default.
	Layer string
}

// Explain returns the model's fields and the layer that set each of
// them.
//
// The fields come in the order of the model info, so the explanation
// reads like the model info it explains.
func (c *Catalog) Explain(name string) (poo.ModelInfo, []Explanation, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	e, ok := c.entries[name]
	if !ok {
		return poo.ModelInfo{}, nil, &UnresolvedError{Name: name}
	}
	info, source := c.merge(e)
	out := []Explanation{
		{Field: fieldName, Value: info.Name, Layer: source[fieldName]},
		{Field: fieldProvider, Value: info.Provider, Layer: source[fieldProvider]},
		{Field: fieldEndpoint, Value: info.Endpoint, Layer: source[fieldEndpoint]},
		{Field: fieldModelID, Value: info.ModelID, Layer: source[fieldModelID]},
		{Field: fieldAPI, Value: string(info.API), Layer: source[fieldAPI]},
		{Field: fieldOps, Value: joinOps(info.Ops), Layer: source[fieldOps]},
		{Field: fieldInputs, Value: joinInputs(info.Inputs), Layer: source[fieldInputs]},
		{Field: fieldContextLimit, Value: itoa(info.ContextLimit), Layer: source[fieldContextLimit]},
		{Field: fieldInputLimit, Value: itoa(info.InputLimit), Layer: source[fieldInputLimit]},
		{Field: fieldMaxOutput, Value: itoa(info.MaxOutput), Layer: source[fieldMaxOutput]},
		{Field: "sampling.temperature", Value: ftoa(info.Sampling.Temperature), Layer: source["sampling.temperature"]},
		{Field: "sampling.top_p", Value: ftoa(info.Sampling.TopP), Layer: source["sampling.top_p"]},
		{Field: "sampling.top_k", Value: ftoa(info.Sampling.TopK), Layer: source["sampling.top_k"]},
		{Field: "sampling.min_p", Value: ftoa(info.Sampling.MinP), Layer: source["sampling.min_p"]},
		{Field: fieldPrefixCache, Value: btoa(info.PrefixCache), Layer: source[fieldPrefixCache]},
		{Field: "tools.mode", Value: string(info.Tools.Mode), Layer: source["tools.mode"]},
		{Field: "tools.text_format", Value: info.Tools.TextFormat, Layer: source["tools.text_format"]},
		{Field: "tools.parallel", Value: btoa(boolp(info.Tools.Parallel)), Layer: source["tools.parallel"]},
		{Field: "reasoning.mode", Value: string(info.Reasoning.Mode), Layer: source["reasoning.mode"]},
		{Field: "reasoning.param", Value: info.Reasoning.Param, Layer: source["reasoning.param"]},
		{Field: fieldReasoningLevels, Value: joinStrings(info.ReasoningLevels), Layer: source[fieldReasoningLevels]},
		{Field: fieldTemperature, Value: string(info.Temperature), Layer: source[fieldTemperature]},
		{Field: fieldPromptCache, Value: string(info.PromptCache), Layer: source[fieldPromptCache]},
		{Field: fieldPromptVariant, Value: info.PromptVariant, Layer: source[fieldPromptVariant]},
		{Field: fieldQuirks, Value: joinStrings(info.Quirks), Layer: source[fieldQuirks]},
	}
	return info, out, nil
}

// Notes returns the curated notes about a model.
//
// The notes say what the model's family needs: the text tool-call
// parser, a reasoning switch, a context limit that bites.
func (c *Catalog) Notes(name string) []string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	e, ok := c.entries[name]
	if !ok {
		return nil
	}
	return e.notes
}

// Role returns the model assigned to a role, checked the way a session checks it.
//
// A role that no model is assigned to is an error, and so is a model that is in the
// catalog but does not serve what the role needs: a caller that asks a role for its
// model is asking what a session would run, and a session refuses the second.
func (c *Catalog) Role(role Role) (poo.ModelInfo, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	name, ok := c.roles[role]
	if !ok {
		return poo.ModelInfo{}, c.unassignedRole(role)
	}
	if _, ok := c.entries[name]; !ok {
		return poo.ModelInfo{}, c.unresolved(name, role)
	}
	info := c.effective(name)
	if !servesRole(info, role) {
		return poo.ModelInfo{}, &OpError{Role: role, Model: name, Ops: opsForRole(role)}
	}
	return info, nil
}

// Roles returns the model assigned to each role, by role name.
//
// It is the roles the settings assigned, which is the set the session enforces: a
// caller that reports on the roles reports on these, and adds the roles no one
// assigned for itself.
func (c *Catalog) Roles() map[Role]string {
	out := make(map[Role]string, len(c.roles))
	for role, name := range c.roles {
		out[role] = name
	}
	return out
}

// unresolved is the error for a model the catalog does not hold, with the models
// that would have served the role.
func (c *Catalog) unresolved(name string, role Role) *UnresolvedError {
	return &UnresolvedError{Name: name, Role: role, Known: c.servable(role)}
}

// unassignedRole is the error for a role no model is assigned to.
func (c *Catalog) unassignedRole(role Role) *UnresolvedError {
	return &UnresolvedError{Role: role, Known: c.servable(role)}
}

// servable returns the models that serve a role's operations, lowest name
// first.
//
// It is the list a reader copies a name from, so it is the names the catalog
// holds and not the ids the endpoints call them.
func (c *Catalog) servable(role Role) []string {
	var out []string
	for name := range c.entries {
		info := c.effective(name)
		if servesRole(info, role) {
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out
}

// CheckRoles checks the role assignments.
//
// Every role's model must serve the operation the role needs. The chat
// model must also resolve: a session without one cannot run, so a chat
// model the catalog does not hold is an error. Any other model the
// catalog does not hold is a warning, and the catalog carries on
// without it.
func (c *Catalog) CheckRoles() error {
	c.mu.RLock()
	defer c.mu.RUnlock()
	roles := make([]string, 0, len(c.roles))
	for role := range c.roles {
		roles = append(roles, string(role))
	}
	sort.Strings(roles)
	for _, name := range roles {
		role := Role(name)
		model, ok := c.roles[role]
		if !ok {
			continue
		}
		_, found := c.entries[model]
		if !found {
			if role == RoleChat {
				return c.unresolved(model, role)
			}
			c.warn("the model assigned to a role is not in the catalog",
				slog.String("role", name),
				slog.String("model", model))
			continue
		}
		info := c.effective(model)
		if !servesRole(info, role) {
			return &OpError{Role: role, Model: model, Ops: opsForRole(role)}
		}
	}
	return nil
}

// Endpoints returns the endpoints, by name.
func (c *Catalog) Endpoints() map[string]Endpoint {
	out := make(map[string]Endpoint, len(c.endpoints))
	for name, endpoint := range c.endpoints {
		out[name] = endpoint
	}
	return out
}

// CredentialEnvNames returns the names of the environment variables the
// endpoints' credentials read.
//
// The bash tool scrubs these names, so a key never reaches a child
// process. The names are sorted and hold no duplicates.
func (c *Catalog) CredentialEnvNames() []string {
	var names []string
	seen := map[string]bool{}
	for _, endpoint := range c.endpoints {
		for _, name := range endpoint.Credential.EnvNames() {
			if seen[name] {
				continue
			}
			seen[name] = true
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names
}

// effective returns the model's effective model info.
func (c *Catalog) effective(name string) poo.ModelInfo {
	e := c.entries[name]
	info, _ := c.merge(e)
	return info
}

// merge merges the layers of a model and records the layer that set
// each field.
//
// The curated layer comes first, the discovery layer next and the
// settings layer last, so a later layer wins field by field. A field no
// layer set keeps the catalog's default.
func (c *Catalog) merge(e *entry) (poo.ModelInfo, map[string]string) {
	out := poo.ModelInfo{}
	source := map[string]string{}
	applyLayer(&out, source, LayerCurated, e.curated)
	applyLayer(&out, source, LayerDiscovery, e.discovery)
	applyLayer(&out, source, LayerModelsFile, e.file)
	applyLayer(&out, source, LayerSettings, e.settings)
	if out.API == "" {
		// A model no layer gave an API to speaks the one its endpoint
		// declares, and an endpoint that declares none speaks the one the
		// endpoints poo reaches by default do.
		if endpoint, ok := c.endpoints[e.endpoint]; ok && endpoint.API != "" {
			out.API = endpoint.API
		} else {
			out.API = poo.APIChatCompletions
		}
		source[fieldAPI] = LayerDefault
	}
	if out.Name == "" {
		out.Name = e.name
		source[fieldName] = LayerDefault
	}
	// The provider is the endpoint the model was found under, and no layer
	// sets it as a field of its own: an entry is created for one endpoint,
	// so the entry is what says which provider serves the model. The layer it
	// is attributed to is the one that chose that endpoint, because that is
	// the choice a reader of the explanation is asking about.
	if out.Provider == "" {
		out.Provider = e.endpoint
		layer := source[fieldEndpoint]
		if layer == "" {
			layer = LayerDefault
		}
		source[fieldProvider] = layer
	}
	if len(out.Ops) == 0 {
		// A model no layer gives ops to is a chat model: the endpoints
		// poo speaks are chat-completions, and a role naming it has to
		// resolve. A model a family knows keeps the family's ops, since
		// only a layer that set them reaches here.
		out.Ops = []poo.Op{poo.OpChat}
		source[fieldOps] = LayerDefault
	}
	out.Source = source
	return out, source
}

// applyLayer copies the fields one layer set into the merged model.
//
// A field the layer did not set leaves the merged model alone, and a
// field it did set replaces what the earlier layers said.
func applyLayer(dst *poo.ModelInfo, source map[string]string, layer string, src poo.ModelInfo) {
	if src.Name != "" {
		dst.Name = src.Name
		source[fieldName] = layer
	}
	if src.Endpoint != "" {
		dst.Endpoint = src.Endpoint
		source[fieldEndpoint] = layer
	}
	if src.ModelID != "" {
		dst.ModelID = src.ModelID
		source[fieldModelID] = layer
	}
	if src.API != "" {
		dst.API = src.API
		source[fieldAPI] = layer
	}
	if len(src.Ops) > 0 {
		dst.Ops = src.Ops
		source[fieldOps] = layer
	}
	if len(src.Inputs) > 0 {
		dst.Inputs = src.Inputs
		source[fieldInputs] = layer
	}
	if src.ContextLimit > 0 {
		dst.ContextLimit = src.ContextLimit
		source[fieldContextLimit] = layer
	}
	if src.InputLimit > 0 {
		dst.InputLimit = src.InputLimit
		source[fieldInputLimit] = layer
	}
	if src.MaxOutput > 0 {
		dst.MaxOutput = src.MaxOutput
		source[fieldMaxOutput] = layer
	}
	if src.Sampling.Temperature != nil {
		dst.Sampling.Temperature = src.Sampling.Temperature
		source["sampling.temperature"] = layer
	}
	if src.Sampling.TopP != nil {
		dst.Sampling.TopP = src.Sampling.TopP
		source["sampling.top_p"] = layer
	}
	if src.Sampling.TopK != nil {
		dst.Sampling.TopK = src.Sampling.TopK
		source["sampling.top_k"] = layer
	}
	if src.Sampling.MinP != nil {
		dst.Sampling.MinP = src.Sampling.MinP
		source["sampling.min_p"] = layer
	}
	if src.PrefixCache != nil {
		dst.PrefixCache = src.PrefixCache
		source[fieldPrefixCache] = layer
	}
	if src.Tools.Mode != "" {
		dst.Tools.Mode = src.Tools.Mode
		source["tools.mode"] = layer
	}
	if src.Tools.TextFormat != "" {
		dst.Tools.TextFormat = src.Tools.TextFormat
		source["tools.text_format"] = layer
	}
	if src.Tools.Parallel {
		dst.Tools.Parallel = true
		source["tools.parallel"] = layer
	}
	if src.Reasoning.Mode != "" {
		dst.Reasoning.Mode = src.Reasoning.Mode
		source["reasoning.mode"] = layer
	}
	if src.Reasoning.Param != "" {
		dst.Reasoning.Param = src.Reasoning.Param
		source["reasoning.param"] = layer
	}
	if len(src.ReasoningLevels) > 0 {
		dst.ReasoningLevels = src.ReasoningLevels
		source[fieldReasoningLevels] = layer
	}
	if src.Temperature != "" {
		dst.Temperature = src.Temperature
		source[fieldTemperature] = layer
	}
	if src.PromptCache != "" {
		dst.PromptCache = src.PromptCache
		source[fieldPromptCache] = layer
	}
	if src.PromptVariant != "" {
		dst.PromptVariant = src.PromptVariant
		source[fieldPromptVariant] = layer
	}
	if len(src.Quirks) > 0 {
		dst.Quirks = src.Quirks
		source[fieldQuirks] = layer
	}
}

// warn logs a warning, if the catalog has a logger.
func (c *Catalog) warn(msg string, args ...any) {
	if c.opts.Logger == nil {
		return
	}
	c.opts.Logger.Warn(msg, args...)
}

// joinOps renders the operations for an explanation.
func joinOps(ops []poo.Op) string {
	out := make([]string, 0, len(ops))
	for _, op := range ops {
		out = append(out, string(op))
	}
	return joinStrings(out)
}

// joinInputs renders the modalities for an explanation.
func joinInputs(inputs []poo.Modality) string {
	out := make([]string, 0, len(inputs))
	for _, input := range inputs {
		out = append(out, string(input))
	}
	return joinStrings(out)
}

// joinStrings renders a list for an explanation.
func joinStrings(values []string) string {
	if len(values) == 0 {
		return ""
	}
	out := values[0]
	for _, value := range values[1:] {
		out += ", " + value
	}
	return out
}

// itoa renders an int for an explanation.
func itoa(v int) string { return fmt.Sprintf("%d", v) }

// ftoa renders a sampling value for an explanation.
func ftoa(v *float64) string {
	if v == nil {
		return ""
	}
	return fmt.Sprintf("%g", *v)
}

// btoa renders a bool for an explanation.
func btoa(v *bool) string {
	if v == nil {
		return ""
	}
	return fmt.Sprintf("%t", *v)
}

// boolp returns a pointer to a bool.
func boolp(v bool) *bool { return &v }
