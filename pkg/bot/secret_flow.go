package bot

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

const secretPrompt = "请在 5 分钟内用一条消息提交以下两行（不带引号）：\nGATE_API_KEY=新的API_KEY\nGATE_API_SECRET=新的API_SECRET\n格式检查不代表交易所验证。消息删除失败时不会开始更新。"
const secretExpired = "未在等待密钥，或等待已过期；请重新发送 /updatesecret。"
const secretWorking = "该消息已领取处理，结果尚未确认；请管理员核对状态，不会自动重复更新。"
const secretLocked = "目标密钥正在更新或上次结果未确认；请稍后重试，持续出现时请管理员核对更新锁。"
const secretStateError = "状态服务不可用：请检查 Redis REST 地址、Token 和服务状态；未开始新的密钥更新。"

type stateRequest struct {
	Scope     string `json:"p_scope"`
	User      int64  `json:"p_user"`
	Chat      int64  `json:"p_chat"`
	Update    int64  `json:"p_update"`
	Message   int64  `json:"p_message"`
	Op        string `json:"p_op"`
	Result    string `json:"p_result"`
	Uncertain bool   `json:"p_uncertain"`
}
type stateResponse struct {
	State  string `json:"state"`
	Result string `json:"result"`
}
type SecretFlow struct {
	Infisical *Infisical
	State     func(context.Context, stateRequest) (stateResponse, error)
	Update    func(context.Context, GateSecrets) (string, bool)
	Now       func() time.Time
}

func productionSecrets() *SecretFlow {
	i := &Infisical{URL: envDefault("INFISICAL_URL", "https://app.infisical.com"), ClientID: os.Getenv("INFISICAL_CLIENT_ID"), ClientSecret: os.Getenv("INFISICAL_CLIENT_SECRET"), Project: os.Getenv("INFISICAL_PROJECT_ID"), Environment: envDefault("INFISICAL_ENVIRONMENT", "prod"), Path: envDefault("INFISICAL_SECRET_PATH", "/"), Client: boundedClient()}
	s := &redisState{URL: os.Getenv("UPSTASH_REDIS_REST_URL"), Key: os.Getenv("UPSTASH_REDIS_REST_TOKEN")}
	return &SecretFlow{Infisical: i, State: s.call, Update: i.Update, Now: time.Now}
}

func (a *App) secretMessage(parent context.Context, user, chat, update, message, date int64, text string, deleted bool) string {
	f := a.Secrets
	if f == nil || f.Infisical == nil || !f.Infisical.configured() {
		return "配置缺失：请补齐或修正 Infisical 的六项环境变量。"
	}
	ctx, cancel := context.WithTimeout(parent, 38*time.Second)
	defer cancel()
	// Include target location in every transaction. No credentials are sent to
	// Redis; a digest makes the scope stable and non-sensitive.
	digest := sha256.Sum256([]byte(strings.TrimRight(f.Infisical.URL, "/") + "\x00" + f.Infisical.Project + "\x00" + f.Infisical.Environment + "\x00" + f.Infisical.Path))
	q := stateRequest{Scope: hex.EncodeToString(digest[:]), User: user, Chat: chat, Update: update, Message: message, Op: "peek"}
	// Bound replay eligibility as well as session expiry; reject undated or stale
	// secret messages. Existing event records never expire automatically.
	if message <= 0 || date <= 0 || date > f.Now().Unix()+30 || f.Now().Unix()-date > 300 {
		return "消息已过期或缺少有效时间；请重新发送 /updatesecret 并提交新消息。"
	}
	r, err := f.State(ctx, q)
	if err != nil {
		return secretStateError
	}
	if r.State == "done" {
		return r.Result
	}
	if r.State == "working" {
		return secretWorking
	}
	if text == "/updatesecret" {
		q.Op = "begin"
		q.Result = secretPrompt
	} else {
		if !deleted {
			return "删除消息失败：未开始密钥更新。"
		}
		values, parseErr := parseGate(text)
		q.Op = "claim"
		if parseErr != nil {
			q.Op = "invalid"
			q.Result = parseErr.Error()
		}
		r, err = f.State(ctx, q)
		if err != nil {
			return secretStateError
		}
		if r.State != "claimed" {
			return flowReply(r)
		}
		// The atomic claim consumes the session and writes an in-progress event
		// before any external update; crashes must never lead to a second write.
		result, uncertain := f.Update(ctx, values)
		q.Op, q.Result, q.Uncertain = "finish", result, uncertain
		finishCtx, finishCancel := context.WithTimeout(parent, 4*time.Second)
		defer finishCancel()
		if _, err := f.State(finishCtx, q); err != nil {
			return "结果记录失败：操作可能已经执行，请管理员核对两个密钥和更新锁；不会自动重复更新。"
		}
		return result
	}
	r, err = f.State(ctx, q)
	if err != nil {
		return secretStateError
	}
	return flowReply(r)
}

