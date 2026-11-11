package config

// CoreToolNames is the list of the tools the core adds by itself, for the
// tests that check the settings refuse to disable one.
var CoreToolNames = coreToolNames

// ModelAPIKey is the key the runtime sends for a model, for the tests
// that read the credential path. // that check which credential an endpoint resolves to.

func (r *Runtime) ModelAPIKey(model string) string { return r.apiKey(model) }
