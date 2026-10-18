package bot

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestParseGate(t *testing.T) {
	for _, text := range []string{"", "GATE_API_KEY=x", "GATE_API_KEY=\nGATE_API_SECRET=x", "GATE_API_KEY=x\nGATE_API_KEY=y", "OTHER=x\nGATE_API_SECRET=y", "GATE_API_KEY= x\nGATE_API_SECRET=y", "GATE_API_KEY=x\nGATE_API_SECRET=y\nextra", "GATE_API_KEY=\"x\"\nGATE_API_SECRET=y", "GATE_API_KEY=x\nGATE_API_SECRET=秘密"} {
		if _, err := parseGate(text); err == nil {
			t.Fatal("invalid input accepted")
		}
	}
	v, err := parseGate("GATE_API_SECRET=secret==\r\nGATE_API_KEY=key")
	if err != nil || v != (GateSecrets{"key", "secret=="}) {
		t.Fatal("valid input rejected")
	}
}

func TestInfisicalBoundaries(t *testing.T) {
	for _, mode := range []string{"ok", "auth", "missing", "partial", "mismatch", "network", "badJSON", "hidden"} {
		t.Run(mode, func(t *testing.T) {
			patches, reads := 0, 0
			values := GateSecrets{"test-key", "test-secret"}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if r.URL.Path == "/api/v1/auth/universal-auth/login" {
					if mode == "auth" {
						w.WriteHeader(401)
						fmt.Fprint(w, `{"error":"test-key test-secret machine-secret"}`)
						return
					}
					fmt.Fprint(w, `{"accessToken":"test-access-token"}`)
					return
				}
				if r.Header.Get("Authorization") != "Bearer test-access-token" {
					t.Error("missing authorization")
				}
				name := strings.TrimPrefix(r.URL.Path, "/api/v4/secrets/")
				if name != gateNames[0] && name != gateNames[1] {
					t.Error("unexpected secret target")
				}
				if r.Method == "GET" {
					reads++
					q := r.URL.Query()
					if q.Get("projectId") != "project" || q.Get("environment") != "prod" || q.Get("secretPath") != "/fixed" || q.Get("includeImports") != "false" {
						t.Error("scope changed")
					}
					if mode == "missing" && name == gateNames[1] {
						w.WriteHeader(404)
						return
					}
					if mode == "badJSON" {
						fmt.Fprint(w, "test-key test-secret")
						return
					}
					value := "old"
					if patches == 2 && mode != "mismatch" {
						if name == gateNames[0] {
							value = values[0]
						} else {
							value = values[1]
						}
					}
					_ = json.NewEncoder(w).Encode(map[string]any{"secret": map[string]any{"secretKey": name, "secretValue": value, "secretValueHidden": mode == "hidden"}})
					return
				}
				if r.Method != "PATCH" {
					t.Error("must only update existing secrets")
				}
				patches++
				var body map[string]string
				_ = json.NewDecoder(r.Body).Decode(&body)
				if len(body) != 5 || body["projectId"] != "project" || body["environment"] != "prod" || body["secretPath"] != "/fixed" || body["type"] != "shared" || body["secretValue"] != values[patches-1] {
					t.Error("modified scope or metadata")
				}
				if mode == "partial" && patches == 2 {
					w.WriteHeader(500)
					fmt.Fprint(w, "test-key test-secret")
					return
				}
				fmt.Fprint(w, `{}`)
			}))
			defer server.Close()
			i := &Infisical{URL: "https://infisical.test", ClientID: "machine-id", ClientSecret: "machine-secret", Project: "project", Environment: "prod", Path: "/fixed", Client: server.Client()}
			i.Client.Transport = &rewriteTransport{base: server.URL, delegate: i.Client.Transport, fail: mode == "network"}
			result, uncertain := i.Update(context.Background(), values)
			if !safeFlowResult(result) {
				t.Fatal("unrecognized or sensitive result")
			}
			for _, forbidden := range []string{"test-key", "test-secret", "test-access-token", "machine-secret"} {
				if strings.Contains(result, forbidden) {
					t.Fatal("secret leaked")
				}
			}
			if mode == "ok" {
				if patches != 2 || reads != 4 || uncertain || !strings.Contains(result, "更新成功") {
					t.Fatal("success not verified")
				}
			} else if strings.Contains(result, "更新成功") {
				t.Fatal("false success")
			}
			if (mode == "partial" || mode == "mismatch") != uncertain {
				t.Fatal("incorrect uncertainty")
			}
			if mode == "missing" && patches != 0 {
				t.Fatal("wrote before preflight completed")
			}
		})
	}
}

type rewriteTransport struct {
	base     string
	delegate http.RoundTripper
	fail     bool
}

func (rt *rewriteTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if rt.fail {
		return nil, fmt.Errorf("sensitive transport error test-secret")
	}
	copy := r.Clone(r.Context())
	copy.URL.Scheme = "http"
	copy.URL.Host = strings.TrimPrefix(rt.base, "http://")
	return rt.delegate.RoundTrip(copy)
}

