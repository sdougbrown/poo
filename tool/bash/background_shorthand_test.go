package bash_test

import (
	"encoding/json"
	"testing"

	"github.com/sdougbrown/poo/tool/bash"
)

func TestBackgroundShorthand(t *testing.T) {
	for _, tt := range []struct {
		command    string
		background string
		want       string
		refuse     bool
	}{
		{command: `listen&`, want: `listen`},
		{command: `listen &# ready`, want: `listen # ready`},
		{command: `prepare && listen >out 2>&1 & # ready`, want: `prepare && listen >out 2>&1 # ready`},
		{command: `printf '%s' café\ &`, want: `printf '%s' café\ `},
		{command: `printf '%s' '&'`},
		{command: `printf '%s' \&`},
		{command: `listen & wait "$!"`},
		{command: `prepare; listen &`, refuse: true},
		{command: `listen & other &`, refuse: true},
		{command: `listen "$(other)" &`, refuse: true},
		{command: `setsid listen &`, refuse: true},
		{command: "cat <<'END' &\ncontent\nEND", refuse: true},
		// The reserved argument stays out of the shorthand: the
		// shorthand translates the call, and the core decides the
		// argument itself.
		{command: `listen &`, background: "false", want: `listen`},
	} {
		t.Run(tt.command+" background="+tt.background, func(t *testing.T) {
			command, err := json.Marshal(tt.command)
			if err != nil {
				t.Fatal(err)
			}
			args := map[string]json.RawMessage{"command": command}
			if tt.background != "" {
				args["background"] = json.RawMessage(tt.background)
			}
			raw, err := json.Marshal(args)
			if err != nil {
				t.Fatal(err)
			}
			got, err := bash.New().BackgroundShorthand(raw)
			if tt.refuse {
				if err == nil || got != nil {
					t.Fatalf("got %s, %v; want a refusal", got, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if tt.want == "" {
				if got != nil {
					t.Fatalf("got %s; want no rewrite", got)
				}
				return
			}
			var effective struct {
				Command    string          `json:"command"`
				Background json.RawMessage `json:"background"`
			}
			if err := json.Unmarshal(got, &effective); err != nil {
				t.Fatal(err)
			}
			if effective.Command != tt.want {
				t.Fatalf("got %s; want command %q", got, tt.want)
			}
			if tt.background != "" && effective.Background != nil {
				t.Fatalf("got %s; want the reserved argument out of the shorthand", got)
			}
		})
	}
}
