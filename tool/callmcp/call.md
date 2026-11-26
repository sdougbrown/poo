Call an MCP (Model Context Protocol) tool:
- call_mcp is only for the tools the MCP servers of this session offer.
- It is not a dispatcher: bash, read, grep, edit, fetch, codemode, jobs, finish and the rest are tools you call directly. call_mcp cannot run them, itself, or anything a server does not list.
- "name" is one line of the catalog below: the tool's qualified name, as "server/tool".
- "args" is an object of that tool's arguments.
- Pass "background": true to run the call asynchronously as a background job. The call returns at once with a job handle, and the result arrives later as a message.
- A tool the servers do not list is refused before anything runs, with the reason. The refusal names the search when this session offers one, and the catalog below when it does not.
- The result is what the tool answered, elided in the middle when it is long.
- A tool that reports its own failure answers with that failure as the result: read it and correct the call.
- A server that stopped is reported as such, and the session keeps running.
