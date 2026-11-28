# Wire Avenor into poo over MCP

The MCP path to Avenor: poo starts Avenor's MCP server beside the session, the
model reaches the server's tools through `call_mcp`, and the session reads the
server's inbox as a resource feed. No feed command, no CLI tools, and no broker
address to set: the server binds itself to the session, and its inbox wakes an
idle session.

The older example, [examples/avenor](../avenor/README.md), reaches the same
supervisor through `avenor feed broker` and four registered CLI commands. Use
one or the other for a session, never both (see [One receive
path](#one-receive-path)).

## Set up

1. Put `avenor` on `PATH`. The build must carry the bound MCP server: `avenor
   mcp --bind-host poo --bind-session <id>` on stdio serves the inbox resource
   and the `spawn`, `status`, `result`, `message` and other tools.
2. Put `jq` on `PATH`. The example's feed filter is a `jq` program.
3. Run the settings from the poo checkout, so the relative `mcp.paths` entry
   resolves:

   ```sh
   cd ~/Code/poo
   poo run --settings examples/avenor-mcp/settings.yaml \
     "spawn a child to run the test suite, then answer its questions"
   ```

   Point `endpoints`, `models` and `roles` at your own model first.

The [manifest](mcp.json) declares the server, and its `poo` block adds the
inbox feed. The [settings](settings.yaml) name that manifest directory and
select the server.

## What starts, and what selects it

`mcp.paths` names the manifests to read, and `mcp.defaults: false` keeps poo's
own mcp directory out of the session. Loading a manifest declares servers; it
starts none. The profile's `mcp_servers` is what starts one:

```yaml
profiles:
  avenor:
    base: code
    mcp_servers: [avenor]
```

`mcp_servers` selects; `feeds` only declares. A feed manifest names feeds,
and every one of them runs beside the session: nothing in `feeds` decides
whether a feed starts, because the manifest is that decision. A server is not
like that. Its manifest declares it, and the profile's `mcp_servers` is what
starts it. `feeds.defaults: false` and `cli_registry.defaults: false` are here
so no ambient manifest joins this session either.

The server's command resolves per session:
`avenor mcp --bind-host poo --bind-session ${session:id}`, where
`${session:id}` is the poo session's own ID, the same one after a resume. The
supervisor a binding like this starts is private to the session and stops when
the server exits; a shared one only loses this session's connection.

To share a supervisor another process already owns, append its socket and the
flag that forbids starting a second one, in `mcp.json`'s `args`:
`--supervisor-socket <path> --no-autostart`.

## The tools and the inbox

The server lists its own tools, so the model calls them by qualified name:
`call_mcp` with `{"name": "avenor/spawn", "args": {...}}`. The catalog in the
prompt lists what the running server offers, with each tool's argument hint,
and a session with a decision model searches the same names with `find_tool`
beside the registered commands.

The `avenor-inbox` feed subscribes to `avenor://inbox`, which carries one JSON
object per read:

```json
{"messages": [{"id": "...", "from": "...", "text": "...", "expects_reply": false, "answerable": false}]}
```

The feed's `jq` filter turns that object into one input per message, so a read
that drained three messages delivers three inputs and a read of an empty inbox
delivers none. A message that expects a reply carries a footer naming the call
that answers it, and the model makes that call:

```json
{"name": "avenor/message", "args": {"action": "reply", "reply_to": "<id>", "message": "..."}}
```

The id in the footer is the message's own, so two asks in one batch are
answered apart. The footer's wording is the one `avenor feed broker` writes,
and the text before it is the message's text, unchanged.

An ask the session cannot answer — a sender this run cannot route a reply to —
says so instead, so the model does not spend a call on it.

## Trust

MCP servers are the operator's chosen processes, started directly: they run as
you run poo, unsandboxed, with the host environment minus the names
`tools.env.scrub` removes, plus the manifest's `env`. poo does not put them
through the session's executor or sandbox. A `${VAR}` the manifest writes puts a
scrubbed variable back; that is the manifest author's choice. Select a server
the way you would install a program.

## One receive path

Avenor's inbox has one consumer at a time. Both this example's feed and
`examples/avenor`'s `avenor feed broker` read the same queue, and Avenor's
consumer lock makes the second one fail with `the inbox consumer lock is held
by another consumer, such as avenor feed broker`. The lock lives in Avenor's
state home, so two sessions sharing a state home refuse the second read; two
state homes do not see each other's lock, and two readers would split one
queue between them.

So: run this example or the feed-and-CLI example for a session, not both. This
example's settings turn the ambient `feeds` and `cli_registry` manifests off
for exactly that reason. The older example's instructions add its manifests to
a settings file of your own, so keep `examples/avenor-mcp` out of that file's
`mcp.paths` and the two never meet.

## What happens when the server stops

poo relaunches a server whose process exits, waits 1s before the first attempt
and doubles the wait to 30s, and treats a generation that lived a minute or
more as proof the server works. After five consecutive failures the server is
gone for the session: a call answers `server avenor is down`, and the inbox
feed's next attempt reports a fault that suspends the feed instead of
restarting against a server that will not come back. The session itself keeps
running; a resume starts over.

After Avenor's supervisor is replaced, the bound server refuses spawns and
asks the host to reconnect. Killing the server process is the recovery poo
offers: the relaunch binds a fresh conversation. poo adds nothing Avenor's own
contract does not already say.

See [MCP servers](../../docs/capabilities/mcp-servers.md) for the
manifest, the templates and the feed's settings, and [Feeds & Daemon](../../docs/capabilities/feeds-and-daemon.md)
for what a filter may write.
