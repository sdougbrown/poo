Find a tool:
- Call find_tool with what you want to do, in your own words. It returns the registered CLIs that fit, best first, with their arguments.
- A search asks the decision model one question per registered tool. Call it when you need a tool you have not used before, not before every call.
- find_tool narrows the list; it never chooses for you. Call the tool it returns with call_cli.
