package catalog

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// cacheVersion is the version of the cache file's format.
//
// A file with another version is ignored: the catalog reads it as
// empty and probes the endpoints again.
const cacheVersion = 1

// cacheFile is the on-disk shape of the discovery cache.
//
// The file holds what the endpoints reported and when they reported it,
// so a catalog that cannot reach an endpoint still knows what the
// endpoint served.
type cacheFile struct {
	// Version is the format's version.
	Version int `json:"version"`
	// FetchedAt is when the probes ran.
	FetchedAt time.Time `json:"fetched_at"`
	// Endpoints holds each endpoint's result, by endpoint name.
	Endpoints map[string]cacheEndpoint `json:"endpoints"`
}

// cacheEndpoint is one endpoint's cached result.
type cacheEndpoint struct {
	// FetchedAt is when the endpoint was probed.
	FetchedAt time.Time `json:"fetched_at"`
	// Shape names the shape that answered.
	Shape string `json:"shape,omitempty"`
	// Models are the models the endpoint reported, by id.
	Models map[string]Discovered `json:"models"`
}

// cache is the discovery cache on disk.
type cache struct {
	// path is the cache file's path.
	path string
	// ttl is how long a cached result stays fresh.
	ttl time.Duration
	// now reads the clock.
	now func() time.Time
}

// cache returns the catalog's discovery cache.
func (c *Catalog) cache() *cache {
	return &cache{
		path: cachePath(c.opts.StateDir),
		ttl:  c.discoveryTTL(),
		now:  time.Now,
	}
}

// discoveryTTL returns how long a cached result stays fresh.
func (c *Catalog) discoveryTTL() time.Duration {
	if c.opts.DiscoveryTTL > 0 {
		return c.opts.DiscoveryTTL
	}
	return DefaultDiscoveryTTL
}

// load reads the cache.
//
// A cache that is not there, or that holds another version, reads as an
// empty cache: the catalog probes the endpoints again.
func (c *cache) load() (*cacheFile, error) {
	data, err := os.ReadFile(c.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("catalog: reading the discovery cache: %w", err)
	}
	var file cacheFile
	if err := json.Unmarshal(data, &file); err != nil {
		return nil, fmt.Errorf("catalog: the discovery cache is not readable: %w", err)
	}
	if file.Version != cacheVersion {
		return nil, nil
	}
	return &file, nil
}

// save writes the cache.
func (c *cache) save(found discovery) error {
	file := cacheFile{
		Version:   cacheVersion,
		FetchedAt: found.fetchedAt,
		Endpoints: map[string]cacheEndpoint{},
	}
	for name, models := range found.models {
		fetchedAt := found.fetchedAt
		if at, ok := found.fetched[name]; ok {
			fetchedAt = at
		}
		file.Endpoints[name] = cacheEndpoint{
			FetchedAt: fetchedAt,
			Shape:     found.shapes[name],
			Models:    models,
		}
	}
	data, err := json.MarshalIndent(file, "", "  ")
	if err != nil {
		return fmt.Errorf("catalog: the discovery cache is not writable: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(c.path), 0o700); err != nil {
		return fmt.Errorf("catalog: creating the state directory: %w", err)
	}
	if err := os.WriteFile(c.path, data, 0o600); err != nil {
		return fmt.Errorf("catalog: writing the discovery cache: %w", err)
	}
	return nil
}

// freshEndpoint reports whether an endpoint's cached result is still
// within the TTL.
func (c *cache) freshEndpoint(entry cacheEndpoint) bool {
	return c.now().Sub(entry.FetchedAt) < c.ttl
}

// cachePath returns the path of the discovery cache.
//
// The cache lives in the state directory, under the poo directory: the
// state directory is the user's own, and the cache is poo's.
func cachePath(stateDir string) string {
	if stateDir == "" {
		stateDir = defaultStateDir()
	}
	return filepath.Join(stateDir, "poo", "discovery.json")
}

// defaultStateDir returns the user's state directory.
//
// The state directory is the one the XDG specification names, and the
// cache directory is the fallback: a system without a state directory
// still has one.
func defaultStateDir() string {
	if dir := os.Getenv("XDG_STATE_HOME"); dir != "" {
		return dir
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		return filepath.Join(home, ".local", "state")
	}
	if dir, err := os.UserCacheDir(); err == nil && dir != "" {
		return dir
	}
	return "."
}
