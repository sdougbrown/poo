# Delegate to Avenor from poo

Use these manifests to delegate to child agents and receive their questions in the same poo session. The inbox can wake an idle session. This example does not require an MCP client.

## Set up

1. Put `avenor` on `PATH`. Use a build that supports per-session supervisor startup in `avenor feed broker` and the bound `spawn`, `status`, `result`, and `message` commands.
2. Add the manifests to your existing poo settings. Replace `~/Code/poo` with your checkout path:

   ```yaml
   feeds:
     paths: [~/Code/poo/examples/avenor/feeds.json]
   cli_registry:
     paths: [~/Code/poo/examples/avenor/tools.json]
   ```

   If you already have paths under either key, append the new path to the list.
3. Run `poo` with a profile that offers `call_cli`. Ask it to delegate a task and have the child ask a question before finishing.

The [feed manifest](feeds.json) starts `avenor-inbox`. The [CLI manifest](tools.json) registers `avenor_spawn`, `avenor_status`, `avenor_result`, and `avenor_message` (reply only). Poo resolves `session:id` to the same native session ID for the feed and every tool. Avenor starts a separate supervisor for each poo session and stops it when the feed exits. You do not need to start a service or export broker credentials.

Use the `runtime_id` returned by `avenor_spawn` as the `runtime` argument to `avenor_status` and `avenor_result`. When the inbox delivers a child's ask, follow the included `avenor_message` call and use the supplied `reply_to` ID.

To share an existing local supervisor, set `AVENOR_SOCKET` to its socket path in the feed and each tool entry. The feed will attach to that supervisor instead of starting one.

See [Feeds](../../docs/build-your-own-binary.md#feeds) for feed settings and [CLI registry](../../README.md#what-works-today) for tool availability.
