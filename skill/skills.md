The following skills provide specialized instructions for specific tasks.
Read one with the read tool when its description fits the task, and follow what it says. A skill's file is its guidance, not a script: read all of it before you start.
When a skill file references a relative path, resolve it against the skill directory (parent of SKILL.md / dirname of the path) and use that absolute path in tool

<available_skills>{{range .Skills}}
<skill>
    <name>{{.Name}}</name>
    <description>{{.Description}}</description>
    <location>{{.Path}}</location>
</skill>{{end}}
</available_skills>

