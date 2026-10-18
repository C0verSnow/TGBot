package bot

import (
	"context"
	"errors"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
)

func payload(text string, uid int64, kind string) string {
	return fmt.Sprintf(`{"update_id":42,"message":{"from":{"id":%d},"chat":{"id":%d,"type":%q},"text":%q}}`, uid, uid, kind, text)
}
func TestWebhookBoundary(t *testing.T) {
	cases := []struct {
		name, body, secret string
		status, calls      int
	}{
		{"start", payload("/dockerstart", 123, "private"), "secret", 200, 1},
		{"stop", payload("/dockerstop", 123, "private"), "secret", 200, 1},
		{"auth", payload("/dockerstart", 123, "private"), "bad", 403, 0},
		{"nonwhitelist", payload("/dockerstop", 999, "private"), "secret", 200, 0},
		{"group", payload("/dockerstop", 123, "group"), "secret", 200, 0},
		{"argument", payload("/dockerstart other-container", 123, "private"), "secret", 200, 0},
		{"injection", payload("/dockerstop;id", 123, "private"), "secret", 200, 0},
		{"idPublic", payload("/id", 999, "private"), "secret", 200, 0},
		{"helpPublic", payload("/help", 999, "private"), "secret", 200, 0},
		{"malformed", "{", "secret", 400, 0},
		{"booleanID", `{"update_id":true}`, "secret", 400, 0},
		{"edited", `{"update_id":42,"edited_message":{}}`, "secret", 200, 0},
		{"oversized", strings.Repeat("x", 65537), "secret", 413, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			message := ""
			app := &App{Secret: "secret", IDs: map[int64]bool{123: true},
				Send: func(_ context.Context, _ int64, s string) error { message = s; return nil },
				Execute: func(ctx context.Context, action string, id int64) string {
					if _, ok := ctx.Deadline(); !ok {
						t.Fatal("no deadline")
					}
					if id != 42 || (action != "start" && action != "stop") {
						t.Fatal("unrestricted command")
					}
					calls++
					return "done"
				}}
			req := httptest.NewRequest("POST", "/", strings.NewReader(tc.body))
			req.Header.Set("X-Telegram-Bot-Api-Secret-Token", tc.secret)
			res := httptest.NewRecorder()
			app.Webhook(res, req)
			if res.Code != tc.status || calls != tc.calls {
				t.Fatalf("status=%d calls=%d", res.Code, calls)
			}
			if tc.name == "idPublic" && !strings.Contains(message, "999") {
				t.Fatal(message)
			}
			if tc.name == "helpPublic" && !strings.Contains(message, "/dockerstop") {
				t.Fatal(message)
			}
		})
	}
}
func TestPolicyAndHostRestriction(t *testing.T) {
	for _, s := range []string{"@name", "0", "-1", "+12", "01", "9223372036854775808"} {
		if _, err := ParseIDs(s); err == nil {
			t.Fatal(s)
		}
	}
	ids, err := ParseIDs("123 # friend\n456\n# ignored")
	if err != nil || !ids[123] || !ids[456] {
		t.Fatal(ids, err)
	}
	for _, host := range []string{"127.0.0.1", "192.168.1.1", "8.8.8.8", "server.example.com", "100.64.0.1:22"} {
		if tailnetHost(host) {
			t.Fatal(host)
		}
	}
	if !tailnetHost("100.101.102.103") || !tailnetHost("fd7a:115c:a1e0::1") {
		t.Fatal("tailnet denied")
	}
}
func TestResultUncertainty(t *testing.T) {
	for _, s := range []string{`{}`, `garbage`, `{"result":"other"}`, strings.Repeat("x", 4097)} {
		if resultMessage([]byte(s), "start") != uncertain {
			t.Fatal(s)
		}
	}
	for _, state := range []string{"duplicate", "timeout", "failed"} {
		if strings.Contains(resultMessage([]byte(`{"result":"`+state+`"}`), "start"), "成功") {
			t.Fatal(state)
		}
	}
	if resultMessage([]byte(`{"result":"ok"}`), "stop") != "容器停止成功。" {
		t.Fatal("success")
	}
	b := &limitedBuffer{}
	_, _ = b.Write([]byte(strings.Repeat("x", 100000)))
	if b.Len() != 4097 {
		t.Fatal(b.Len())
	}
}
func TestProbeAuthAndNoCommand(t *testing.T) {
	calls := 0
	a := &App{ProbeSecret: "probe", Check: func(context.Context) error { calls++; return nil }, Execute: func(context.Context, string, int64) string { t.Fatal("probe executed a command"); return "" }}
	for _, secret := range []string{"", "wrong", "probe"} {
		req := httptest.NewRequest("POST", "/", nil)
		req.Header.Set("X-TGBot-Probe-Secret", secret)
		res := httptest.NewRecorder()
		a.Probe(res, req)
		if (res.Code == 200) != (secret == "probe") {
			t.Fatal(res.Code)
		}
	}
	if calls != 1 {
		t.Fatal(calls)
	}
}

func TestProbeFailureStagesDoNotLeakErrors(t *testing.T) {
	for _, value := range []string{"tailnet join", "private-token-sensitive-output"} {
		a := &App{ProbeSecret: "probe", Check: func(context.Context) error { return errors.New(value) }}
		req := httptest.NewRequest("POST", "/", nil)
		req.Header.Set("X-TGBot-Probe-Secret", "probe")
		res := httptest.NewRecorder()
		a.Probe(res, req)
		want := "connection"
		if value == "tailnet join" {
			want = value
		}
		if res.Code != 503 || res.Header().Get("X-TGBot-Probe-Stage") != want || strings.Contains(res.Body.String(), value) {
			t.Fatal("probe diagnostic leaked an error or omitted its safe stage")
		}
	}
}
