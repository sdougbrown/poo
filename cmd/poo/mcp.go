package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"text/tabwriter"

	clicontract "github.com/sdougbrown/cli-contract"

	"github.com/sdougbrown/poo/config"
	"github.com/sdougbrown/poo/mcp/manifest"
)

// mcpLong is the help body of `poo mcp`.
const mcpLong = `The mcp.json servers the settings name. list reads them the way a session
loads them; add and remove edit one file.

add and remove write a file, and which file is the caller's choice:
--global writes poo's own mcp directory (the one the defaults load), and
--project writes the project's .poo directory. A name the scope already
defines is refused naming the file that holds it, so an edit never
silently replaces one.
`

// mcpOptions are the flags the mcp commands share.
type mcpOptions struct {
	sharedFlags
	// json prints the listing as JSON.
	json bool
	// global and project are the scope an edit writes. Exactly one is
	// the caller's choice.
	global  bool
	project bool
	// env are the environment entries an add writes, as K=V words.
	env []string
}

// bind reads the mcp command's flags out of a parsed invocation.
func (o *mcpOptions) bind(inv clicontract.Invocation) {
	o.sharedFlags.bind(inv)
	o.json = inv.Bools["json"]
	o.global = inv.Bools["global"]
	o.project = inv.Bools["project"]
	o.env = inv.Repeated["env"]
}

// mcpListCommand declares `poo mcp list`.
func mcpListCommand(stdout io.Writer) *clicontract.Command {
	return &clicontract.Command{
		Name:  "list",
		Short: "List the mcp.json servers the settings name",
		Flags: []clicontract.Flag{
			{Name: "json", Kind: clicontract.Bool, Usage: "Print the servers as JSON"},
		},
		Handle: func(_ context.Context, inv clicontract.Invocation, cmdStderr io.Writer) (any, error) {
			return handleMCPList(inv, stdout, cmdStderr), nil
		},
	}
}

// mcpAddCommand declares `poo mcp add`.
func mcpAddCommand(stdout io.Writer) *clicontract.Command {
	return &clicontract.Command{
		Name:  "add",
		Short: "Write one mcp.json server into the scope's manifest",
		Long: `poo mcp add <name> [--global | --project] [--env K=V]... -- command [args...]

Writes one mcpServers entry: the name is the key, and the words after --
are the command and its arguments, stored the way the manifest reads
them. The manifest's template references (${host:...}) are written as
the words give them; the load validates them.`,
		Positionals: []clicontract.Positional{{Name: "name", Required: true}, {Name: "command", Variadic: true}},
		Flags: []clicontract.Flag{
			{Name: "global", Kind: clicontract.Bool, Usage: "Write poo's own mcp directory"},
			{Name: "project", Kind: clicontract.Bool, Usage: "Write the project's .poo directory"},
			{Name: "env", Kind: clicontract.Repeat, Usage: "Environment entry as K=V (repeatable)"},
		},
		Handle: func(_ context.Context, inv clicontract.Invocation, cmdStderr io.Writer) (any, error) {
			return handleMCPAdd(inv, stdout, cmdStderr), nil
		},
	}
}

// mcpRemoveCommand declares `poo mcp remove`.
func mcpRemoveCommand(stdout io.Writer) *clicontract.Command {
	return &clicontract.Command{
		Name:  "remove",
		Short: "Delete one mcp.json server from the scope's manifests",
		Long: `poo mcp remove <name> [--global | --project]

Deletes the entry the scope's manifest files define under the name. The
file the entry was read from is the one edited, and the file's other
entries stay as they are.`,
		Positionals: []clicontract.Positional{{Name: "name", Required: true}},
		Flags: []clicontract.Flag{
			{Name: "global", Kind: clicontract.Bool, Usage: "Search poo's own mcp directory"},
			{Name: "project", Kind: clicontract.Bool, Usage: "Search the project's .poo directory"},
		},
		Handle: func(_ context.Context, inv clicontract.Invocation, cmdStderr io.Writer) (any, error) {
			return handleMCPRemove(inv, stdout, cmdStderr), nil
		},
	}
}

