package catalog

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
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
	// InputLimit is the largest prompt the endpoint accepts. Zero means
	// unknown.
	InputLimit int `json:"input_limit,omitempty"`
	// MaxOutput is the most tokens the model writes in one response. Zero
	// means unknown.
	MaxOutput int `json:"max_output,omitempty"`
	// PrefixCache reports whether the endpoint caches prompt prefixes.
	// Nil means unknown.
	PrefixCache *bool `json:"prefix_cache,omitempty"`
	// Tools is how the model makes tool calls. Empty means unknown.
	Tools poo.ToolsMode `json:"tools,omitempty"`
	// Reasoning is how the model's reasoning is controlled. Nil means
	// unknown.
	Reasoning *poo.ReasoningControl `json:"reasoning,omitempty"`
	// ReasoningLevels are the reasoning levels the model accepts, lowest
	// first. Empty means unknown.
	ReasoningLevels []string `json:"reasoning_levels,omitempty"`
	// Inputs are the modalities the model reads. Empty means unknown.
	Inputs []poo.Modality `json:"inputs,omitempty"`
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
	return c.refresh(ctx, c.endpointNames(), false)
}

// Reprobe probes every endpoint and merges the discovery layer, whatever
// the cache says.
//
// The wire's models/list reprobes when its caller asks for a refresh.
// That ask lands here, not at Refresh, which trusts a cache that is
// still fresh.
func (c *Catalog) Reprobe(ctx context.Context) error {
	return c.refresh(ctx, c.endpointNames(), true)
}

// ReprobeEndpoint probes one endpoint and merges its discovery layer, whatever
// the cache says.
//
// A caller that has just given an endpoint its credential asks this one, not
// Reprobe: the models a key just bought are worth learning, and the endpoints the
// operator was already running are not worth asking again at that moment.
func (c *Catalog) ReprobeEndpoint(ctx context.Context, name string) error {
	endpoint, ok := c.endpoints[name]
	if !ok {
		return nil
	}
	if endpoint.SkipProbe {
		return nil
	}
	return c.refresh(ctx, []string{name}, true)
}

// refresh probes the endpoints it names and merges the discovery layer.
//
// A probe that fails falls back to the cached result, and then to what the layers
// declare, with a warning. The refresh never fails over a probe: only the role
// check decides whether a model the catalog cannot resolve is an error.
//
// The cache is saved with what the endpoints it did not ask about still say, so
// a probe of one endpoint does not erase another's answer.
func (c *Catalog) refresh(ctx context.Context, names []string, force bool) error {
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
	asked := make(map[string]bool, len(names))
	for _, name := range names {
		asked[name] = true
		endpoint := c.endpoints[name]
		if !force {
			if models, shape, fetchedAt, ok := cache.fresh(cached, name); ok {
				found.models[name] = models
				found.shapes[name] = shape
				found.fetched[name] = fetchedAt
				continue
			}
		}
		models, shape, err := c.probeEndpoint(ctx, endpoint, c.modelsForEndpoint(name))
		if err != nil {
			c.warn("the endpoint could not be probed",
				slog.String("endpoint", name),
				slog.Any("error", err))
			cachedModels, cachedShape, fetchedAt, ok := cache.stale(cached, name)
			if !ok {
				continue
			}
			c.warn("the endpoint falls back to the cached result",
				slog.String("endpoint", name))
			models, shape = cachedModels, cachedShape
			found.fetched[name] = fetchedAt
		} else {
			found.fetched[name] = now
		}
		if len(models) == 0 {
			continue
		}
		found.models[name] = models
		found.shapes[name] = shape
	}
	// An endpoint this refresh did not ask about keeps what the cache already
	// said about it: the cache is one file for every endpoint, and a probe of one
	// is not a statement about the others.
	for name := range c.endpoints {
		if asked[name] {
			continue
		}
		models, shape, fetchedAt, ok := cache.stale(cached, name)
		if !ok {
			continue
		}
		found.models[name] = models
		found.shapes[name] = shape
		found.fetched[name] = fetchedAt
	}
	c.applyDiscovery(found)
	if err := cache.save(*found); err != nil {
		c.warn("the discovery cache could not be written",
			slog.Any("error", err))
	}
	return nil
}

