Feeds you can start:
- subscribe_feed starts one of the feeds below. They do not run at a session's start.
- The jobs tool lists the instance you started, and cancel_job stops it.
- Subscribing the same feed and scope again reaches the instance that is already there.
- A feed whose filter failed is suspended, and subscribing to it again starts it again.
- A scope says what to watch. Each field takes one string. Where the feed gives a pattern, the whole value must match it.
{{range .OptionalFeeds}}- {{.Name}}{{with .Description}}: {{.}}{{end}}
{{with .Scope}}  Its scope is {{range $i, $field := .}}{{if $i}}, {{end}}{{$field.Name}}{{with $field.Pattern}} ({{.}}){{end}}{{end}}.
{{end}}{{end}}
