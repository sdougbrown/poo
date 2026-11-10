Fetch:
- The result is one JSON object. Its fields are status, final_url after the redirects, content_type, size, and the body.
- An HTML page comes back as Markdown unless you ask for "raw"; a response that is not text comes back as its metadata alone.
- A body past the observation cap keeps its head and its tail, and "elided_chars" says how much the middle held.
- The fetch refuses loopback, link-local and private addresses. The settings' tools.fetch.allow_private_hosts lifts the refusal.
- Use bash and curl for what a plain read cannot do: another method, headers that authenticate, a body saved to a file.
- The fetch runs in poo's own process, so a sandbox that cuts the network for bash does not cut it. The refusals above are what bounds it.
