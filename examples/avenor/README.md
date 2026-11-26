# Delegate to Avenor from poo

Use these manifests to delegate to child agents and receive their questions in the same poo session. The inbox can wake an idle session. This example does not require an MCP client.

## Set up

1. Put `avenor` on `PATH`. Use a build that supports per-session supervisor startup in `avenor feed broker`; the bound `spawn`, `status`, `result`, `await`, and `message` commands; and the bind-coordinate flags on `await` (avenor v0.52.0 or later).
2. Add the manifests to your existing poo settings. Replace `~/Code/poo` with your checkout path:

   ```yaml
   feeds:
     paths: [~/Code/poo/examples/avenor/feeds.json]
   cli_registry:
     paths: [~/Code/poo/examples/avenor/tools.json]
   ```

   If you already have paths under either key, append the new path to the list.
   Alternatively, copy the manifests into poo's default directories. The
   files are `feeds.json` under `$XDG_CONFIG_HOME/poo/feeds/` and
   `tools.json` under `$XDG_CONFIG_HOME/poo/cli-registry/`. Every session
   then starts with the delegation tools, and no settings file names them.
   A settings file that sets `feeds.defaults: false` and
   `cli_registry.defaults: false` runs without them. A profile that turns
   the defaults off does the same.
3. Run `poo` with a profile that offers the registered tools: a profile with a side-effecting tool earns the catalog, and a catalog this short is offered entry by entry. Ask it to delegate a task and have the child ask a question before finishing.

The [feed manifest](feeds.json) defines the `avenor-inbox` feed, and poo starts the feed's command when the session opens. The [tool manifest](tools.json) registers five tools: `avenor_spawn`, `avenor_status`, `avenor_await`, `avenor_result`, and `avenor_message`. Poo resolves each manifest's `AVENOR_BIND_SESSION: session:id` to the session's own native ID, so the feed and every tool reach the same supervisor. Avenor starts one supervisor per poo session. When the feed exits, it stops the supervisor, so no service needs starting and no broker credentials need exporting.

Use the `runtime_id` returned by `avenor_spawn` as the target for `avenor_await`. Use the same ID as the `runtime` argument to `avenor_status` and `avenor_result`. `avenor_await` blocks until the child reaches a terminal state, and it returns the child's final output. Pass `background: true` to keep working in your own turn while `avenor_await` waits as a background job; its result arrives as a message when the child settles. Call `avenor_status` to check the current state of a run. Call `avenor_result` to retrieve the output of a completed run.

When the inbox delivers a child's ask, answer it with the feed's `avenor_reply` tool; use the `reply_to` ID the message carries. To message a child unprompted, call `avenor_message` with `action` set to `tell` (no reply edge) or `ask` (blocks until the child answers). The action `withdraw` retracts an ask you sent.

To share an existing local supervisor, set `AVENOR_SOCKET` to its socket path in the feed and each tool entry. The feed will attach to that supervisor instead of starting one.

See [Feeds](../../docs/build-your-own-binary.md#feeds) for feed settings and [CLI registry](../../README.md#what-works-today) for tool availability.
