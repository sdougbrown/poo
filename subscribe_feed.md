Feeds you can start:
- subscribe_feed starts one of the feeds below. They do not run at a session's start.
- The jobs tool lists the instance you started, and cancel_job stops it.
- Subscribing the same feed and scope again reaches the instance that is already there.
- A session runs at most eight feed instances at once. A new one beyond that is refused: stop one with cancel_job first.
- A feed whose filter failed is suspended, and subscribing to it again starts it again.
- A scope says what to watch. Each field takes one string. Where the feed gives a pattern, the whole value must match it.
- A prompt is the question the decision model is asked about each message, and it replaces the feed's own question while it is set. An empty prompt clears it.
- A feed whose manifest runs a filter command takes no prompt, and neither does a session with no decision model.
{{range .OptionalFeeds}}- {{.Name}}{{with .Description}}: {{.}}{{end}}
{{with .Scope}}  Its scope is {{range $i, $field := .}}{{if $i}}, {{end}}{{$field.Name}}{{with $field.Pattern}} ({{.}}){{end}}{{end}}.
{{end}}{{end}}