// handleMCPList executes `poo mcp list`.
//
// The listing is the set the same resolution a session loads: the
// default directory when the settings' word keeps it on, then the
// settings' paths, then the paths the flags add. A server poo does not
// start is a warning on the diagnostics, the way the session reports
// it.
func handleMCPList(inv clicontract.Invocation, stdout, stderr io.Writer) exit {
	var o mcpOptions
	o.bind(inv)

	settings, err := o.settings("")
	if err != nil {
		fmt.Fprintf(stderr, "poo mcp list: %v\n", err)
		return exit{usageExitCode}
	}
	workingDir, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(stderr, "poo mcp list: %v\n", err)
		return exit{operationalExitCode}
	}
	paths, err := config.MCPManifestPaths(settings, workingDir, o.mcpPaths)
	if err != nil {
		fmt.Fprintf(stderr, "poo mcp list: %v\n", err)
		return exit{usageExitCode}
	}
	set, err := manifest.Load(paths...)
	if err != nil {
		fmt.Fprintf(stderr, "poo mcp list: %v\n", err)
		return exit{usageExitCode}
	}
	for _, warning := range set.Warnings() {
		fmt.Fprintf(stderr, "poo mcp list: %s\n", warning)
	}

	type listed struct {
		Name    string            `json:"name"`
		Command string            `json:"command"`
		Args    []string          `json:"args"`
		Env     map[string]string `json:"env,omitempty"`
		Source  string            `json:"source"`
	}
	servers := make([]listed, 0, len(set.Names()))
	for _, name := range set.Names() {
		server, _ := set.Lookup(name)
		source, _ := set.Source(name)
		entry := listed{Name: name, Source: source, Args: []string{}, Env: map[string]string{}}
		entry.Command = render(server.Command)
		for _, arg := range server.Args {
			entry.Args = append(entry.Args, render(arg))
		}
		for _, key := range sortedEnvKeys(server.Env) {
			entry.Env[key] = render(server.Env[key])
		}
		servers = append(servers, entry)
	}

	if o.json {
		encoded, err := json.MarshalIndent(servers, "", "  ")
		if err != nil {
			fmt.Fprintf(stderr, "poo mcp list: %v\n", err)
			return exit{operationalExitCode}
		}
		fmt.Fprintln(stdout, string(encoded))
		return exit{}
	}
	w := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "NAME\tCOMMAND\tSOURCE")
	for _, server := range servers {
		words := append([]string{server.Command}, server.Args...)
		fmt.Fprintf(w, "%s\t%s\t%s\n", server.Name, strings.Join(words, " "), server.Source)
	}
	_ = w.Flush()
	return exit{}
}

// render shows one template the way the manifest writes it: the
// listing is a reading of the file, not a resolution of one session's
// environment.
func render(template manifest.Template) string {
	return template.Source()
}

