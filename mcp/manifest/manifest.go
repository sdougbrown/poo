// Package manifest loads mcp.json manifests.
//
// A manifest declares MCP servers in the shape Claude Code and other
// harnesses read: a top-level `mcpServers` object keyed by server name.
// Each entry names the command a server runs, its arguments, and the
// environment it runs in. poo starts the stdio servers and nothing else;
// an entry that names a remote transport is left out with a warning.
//
// poo's own additions live in a top-level `poo` key beside `mcpServers`,
// never inside an entry, so the file stays one the other harnesses read
// unchanged: a harness ignores a top-level key it does not know, which
// Claude Code 2.1.332 does for a file that carries `poo` beside
// `mcpServers` and still lists the servers (2026-11-26). That is the
// format contract this package keeps. An entry's fields poo does not read
// are ignored for the same reason — other harnesses add their own there —
// while the `poo` block is poo's alone, so an unknown field in it, or a
// top-level key that is neither, fails the load.
//
// The package is JSON only, and it knows nothing of a transport or a
// process. It hands the config layer the servers a session may start,
// with the templates their launch resolves and the resource feeds their
// `poo` block declares.
package manifest

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	// The cli-registry manifest scanner, which discovers the files and
	// checks a document for duplicate keys. It is aliased: this package is
	// a manifest too, and its own name would read as its own.
	climanifest "github.com/sdougbrown/cli-registry/manifest"

	"github.com/sdougbrown/poo/feed"
)

// serverNamePattern is the shape of a server's name: the entry's key, and
// the name a profile selects a server by.
var serverNamePattern = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

// remoteTransports are the entry types poo does not start. A session
// reaches those servers through the harness that owns them.
var remoteTransports = map[string]bool{"http": true, "sse": true, "streamable-http": true}

// Server is one validated `mcpServers` entry: the process a session
// starts, and the values that launch it.
type Server struct {
	// Name is the name the server answers to: the entry's key.
	Name string
	// Command is the process the server runs, unresolved.
	Command Template
	// Args are the command's arguments, unresolved, in the order the
	// entry gives them.
	Args []Template
	// Env are the environment entries the entry declares, by name,
	// unresolved. They add to the scrubbed host environment.
	Env map[string]Template
}

// ResourceFeedDecl is one resource feed a manifest's `poo` block
// declares: a resource a session may subscribe to, and what the feed's
// inputs do when the session is idle.
type ResourceFeedDecl struct {
	// Name is the feed's name. It is unique across the set, and a feed
	// manifest's feed may not hold the same one.
	Name string
	// Resource is the URI the feed reads.
	Resource string
	// Description is what the resource carries, as the manifest says.
	Description string
	// OnIdle is the feed's idle policy, or the empty string to inherit
	// the session's.
	OnIdle string
	// Filter is the delivery screen the feed's records pass, and nil for
	// a feed that delivers what it reads. It has a feed manifest's filter
	// shape, and the feed package validates it.
	Filter *feed.Filter
}

// Set is the set of servers the manifests declare, with the resource
// feeds they declare for them.
//
// The set is immutable after Load, so every reader may read it
// concurrently.
type Set struct {
	// servers are the servers poo starts, in name order.
	servers []Server
	// feeds are the resource feeds, by server name, each list in name
	// order.
	feeds map[string][]ResourceFeedDecl
	// warnings are the entries poo does not start, sorted by file and
	// then by server name.
	warnings []string
}

