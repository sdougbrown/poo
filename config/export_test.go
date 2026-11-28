package config

import "slices"

// CoreToolNames is the list of the tools the core adds by itself, for the
// tests that check the settings refuse to disable one.
var CoreToolNames = coreToolNames

// MCPServerNames is the servers the build's mcp manifests declare, in name
// order, for the tests that check which manifests a build loaded.
func (r *Runtime) MCPServerNames() []string {
	return r.mcpSet.Names()
}

// ProfileHooks is the hook chain a profile resolves to, for the tests that
// check inheritance and opt-out.
func (r *Runtime) ProfileHooks(name string) []string {
	return slices.Clone(r.resolvedProfiles[name].hooks)
}

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
	return r.EndpointKey(model)
}

// RegisterStoreBackendForTest registers a store opener for a test and
// returns the cleanup that restores the previous entry, so a test that
// registers a backend of its own leaves the registry the way it found
// it.
func RegisterStoreBackendForTest(name string, fn StoreOpener) func() {
	storeBackendsMu.Lock()
	prev, hadPrev := storeBackends[name]
	storeBackends[name] = fn
	storeBackendsMu.Unlock()
	return func() {
		storeBackendsMu.Lock()
		defer storeBackendsMu.Unlock()
		if hadPrev {
			storeBackends[name] = prev
		} else {
			delete(storeBackends, name)
		}
	}
}