// sortedEnvKeys returns an environment's keys, sorted.
func sortedEnvKeys(env map[string]manifest.Template) []string {
	keys := make([]string, 0, len(env))
	for key := range env {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// handleMCPAdd executes `poo mcp add`.
//
// The entry goes into the scope's manifest file: poo's own mcp
// directory, or the project's .poo directory. The file the write
// produces is validated by the same load a session runs. A load that
// fails puts the file back as it was, so a refused add leaves the
// manifest as it found it.
func handleMCPAdd(inv clicontract.Invocation, stdout, stderr io.Writer) exit {
	var o mcpOptions
	o.bind(inv)

	name := inv.Args[0]
	words := inv.Args[1:]
	if len(words) == 0 {
		fmt.Fprintln(stderr, "poo mcp add: the command and its arguments follow the name, after --")
		return exit{usageExitCode}
	}
	scope, err := o.scope()
	if err != nil {
		fmt.Fprintf(stderr, "poo mcp add: %v\n", err)
		return exit{usageExitCode}
	}
	target, err := scope.target()
	if err != nil {
		fmt.Fprintf(stderr, "poo mcp add: %v\n", err)
		return exit{operationalExitCode}
	}
	if file, defined := scope.defined(name); defined {
		fmt.Fprintf(stderr, "poo mcp add: the mcp server %q is already defined in %s; remove it first\n", name, file)
		return exit{usageExitCode}
	}

	entry := map[string]any{"command": words[0]}
	if len(words) > 1 {
		entry["args"] = words[1:]
	}
	if env, err := theEnv(o.env); err != nil {
		fmt.Fprintf(stderr, "poo mcp add: %v\n", err)
		return exit{usageExitCode}
	} else if len(env) > 0 {
		entry["env"] = env
	}
	if err := writeServer(target, name, entry); err != nil {
		fmt.Fprintf(stderr, "poo mcp add: %v\n", err)
		return exit{operationalExitCode}
	}
	fmt.Fprintf(stdout, "wrote the mcp server %q to %s\n", name, target)
	if o.project {
		if hint := theProjectHint(target); hint != "" {
			fmt.Fprintln(stdout, hint)
		}
	}
	return exit{}
}

// handleMCPRemove executes `poo mcp remove`.
//
// The removal edits the file the load read the entry from, found
// through the set's own source of each server. The same load a session
// runs validates the result; a load that fails puts the file back as it
// was.
func handleMCPRemove(inv clicontract.Invocation, stdout, stderr io.Writer) exit {
	var o mcpOptions
	o.bind(inv)

	name := inv.Args[0]
	scope, err := o.scope()
	if err != nil {
		fmt.Fprintf(stderr, "poo mcp remove: %v\n", err)
		return exit{usageExitCode}
	}
	file, defined := scope.defined(name)
	if !defined {
		fmt.Fprintf(stderr, "poo mcp remove: the mcp server %q is in none of the manifests %s loads\n", name, scope.dir)
		return exit{usageExitCode}
	}
	if err := removeServer(file, name); err != nil {
		fmt.Fprintf(stderr, "poo mcp remove: %v\n", err)
		return exit{operationalExitCode}
	}
	fmt.Fprintf(stdout, "removed the mcp server %q from %s\n", name, file)
	return exit{}
}

// scope is the manifest directory an edit writes: poo's own, or the
// project's.
type mcpScope struct {
	// dir is the manifest directory the scope names.
	dir string
	// project says whether the scope is the project's, for the hint a
	// project add prints.
	project bool
}

// scope resolves the caller's choice of scope: exactly one of --global
// and --project, so a write lands where the caller chose and nowhere
// else.
func (o *mcpOptions) scope() (mcpScope, error) {
	switch {
	case o.global && o.project:
		return mcpScope{}, fmt.Errorf("choose one scope: --global or --project")
	case o.global:
		return mcpScope{dir: theGlobalDir()}, nil
	case o.project:
		workingDir, err := os.Getwd()
		if err != nil {
			return mcpScope{}, err
		}
		dir, err := config.ProjectDir(workingDir)
		if err != nil {
			return mcpScope{}, err
		}
		return mcpScope{dir: dir, project: true}, nil
	default:
		return mcpScope{}, fmt.Errorf("choose a scope: --global writes poo's own mcp directory, --project writes the project's .poo directory")
	}
}

// target is the manifest file an add writes: the scope's directory's
// own mcp.json.
func (s mcpScope) target() (string, error) {
	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		return "", err
	}
	return filepath.Join(s.dir, "mcp.json"), nil
}

// defined returns the file in the scope's directory that defines the
// name, and whether one does. The lookup is the same load a session
// runs over the scope's manifests, so a name two files define fails
// here the way it fails the session.
func (s mcpScope) defined(name string) (string, bool) {
	set, err := manifest.Load(s.dir)
	if err != nil {
		return "", false
	}
	if _, ok := set.Lookup(name); !ok {
		return "", false
	}
	file, _ := set.Source(name)
	return file, true
}

// theGlobalDir is poo's own mcp manifest directory.
func theGlobalDir() string {
	dir, err := config.UserConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "mcp")
}

