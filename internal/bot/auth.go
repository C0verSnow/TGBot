package bot

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Mint a single-use ephemeral auth key with request cancellation. tsnet's built-in
// OAuth path uses a server lifetime context during synchronous Start instead.
func authKey(ctx context.Context, key, secret string) (string, error) {
	if key != "" {
		if !strings.HasPrefix(key, "tskey-auth-") {
			return "", errors.New("invalid auth key")
		}
		return key, nil
	}
	if !strings.HasPrefix(secret, "tskey-client-") || strings.Contains(secret, "?") {
		return "", errors.New("invalid OAuth configuration")
	}
	client := &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	form := url.Values{"client_id": {"tgbot"}, "client_secret": {secret}, "grant_type": {"client_credentials"}}
	req, err := http.NewRequestWithContext(ctx, "POST", "https://api.tailscale.com/api/v2/oauth/token", strings.NewReader(form.Encode()))
	if err != nil {
		return "", errors.New("OAuth request")
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	res, err := client.Do(req)
	if err != nil {
		return "", errors.New("OAuth request")
	}
	var token struct {
		Token string `json:"access_token"`
	}
	err = json.NewDecoder(io.LimitReader(res.Body, 65536)).Decode(&token)
	_ = res.Body.Close()
	if err != nil || res.StatusCode != 200 || token.Token == "" {
		return "", errors.New("OAuth authentication")
	}
	payload := []byte(`{"capabilities":{"devices":{"create":{"reusable":false,"ephemeral":true,"preauthorized":true,"tags":["tag:tgbot-ci"]}}},"expirySeconds":300}`)
	req, err = http.NewRequestWithContext(ctx, "POST", "https://api.tailscale.com/api/v2/tailnet/-/keys", bytes.NewReader(payload))
	if err != nil {
		return "", errors.New("auth key request")
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token.Token)
	res, err = client.Do(req)
	if err != nil {
		return "", errors.New("auth key request")
	}
	defer res.Body.Close()
	var result struct {
		Key string `json:"key"`
	}
	if json.NewDecoder(io.LimitReader(res.Body, 65536)).Decode(&result) != nil || res.StatusCode != 200 || !strings.HasPrefix(result.Key, "tskey-auth-") {
		return "", errors.New("auth key creation")
	}
	return result.Key, nil
}
