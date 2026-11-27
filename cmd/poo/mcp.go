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
--project writes the project's .poo directory. A name the session's own
resolution already defines is refused naming the file that holds it, so
an edit never silently replaces one. remove searches the scope's
directory only: a server a settings path loads is removed in the file
the listing names.
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
them. The manifest's template references (${NAME}) are written as the
words give them; the load validates them.`,
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
// default directory when the profile's word keeps it on, then the
// settings' paths, then the paths the flags and the environment add. A
// server poo does not start is a warning on the diagnostics, the way
// the session reports it.
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
	paths, err := theSessionPaths(settings, workingDir, o)
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
		entry.Command = server.Command.Source()
		for _, arg := range server.Args {
			entry.Args = append(entry.Args, arg.Source())
		}
		for _, key := range sortedEnvKeys(server.Env) {
			entry.Env[key] = server.Env[key].Source()
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

// sortedEnvKeys returns an environment's keys, sorted.
func sortedEnvKeys(env map[string]manifest.Template) []string {
	keys := make([]string, 0, len(env))
	for key := range env {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// theSessionPaths resolves the manifest paths a session in this
// directory loads: the settings' own, the defaults the profile's word
// keeps on, and the paths the flags and the environment add. The
// profile's name resolves the way a session's own does — the flag, then
// the settings' profile key, then the binary's default — and the word
// comes from the profile resolution a build runs, so what list shows is
// what a session starts.
func theSessionPaths(settings *config.Settings, workingDir string, o mcpOptions) ([]string, error) {
	extras := config.EnvOverrides(config.Overrides{MCPPaths: o.mcpPaths}).MCPPaths
	word, err := config.ProfileMCPDefaults(settings, config.BuildOptions{
		Tools:     linkedTools(),
		Profiles:  linkedProfiles(),
		Executors: executorRegistry(),
	}, theProfileName(settings, o.profile))
	if err != nil {
		return nil, err
	}
	paths, _, err := config.MCPManifestPaths(settings, workingDir, extras, config.MCPDefaultsOn(settings.MCP.Defaults, word))
	return paths, err
}

// theProfileName resolves the profile the session runs under: the
// flag's name, then the settings'. The build's own resolution falls on
// to the binary's default and the first linked profile, but a name only
// the binary links is not one the settings resolution carries, and it
// holds no mcp_defaults of its own — so the settings' word stands, the
// word this resolution answers with.
func theProfileName(settings *config.Settings, fromFlag string) string {
	if fromFlag != "" {
		return fromFlag
	}
	return settings.Profile
}

// handleMCPAdd executes `poo mcp add`.
//
// The entry goes into the scope's manifest file: poo's own mcp
// directory, or the project's .poo directory. The name is refused when
// the session's own resolution already defines it, naming the file —
// a duplicate the load would fail every session on. The edit is written
// to a scratch file that the same load a session runs validates before
// it takes the target's place, so a refused add leaves the manifest as
// it found it.
func handleMCPAdd(inv clicontract.Invocation, stdout, stderr io.Writer) exit {
	var o mcpOptions
	o.bind(inv)

	name := inv.Args[0]
	words := inv.Args[1:]
	if len(words) == 0 {
		fmt.Fprintln(stderr, "poo mcp add: the command and its arguments follow the name, after --")
		return exit{usageExitCode}
	}
	scope, target, err := theScope(o)
	if err != nil {
		fmt.Fprintf(stderr, "poo mcp add: %v\n", err)
		return exit{usageExitCode}
	}
	settings, err := o.settings("")
	if err != nil {
		fmt.Fprintf(stderr, "poo mcp add: %v\n", err)
		return exit{usageExitCode}
	}
	workingDir, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(stderr, "poo mcp add: %v\n", err)
		return exit{operationalExitCode}
	}
	paths, err := theSessionPaths(settings, workingDir, o)
	if err != nil {
		fmt.Fprintf(stderr, "poo mcp add: %v\n", err)
		return exit{usageExitCode}
	}
	if file, defined, err := definedIn(paths, target, name); err != nil {
		fmt.Fprintf(stderr, "poo mcp add: %v\n", err)
		return exit{usageExitCode}
	} else if defined {
		fmt.Fprintf(stderr, "poo mcp add: the mcp server %q is already defined in %s; remove it first\n", name, file)
		return exit{usageExitCode}
	}

	entry := map[string]any{"command": words[0]}
	if len(words) > 1 {
		entry["args"] = words[1:]
	}
	env, err := theEnv(o.env)
	if err != nil {
		fmt.Fprintf(stderr, "poo mcp add: %v\n", err)
		return exit{usageExitCode}
	}
	if len(env) > 0 {
		entry["env"] = env
	}
	if err := writeServer(target, name, entry, paths); err != nil {
		fmt.Fprintf(stderr, "poo mcp add: %v\n", err)
		return exit{operationalExitCode}
	}
	fmt.Fprintf(stdout, "wrote the mcp server %q to %s\n", name, target)
	if scope.project {
		if hint := theProjectHint(target, o); hint != "" {
			fmt.Fprintln(stdout, hint)
		}
	}
	return exit{}
}

// handleMCPRemove executes `poo mcp remove`.
//
// The removal edits the file the session's resolution read the entry
// from, found through the set's own source of each server. The edit is
// written to a scratch file that the same load a session runs validates
// before it takes the target's place; a load that fails leaves the
// manifest as it was.
func handleMCPRemove(inv clicontract.Invocation, stdout, stderr io.Writer) exit {
	var o mcpOptions
	o.bind(inv)

	name := inv.Args[0]
	scope, target, err := theScope(o)
	if err != nil {
		fmt.Fprintf(stderr, "poo mcp remove: %v\n", err)
		return exit{usageExitCode}
	}
	settings, err := o.settings("")
	if err != nil {
		fmt.Fprintf(stderr, "poo mcp remove: %v\n", err)
		return exit{usageExitCode}
	}
	workingDir, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(stderr, "poo mcp remove: %v\n", err)
		return exit{operationalExitCode}
	}
	paths, err := theSessionPaths(settings, workingDir, o)
	if err != nil {
		fmt.Fprintf(stderr, "poo mcp remove: %v\n", err)
		return exit{usageExitCode}
	}
	set, err := manifest.Load(unionPaths(paths, target)...)
	if err != nil {
		fmt.Fprintf(stderr, "poo mcp remove: %v\n", err)
		return exit{usageExitCode}
	}
	if _, ok := set.Lookup(name); !ok {
		fmt.Fprintf(stderr, "poo mcp remove: the mcp server %q is in none of the manifests %s loads\n", name, scope.dir)
		return exit{usageExitCode}
	}
	file, _ := set.Source(name)
	if filepath.Dir(file) != scope.dir {
		fmt.Fprintf(stderr, "poo mcp remove: the mcp server %q is defined in %s, outside the %s directory the command edits\n", name, file, scope.dir)
		return exit{usageExitCode}
	}
	if err := removeServer(file, name, paths); err != nil {
		fmt.Fprintf(stderr, "poo mcp remove: %v\n", err)
		return exit{operationalExitCode}
	}
	fmt.Fprintf(stdout, "removed the mcp server %q from %s\n", name, file)
	return exit{}
}

// mcpScope is the manifest directory an edit writes: poo's own, or the
// project's.
type mcpScope struct {
	// dir is the manifest directory the scope names.
	dir string
	// project says whether the scope is the project's, for the hint a
	// project add prints.
	project bool
}

// theScope resolves the caller's choice of scope: exactly one of
// --global and --project, so a write lands where the caller chose and
// nowhere else. The scope's directory is created: a scope with no
// manifest directory yet is where the first write goes.
func theScope(o mcpOptions) (mcpScope, string, error) {
	switch {
	case o.global && o.project:
		return mcpScope{}, "", fmt.Errorf("choose one scope: --global or --project")
	case o.global:
		dir, err := config.MCPManifestDir()
		if err != nil {
			return mcpScope{}, "", err
		}
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return mcpScope{}, "", err
		}
		return mcpScope{dir: dir}, filepath.Join(dir, "mcp.json"), nil
	case o.project:
		workingDir, err := os.Getwd()
		if err != nil {
			return mcpScope{}, "", err
		}
		dir, err := config.ProjectDir(workingDir)
		if err != nil {
			return mcpScope{}, "", err
		}
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return mcpScope{}, "", err
		}
		return mcpScope{dir: dir, project: true}, filepath.Join(dir, "mcp.json"), nil
	default:
		return mcpScope{}, "", fmt.Errorf("choose a scope: --global writes poo's own mcp directory, --project writes the project's .poo directory")
	}
}

// definedIn returns the file of the session's manifest resolution that
// defines the name, and whether one does. A load failure comes back as
// an error: a resolution that does not load is a session that does not
// start, and an edit on top of it would report against nothing.
func definedIn(paths []string, target, name string) (string, bool, error) {
	set, err := manifest.Load(unionPaths(paths, target)...)
	if err != nil {
		return "", false, err
	}
	if _, ok := set.Lookup(name); !ok {
		return "", false, nil
	}
	file, _ := set.Source(name)
	return file, true, nil
}

// unionPaths joins the session's manifest resolution with a file's own
// directory when the resolution does not hold it: an add that targets a
// directory no settings path names still refuses a name that file
// already holds.
func unionPaths(paths []string, target string) []string {
	dir := filepath.Dir(target)
	for _, path := range paths {
		if path == dir || path == target {
			return paths
		}
	}
	return append(append([]string{}, paths...), dir)
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

// theProjectHint tells an add that wrote a project manifest whether the
// project's settings load it. The project's settings name the manifests
// their sessions read, so a file they do not name is one no session
// reads, and the hint gives the exact path to name. The project's own
// settings file is the only one the hint reads: the user's settings are
// not the project's, whatever --settings names.
func theProjectHint(target string, o mcpOptions) string {
	workingDir, err := os.Getwd()
	if err != nil {
		return ""
	}
	path, ok := config.ProjectSettingsPath(workingDir)
	if !ok {
		return fmt.Sprintf("no project settings load this manifest yet: name %q in the mcp.paths of a settings file in the project's .poo directory", target)
	}
	settings, err := config.Load(path)
	if err != nil {
		return fmt.Sprintf("the project settings at %s do not load: %v", path, err)
	}
	word, err := config.ProfileMCPDefaults(settings, config.BuildOptions{
		Tools:     linkedTools(),
		Profiles:  linkedProfiles(),
		Executors: executorRegistry(),
	}, theProfileName(settings, o.profile))
	if err != nil {
		return fmt.Sprintf("the project settings' profiles do not resolve: %v", err)
	}
	paths, _, err := config.MCPManifestPaths(settings, workingDir, nil, config.MCPDefaultsOn(settings.MCP.Defaults, word))
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
// preserving the file's other content. The edit lands through a scratch
// file the load a session runs validates — the file's own content, and
// the session's whole resolution once the target takes its place — and
// a validation that fails puts the original bytes back.
func writeServer(target, name string, entry map[string]any, sessionPaths []string) error {
	original, err := os.ReadFile(target)
	present := err == nil
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	servers := theServers(original)
	if _, clash := servers[name]; clash {
		return fmt.Errorf("the mcp server %q is already in %s", name, target)
	}
	servers[name] = entry
	return applyEdit(target, original, present, servers, sessionPaths)
}

// removeServer deletes one mcpServers entry from a manifest file,
// preserving the file's other content. The edit lands through a scratch
// file the load a session runs validates — the file's own content, and
// the session's whole resolution once the target takes its place — and
// a validation that fails puts the original bytes back.
func removeServer(target, name string, sessionPaths []string) error {
	original, err := os.ReadFile(target)
	if err != nil {
		return err
	}
	servers := theServers(original)
	if _, ok := servers[name]; !ok {
		return fmt.Errorf("the mcp server %q is not in %s", name, target)
	}
	delete(servers, name)
	return applyEdit(target, original, true, servers, sessionPaths)
}

// theServers reads the file's mcpServers object as writable entries, or
// an empty one when the file names none yet. The entries' values stay
// raw where the format allows, so a number or a repeated key another
// harness wrote is preserved the way the file holds it.
func theServers(original []byte) map[string]any {
	servers := map[string]any{}
	document := theDocument(original)
	if document == nil {
		return servers
	}
	raw, ok := document["mcpServers"]
	if !ok {
		return servers
	}
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.UseNumber()
	if err := decoder.Decode(&servers); err != nil {
		return map[string]any{}
	}
	return servers
}

// applyEdit lands an edited mcpServers object in the target manifest
// file, preserving the file's other top-level content. The new body is
// written to a scratch file beside it, the file's own content is
// validated by the load a session runs, and only then does the scratch
// take the target's place — after which the session's whole resolution
// is validated again, and a load that fails puts the original bytes
// back.
func applyEdit(target string, original []byte, present bool, servers map[string]any, sessionPaths []string) error {
	document := theDocument(original)
	if present && document == nil {
		return fmt.Errorf("%s does not parse as JSON, and the edit does not replace a file it cannot read", target)
	}
	out := normalizeDocument(document)
	out["mcpServers"] = servers
	body, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return err
	}
	body = append(body, '\n')
	scratch, err := os.CreateTemp(filepath.Dir(target), ".poo-edit-*.json")
	if err != nil {
		return err
	}
	scratchName := scratch.Name()
	defer os.Remove(scratchName)
	if info, err := os.Stat(target); err == nil {
		// The rename gives the manifest the scratch's mode, so the
		// scratch takes the mode the file had: a shared manifest stays
		// readable to the harnesses that read it.
		if err := os.Chmod(scratchName, info.Mode().Perm()); err != nil {
			scratch.Close()
			return err
		}
	}
	if _, err := scratch.Write(body); err != nil {
		scratch.Close()
		return err
	}
	if err := scratch.Close(); err != nil {
		return err
	}
	if _, err := manifest.Load(scratchName); err != nil {
		return err
	}
	if err := os.Rename(scratchName, target); err != nil {
		return err
	}
	if _, err := manifest.Load(unionPaths(sessionPaths, target)...); err != nil {
		if present {
			_ = os.WriteFile(target, original, 0o644)
		} else {
			_ = os.Remove(target)
		}
		return err
	}
	return nil
}

// theDocument reads a manifest file's top level as a JSON object whose
// values stay raw, so an edit preserves them byte for byte where the
// format allows. A file that is not there reads as an empty one; a file
// that does not parse reads as nil, and the caller refuses it.
func theDocument(original []byte) map[string]json.RawMessage {
	document := map[string]json.RawMessage{}
	if len(original) == 0 {
		return document
	}
	decoder := json.NewDecoder(strings.NewReader(string(original)))
	decoder.UseNumber()
	if err := decoder.Decode(&document); err != nil {
		return nil
	}
	return document
}

// normalizeDocument turns the raw top level into values the JSON
// encoder writes out: numbers as the file wrote them (UseNumber keeps
// the digits), strings and objects decoded the same way.
func normalizeDocument(document map[string]json.RawMessage) map[string]any {
	out := make(map[string]any, len(document))
	for key, raw := range document {
		var value any
		decoder := json.NewDecoder(strings.NewReader(string(raw)))
		decoder.UseNumber()
		if err := decoder.Decode(&value); err != nil {
			out[key] = raw
			continue
		}
		out[key] = value
	}
	return out
}
