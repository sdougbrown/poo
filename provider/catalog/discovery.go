package catalog

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/sdougbrown/poo"
)

// DefaultDiscoveryTTL is how long a cached discovery result stays
// fresh.
const DefaultDiscoveryTTL = 10 * time.Minute

// DefaultProbeTimeout bounds one probe.
const DefaultProbeTimeout = 5 * time.Second

// The shapes an endpoint may serve.
const (
	// shapeOpenAI is the OpenAI-compatible model list.
	shapeOpenAI = "openai"
	// shapeLlamaCPP is llama.cpp's server props.
	shapeLlamaCPP = "llamacpp"
	// shapeOllama is Ollama's model show.
	shapeOllama = "ollama"
)

// Discovered is what an endpoint reported about one model.
type Discovered struct {
	// ModelID is the id the endpoint calls the model.
	ModelID string `json:"model_id"`
	// ContextLimit is the model's context window. Zero means unknown.
	ContextLimit int `json:"context_limit,omitempty"`
	// PrefixCache reports whether the endpoint caches prompt prefixes.
	// Nil means unknown.
	PrefixCache *bool `json:"prefix_cache,omitempty"`
	// Tools is how the model makes tool calls. Empty means unknown.
	Tools poo.ToolsMode `json:"tools,omitempty"`
	// Reasoning is how the model's reasoning is controlled. Nil means
	// unknown.
	Reasoning *poo.ReasoningControl `json:"reasoning,omitempty"`
}

// discovery is what the probes found.
type discovery struct {
	// models holds each endpoint's models, by endpoint name.
	models map[string]map[string]Discovered
	// shapes names the shape that answered for each endpoint.
	shapes map[string]string
	// fetched holds when each endpoint was probed.
	fetched map[string]time.Time
	// fetchedAt is when the probes ran.
	fetchedAt time.Time
}

// Refresh merges the discovery layer, probing the endpoints whose
// cached result has gone stale.
//
// The cached result of an endpoint that was probed recently is used as
// it is, so a catalog that is built often does not ask the endpoints
// again on every run.
func (c *Catalog) Refresh(ctx context.Context) error {
	return c.refresh(ctx, false)
}

// Reprobe probes every endpoint and merges the discovery layer, whatever
// the cache says.
//
// The wire's models/list asks for a refresh, and a refresh means the
// endpoints are asked again.
func (c *Catalog) Reprobe(ctx context.Context) error {
	return c.refresh(ctx, true)
}

// refresh probes the endpoints and merges the discovery layer.
//
// A probe that fails falls back to the cached result, and then to the
// curated catalog, with a warning. The refresh never fails over a
// probe: only the role check decides whether a model the catalog cannot
// resolve is an error.
func (c *Catalog) refresh(ctx context.Context, force bool) error {
	cache := c.cache()
	cached, err := cache.load()
	if err != nil {
		c.warn("the discovery cache could not be read",
			slog.Any("error", err))
	}
	now := time.Now()
	found := &discovery{
		models:    map[string]map[string]Discovered{},
		shapes:    map[string]string{},
		fetched:   map[string]time.Time{},
		fetchedAt: now,
	}
	for _, name := range c.endpointNames() {
		endpoint := c.endpoints[name]
		if !force {
			if models, shape, ok := c.freshCache(cached, name); ok {
				found.models[name] = models
				found.shapes[name] = shape
				found.fetched[name] = cached.Endpoints[name].FetchedAt
				continue
			}
		}
		models, shape, err := c.probeEndpoint(ctx, endpoint, c.modelsForEndpoint(name))
		if err != nil {
			c.warn("the endpoint could not be probed",
				slog.String("endpoint", name),
				slog.Any("error", err))
			models, shape = c.fromCache(cached, name)
			if len(models) == 0 {
				continue
			}
			c.warn("the endpoint falls back to the cached result",
				slog.String("endpoint", name))
			found.fetched[name] = cached.Endpoints[name].FetchedAt
		} else {
			found.fetched[name] = now
		}
		if len(models) == 0 {
			continue
		}
		found.models[name] = models
		found.shapes[name] = shape
	}
	c.applyDiscovery(found)
	if err := cache.save(*found); err != nil {
		c.warn("the discovery cache could not be written",
			slog.Any("error", err))
	}
	return nil
}

// freshCache returns an endpoint's cached models, when the cache is
// still fresh.
func (c *Catalog) freshCache(cached *cacheFile, endpoint string) (map[string]Discovered, string, bool) {
	if cached == nil {
		return nil, "", false
	}
	entry, ok := cached.Endpoints[endpoint]
	if !ok || !c.cache().freshEndpoint(entry) {
		return nil, "", false
	}
	return entry.Models, entry.Shape, true
}