// Load reads the mcp.json manifests reachable from paths and validates
// them.
//
// A path is a manifest file, a directory of manifests, or several of
// them. A directory contributes the `.json` files it holds and skips the
// rest: mcp.json is JSON, and a file in another format belongs to another
// tool. A path that names a file is the operator's own word, so one that
// is not `.json` fails the load.
//
// The servers poo starts, their templates, and the resource feeds their
// `poo` blocks declare come back together. A server name two files define
// fails the load naming both files: a selected name has to mean one
// server. A resource feed name two servers declare fails it too, and so do
// two feeds on one server's resource: a drained read has one reader.
func Load(paths ...string) (*Set, error) {
	files, err := discover(paths)
	if err != nil {
		return nil, fmt.Errorf("mcp manifests: %w", err)
	}
	set := &Set{feeds: map[string][]ResourceFeedDecl{}}
	defined := make(map[string]string, len(files))
	feeds := make(map[string]string, len(files))
	for _, file := range files {
		content, err := readFile(file)
		if err != nil {
			return nil, err
		}
		for _, server := range content.servers {
			if first, dup := defined[server.Name]; dup {
				return nil, fmt.Errorf("the mcp server %q is defined in both %s and %s", server.Name, first, file)
			}
			defined[server.Name] = file
			set.servers = append(set.servers, server)
		}
		set.warnings = append(set.warnings, content.warnings...)
		// A server's resource feeds are in the file that defines it, so a
		// name two of them declare can only mean two servers, here or in
		// another file: the namespace is the set's, and the load checks it
		// here.
		for _, server := range sortedKeys(content.feeds) {
			for _, decl := range content.feeds[server] {
				if first, dup := feeds[decl.Name]; dup {
					return nil, fmt.Errorf("the resource feed %q is declared by both %s and the server %q in %s", decl.Name, first, server, file)
				}
				feeds[decl.Name] = fmt.Sprintf("the server %q in %s", server, file)
				set.feeds[server] = append(set.feeds[server], decl)
			}
		}
	}
	sort.Slice(set.servers, func(i, j int) bool { return set.servers[i].Name < set.servers[j].Name })
	return set, nil
}

// Names returns the servers poo starts, in name order.
func (s *Set) Names() []string {
	if s == nil {
		return nil
	}
	names := make([]string, len(s.servers))
	for i, server := range s.servers {
		names[i] = server.Name
	}
	return names
}

// Lookup returns the named server, and whether the set holds it.
//
// The server and its values are copies.
func (s *Set) Lookup(name string) (Server, bool) {
	if s == nil {
		return Server{}, false
	}
	for _, server := range s.servers {
		if server.Name != name {
			continue
		}
		server.Args = append([]Template(nil), server.Args...)
		env := make(map[string]Template, len(server.Env))
		for key, value := range server.Env {
			env[key] = value
		}
		server.Env = env
		return server, true
	}
	return Server{}, false
}

// Warnings returns what the load left out, one line per server, sorted by
// file and then by server name.
func (s *Set) Warnings() []string {
	if s == nil {
		return nil
	}
	return append([]string(nil), s.warnings...)
}

// Feeds returns the resource feeds the manifests declare for the named
// server, in name order. A server whose manifests declare none has none.
//
// The declarations are copies, including each filter's command.
func (s *Set) Feeds(server string) []ResourceFeedDecl {
	if s == nil || len(s.feeds[server]) == 0 {
		return nil
	}
	decls := s.feeds[server]
	out := make([]ResourceFeedDecl, len(decls))
	for i, decl := range decls {
		if decl.Filter != nil {
			filter := *decl.Filter
			filter.Command = append([]string(nil), decl.Filter.Command...)
			decl.Filter = &filter
		}
		out[i] = decl
	}
	return out
}

// loaded is one manifest file's valid contents.
type loaded struct {
	// servers are the entries poo starts, in name order.
	servers []Server
	// feeds are the resource feeds the file's `poo` block declares, by
	// server, each list in name order.
	feeds map[string][]ResourceFeedDecl
	// warnings are the entries the file declares and poo does not start,
	// in name order.
	warnings []string
}

// envelope is the manifest's top-level shape, decoded with unknown fields
// rejected and duplicate keys detected: the file's only top-level keys are
// the servers every harness reads and poo's own block, so a key that is
// neither is a misspelling the load has to name.
type envelope struct {
	MCPServers map[string]json.RawMessage `json:"mcpServers"`
	Poo        *pooBlock                  `json:"poo"`
}

// pooBlock is the top-level `poo` key: poo's own additions to the file.
type pooBlock struct {
	Servers map[string]pooServer `json:"servers"`
}

// pooServer is what the `poo` block says about one server.
type pooServer struct {
	Feeds map[string]resourceFeed `json:"feeds"`
}

