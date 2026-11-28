---
title: Build Your Own Binary
description: Compose a custom poo binary with server.New, wire.Serve, wire.Factory, profiles, executors, and a compile-time closure gate.
---

# Build Your Own Binary

Poo is a library first. A consumer composes only the packages it wants into its own binary—selecting the tools, executors, profiles, and providers it links. Anything the binary omits is not reachable at runtime because it is not compiled into the executable.

`cmd/poo` is one such composition. `wire/conformance/testdata/readonly` is the smallest: it serves the `search` profile, `read`, `grep`, and the OpenAI transport, with no shell and no tool capable of mutating state.

## Wiring `server.New` and `wire.Serve`

`server.New` takes loaded settings, linked tools, executors, and profiles, and returns a `*server.Server` that implements `wire.Factory`. Pass it to `wire.Serve` to expose the JSON-RPC 2.0 protocol over stdio:

```go
settings, err := config.Load(*settingsPath)
if err != nil {
	return err
}
srv, err := server.New(server.Options{
	Settings:       settings,
	WorkingDir:     dir,
	DefaultProfile: search.Name,
	Tools: map[string]config.ToolFactory{
		"read": func(config.ToolOptions) poo.Tool { return read.New() },
		"grep": func(config.ToolOptions) poo.Tool { return grep.New() },
	},
	Profiles: []profile.Profile{search.Profile()},
	Build:    poo.BuildInfo{Name: "poo-readonly", Version: version},
})
if err != nil {
	return err
}
return wire.Serve(wire.Options{
	In:      os.Stdin,
	Out:     os.Stdout,
	Factory: srv,
	Build: protocol.Build{
		Name:       "poo-readonly",
		Version:    version,
		PooVersion: version,
	},
})
```

### `wire.Factory`

`wire.Factory` is the seam between the wire transport (`poo/wire`) and your binary's runtime configuration. The wire server never reads settings or constructs sessions directly; it delegates to the factory:

- `Profiles()`, `DefaultProfile()`, `Capabilities()`, `Roles()`: reported during the `poo/initialize` handshake.
- `Create(...)` and `Load(...)`: construct or resume a `*poo.Session` for a client request, receiving a `wire.FactoryEnv` with the server's `Observer` and permission `Client`.
- `Configure(...)`: applies between-run session updates (model, reasoning, prompt variant, etc.).
- `Models(...)` and `Settings(...)`: serve `models/list` and `settings/get`.

`server.New` implements `wire.Factory` on top of `config.Build`, caching runtimes per profile/model combination while keeping each session's store, prompt overrides, and permission asks isolated.

## Profiles, executors, and session options

### Profiles and tools

`server.Options.Profiles` lists the profiles the binary offers, and `server.Options.Tools` maps tool names to `config.ToolFactory` constructors. Each map key must match the name returned by the tool's `Spec()`. A profile, permission rule, or manifest that references an unlinked tool fails validation with a typed error.

Use `server.Options.SessionTools` to mutate each runtime the factory builds before creating a session. For example, it can add `finish` for structured output when settings opt into a schema no profile lists.

### Executors

Register only the command executors your binary links using `exec.NewRegistry`:

```go
registry := exec.NewRegistry()
registry.Register("local", func(exec.Options) (exec.Executor, error) {
	return bash.NewLocal(), nil
})
```

Pass `registry` in `server.Options.Executors`. An executor is resolved only when a profile offers `bash` (or runs feeds or CLI registry commands). A read-only binary passes `nil` and cannot spawn commands. If settings request an executor name that is not registered, `config.Build` returns `exec.ErrUnknownExecutor` rather than falling back to another executor.

### Overrides and manifest paths

`server.Options.Overrides` carries CLI flag overrides (such as `FeedsPaths`, `CLIRegistryPaths`, and `MCPPaths`). `server.New` automatically merges environment overrides (`POO_FEEDS_PATH`, `POO_CLI_REGISTRY_PATH`, `POO_MCP_PATH`, `POO_SKILLS_PATH`) via `config.EnvOverrides`. A binary that calls `config.Build` directly must call `config.EnvOverrides` itself if it wants environment path support.

For manifest-driven capabilities that do not require linking custom Go tools, see:

- [CLI Registry & Script Tools](/capabilities/cli-registry)
- [Feeds & Daemon](/capabilities/feeds-and-daemon)
- [MCP Servers](/capabilities/mcp-servers)
- [Manifests Reference](/reference/manifests)

### Permission askers

`permission.askers` distinguishes between an absent list and an explicit empty list:

- **Absent**: uses the binary's default chain (`[wire]` in `server.New` / `poo serve`).
- **Explicit empty list (`[]`)**: configures a chain with no askers, so any `ask` decision goes straight to the configured fallback.

A chain may name an asker the runtime cannot provide, such as `wire` in headless `poo run`. That asker is dropped with a schema-backed warning (`fairWhen` on `has_wire_client`) instead of failing the settings file.

Over the wire, the `wire` asker only sends `permission/ask` requests to a client that declared `capabilities.permission_answers: true` in `poo/initialize`. Otherwise every ask falls back and `session/create` / `session/load` include a warning explaining the missing capability.

## Inspecting linked packages

Check which tool packages a binary actually links with `go list`:

```sh
go list -deps ./cmd/my-poo | grep 'poo/tool'
```

## The closure gate

The main security and footprint guarantee of composing your own binary is what it does *not* link. Enforce this in CI with a closure test that inspects `go list -deps`:

```go
deps := listDeps(t, "./wire/conformance/testdata/readonly")
for _, excluded := range []string{
	"tool/bash",
	"tool/edit",
	"tool/cliregistry",
	"tool/codemode",
	"tool/fetch",
	"codemode/starlark",
	"codemode/quickjs",
	"exec/bwrap",
	"mcp",
	"tool/callmcp",
} {
	if slices.Contains(deps, modulePath+"/"+excluded) {
		t.Errorf("the read-only binary links %s", excluded)
	}
}
```

`internal/depcheck/closure_test.go` implements this gate for the reference read-only binary. Always assert both **exclusions** (forbidden packages and third-party SDKs are absent) and **inclusions** (intended packages are present). Otherwise, a broken target that imports nothing could vacuously pass the exclusion check.
