Tools:{{range .Tools}}
- {{.Name}}: {{.Description}}{{end}}

Call a tool by writing a <tool_call> block holding a JSON object with a name and an arguments object, for example:
<tool_call>{"name": "bash", "arguments": {"command": "ls -la"}}</tool_call>
Call one tool at a time, and wait for its result before the next call.
