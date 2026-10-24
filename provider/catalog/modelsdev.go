package catalog

//go:generate go run ./internal/genmodelsdev/cmd/genmodelsdev -input testdata/models.dev.json -output modelsdev_gen.go

// hostedEntry is one hosted model, as the models.dev data describes it.
//
// The entries are generated: the generator reads a models.dev JSON file
// and writes them into modelsdev_gen.go. Fetching the data itself is
// not the generator's job, so the build never reaches the network.
type hostedEntry struct {
	// Provider is the models.dev provider that serves the model.
	Provider string
	// ModelID is the model's id.
	ModelID string
	// Name is the model's display name.
	Name string
	// BaseURL is the provider's API base URL.
	BaseURL string
	// Env names the environment variables that hold the provider's key.
	Env []string
	// ContextLimit is the model's context window.
	ContextLimit int
	// MaxOutput is the model's output limit.
	MaxOutput int
	// Reasoning reports whether the model reasons.
	Reasoning bool
	// ToolCall reports whether the model makes tool calls.
	ToolCall bool
	// Temperature reports whether the model takes a temperature.
	Temperature bool
	// Inputs are the modalities the model reads.
	Inputs []string
}