// applyDiscovery merges the discovery layer into the catalog.
func (c *Catalog) applyDiscovery(found *discovery) {
	for _, endpoint := range sortedKeys(found.models) {
		models := found.models[endpoint]
		for _, id := range sortedModelIDs(models) {
			model := models[id]
			e := c.entryForModel(endpoint, id)
			if e == nil {
				// A model no layer declared is named for where it is served from, the way a
				// models file names one: two endpoints can serve one id, and a role that names
				// the bare id could not say which it meant.
				name := endpoint + "/" + id
				if _, taken := c.entries[name]; taken {
					continue
				}
				e = &entry{
					name:     name,
					endpoint: endpoint,
					modelID:  id,
				}
				c.entries[name] = e
				c.modelIndex[modelRef{endpoint: endpoint, modelID: id}] = e
			}
			e.discovery = discoveredLayer(model, c.endpoints[endpoint].Endpoint)
		}
	}
}

// endpointNames returns the names of the endpoints the catalog probes,
// sorted.
//
// The endpoints a probe asks are the ones the catalog offers: the models file's —
// which carries the built-in providers' record — and the settings'. An endpoint
// whose models are declared rather than listed is not asked: a probe would send a
// request the endpoint has no list to answer.
func (c *Catalog) endpointNames() []string {
	var names []string
	for name := range c.declared() {
		// The flag is read from the endpoint the catalog built, which is
		// where the settings and the models file meet.
		if c.endpoints[name].SkipProbe {
			continue
		}
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// declared returns the endpoints a layer declared, by name.
func (c *Catalog) declared() map[string]bool {
	names := make(map[string]bool, len(c.opts.Settings.Endpoints)+len(c.opts.File.Endpoints))
	for name := range c.opts.Settings.Endpoints {
		names[name] = true
	}
	for name := range c.opts.File.Endpoints {
		names[name] = true
	}
	return names
}

// modelsForEndpoint returns the ids of the models the operator named for an
// endpoint, in the settings or in the models file.
func (c *Catalog) modelsForEndpoint(endpoint string) []string {
	var ids []string
	seen := map[string]bool{}
	for _, declared := range []map[string]ModelSettings{c.opts.Settings.Models, c.opts.File.Models} {
		for _, model := range declared {
			if model.Endpoint != endpoint || model.ModelID == "" {
				continue
			}
			if seen[model.ModelID] {
				continue
			}
			seen[model.ModelID] = true
			ids = append(ids, model.ModelID)
		}
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
	// The three shapes share one budget, so an endpoint that does not
	// answer costs one timeout and not three.
	ctx, cancel := context.WithTimeout(ctx, c.probeTimeout())
	defer cancel()
	list, err := c.probeOpenAI(ctx, client, endpoint)
	if err != nil {
		return nil, "", err
	}
	props, err := c.probeLlamaCPP(ctx, client, endpoint)
	if err != nil {
		return nil, "", err
	}
	shown, err := c.probeOllama(ctx, client, endpoint, c.probeIDs(modelIDs, list))
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
	for _, id := range c.probeIDs(modelIDs, list) {
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
		if model.InputLimit == 0 {
			model.InputLimit = item.InputLimit
		}
		if model.MaxOutput == 0 {
			model.MaxOutput = item.MaxOutput
		}
		if model.PrefixCache == nil {
			model.PrefixCache = item.PrefixCache
		}
		if model.Tools == "" {
			model.Tools = item.Tools
		}
		if model.Reasoning == nil {
			model.Reasoning = item.Reasoning
		}
		if len(model.ReasoningLevels) == 0 {
			model.ReasoningLevels = item.ReasoningLevels
		}
		if len(model.Inputs) == 0 {
			model.Inputs = item.Inputs
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

// probeIDs returns the model ids the Ollama probe asks about.
//
// The ids the settings name, then the ids the model list gave, deduplicated.
// Every id the list named is asked about: a list is a server's own word
// on what it serves, and a model left unasked arrives as a bare id with no
// context window and no capabilities, which is a worse answer than not listing
// it. The requests are bounded by the list itself, which is the set the server
// says it has.
func (c *Catalog) probeIDs(modelIDs []string, list map[string]Discovered) []string {
	seen := make(map[string]bool, len(modelIDs)+len(list))
	out := make([]string, 0, len(modelIDs)+len(list))
	for _, id := range modelIDs {
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	for _, id := range sortedModelIDs(list) {
		if seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out
}

// tokenCount is a token budget as a model list reports it.
//
// Gateways write the same number as 128000, 128000.0, 1e5, "128000" or
// null. The count reads all of them, and anything that is not a whole
// number of tokens, such as a negative, a fraction, text or a value past
// what a context window can be, reads as zero, which means unknown. One
// odd field costs that field, not the whole list.
type tokenCount int

// UnmarshalJSON reads a token budget, and never fails.
func (n *tokenCount) UnmarshalJSON(data []byte) error {
	*n = 0
	text := strings.TrimSpace(string(data))
	if unquoted, err := strconv.Unquote(text); err == nil {
		text = strings.TrimSpace(unquoted)
	}
	value, err := strconv.ParseFloat(text, 64)
	if err != nil || value < 1 || value > math.MaxInt32 || value != math.Trunc(value) {
		return nil
	}
	*n = tokenCount(value)
	return nil
}

// probeOpenAI reads the OpenAI-compatible model list.
//
// The shape every OpenAI-compatible endpoint serves is
// {"data":[{"id":"...","max_model_len":32768}]}. A gateway that knows its
// models better says more in the same place, and the probe reads what is
// there: capabilities for the tools and the reasoning, modalities for the
// inputs, and the token budgets in whichever shape the gateway uses:
//
//   - an opencode limit object, {"limit":{"context":N,"input":N,"output":N}};
//   - LiteLLM's max_input_tokens and max_output_tokens;
//   - OpenRouter's context_length and top_provider, which carries its own
//     context_length and max_completion_tokens;
//   - vLLM's max_model_len.
//
// The output limit is the first of limit.output, max_output_tokens and
// top_provider.max_completion_tokens. The context limit is the first of
// limit.context, context_length, top_provider.context_length and
// max_model_len. The input limit is the largest prompt the endpoint takes,
// which can be under the context: the first of limit.input and
// max_input_tokens. A field the endpoint does not send stays unknown. A
// server that does not serve the list answers with an error, and the probe
// reports that the shape is not there.
func (c *Catalog) probeOpenAI(ctx context.Context, client *http.Client, endpoint Endpoint) (map[string]Discovered, error) {
	body, err := c.get(ctx, client, openAIBase(endpoint.Endpoint)+"/models", endpoint)
	if err != nil {
		if isNotFound(err) {
			return nil, nil
		}
		return nil, err
	}
	var list struct {
		Data []struct {
			ID              string     `json:"id"`
			MaxModelLen     tokenCount `json:"max_model_len"`
			MaxInputTokens  tokenCount `json:"max_input_tokens"`
			MaxOutputTokens tokenCount `json:"max_output_tokens"`
			ContextLength   tokenCount `json:"context_length"`
			PrefixCaching   *bool      `json:"prefix_caching"`
			Limit           struct {
				Context tokenCount `json:"context"`
				Input   tokenCount `json:"input"`
				Output  tokenCount `json:"output"`
			} `json:"limit"`
			TopProvider struct {
				ContextLength       tokenCount `json:"context_length"`
				MaxCompletionTokens tokenCount `json:"max_completion_tokens"`
			} `json:"top_provider"`
			Capabilities struct {
				Tools struct {
					Supported *bool `json:"supported"`
				} `json:"tools"`
				Reasoning struct {
					Effort []string `json:"effort"`
				} `json:"reasoning"`
			} `json:"capabilities"`
			Modalities struct {
				Input []string `json:"input"`
			} `json:"modalities"`
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
		reasoning := item.Capabilities.Reasoning.Effort
		model := Discovered{
			ModelID:     item.ID,
			InputLimit:  int(cmp.Or(item.Limit.Input, item.MaxInputTokens)),
			MaxOutput:   int(cmp.Or(item.Limit.Output, item.MaxOutputTokens, item.TopProvider.MaxCompletionTokens)),
			PrefixCache: item.PrefixCaching,
			Inputs:      modalities(item.Modalities.Input),
		}
		if len(reasoning) > 0 {
			// The model takes a level as the effort parameter, and the levels
			// are the ones the list named.
			model.Reasoning = &poo.ReasoningControl{
				Mode:  poo.ReasoningEffort,
				Param: poo.ReasoningPathEffort,
			}
			model.ReasoningLevels = reasoning
		}
		// The limit object wins when the list carries one; max_model_len is
		// the plain shape's answer, read only when nothing else names the
		// context.
		model.ContextLimit = int(cmp.Or(item.Limit.Context, item.ContextLength, item.TopProvider.ContextLength, item.MaxModelLen))
		if item.Capabilities.Tools.Supported != nil {
			model.Tools = poo.ToolsNone
			if *item.Capabilities.Tools.Supported {
				model.Tools = poo.ToolsNative
			}
		}
		out[item.ID] = model
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
func (c *Catalog) probeLlamaCPP(ctx context.Context, client *http.Client, endpoint Endpoint) (*llamaProps, error) {
	body, err := c.get(ctx, client, serverRoot(endpoint.Endpoint)+"/props", endpoint)
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
//
// One model is one request, so a hosted endpoint that serves a long list is
// asked about every id at once up to showWorkers: a local server answering a
// handful of requests together is unbothered, and a cloud list of two dozen
// models arrives in the time one request took.
func (c *Catalog) probeOllama(ctx context.Context, client *http.Client, endpoint Endpoint, modelIDs []string) (map[string]Discovered, error) {
	out := map[string]Discovered{}
	var (
		mu      sync.Mutex
		workers sync.WaitGroup
		failed  error
	)
	asking := make(chan struct{}, showWorkers)
	for _, id := range modelIDs {
		workers.Add(1)
		go func(id string) {
			defer workers.Done()
			asking <- struct{}{}
			defer func() { <-asking }()
			body, err := c.post(ctx, client, serverRoot(endpoint.Endpoint)+"/api/show", map[string]string{"model": id}, endpoint)
			if err != nil {
				if isNotFound(err) {
					// The server does not serve the show shape. One id's answer
					// settles it for the probe, and the list's other fields stand.
					return
				}
				mu.Lock()
				if failed == nil {
					failed = err
				}
				mu.Unlock()
				return
			}
			var show struct {
				Template     string            `json:"template"`
				ModelInfo    map[string]any    `json:"model_info"`
				Capabilities []string          `json:"capabilities"`
				Details      map[string]string `json:"details"`
			}
			if err := json.Unmarshal(body, &show); err != nil {
				mu.Lock()
				if failed == nil {
					failed = fmt.Errorf("catalog: the model show is not readable: %w", err)
				}
				mu.Unlock()
				return
			}
			model := Discovered{ModelID: id}
			model.ContextLimit = contextLength(show.ModelInfo)
			if hasCapability(show.Capabilities, "tools") {
				model.Tools = poo.ToolsNative
			}
			if reasoning := templateReasoning(show.Template); reasoning != nil {
				model.Reasoning = reasoning
			}
			mu.Lock()
			out[id] = model
			mu.Unlock()
		}(id)
	}
	workers.Wait()
	if failed != nil {
		return nil, failed
	}
	return out, nil
}

// showWorkers is how many model shows one probe has in flight at once.
const showWorkers = 6

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
		Endpoint:        endpointURL,
		ModelID:         model.ModelID,
		ContextLimit:    model.ContextLimit,
		InputLimit:      model.InputLimit,
		MaxOutput:       model.MaxOutput,
		PrefixCache:     model.PrefixCache,
		Tools:           poo.ToolCalling{Mode: model.Tools},
		Inputs:          model.Inputs,
		ReasoningLevels: model.ReasoningLevels,
	}
	if model.Reasoning != nil {
		info.Reasoning = *model.Reasoning
	}
	return info
}

// get reads a URL with the endpoint's credential.
func (c *Catalog) get(ctx context.Context, client *http.Client, url string, endpoint Endpoint) ([]byte, error) {
	ctx, cancel := c.withTimeout(ctx)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("catalog: the request for %s is not buildable: %w", url, err)
	}
	return c.do(client, req, endpoint)
}

// post sends a JSON body to a URL with the endpoint's credential.
func (c *Catalog) post(ctx context.Context, client *http.Client, url string, payload any, endpoint Endpoint) ([]byte, error) {
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
	return c.do(client, req, endpoint)
}

// do sends a request and reads the answer.
//
// The request carries the endpoint's key when one is in reach, in the header the
// OpenAI-compatible shape reads it from: a hosted provider serves its model list
// to a key, and the probe is how the catalog learns what it serves.
func (c *Catalog) do(client *http.Client, req *http.Request, endpoint Endpoint) ([]byte, error) {
	if c.opts.UserAgent != "" {
		req.Header.Set("User-Agent", c.opts.UserAgent)
	}
	if c.opts.EndpointKey != nil {
		if key := c.opts.EndpointKey(endpoint.Name, endpoint.Credential); key != "" {
			req.Header.Set("Authorization", "Bearer "+key)
		}
	}
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