// resourceFeed is one resource feed the `poo` block declares.
type resourceFeed struct {
	Resource    string       `json:"resource"`
	Description string       `json:"description"`
	OnIdle      string       `json:"on_idle"`
	Filter      *feed.Filter `json:"filter"`
}

// entry is one `mcpServers` entry, as a harness writes it. The fields poo
// does not read are ignored rather than rejected: another harness adds its
// own inside an entry, and a file poo shares with one still loads.
type entry struct {
	Command string            `json:"command"`
	Args    []string          `json:"args"`
	Env     map[string]string `json:"env"`
	Type    string            `json:"type"`
	URL     string            `json:"url"`
}

// readFile reads one manifest and returns its valid contents.
func readFile(path string) (loaded, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return loaded{}, fmt.Errorf("mcp manifest %s: %w", path, err)
	}
	var env envelope
	// Decode rejects unknown fields and duplicate keys, and names the file
	// in its failures. The `poo` block is decoded with it, so a field poo
	// does not know fails the load wherever poo's own key holds it.
	if err := climanifest.Decode(path, data, &env); err != nil {
		return loaded{}, fmt.Errorf("mcp manifest: %w", err)
	}
	var content loaded
	skipped := map[string]string{}
	for _, name := range sortedKeys(env.MCPServers) {
		server, transport, err := readServer(path, name, env.MCPServers[name])
		if err != nil {
			return loaded{}, err
		}
		if transport != "" {
			content.warnings = append(content.warnings, fmt.Sprintf("server %s uses transport %s, which poo does not start", name, transport))
			skipped[name] = transport
			continue
		}
		content.servers = append(content.servers, server)
	}
	content.feeds, err = readPoo(path, env.Poo, env.MCPServers, skipped)
	if err != nil {
		return loaded{}, err
	}
	return content, nil
}

// readServer validates one `mcpServers` entry.
//
// It returns the transport the entry names when poo does not start it, and
// the empty string for an entry poo starts.
func readServer(path, name string, raw json.RawMessage) (Server, string, error) {
	if !serverNamePattern.MatchString(name) {
		return Server{}, "", fmt.Errorf("mcp manifest %s: the server name %q is not a valid server name", path, name)
	}
	var e entry
	if err := json.Unmarshal(raw, &e); err != nil {
		return Server{}, "", fmt.Errorf("mcp manifest %s: the server %q: %w", path, name, err)
	}
	transport := e.Type
	switch {
	case remoteTransports[transport]:
		return Server{}, transport, nil
	case transport != "" && transport != "stdio":
		return Server{}, "", fmt.Errorf("mcp manifest %s: the server %q has the type %q, and poo starts a stdio server only", path, name, transport)
	case transport == "" && e.URL != "":
		// A url without a type is the http transport every harness reads.
		return Server{}, "http", nil
	}
	if e.Command == "" {
		return Server{}, "", fmt.Errorf("mcp manifest %s: the server %q has no command", path, name)
	}
	command, err := ParseTemplate(e.Command)
	if err != nil {
		return Server{}, "", fmt.Errorf("mcp manifest %s: the server %q: the command %w", path, name, err)
	}
	server := Server{Name: name, Command: command, Args: make([]Template, len(e.Args)), Env: make(map[string]Template, len(e.Env))}
	for i, arg := range e.Args {
		server.Args[i], err = ParseTemplate(arg)
		if err != nil {
			return Server{}, "", fmt.Errorf("mcp manifest %s: the server %q: the argument %q %w", path, name, arg, err)
		}
	}
	for _, key := range sortedKeys(e.Env) {
		server.Env[key], err = ParseTemplate(e.Env[key])
		if err != nil {
			return Server{}, "", fmt.Errorf("mcp manifest %s: the server %q: the environment value %q %w", path, name, key, err)
		}
	}
	return server, "", nil
}

