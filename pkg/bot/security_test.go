package bot

import (
	"context"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/json"
	"encoding/pem"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

type transportFunc func(*http.Request) (*http.Response, error)

func (f transportFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func mockHTTP(t *testing.T, f transportFunc) {
	old := http.DefaultTransport
	http.DefaultTransport = f
	t.Cleanup(func() { http.DefaultTransport = old })
}
func response(code int, body string) *http.Response {
	return &http.Response{StatusCode: code, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}
}
func TestOAuthRestrictionsAndRedaction(t *testing.T) {
	calls := 0
	mockHTTP(t, func(r *http.Request) (*http.Response, error) {
		calls++
		if r.URL.Scheme != "https" || r.URL.Host != "api.tailscale.com" {
			t.Fatal("unexpected endpoint")
		}
		if calls == 1 {
			_ = r.ParseForm()
			if r.Form.Get("client_secret") != "tskey-client-fixture" {
				t.Fatal("missing OAuth authentication")
			}
			return response(200, `{"access_token":"fixture-token"}`), nil
		}
		if r.Header.Get("Authorization") != "Bearer fixture-token" {
			t.Fatal("missing bearer")
		}
		var data struct {
			Capabilities struct {
				Devices struct {
					Create struct {
						Reusable, Ephemeral, Preauthorized bool
						Tags                               []string
					}
				}
			}
		}
		if json.NewDecoder(r.Body).Decode(&data) != nil {
			t.Fatal("invalid key request")
		}
		c := data.Capabilities.Devices.Create
		if c.Reusable || !c.Ephemeral || !c.Preauthorized || len(c.Tags) != 1 || c.Tags[0] != "tag:tgbot-ci" {
			t.Fatal("broader capabilities")
		}
		return response(200, `{"key":"tskey-auth-fixture"}`), nil
	})
	key, err := authKey(context.Background(), "", "tskey-client-fixture")
	if err != nil || key != "tskey-auth-fixture" || calls != 2 {
		t.Fatal("mint failed", err, calls)
	}
	if _, err := authKey(context.Background(), "tskey-auth-a", "tskey-client-b"); err == nil {
		t.Fatal("ambiguous auth")
	}
	if _, err := authKey(context.Background(), "", "tskey-client-a?baseURL=https://attacker.example"); err == nil {
		t.Fatal("custom endpoint accepted")
	}
	mockHTTP(t, func(*http.Request) (*http.Response, error) { return nil, errors.New("secret-fixture-private") })
	_, err = authKey(context.Background(), "", "tskey-client-fixture")
	if err == nil || strings.Contains(err.Error(), "secret-fixture-private") {
		t.Fatal("error not redacted")
	}
}
func TestNotificationFailureReturnsRetryAndNoSecrets(t *testing.T) {
	app := &App{Secret: "secret", IDs: map[int64]bool{123: true}, Execute: func(context.Context, string, int64) string { return "done" },
		Send: func(context.Context, int64, string) error { return errors.New("private-fixture") }}
	req := httptest.NewRequest("POST", "/", strings.NewReader(payload("/dockerstart", 123, "private")))
	req.Header.Set("X-Telegram-Bot-Api-Secret-Token", "secret")
	res := httptest.NewRecorder()
	app.Webhook(res, req)
	if res.Code != 503 || strings.Contains(res.Body.String(), "private-fixture") {
		t.Fatal(res.Code, res.Body.String())
	}
	t.Setenv("TELEGRAM_BOT_TOKEN", "fixture-token")
	mockHTTP(t, func(*http.Request) (*http.Response, error) { return nil, errors.New("fixture-token private SSH data") })
	err := telegram(context.Background(), 123, "result")
	if err == nil || strings.Contains(err.Error(), "fixture-token") {
		t.Fatal("token in error")
	}
}

func TestTelegramCopyTemplateOnlyOnSecretPrompt(t *testing.T) {
	t.Setenv("TELEGRAM_BOT_TOKEN", "fixture-token")
	for _, text := range []string{secretPrompt, help, secretExpired, secretLocked, "普通通知"} {
		mockHTTP(t, func(r *http.Request) (*http.Response, error) {
			var data struct {
				Chat   int64  `json:"chat_id"`
				Text   string `json:"text"`
				Markup *struct {
					Rows [][]struct {
						Text string `json:"text"`
						Copy struct {
							Text string `json:"text"`
						} `json:"copy_text"`
						Callback *string `json:"callback_data"`
					} `json:"inline_keyboard"`
				} `json:"reply_markup"`
			}
			if json.NewDecoder(r.Body).Decode(&data) != nil || data.Chat != 123 || data.Text != text || !strings.HasSuffix(r.URL.Path, "/sendMessage") {
				t.Fatal("invalid Telegram request")
			}
			if text == secretPrompt {
				if data.Markup == nil || len(data.Markup.Rows) != 1 || len(data.Markup.Rows[0]) != 1 {
					t.Fatal("missing copy button")
				}
				button := data.Markup.Rows[0][0]
				if button.Text != "📋 一键复制密钥模板" || button.Copy.Text != "GATE_API_KEY=\nGATE_API_SECRET=" || button.Callback != nil {
					t.Fatal("incorrect copy template or callback")
				}
			} else if data.Markup != nil {
				t.Fatal("unrelated reply has keyboard")
			}
			return response(200, `{"ok":true}`), nil
		})
		if err := telegram(context.Background(), 123, text); err != nil {
			t.Fatal(err)
		}
	}
}
func TestSSHRejectsUnknownAndChangedHostKeys(t *testing.T) {
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	block, err := ssh.MarshalPrivateKey(priv, "fixture")
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatal(err)
	}
	host := "100.101.102.103:22"
	config, err := sshConfiguration(t.TempDir(), string(pem.EncodeToMemory(block)), knownhosts.Line([]string{host}, signer.PublicKey()), host)
	if err != nil || config.User != "deploy" || len(config.Auth) != 1 {
		t.Fatal("SSH configuration", err)
	}
	addr := &net.TCPAddr{IP: net.ParseIP("100.101.102.103"), Port: 22}
	if config.HostKeyCallback(host, addr, signer.PublicKey()) != nil {
		t.Fatal("trusted key rejected")
	}
	_, other, _ := ed25519.GenerateKey(rand.Reader)
	wrong, _ := ssh.NewSignerFromKey(other)
	if config.HostKeyCallback(host, addr, wrong.PublicKey()) == nil {
		t.Fatal("changed key accepted")
	}
	if config.HostKeyCallback("100.101.102.104:22", addr, signer.PublicKey()) == nil {
		t.Fatal("unknown host accepted")
	}
	if _, err := sshConfiguration(t.TempDir(), "private fixture", "invalid", host); err == nil || strings.Contains(err.Error(), "private fixture") {
		t.Fatal("private key error disclosure")
	}
}
func TestFailClosedConfiguration(t *testing.T) {
	for _, a := range []*App{{}, {Secret: "secret", PolicyErr: errors.New("invalid whitelist")}} {
		req := httptest.NewRequest("POST", "/", strings.NewReader("{}"))
		res := httptest.NewRecorder()
		a.Webhook(res, req)
		if res.Code != 503 {
			t.Fatal(res.Code)
		}
	}
	a := &App{}
	req := httptest.NewRequest("POST", "/", nil)
	res := httptest.NewRecorder()
	a.Probe(res, req)
	if res.Code != 403 {
		t.Fatal("unconfigured probe allowed")
	}
}

func TestSSHNegotiatesPinnedKeyWhenServerOffersECDSAFirst(t *testing.T) {
	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	pinned, err := ssh.NewSignerFromKey(privateKey)
	if err != nil {
		t.Fatal(err)
	}
	ecdsaKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	other, err := ssh.NewSignerFromKey(ecdsaKey)
	if err != nil {
		t.Fatal(err)
	}
	block, err := ssh.MarshalPrivateKey(privateKey, "fixture")
	if err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	serverConfig := &ssh.ServerConfig{NoClientAuth: true}
	serverConfig.AddHostKey(other)
	serverConfig.AddHostKey(pinned)
	result := make(chan error, 1)
	go func() {
		conn, acceptErr := listener.Accept()
		if acceptErr != nil {
			result <- acceptErr
			return
		}
		defer conn.Close()
		_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
		server, _, _, handshakeErr := ssh.NewServerConn(conn, serverConfig)
		if server != nil {
			_ = server.Close()
		}
		result <- handshakeErr
	}()
	address := listener.Addr().String()
	config, err := sshConfiguration(t.TempDir(), string(pem.EncodeToMemory(block)), knownhosts.Line([]string{address}, pinned.PublicKey()), address)
	if err != nil {
		t.Fatal(err)
	}
	config.Timeout = 3 * time.Second
	client, err := ssh.Dial("tcp", address, config)
	if err != nil {
		t.Fatal("server's unpinned ECDSA key displaced the pinned Ed25519 key", err)
	}
	_ = client.Close()
	if err := <-result; err != nil {
		t.Fatal(err)
	}
}