// applyDiscovery merges the discovery layer into the catalog.
func (c *Catalog) applyDiscovery(found *discovery) {
	for _, endpoint := range sortedKeys(found.models) {
		models := found.models[endpoint]
		for _, id := range sortedModelIDs(models) {
			model := models[id]
			e := c.entryForModel(endpoint, id)
			if e == nil {
				e = &entry{
					name:     id,
					endpoint: endpoint,
					modelID:  id,
				}
				c.entries[id] = e
			}
			e.discovery = discoveredLayer(model, c.endpoints[endpoint].BaseURL)
		}
	}
}

// fromCache returns an endpoint's cached models.
func (c *Catalog) fromCache(cached *cacheFile, endpoint string) (map[string]Discovered, string) {
	if cached == nil {
		return nil, ""
	}
	entry, ok := cached.Endpoints[endpoint]
	if !ok {
		return nil, ""
	}
	return entry.Models, entry.Shape
}

// endpointNames returns the names of the endpoints the catalog probes,
// sorted.
//
// The hosted providers are not probed: a hosted endpoint is asked for a
// key that is not there, and the curated entry already says what the
// provider's models are.
func (c *Catalog) endpointNames() []string {
	var names []string
	for name := range c.opts.Settings.Endpoints {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// modelsForEndpoint returns the ids of the models the settings name for
// an endpoint.
func (c *Catalog) modelsForEndpoint(endpoint string) []string {
	var ids []string
	seen := map[string]bool{}
	for _, model := range c.opts.Settings.Models {
		if model.Endpoint != endpoint || model.ModelID == "" {
			continue
		}
		if seen[model.ModelID] {
			continue
		}
		seen[model.ModelID] = true
		ids = append(ids, model.ModelID)
	}
	sort.Strings(ids)
	return ids
}

// probeEndpoint asks an endpoint what it serves.
//
// The three shapes are asked in turn and their answers are combined: the
// OpenAI-compatible list gives the model ids, llama.cpp's props give
// the server's context and chat template, and Ollama's show gives a
// model's own context, template and capabilities. A shape the endpoint
// does not serve is skipped.
//
// A model's own answer wins over the server's: Ollama's context length
// is the model's, the model list's max_model_len is the model's, and
// llama.cpp's n_ctx is the server's, which is what is left when the
// model says nothing.
func (c *Catalog) probeEndpoint(ctx context.Context, endpoint Endpoint, modelIDs []string) (map[string]Discovered, string, error) {
	client := c.opts.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: c.probeTimeout()}
	}
	list, err := c.probeOpenAI(ctx, client, endpoint.BaseURL)
	if err != nil {
		return nil, "", err
	}
	props, err := c.probeLlamaCPP(ctx, client, endpoint.BaseURL)
	if err != nil {
		return nil, "", err
	}
	shown, err := c.probeOllama(ctx, client, endpoint.BaseURL, c.candidates(modelIDs, list))
	if err != nil {
		return nil, "", err
	}

	shape := ""
	switch {
	case len(list) > 0:
		shape = shapeOpenAI
	case props != nil:
		shape = shapeLlamaCPP
	case len(shown) > 0:
		shape = shapeOllama
	}
	if shape == "" {
		return nil, "", nil
	}

	models := map[string]Discovered{}
	for _, id := range c.candidates(modelIDs, list) {
		models[id] = Discovered{ModelID: id}
	}
	for id, item := range list {
		model, ok := models[id]
		if !ok {
			model = Discovered{ModelID: id}
		}
		if model.ContextLimit == 0 {
			model.ContextLimit = item.ContextLimit
		}
		if model.PrefixCache == nil {
			model.PrefixCache = item.PrefixCache
		}
		models[id] = model
	}
	for id, item := range shown {
		model, ok := models[id]
		if !ok {
			model = Discovered{ModelID: id}
		}
		if item.ContextLimit > 0 {
			model.ContextLimit = item.ContextLimit
		}
		if item.PrefixCache != nil {
			model.PrefixCache = item.PrefixCache
		}
		if item.Tools != "" {
			model.Tools = item.Tools
		}
		if item.Reasoning != nil {
			model.Reasoning = item.Reasoning
		}
		models[id] = model
	}
	if props != nil {
		for id, model := range models {
			if model.ContextLimit == 0 {
				model.ContextLimit = props.nCtx
			}
			if model.PrefixCache == nil {
				model.PrefixCache = props.cachePrompt
			}
			if model.Reasoning == nil {
				model.Reasoning = templateReasoning(props.template)
			}
			models[id] = model
		}
	}
	return models, shape, nil
}

