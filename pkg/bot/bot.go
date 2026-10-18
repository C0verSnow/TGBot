// Package bot implements synchronous, bounded Telegram requests. No background jobs.
package bot

import (
	"bytes"
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/C0verSnow/TGBot/pkg/allowlist"
)

const help = "/id：查看你的 Telegram 用户 ID\n/help：查看可用命令\n/dockerstart：启动固定容器（仅白名单）\n/dockerstop：停止固定容器（仅白名单）\n/updatesecret：更新 Infisical 的两个 Gate 密钥（仅白名单）\n请私聊发送完整命令，不要带参数。"

// Execute's empty action is a handshake-only probe; it must not open a session.
type App struct {
	Secret, ProbeSecret string
	IDs                 map[int64]bool
	PolicyErr           error
	Send                func(context.Context, int64, string) error
	Execute             func(context.Context, string, int64) string
	Check               func(context.Context) error
	Secrets             *SecretFlow
	Delete              func(context.Context, int64, int64) error
}

func Production() *App {
	ids, err := ParseIDs(allowlist.Whitelist)
	return &App{Secret: os.Getenv("TELEGRAM_WEBHOOK_SECRET"), ProbeSecret: os.Getenv("TSNET_PROBE_SECRET"),
		IDs: ids, PolicyErr: err, Send: telegram, Execute: execute, Check: probe,
		Secrets: productionSecrets(), Delete: telegramDelete}
}

func ParseIDs(text string) (map[int64]bool, error) {
	ids := map[int64]bool{}
	for _, line := range strings.Split(text, "\n") {
		value := strings.TrimSpace(strings.SplitN(line, "#", 2)[0])
		if value == "" {
			continue
		}
		n, err := strconv.ParseInt(value, 10, 64)
		if err != nil || n <= 0 || strconv.FormatInt(n, 10) != value {
			return nil, errors.New("invalid whitelist")
		}
		ids[n] = true
	}
	return ids, nil
}

type Update struct {
	ID      *int64 `json:"update_id"`
	Message *struct {
		ID   int64 `json:"message_id"`
		Date int64 `json:"date"`
		From *struct {
			ID  int64 `json:"id"`
			Bot bool  `json:"is_bot"`
		} `json:"from"`
		Chat *struct {
			ID   int64  `json:"id"`
			Type string `json:"type"`
		} `json:"chat"`
		Text string `json:"text"`
	} `json:"message"`
}

