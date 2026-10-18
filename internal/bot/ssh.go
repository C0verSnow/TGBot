package bot

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
	"tailscale.com/ipn/store/mem"
	"tailscale.com/tsnet"
)

const uncertain = "连接或执行未完成，结果暂不确定。请先查看容器状态，再发送新命令。"

// limitedBuffer drains SSH output without allowing unbounded memory use.
type limitedBuffer struct{ bytes.Buffer }

func (b *limitedBuffer) Write(p []byte) (int, error) {
	n := len(p)
	if b.Len() < 4097 {
		_, _ = b.Buffer.Write(p[:min(n, 4097-b.Len())])
	}
	return n, nil
}

func tailnetHost(host string) bool {
	ip, err := netip.ParseAddr(host)
	return err == nil && (netip.MustParsePrefix("100.64.0.0/10").Contains(ip) || netip.MustParsePrefix("fd7a:115c:a1e0::/48").Contains(ip))
}

func connect(ctx context.Context) (*ssh.Client, func(), error) {
	host := strings.TrimSpace(os.Getenv("SSH_HOST"))
	key, hosts := os.Getenv("SSH_PRIVATE_KEY"), os.Getenv("SSH_KNOWN_HOSTS")
	auth, oauth := os.Getenv("TS_AUTHKEY"), os.Getenv("TS_OAUTH_SECRET")
	// Only Tailnet IPs; never fall back to the public network or ambient SSH agent.
	if !tailnetHost(host) || key == "" || hosts == "" || (auth == "" && oauth == "") {
		return nil, nil, errors.New("configuration")
	}
	joinCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	auth, err := authKey(joinCtx, auth, oauth)
	if err != nil {
		return nil, nil, err
	}
	signer, err := ssh.ParsePrivateKey([]byte(key))
	if err != nil {
		return nil, nil, errors.New("configuration")
	}
	dir, err := os.MkdirTemp("", "tgbot-")
	if err != nil {
		return nil, nil, errors.New("temporary storage")
	}
	cleanup := func() { _ = os.RemoveAll(dir) }
	path := filepath.Join(dir, "known_hosts")
	if os.WriteFile(path, []byte(hosts+"\n"), 0600) != nil {
		cleanup()
		return nil, nil, errors.New("configuration")
	}
	verify, err := knownhosts.New(path)
	if err != nil {
		cleanup()
		return nil, nil, errors.New("configuration")
	}
	server := &tsnet.Server{Dir: dir, Store: new(mem.Store), Hostname: filepath.Base(dir), Ephemeral: true,
		AuthKey: auth, AdvertiseTags: []string{"tag:tgbot-ci"},
		UserLogf: func(string, ...any) {}, Logf: func(string, ...any) {}}
	// Start synchronously before Close; Up and Dial are bounded by context.
	if err := server.Start(); err != nil {
		cleanup()
		return nil, nil, errors.New("tailnet start")
	}
	cleanup = func() { _ = server.Close(); _ = os.RemoveAll(dir) }
	_, err = server.Up(joinCtx)
	cancel()
	if err != nil {
		cleanup()
		return nil, nil, errors.New("tailnet join")
	}
	addr := net.JoinHostPort(host, "22")
	dialCtx, stop := context.WithTimeout(ctx, 10*time.Second)
	conn, err := server.Dial(dialCtx, "tcp", addr)
	stop()
	if err != nil {
		cleanup()
		return nil, nil, errors.New("tailnet dial")
	}
	deadline, _ := ctx.Deadline()
	_ = conn.SetDeadline(minDeadline(deadline, time.Now().Add(10*time.Second)))
	stopClose := context.AfterFunc(ctx, func() { _ = conn.Close() })
	c, chans, reqs, err := ssh.NewClientConn(conn, addr, &ssh.ClientConfig{
		User: "deploy", Auth: []ssh.AuthMethod{ssh.PublicKeys(signer)}, HostKeyCallback: verify,
	})
	if err != nil {
		stopClose()
		_ = conn.Close()
		cleanup()
		return nil, nil, errors.New("SSH verification or authentication")
	}
	_ = conn.SetDeadline(deadline)
	client := ssh.NewClient(c, chans, reqs)
	closeServer := cleanup
	cleanup = func() { stopClose(); _ = client.Close(); closeServer() }
	return client, cleanup, nil
}
func minDeadline(a, b time.Time) time.Time {
	if a.IsZero() || b.Before(a) {
		return b
	}
	return a
}

func probe(ctx context.Context) error {
	client, closeAll, err := connect(ctx)
	if err != nil {
		return err
	}
	_ = client
	defer closeAll()
	// Authentication only: no NewSession, exec, shell, or Docker request.
	return nil
}

func execute(ctx context.Context, action string, id int64) string {
	if (action != "start" && action != "stop") || id < 0 {
		return "拒绝无效命令。"
	}
	client, closeAll, err := connect(ctx)
	if err != nil {
		return uncertain
	}
	defer closeAll()
	session, err := client.NewSession()
	if err != nil {
		return uncertain
	}
	defer session.Close()
	out := &limitedBuffer{}
	session.Stdout, session.Stderr = out, io.Discard
	// Server's forced command remains responsible for durable SQLite deduplication.
	if session.Run(action+" "+strconv.FormatInt(id, 10)) != nil {
		return uncertain
	}
	return resultMessage(out.Bytes(), action)
}
func resultMessage(data []byte, action string) string {
	if len(data) > 4096 {
		return uncertain
	}
	var res struct {
		Result string `json:"result"`
	}
	if json.Unmarshal(data, &res) != nil {
		return uncertain
	}
	switch res.Result {
	case "ok":
		if action == "start" {
			return "容器启动成功。"
		}
		return "容器停止成功。"
	case "duplicate":
		return "这条指令已经领取，本次未重复执行；之前的结果可能不确定，请查看容器状态。"
	case "failed":
		return "Docker 操作失败，请检查固定容器、权限和 Docker 服务。"
	case "timeout":
		return "Docker 操作超时，结果暂不确定，请先查看容器状态。"
	default:
		return uncertain
	}
}
