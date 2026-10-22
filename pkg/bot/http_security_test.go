package bot

import (
	"context"
	"crypto/x509"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
)

func TestConnectRejectedBeforeAnyWork(t *testing.T) {
	a := &App{Secret: "webhook", ProbeSecret: "probe", Check: func(context.Context) error {
		t.Fatal("CONNECT joined tailnet")
		return nil
	}, Send: func(context.Context, int64, string) error {
		t.Fatal("CONNECT sent notification")
		return nil
	}}
	for _, handler := range []http.HandlerFunc{a.Webhook, a.Probe} {
		r := httptest.NewRequest(http.MethodConnect, "https://example.test/", strings.NewReader("untrusted body"))
		r.Header.Set("X-Telegram-Bot-Api-Secret-Token", "webhook")
		r.Header.Set("X-TGBot-Probe-Secret", "probe")
		w := httptest.NewRecorder()
		handler(w, r)
		if w.Code != http.StatusMethodNotAllowed {
			t.Fatal("CONNECT must never receive a tunnel success response")
		}
	}
}

func TestRuntimeInspectionRequiresProbeSecretAndHasNoSideEffects(t *testing.T) {
	a := &App{ProbeSecret: "probe", Check: func(context.Context) error {
		t.Fatal("runtime inspection joined tailnet")
		return nil
	}}
	for _, secret := range []string{"", "wrong", "probe"} {
		r := httptest.NewRequest(http.MethodGet, "/tsnet-probe", nil)
		r.Header.Set("X-TGBot-Probe-Secret", secret)
		w := httptest.NewRecorder()
		a.Probe(w, r)
		if secret == "probe" {
			if w.Code != 200 || w.Header().Get("X-TGBot-Go-Version") != runtime.Version() || w.Header().Get("Cache-Control") != "no-store" {
				t.Fatal("missing authenticated runtime evidence")
			}
		} else if w.Code != 403 || w.Header().Get("X-TGBot-Go-Version") != "" {
			t.Fatal("unauthenticated runtime disclosure")
		}
	}
}

// Exercise real TLS verification using an isolated test CA and server.
// A test-only transport rewrites endpoints; production transport is unchanged.
func TestHTTPSClientsVerifyCertificates(t *testing.T) {
	var calls atomic.Int32
	s := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Method != http.MethodPost {
			t.Error("unexpected outbound method")
		}
		switch r.URL.Path {
		case "/api/v2/oauth/token":
			_, _ = io.WriteString(w, `{"access_token":"fixture-token"}`)
		case "/api/v2/tailnet/-/keys":
			_, _ = io.WriteString(w, `{"key":"tskey-auth-fixture"}`)
		default:
			_, _ = io.WriteString(w, `{"ok":true}`)
		}
	}))
	defer s.Close()
	s.Config.ErrorLog = log.New(io.Discard, "", 0)
	t.Setenv("TELEGRAM_BOT_TOKEN", "fixture")
	for _, trusted := range []bool{false, true} {
		tr := s.Client().Transport.(*http.Transport).Clone()
		if !trusted {
			tr.TLSClientConfig.RootCAs = x509.NewCertPool()
		}
		t.Cleanup(tr.CloseIdleConnections)
		// Use the test certificate's IP for hostname verification, preserving
		// the original method/path/body used by each production request.
		old := http.DefaultTransport
		http.DefaultTransport = transportFunc(func(r *http.Request) (*http.Response, error) {
			copy := r.Clone(r.Context())
			copy.URL.Scheme = "https"
			copy.URL.Host = strings.TrimPrefix(s.URL, "https://")
			return tr.RoundTrip(copy)
		})
		t.Cleanup(func() { http.DefaultTransport = old })
		before := calls.Load()
		errTelegram := telegramCall(context.Background(), "sendMessage", map[string]any{"chat_id": 123, "text": "fixture"})
		_, errOAuth := authKey(context.Background(), "", "tskey-client-fixture")
		i := &Infisical{URL: s.URL, Client: boundedClient()}
		errInfisical := i.request(context.Background(), http.MethodPost, "/api/v1/auth/universal-auth/login", "", map[string]string{"clientSecret": "fixture"}, nil)
		for _, err := range []error{errTelegram, errOAuth, errInfisical} {
			if (err == nil) != trusted {
				t.Fatal("TLS trust decision was not enforced")
			}
		}
		if !trusted && calls.Load() != before {
			t.Fatal("untrusted peer received application credentials")
		}
		if trusted && calls.Load()-before != 4 {
			t.Fatal("trusted TLS path did not complete")
		}
	}
}

func TestHTTPSClientsDoNotFollowRedirects(t *testing.T) {
	var redirects atomic.Int32
	s := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/leak" {
			redirects.Add(1)
		}
		http.Redirect(w, r, "/leak", http.StatusTemporaryRedirect)
	}))
	defer s.Close()
	tr := s.Client().Transport.(*http.Transport).Clone()
	defer tr.CloseIdleConnections()
	mockHTTP(t, func(r *http.Request) (*http.Response, error) {
		copy := r.Clone(r.Context())
		copy.URL.Scheme = "https"
		copy.URL.Host = strings.TrimPrefix(s.URL, "https://")
		return tr.RoundTrip(copy)
	})
	t.Setenv("TELEGRAM_BOT_TOKEN", "fixture")
	if telegramCall(context.Background(), "sendMessage", map[string]any{}) == nil {
		t.Fatal("Telegram redirect accepted")
	}
	if _, err := authKey(context.Background(), "", "tskey-client-fixture"); err == nil {
		t.Fatal("OAuth redirect accepted")
	}
	i := &Infisical{URL: s.URL, Client: boundedClient()}
	if i.request(context.Background(), http.MethodPost, "/login", "", nil, nil) == nil {
		t.Fatal("Infisical redirect accepted")
	}
	if redirects.Load() != 0 {
		t.Fatal("credentials followed a redirect")
	}
}