func flowReply(r stateResponse) string {
	switch r.State {
	case "done":
		return r.Result
	case "working":
		return secretWorking
	case "locked":
		return secretLocked
	default:
		return secretExpired
	}
}

type redisState struct {
	URL, Key string
	Client   *http.Client
}

func (s *redisState) call(ctx context.Context, q stateRequest) (stateResponse, error) {
	var result stateResponse
	if !safeHTTPS(s.URL) || s.Key == "" {
		return result, errors.New("state configuration")
	}
	// All three keys share a hash slot, so the script is atomic on clustered Redis.
	prefix := "tgbot:{" + q.Scope + "}:"
	args := []any{"EVAL", redisSecretScript, 3, prefix + "session:" + intString(q.User) + ":" + intString(q.Chat), prefix + "event:" + intString(q.Update), prefix + "lock", q.Op, intString(q.User), intString(q.Chat), intString(q.Update), intString(q.Message), q.Result, strconv.FormatBool(q.Uncertain)}
	body, _ := json.Marshal(args)
	req, err := http.NewRequestWithContext(ctx, "POST", strings.TrimRight(s.URL, "/"), bytes.NewReader(body))
	if err != nil {
		return result, errors.New("state request")
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+s.Key)
	client := s.Client
	if client == nil {
		client = boundedClient()
	}
	res, err := client.Do(req)
	if err != nil {
		return result, errors.New("state request")
	}
	defer res.Body.Close()
	var envelope struct {
		Result string `json:"result"`
		Error  string `json:"error"`
	}
	if res.StatusCode != 200 || json.NewDecoder(io.LimitReader(res.Body, 65536)).Decode(&envelope) != nil || envelope.Error != "" || json.Unmarshal([]byte(envelope.Result), &result) != nil {
		return result, errors.New("state response")
	}
	if result.State == "done" && !safeFlowResult(result.Result) {
		return stateResponse{}, errors.New("state result")
	}
	return result, nil
}

func safeFlowResult(s string) bool {
	// All persisted text originates from fixed local messages, never user input or
	// upstream bodies. Reject unexpected database text rather than echoing it.
	for _, known := range safeSecretResults {
		if s == known {
			return true
		}
	}
	return false
}

var safeSecretResults = []string{
	secretPrompt,
	"格式错误：请用两行提交 GATE_API_KEY 和 GATE_API_SECRET。",
	"字段错误：只允许 GATE_API_KEY 和 GATE_API_SECRET，各填写一次。",
	"密钥格式无效：不能为空，且每项最多 512 个字符。",
	"密钥格式无效：请提交不带引号、空白或非 ASCII 字符的原始值。",
	"配置缺失：请补齐或修正 Infisical 的六项环境变量。",
	"配置错误：Infisical 请求配置无效。",
	"网络请求失败：Infisical 不可达或请求超时。",
	"认证失败：请检查 Machine Identity 凭据。",
	"权限不足：请检查目标项目、环境、路径的读取和更新权限。",
	"目标不存在：请核对项目、环境、路径及两个已有密钥；不会创建密钥。",
	"接口请求失败：Infisical 拒绝请求，请检查服务状态和密钥策略。",
	"响应错误：Infisical 返回了无效数据。",
	"认证失败：Infisical 未返回有效令牌。",
	"读取失败：无法读取指定密钥的完整值。",
	"更新验证失败：无法重新读取两个密钥；请管理员核对后解除更新锁。",
	"更新验证失败：读取值与提交值不一致；请管理员核对后解除更新锁。",
	"Infisical 密钥更新成功，GATE_API_KEY 和 GATE_API_SECRET 已更新。",
}

func init() {
	base := append([]string(nil), safeSecretResults...)
	for _, s := range base {
		safeSecretResults = append(safeSecretResults, s+" 可能已部分更新，请管理员核对两个值后解除更新锁。")
	}
}

func intString(n int64) string { return strconv.FormatInt(n, 10) }
