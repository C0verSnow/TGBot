package bot

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

var gateNames = [2]string{"GATE_API_KEY", "GATE_API_SECRET"}

type GateSecrets [2]string

// Validate syntax, never claim to validate exchange permissions or key activity.
func parseGate(text string) (GateSecrets, error) {
	var values GateSecrets
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	if len(lines) != 2 {
		return values, errors.New("格式错误：请用两行提交 GATE_API_KEY 和 GATE_API_SECRET。")
	}
	seen := map[string]bool{}
	for _, line := range lines {
		name, value, ok := strings.Cut(line, "=")
		if !ok || seen[name] || (name != gateNames[0] && name != gateNames[1]) {
			return GateSecrets{}, errors.New("字段错误：只允许 GATE_API_KEY 和 GATE_API_SECRET，各填写一次。")
		}
		if len(value) == 0 || len(value) > 512 {
			return GateSecrets{}, errors.New("密钥格式无效：不能为空，且每项最多 512 个字符。")
		}
		for _, c := range value {
			if c < 33 || c > 126 || c == '"' || c == '\'' || c == '`' {
				return GateSecrets{}, errors.New("密钥格式无效：请提交不带引号、空白或非 ASCII 字符的原始值。")
			}
		}
		seen[name] = true
		if name == gateNames[0] {
			values[0] = value
		} else {
			values[1] = value
		}
	}
	return values, nil
}

type Infisical struct {
	URL, ClientID, ClientSecret, Project, Environment, Path string
	Client                                                  *http.Client
}

func envDefault(name, fallback string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return fallback
}

func safeHTTPS(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && u.Scheme == "https" && u.Hostname() != "" && u.User == nil && u.RawQuery == "" && u.Fragment == "" && (u.Path == "" || u.Path == "/")
}

func (i *Infisical) configured() bool {
	return safeHTTPS(i.URL) && i.ClientID != "" && i.ClientSecret != "" && i.Project != "" && i.Environment != "" && strings.HasPrefix(i.Path, "/")
}

// Errors are fixed labels. HTTP bodies, tokens and transport errors never escape.
func (i *Infisical) request(ctx context.Context, method, path, token string, payload any, output any) error {
	var body []byte
	if payload != nil {
		body, _ = json.Marshal(payload)
	}
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(i.URL, "/")+path, bytes.NewReader(body))
	if err != nil {
		return errors.New("配置错误：Infisical 请求配置无效。")
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	res, err := i.Client.Do(req)
	if err != nil {
		return errors.New("网络请求失败：Infisical 不可达或请求超时。")
	}
	defer res.Body.Close()
	switch res.StatusCode {
	case 401:
		return errors.New("认证失败：请检查 Machine Identity 凭据。")
	case 403:
		return errors.New("权限不足：请检查目标项目、环境、路径的读取和更新权限。")
	case 404:
		return errors.New("目标不存在：请核对项目、环境、路径及两个已有密钥；不会创建密钥。")
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return errors.New("接口请求失败：Infisical 拒绝请求，请检查服务状态和密钥策略。")
	}
	if output != nil && json.NewDecoder(io.LimitReader(res.Body, 65536)).Decode(output) != nil {
		return errors.New("响应错误：Infisical 返回了无效数据。")
	}
	return nil
}

func (i *Infisical) read(ctx context.Context, token, name string) (string, error) {
	q := url.Values{"projectId": {i.Project}, "environment": {i.Environment}, "secretPath": {i.Path}, "type": {"shared"}, "expandSecretReferences": {"false"}, "includeImports": {"false"}}
	var result struct {
		Secret struct {
			Key    string  `json:"secretKey"`
			Value  *string `json:"secretValue"`
			Hidden bool    `json:"secretValueHidden"`
		} `json:"secret"`
	}
	if err := i.request(ctx, "GET", "/api/v4/secrets/"+name+"?"+q.Encode(), token, nil, &result); err != nil {
		return "", err
	}
	if result.Secret.Key != name || result.Secret.Value == nil || result.Secret.Hidden {
		return "", errors.New("读取失败：无法读取指定密钥的完整值。")
	}
	return *result.Secret.Value, nil
}

// uncertain stays true after the first PATCH attempt until both readbacks match.
// The caller keeps the durable target lock on any uncertain outcome.
func (i *Infisical) Update(ctx context.Context, values GateSecrets) (message string, uncertain bool) {
	if !i.configured() {
		return "配置缺失：请补齐或修正 Infisical 的六项环境变量。", false
	}
	var auth struct {
		Token string `json:"accessToken"`
	}
	if err := i.request(ctx, "POST", "/api/v1/auth/universal-auth/login", "", map[string]string{"clientId": i.ClientID, "clientSecret": i.ClientSecret}, &auth); err != nil {
		return err.Error(), false
	}
	if auth.Token == "" {
		return "认证失败：Infisical 未返回有效令牌。", false
	}
	for _, name := range gateNames {
		if _, err := i.read(ctx, auth.Token, name); err != nil {
			return err.Error(), false
		}
	}
	for n, name := range gateNames {
		payload := map[string]string{"projectId": i.Project, "environment": i.Environment, "secretPath": i.Path, "type": "shared", "secretValue": values[n]}
		if err := i.request(ctx, "PATCH", "/api/v4/secrets/"+name, auth.Token, payload, nil); err != nil {
			return err.Error() + " 可能已部分更新，请管理员核对两个值后解除更新锁。", true
		}
	}
	for n, name := range gateNames {
		value, err := i.read(ctx, auth.Token, name)
		if err != nil {
			return "更新验证失败：无法重新读取两个密钥；请管理员核对后解除更新锁。", true
		}
		if value != values[n] {
			return "更新验证失败：读取值与提交值不一致；请管理员核对后解除更新锁。", true
		}
	}
	return "Infisical 密钥更新成功，GATE_API_KEY 和 GATE_API_SECRET 已更新。", false
}

func boundedClient() *http.Client {
	return &http.Client{Timeout: 5 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}