func authorized(supplied, expected string) bool {
	return expected != "" && subtle.ConstantTimeCompare([]byte(supplied), []byte(expected)) == 1
}
func reply(w http.ResponseWriter, status int, ok bool) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]bool{"ok": ok})
}
func (a *App) Webhook(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		reply(w, 200, true)
		return
	}
	if r.Method != http.MethodPost {
		reply(w, 405, false)
		return
	}
	if a.Secret == "" || a.PolicyErr != nil {
		reply(w, 503, false)
		return
	}
	if !authorized(r.Header.Get("X-Telegram-Bot-Api-Secret-Token"), a.Secret) {
		reply(w, 403, false)
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 65536))
	if err != nil {
		reply(w, 413, false)
		return
	}
	var u Update
	if json.Unmarshal(body, &u) != nil {
		reply(w, 400, false)
		return
	}
	m := u.Message
	if u.ID == nil || *u.ID < 0 || m == nil || m.From == nil || m.Chat == nil || m.From.Bot ||
		m.From.ID <= 0 || m.Chat.ID != m.From.ID || m.Chat.Type != "private" {
		reply(w, 200, true)
		return
	}
	text := strings.TrimSpace(m.Text)
	var result string
	// Remove recognizable credentials even when the sender is unauthorized or
	// their session has expired. Never send message contents to the state store.
	sensitive := strings.Contains(m.Text, "GATE_API_KEY") || strings.Contains(m.Text, "GATE_API_SECRET")
	deleted := true
	if sensitive {
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		deleted = a.Delete != nil && m.ID > 0 && a.Delete(ctx, m.Chat.ID, m.ID) == nil
		cancel()
	}
	switch text {
	case "/id":
		result = fmt.Sprintf("你的 Telegram 用户 ID：%d", m.From.ID)
	case "/help":
		result = help
	default:
		if !a.IDs[m.From.ID] {
			result = "无权限执行此操作"
		} else {
			switch text {
			case "/updatesecret":
				result = a.secretMessage(r.Context(), m.From.ID, m.Chat.ID, *u.ID, m.ID, m.Date, text, deleted)
			case "/dockerstart", "/dockerstop":
				ctx, cancel := context.WithTimeout(r.Context(), 48*time.Second)
				result = a.Execute(ctx, map[string]string{"/dockerstart": "start", "/dockerstop": "stop"}[text], *u.ID)
				cancel()
			default:
				if sensitive || !strings.HasPrefix(text, "/") {
					result = a.secretMessage(r.Context(), m.From.ID, m.Chat.ID, *u.ID, m.ID, m.Date, m.Text, deleted)
				} else {
					result = help
				}
			}
		}
	}
	if sensitive && !deleted {
		result += "\n原始消息删除未确认，请立即手动删除；本次不会开始新的密钥更新。"
	}
	// Even after execution expires, reserve time for the result notification.
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	if a.Send(ctx, m.Chat.ID, result) != nil {
		reply(w, 503, false)
		return
	}
	reply(w, 200, true)
}

func (a *App) Probe(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		reply(w, 405, false)
		return
	}
	if !authorized(r.Header.Get("X-TGBot-Probe-Secret"), a.ProbeSecret) {
		reply(w, 403, false)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 48*time.Second)
	defer cancel()
	if err := a.Check(ctx); err != nil {
		// Only fixed stage labels reach the authenticated probe; never raw errors.
		stage := "connection"
		switch err.Error() {
		case "configuration", "temporary storage", "SSH private key configuration", "SSH host configuration",
			"choose one Tailnet credential", "invalid auth key", "invalid OAuth configuration",
			"OAuth request", "OAuth authentication", "auth key request", "auth key creation",
			"tailnet start", "tailnet join", "tailnet dial", "SSH verification or authentication":
			stage = err.Error()
		}
		w.Header().Set("X-TGBot-Probe-Stage", stage)
		reply(w, 503, false)
		return
	}
	reply(w, 200, true)
}

func telegram(ctx context.Context, chat int64, text string) error {
	payload := map[string]any{"chat_id": chat, "text": text}
	if text == secretPrompt {
		payload["reply_markup"] = map[string]any{"inline_keyboard": [][]any{{
			map[string]any{"text": "📋 一键复制密钥模板", "copy_text": map[string]string{"text": secretCopyTemplate}},
		}}}
	}
	return telegramCall(ctx, "sendMessage", payload)
}

func telegramDelete(ctx context.Context, chat, message int64) error {
	return telegramCall(ctx, "deleteMessage", map[string]any{"chat_id": chat, "message_id": message})
}

func telegramCall(ctx context.Context, method string, payload map[string]any) error {
	token := os.Getenv("TELEGRAM_BOT_TOKEN")
	if token == "" {
		return errors.New("notification failed")
	}
	body, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.telegram.org/bot"+token+"/"+method, bytes.NewReader(body))
	if err != nil {
		return errors.New("notification failed")
	}
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 5 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	res, err := client.Do(req)
	if err != nil {
		return errors.New("notification failed")
	}
	defer res.Body.Close()
	var result struct {
		OK bool `json:"ok"`
	}
	if res.StatusCode != 200 || json.NewDecoder(io.LimitReader(res.Body, 65536)).Decode(&result) != nil || !result.OK {
		return errors.New("notification failed")
	}
	return nil
}