// candidates returns the model ids the Ollama probe asks about.
//
// The models the settings name come first, and the ids the model list
// gave follow, up to a limit: a server that serves a hundred models
// would otherwise be asked about a hundred of them.
func (c *Catalog) candidates(modelIDs []string, list map[string]Discovered) []string {
	const limit = 16
	out := append([]string{}, modelIDs...)
	for _, id := range sortedModelIDs(list) {
		if len(out) >= limit {
			break
		}
		out = append(out, id)
	}
	return out
}

// probeOpenAI reads the OpenAI-compatible model list.
//
// The shape is the one every OpenAI-compatible endpoint serves:
// {"data":[{"id":"...","max_model_len":32768}]}. A server that does not
// serve the list answers with an error, and the probe reports that the
// shape is not there.
func (c *Catalog) probeOpenAI(ctx context.Context, client *http.Client, baseURL string) (map[string]Discovered, error) {
	body, err := c.get(ctx, client, openAIBase(baseURL)+"/models")
	if err != nil {
		if isNotFound(err) {
			return nil, nil
		}
		return nil, err
	}
	var list struct {
		Data []struct {
			ID            string `json:"id"`
			MaxModelLen   int    `json:"max_model_len"`
			PrefixCaching *bool  `json:"prefix_caching"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &list); err != nil {
		return nil, fmt.Errorf("catalog: the model list is not readable: %w", err)
	}
	out := map[string]Discovered{}
	for _, item := range list.Data {
		if item.ID == "" {
			continue
		}
		out[item.ID] = Discovered{
			ModelID:      item.ID,
			ContextLimit: item.MaxModelLen,
			PrefixCache:  item.PrefixCaching,
		}
	}
	return out, nil
}

// llamaProps is what llama.cpp's server reports.
type llamaProps struct {
	// nCtx is the server's context window.
	nCtx int
	// cachePrompt reports whether the server caches the prompt.
	cachePrompt *bool
	// template is the server's chat template.
	template string
}

// probeLlamaCPP reads llama.cpp's server props.
//
// The shape is {"default_generation_settings":{"n_ctx":32768,
// "cache_prompt":true},"chat_template":"..."}. A server that does not
// serve the props answers with an error, and the probe reports that the
// shape is not there.
func (c *Catalog) probeLlamaCPP(ctx context.Context, client *http.Client, baseURL string) (*llamaProps, error) {
	body, err := c.get(ctx, client, serverRoot(baseURL)+"/props")
	if err != nil {
		if isNotFound(err) {
			return nil, nil
		}
		return nil, err
	}
	var props struct {
		DefaultGenerationSettings struct {
			NCtx        int   `json:"n_ctx"`
			CachePrompt *bool `json:"cache_prompt"`
		} `json:"default_generation_settings"`
		ChatTemplate string `json:"chat_template"`
	}
	if err := json.Unmarshal(body, &props); err != nil {
		return nil, fmt.Errorf("catalog: the server props are not readable: %w", err)
	}
	out := &llamaProps{
		nCtx:        props.DefaultGenerationSettings.NCtx,
		cachePrompt: props.DefaultGenerationSettings.CachePrompt,
		template:    props.ChatTemplate,
	}
	return out, nil
}

// probeOllama asks Ollama about the models it serves.
//
// The shape is Ollama's show endpoint: {"template":"...",
// "model_info":{"gemma3.context_length":131072},
// "capabilities":["completion","tools"]}. A server that does not serve
// it answers with an error, and the probe reports that the shape is not
// there.
func (c *Catalog) probeOllama(ctx context.Context, client *http.Client, baseURL string, modelIDs []string) (map[string]Discovered, error) {
	out := map[string]Discovered{}
	for _, id := range modelIDs {
		body, err := c.post(ctx, client, serverRoot(baseURL)+"/api/show", map[string]string{"model": id})
		if err != nil {
			if isNotFound(err) {
				return out, nil
			}
			return nil, err
		}
		var show struct {
			Template     string            `json:"template"`
			ModelInfo    map[string]any    `json:"model_info"`
			Capabilities []string          `json:"capabilities"`
			Details      map[string]string `json:"details"`
		}
		if err := json.Unmarshal(body, &show); err != nil {
			return nil, fmt.Errorf("catalog: the model show is not readable: %w", err)
		}
		model := Discovered{ModelID: id}
		model.ContextLimit = contextLength(show.ModelInfo)
		if hasCapability(show.Capabilities, "tools") {
			model.Tools = poo.ToolsNative
		}
		if reasoning := templateReasoning(show.Template); reasoning != nil {
			model.Reasoning = reasoning
		}
		out[id] = model
	}
	return out, nil
}

// contextLength returns the context window a model reports.
//
// Ollama names it in the model info, under the architecture's key:
// {"gemma3.context_length":131072}.
func contextLength(info map[string]any) int {
	longest := 0
	for key, value := range info {
		if !strings.HasSuffix(key, ".context_length") && key != "context_length" {
			continue
		}
		length, ok := value.(float64)
		if !ok {
			continue
		}
		if int(length) > longest {
			longest = int(length)
		}
	}
	return longest
}

// templateReasoning returns the reasoning control a chat template
// carries.
//
// A template that mentions enable_thinking is switched by the request's
// chat template arguments.
func templateReasoning(template string) *poo.ReasoningControl {
	if !strings.Contains(template, "enable_thinking") {
		return nil
	}
	return &poo.ReasoningControl{
		Mode:  poo.ReasoningToggle,
		Param: poo.ReasoningPathToggle,
	}
}

// hasCapability reports whether a list of capabilities holds one.
func hasCapability(capabilities []string, name string) bool {
	for _, capability := range capabilities {
		if capability == name {
			return true
		}
	}
	return false
}

// discoveredLayer turns a discovered model into the fields of the
// discovery layer.
func discoveredLayer(model Discovered, endpointURL string) poo.ModelInfo {
	info := poo.ModelInfo{
		Endpoint:     endpointURL,
		ModelID:      model.ModelID,
		ContextLimit: model.ContextLimit,
		PrefixCache:  model.PrefixCache,
		Tools:        poo.ToolCalling{Mode: model.Tools},
	}
	if model.Reasoning != nil {
		info.Reasoning = *model.Reasoning
	}
	return info
}

// get reads a URL.
func (c *Catalog) get(ctx context.Context, client *http.Client, url string) ([]byte, error) {
	ctx, cancel := c.withTimeout(ctx)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("catalog: the request for %s is not buildable: %w", url, err)
	}
	return c.do(client, req)
}

// post sends a JSON body to a URL.
func (c *Catalog) post(ctx context.Context, client *http.Client, url string, payload any) ([]byte, error) {
	ctx, cancel := c.withTimeout(ctx)
	defer cancel()
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("catalog: the request for %s is not buildable: %w", url, err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, strings.NewReader(string(body)))
	if err != nil {
		return nil, fmt.Errorf("catalog: the request for %s is not buildable: %w", url, err)
	}
	req.Header.Set("Content-Type", "application/json")
	return c.do(client, req)
}

// do sends a request and reads the answer.
func (c *Catalog) do(client *http.Client, req *http.Request) ([]byte, error) {
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("catalog: the request for %s failed: %w", req.URL, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, &statusError{url: req.URL.String(), status: resp.StatusCode}
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("catalog: the answer from %s is not readable: %w", req.URL, err)
	}
	return body, nil
}

// withTimeout bounds one probe.
func (c *Catalog) withTimeout(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, c.probeTimeout())
}

// probeTimeout returns the bound one probe gets.
func (c *Catalog) probeTimeout() time.Duration {
	if c.opts.ProbeTimeout > 0 {
		return c.opts.ProbeTimeout
	}
	return DefaultProbeTimeout
}

// statusError is a probe that the endpoint refused.
type statusError struct {
	url    string
	status int
}

func (e *statusError) Error() string {
	return fmt.Sprintf("catalog: %s answered with status %d", e.url, e.status)
}

// isNotFound reports whether a failure is the endpoint saying that the
// shape is not there.
func isNotFound(err error) bool {
	var status *statusError
	if !errors.As(err, &status) {
		return false
	}
	return status.status == http.StatusNotFound || status.status == http.StatusMethodNotAllowed
}

// openAIBase returns the OpenAI-compatible base of an endpoint.
func openAIBase(baseURL string) string {
	trimmed := strings.TrimRight(baseURL, "/")
	if strings.HasSuffix(trimmed, "/v1") {
		return trimmed
	}
	return trimmed + "/v1"
}

// serverRoot returns the root of an endpoint, without its OpenAI-
// compatible path.
func serverRoot(baseURL string) string {
	trimmed := strings.TrimRight(baseURL, "/")
	if strings.HasSuffix(trimmed, "/v1") {
		return strings.TrimSuffix(trimmed, "/v1")
	}
	return trimmed
}

// sortedKeys returns the sorted keys of a map.
func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for key := range m {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}

// sortedModelIDs returns the sorted ids of the discovered models.
func sortedModelIDs(m map[string]Discovered) []string {
	return sortedKeys(m)
}
