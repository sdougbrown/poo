Registered CLIs:
{{if .Search}}{{.Count}} command-line tools are registered, too many to list here. Call {{.Find}} with what you want to do, and it returns the tools that fit, with their arguments. Then call {{.Call}} with the name it gives you.{{else}}Call one with {{.Call}}: "name" is the line's name, and "args" is an object of its arguments. The tool checks the arguments against the CLI's own schema and says what is wrong with them.{{range .Entries}}
{{.Name}}: {{.Description}}{{end}}{{end}}
