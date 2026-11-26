package bash_test

import (
	"encoding/json"
	"testing"

	"github.com/sdougbrown/poo/tool/bash"
)

func TestBackgroundShorthand(t *testing.T) {
	for _, tt := range []struct {
		command string
		want    string
		refuse  bool
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
	} {
		t.Run(tt.command, func(t *testing.T) {
			raw, _ := json.Marshal(map[string]string{"command": tt.command})
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
				Command    string `json:"command"`
				Background bool   `json:"background"`
			}
			if err := json.Unmarshal(got, &effective); err != nil {
				t.Fatal(err)
			}
			if effective.Command != tt.want || !effective.Background {
				t.Fatalf("got %s; want %q in the background", got, tt.want)
			}
		})
	}
}