// readPoo validates the manifest's `poo` block and returns the resource
// feeds it declares, by server, each list in name order.
//
// The block adds to the file's own servers and to nothing else: a key that
// names no entry above it, or one whose transport poo does not start, is a
// declaration for a server no session can reach, so it fails the load.
func readPoo(path string, block *pooBlock, entries map[string]json.RawMessage, skipped map[string]string) (map[string][]ResourceFeedDecl, error) {
	if block == nil || len(block.Servers) == 0 {
		return nil, nil
	}
	out := make(map[string][]ResourceFeedDecl, len(block.Servers))
	feeds := make(map[string]string, len(block.Servers))
	for _, server := range sortedKeys(block.Servers) {
		if _, ok := entries[server]; !ok {
			return nil, fmt.Errorf("mcp manifest %s: poo.servers names the server %q, and mcpServers does not define it", path, server)
		}
		if transport, ok := skipped[server]; ok {
			return nil, fmt.Errorf("mcp manifest %s: poo.servers declares feeds for the server %q, and its transport %s is one poo does not start", path, server, transport)
		}
		declared := block.Servers[server].Feeds
		resources := make(map[string]string, len(declared))
		decls := make([]ResourceFeedDecl, 0, len(declared))
		for _, name := range sortedKeys(declared) {
			decl, err := readResourceFeed(path, server, name, declared[name])
			if err != nil {
				return nil, err
			}
			if first, dup := feeds[name]; dup {
				return nil, fmt.Errorf("mcp manifest %s: the resource feed %q is declared by both the servers %q and %q", path, name, first, server)
			}
			if first, dup := resources[decl.Resource]; dup {
				return nil, fmt.Errorf("mcp manifest %s: the server %q subscribes the resource %q with both the feeds %q and %q, and a drained read has one reader", path, server, decl.Resource, first, name)
			}
			feeds[name] = server
			resources[decl.Resource] = name
			decls = append(decls, decl)
		}
		out[server] = decls
	}
	return out, nil
}

// readResourceFeed validates one resource feed the `poo` block declares.
func readResourceFeed(path, server, name string, declared resourceFeed) (ResourceFeedDecl, error) {
	if err := feed.ValidName(name); err != nil {
		return ResourceFeedDecl{}, fmt.Errorf("mcp manifest %s: the server %q: %w", path, server, err)
	}
	if declared.Resource == "" {
		return ResourceFeedDecl{}, fmt.Errorf("mcp manifest %s: the server %q: the resource feed %q names no resource", path, server, name)
	}
	switch declared.OnIdle {
	case "", feed.OnIdleWake, feed.OnIdleHold:
	default:
		return ResourceFeedDecl{}, fmt.Errorf("mcp manifest %s: the server %q: the resource feed %q has an on_idle %q, want %q or %q", path, server, name, declared.OnIdle, feed.OnIdleWake, feed.OnIdleHold)
	}
	if err := feed.ValidateFilter(path, name, declared.Filter); err != nil {
		return ResourceFeedDecl{}, err
	}
	return ResourceFeedDecl{
		Name:        name,
		Resource:    declared.Resource,
		Description: declared.Description,
		OnIdle:      declared.OnIdle,
		Filter:      declared.Filter,
	}, nil
}

// discover returns the JSON manifest files the paths name, in sorted
// order.
//
// A directory contributes the `.json` files it holds, and every other
// manifest format is skipped there: a file beside an mcp.json belongs to
// another tool, and a scan that failed on it would make poo refuse a
// directory it shares. A path that names a file is the operator's own
// word, so one that is not `.json` fails: the cli-registry scanner takes a
// named file whatever its extension, so the check is poo's.
func discover(paths []string) ([]string, error) {
	seen := make(map[string]bool, len(paths))
	var files []string
	for _, path := range paths {
		info, err := os.Stat(path)
		if err != nil {
			return nil, fmt.Errorf("discover %s: %w", path, err)
		}
		if !info.IsDir() && !isJSON(path) {
			return nil, fmt.Errorf("%s is not a .json file, and an mcp manifest is JSON", path)
		}
		found, err := climanifest.Discover(path)
		if err != nil {
			return nil, err
		}
		for _, file := range found {
			if seen[file] || !isJSON(file) {
				continue
			}
			seen[file] = true
			files = append(files, file)
		}
	}
	sort.Strings(files)
	return files, nil
}

// isJSON reports whether the path names a .json file.
func isJSON(path string) bool {
	return strings.EqualFold(filepath.Ext(path), ".json")
}

// sortedKeys returns the map's keys, sorted.
func sortedKeys[V any](values map[string]V) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