// theEnv reads an add's --env words: each is one K=V entry, stored the
// way the manifest reads it.
func theEnv(words []string) (map[string]string, error) {
	env := map[string]string{}
	for _, word := range words {
		key, value, found := strings.Cut(word, "=")
		if !found || key == "" {
			return nil, fmt.Errorf("the environment entry %q is not K=V", word)
		}
		env[key] = value
	}
	return env, nil
}

// theProjectHint tells an add that wrote a project manifest whether
// the session loads it. The project's settings name the manifests their
// sessions read, so a file the settings do not name is one no session
// reads. The hint gives the exact path to name.
func theProjectHint(target string) string {
	workingDir, err := os.Getwd()
	if err != nil {
		return ""
	}
	path, err := config.FindSettings("", workingDir)
	if err != nil || path == "" {
		return fmt.Sprintf("no project settings load this manifest yet: name %q in the mcp.paths of a settings file in the project's .poo directory", target)
	}
	settings, err := config.Load(path)
	if err != nil {
		return fmt.Sprintf("the project settings at %s do not load: %v", path, err)
	}
	paths, err := config.MCPManifestPaths(settings, workingDir, nil)
	if err != nil {
		return fmt.Sprintf("the project settings' mcp.paths do not resolve: %v", err)
	}
	for _, path := range paths {
		if path == target || path == filepath.Dir(target) {
			return ""
		}
	}
	return fmt.Sprintf("the project settings at %s do not load this manifest yet: add %q to mcp.paths", path, filepath.Dir(target))
}

// writeServer writes one mcpServers entry into a manifest file,
// preserving the file's other content. The load a session runs
// validates the result; a load that fails puts the original bytes
// back.
func writeServer(target, name string, entry map[string]any) error {
	original, err := os.ReadFile(target)
	present := err == nil
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	document, err := theDocument(original, present)
	if err != nil {
		return err
	}
	servers, _ := document["mcpServers"].(map[string]any)
	if servers == nil {
		servers = map[string]any{}
	}
	if _, clash := servers[name]; clash {
		return fmt.Errorf("the mcp server %q is already in %s", name, target)
	}
	servers[name] = entry
	document["mcpServers"] = servers
	body, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		return err
	}
	body = append(body, '\n')
	if err := os.WriteFile(target, body, 0o644); err != nil {
		return err
	}
	if _, err := manifest.Load(target); err != nil {
		if present {
			_ = os.WriteFile(target, original, 0o644)
		} else {
			_ = os.Remove(target)
		}
		return err
	}
	return nil
}

// removeServer deletes one mcpServers entry from a manifest file,
// preserving the file's other content. The file the removal produces is
// validated by the load a session runs; a load that fails puts the
// original bytes back.
func removeServer(target, name string) error {
	original, err := os.ReadFile(target)
	if err != nil {
		return err
	}
	document, err := theDocument(original, true)
	if err != nil {
		return err
	}
	servers, _ := document["mcpServers"].(map[string]any)
	if _, ok := servers[name]; !ok {
		return fmt.Errorf("the mcp server %q is not in %s", name, target)
	}
	delete(servers, name)
	document["mcpServers"] = servers
	body, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		return err
	}
	body = append(body, '\n')
	if err := os.WriteFile(target, body, 0o644); err != nil {
		return err
	}
	if _, err := manifest.Load(target); err != nil {
		_ = os.WriteFile(target, original, 0o644)
		return err
	}
	return nil
}

// theDocument reads a manifest file's top level as a JSON object. A
// file that is not there starts empty, and one that is there but does
// not parse is not edited: the write preserves what the file holds, and
// a file another harness wrote stays that harness's own.
func theDocument(original []byte, present bool) (map[string]any, error) {
	document := map[string]any{}
	if !present {
		return document, nil
	}
	if err := json.Unmarshal(original, &document); err != nil {
		return nil, fmt.Errorf("the manifest does not parse as JSON, and the edit does not replace a file it cannot read: %w", err)
	}
	return document, nil
}
