package config

// CoreToolNames is the list of the tools the core adds by itself, for the
// tests that check the settings refuse to disable one.
var CoreToolNames = coreToolNames

// ModelAPIKey is the key the runtime sends for a model, for the tests that check
// which credential an endpoint resolves to.
//
// The model is resolved the way a transport's is, so the answer includes the
// catalog's own resolution of the provider.
func (r *Runtime) ModelAPIKey(name string) string {
	model, err := r.Catalog.Resolve(name)
	if err != nil {
		return ""
	}
	return r.apiKey(model)
}
