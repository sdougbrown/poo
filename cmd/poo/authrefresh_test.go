package main

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sdougbrown/poo/provider/auth"
)

func TestAuthRefresh(t *testing.T) {
	for _, tc := range []struct {
		name     string
		left     time.Duration
		flags    []string
		due      bool
		pi       bool
		refusal  bool
		readonly bool
		wantCode int
		wantErr  string
	}{
		{name: "fresh", left: time.Hour},
		{name: "due", left: time.Minute, due: true},
		{name: "custom leeway in Pi file", left: 20 * time.Minute, flags: []string{"--leeway", "30m"}, due: true, pi: true},
		{name: "forced", left: time.Hour, flags: []string{"--force"}, due: true},
		{name: "refused", left: -time.Minute, due: true, refusal: true, wantCode: operationalExitCode, wantErr: "invalid_grant"},
		{name: "read-only", left: -time.Minute, readonly: true, wantCode: operationalExitCode, wantErr: "read-only"},
		{name: "zero leeway", left: time.Minute, flags: []string{"--leeway=0s"}, wantCode: usageExitCode, wantErr: "--leeway must be positive"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var requests atomic.Int32
			endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				if r.Method != http.MethodPost || r.FormValue("grant_type") != "refresh_token" || r.FormValue("refresh_token") != "old-refresh" {
					t.Error("expected a refresh grant for the stored credential")
				}
				if tc.refusal {
					w.WriteHeader(http.StatusBadRequest)
					fmt.Fprint(w, `{"error":"invalid_grant","error_description":"do not log old-refresh"}`)
					return
				}
				fmt.Fprint(w, `{"access_token":"new-access","refresh_token":"new-refresh","expires_in":3600}`)
			}))
			defer endpoint.Close()

			dir := t.TempDir()
			path := filepath.Join(dir, "auth.json")
			body := fmt.Sprintf(`{"openai-codex":{"type":"oauth","access":"old-access","refresh":"old-refresh","expires":%d,"accountId":"acct-test","tokenUrl":%q}}`, time.Now().Add(tc.left).UnixMilli(), endpoint.URL)
			if !tc.pi {
				body = `{"version":1,"credentials":` + body + `}`
			}
			if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
				t.Fatal(err)
			}
			settings := authSettings(t, dir, "auth.json")
			if tc.pi {
				if err := os.WriteFile(settings, []byte("version: 1\npi_auth_file: auth.json\n"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			if tc.readonly {
				body, err := os.ReadFile(settings)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(settings, append(body, []byte("auth_readonly: true\n")...), 0o600); err != nil {
					t.Fatal(err)
				}
			}

			code, stderr, stdout := runCLIWithStdout(t, append([]string{"auth", "refresh", "--settings", settings}, tc.flags...)...)
			if code != tc.wantCode || !strings.Contains(stderr, tc.wantErr) || (tc.wantErr == "" && stderr != "") || stdout != "" {
				t.Fatalf("got exit %d, stdout %q, stderr %q; want exit %d, empty stdout and stderr %q", code, stdout, stderr, tc.wantCode, tc.wantErr)
			}
			for _, secret := range []string{"old-refresh", "old-access", "new-refresh", "new-access"} {
				if strings.Contains(stderr, secret) {
					t.Error("stderr contains a token")
				}
			}
			var wantRequests int32
			if tc.due {
				wantRequests = 1
			}
			if got := requests.Load(); got != wantRequests {
				t.Errorf("sent %d refresh requests, want %d", got, wantRequests)
			}
			if !tc.due || tc.refusal {
				after, err := os.ReadFile(path)
				if err != nil || !bytes.Equal(after, []byte(body)) {
					t.Fatalf("credential file changed (read error: %v)", err)
				}
				return
			}
			stored, err := (auth.Credentials{Path: path}).Read(auth.Codex)
			if err != nil || stored.Access != "new-access" || stored.Refresh != "new-refresh" {
				t.Fatalf("rotated credential was not saved (read error: %v)", err)
			}
		})
	}
}
