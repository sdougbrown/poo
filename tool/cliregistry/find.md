Find a tool:
- Call find_tool with what you want to do, in your own words. It returns the tools that fit, best first, with their arguments.
- The offerings are the registered commands and the MCP (Model Context Protocol) tools of this session's servers, together.
- Each hit names the tool that calls it: call_cli for a registered command, and call_mcp for an MCP tool. The hit also says which name to pass.
- A search asks the decision model one question per tool. Call it when you need a tool you have not used before, not before every call.
- find_tool narrows the list; it never chooses for you.