func TestSecretFlowClaimAndReplay(t *testing.T) {
	for _, mode := range []string{"normal", "duplicate", "working", "expired", "locked", "delete-failed", "state-failed", "finish-failed", "invalid", "old"} {
		t.Run(mode, func(t *testing.T) {
			writes, claims := 0, 0
			f := &SecretFlow{Infisical: &Infisical{URL: "https://example.com", ClientID: "id", ClientSecret: "secret", Project: "p", Environment: "prod", Path: "/"}, Now: func() time.Time { return time.Unix(1000, 0) }}
			f.State = func(_ context.Context, q stateRequest) (stateResponse, error) {
				serialized, _ := json.Marshal(q)
				if strings.Contains(string(serialized), "submitted-key") || strings.Contains(string(serialized), "submitted-secret") {
					t.Fatal("credentials entered persistent state")
				}
				if mode == "state-failed" || (mode == "finish-failed" && q.Op == "finish") {
					return stateResponse{}, fmt.Errorf("secret internal error")
				}
				if q.Op == "peek" {
					if mode == "duplicate" {
						return stateResponse{State: "done", Result: safeSecretResults[17]}, nil
					}
					if mode == "working" {
						return stateResponse{State: "working"}, nil
					}
					return stateResponse{State: "missing"}, nil
				}
				if q.Op == "claim" {
					claims++
					if mode == "expired" || mode == "locked" {
						return stateResponse{State: mode}, nil
					}
					return stateResponse{State: "claimed"}, nil
				}
				return stateResponse{State: "done", Result: q.Result}, nil
			}
			f.Update = func(ctx context.Context, _ GateSecrets) (string, bool) {
				writes++
				if _, ok := ctx.Deadline(); !ok {
					t.Fatal("unbounded update")
				}
				return safeSecretResults[17], false
			}
			app := &App{Secrets: f}
			text := "GATE_API_KEY=submitted-key\nGATE_API_SECRET=submitted-secret"
			if mode == "invalid" {
				text = "GATE_API_KEY=submitted-key"
			}
			date := int64(1000)
			if mode == "old" {
				date = 1
			}
			result := app.secretMessage(context.Background(), 123, 123, 42, 12, date, text, mode != "delete-failed")
			want := 0
			if mode == "normal" || mode == "finish-failed" {
				want = 1
			}
			if writes != want || (mode == "delete-failed" && claims != 0) {
				t.Fatal("unsafe execution")
			}
			if strings.Contains(result, "submitted-") || strings.Contains(result, "secret internal") {
				t.Fatal("secret leaked")
			}
		})
	}
}

func TestSensitiveWebhookDeletionAndAuthorization(t *testing.T) {
	for _, uid := range []int64{123, 999} {
		deleted := 0
		a := &App{Secret: "s", IDs: map[int64]bool{123: true}, Delete: func(_ context.Context, chat, msg int64) error {
			deleted++
			if chat != uid || msg != 7 {
				t.Fatal("wrong deletion target")
			}
			return nil
		}, Send: func(_ context.Context, _ int64, s string) error {
			if strings.Contains(s, "submitted-key") {
				t.Fatal("echoed input")
			}
			return nil
		}}
		body := fmt.Sprintf(`{"update_id":42,"message":{"message_id":7,"date":1000,"from":{"id":%d},"chat":{"id":%d,"type":"private"},"text":"GATE_API_KEY=submitted-key\nGATE_API_SECRET=submitted-secret"}}`, uid, uid)
		r := httptest.NewRequest("POST", "/", strings.NewReader(body))
		r.Header.Set("X-Telegram-Bot-Api-Secret-Token", "s")
		w := httptest.NewRecorder()
		a.Webhook(w, r)
		if w.Code != 200 || deleted != 1 {
			t.Fatal("sensitive message not deleted")
		}
	}
}

func TestMalformedPendingInputDeletion(t *testing.T) {
	for _, fail := range []bool{false, true} {
		deleted, validated := 0, 0
		f := &SecretFlow{Infisical: &Infisical{URL: "https://example.com", ClientID: "id", ClientSecret: "secret", Project: "p", Environment: "prod", Path: "/"}, Now: func() time.Time { return time.Unix(1000, 0) }}
		f.State = func(_ context.Context, q stateRequest) (stateResponse, error) {
			if q.Op == "peek" {
				return stateResponse{State: "waiting"}, nil
			}
			if q.Op != "invalid" {
				t.Fatal("malformed input reached claim")
			}
			validated++
			return stateResponse{State: "done", Result: q.Result}, nil
		}
		f.Update = func(context.Context, GateSecrets) (string, bool) {
			t.Fatal("malformed input reached Infisical")
			return "", false
		}
		a := &App{Secrets: f, Delete: func(context.Context, int64, int64) error {
			deleted++
			if fail {
				return fmt.Errorf("private deletion error")
			}
			return nil
		}}
		result := a.secretMessage(context.Background(), 123, 123, 42, 7, 1000, "unlabeled-sensitive-value", true)
		if deleted != 1 || (fail && validated != 0) || (!fail && validated != 1) {
			t.Fatal("unsafe malformed-input handling")
		}
		if strings.Contains(result, "unlabeled-sensitive-value") || strings.Contains(result, "private deletion") {
			t.Fatal("sensitive error echoed")
		}
	}
}

func TestRedisStateNeverTransmitsCredentials(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(body), "EVAL") || strings.Contains(string(body), "submitted-secret") {
			t.Error("invalid state payload")
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"result": `{"state":"missing","result":""}`})
	}))
	defer server.Close()
	s := &redisState{URL: server.URL, Key: "token", Client: server.Client()}
	if result, err := s.call(context.Background(), stateRequest{Scope: "test", User: 123, Chat: 123, Update: 42, Message: 7, Op: "peek"}); err != nil || result.State != "missing" {
		t.Fatal("REST EVAL response rejected")
	}
	// Invalid or insecure endpoints must fail before making any network call.
	s.URL = "http://insecure.example"
	if _, err := s.call(context.Background(), stateRequest{}); err == nil {
		t.Fatal("insecure state endpoint accepted")
	}
}
