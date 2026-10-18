# TGBot

Go + tsnet 迁移版本，处理 issue #3。当前生产仍走 Python；Go 版尚未完成 Vercel 私网和真实启停验收，不能当作已可投入生产。配置和后续步骤见 [准备清单](准备清单.md)。

```text
Telegram → Vercel Go Function → 临时 tsnet 节点 → Tailnet SSH
         → 现有受限 remote_control.py → 固定 Docker 容器 → Telegram
```

Go 路径不调用 GitHub API，也不派发控制任务。GitHub Actions 负责远端测试和部署；旧的控制 workflow 为 Python 回滚保留。用户未配置 Preview 前，不切换生产 Webhook。

## 命令

| 私聊命令 | 作用 | 白名单 |
| --- | --- | --- |
| `/id` | 返回发送者自己的数字 ID | 不需要 |
| `/help` | 返回命令说明 | 不需要 |
| `/dockerstart` | 启动服务器预先固定的容器 | 需要 |
| `/dockerstop` | 停止服务器预先固定的容器 | 需要 |

群聊、频道、编辑消息、机器人消息都忽略；命令不能带参数。`/id` 仅返回发送者自己的 ID，不泄露白名单。

## 连接与安全

- Vercel 官方 Go Runtime，`api/go.go` 导出 Handler；`/webhook-go` 是新入口，`/webhook` 保留 Python 生产链路。
- 白名单通过 Go embed 随函数打包。Webhook 密钥常量时间比较，空配置或损坏白名单拒绝请求，请求体最多 64 KiB。
- 每次控制请求独立创建 ephemeral 节点、内存状态和临时目录，不依赖跨请求文件、连接或后台进程。请求完成关闭 SSH、tsnet 并清理目录；被平台强杀时依赖 ephemeral 节点过期清理，不保证瞬时消失。
- OAuth 在请求超时上下文中申请单次使用的预批准 ephemeral 入网密钥，限定现有 `tag:tgbot-ci`。也可提供可重复使用的 ephemeral `TS_AUTHKEY`；两种方式只选一种。目标仅允许 Tailscale IPv4/IPv6，不使用公网或 LAN SSH 回退。
- 专用 SSH 私钥和可信 known_hosts 在 Vercel 环境变量中保存，固定 deploy 用户、22 端口，不使用 agent，不跳过主机身份验证。服务端 restrict、强制命令和固定容器配置保持原样。
- 服务器 SQLite 和文件锁继续负责持久去重。重复消息只表示已领取，不能证明之前成功。超时、非零退出、未知响应均不会报成功；连接中断后先查容器状态，再发送新命令。不要清理服务器去重数据库。
- 控制执行共用 48 秒预算（入网含 OAuth 最多 20 秒、TCP 连接最多 10 秒），结果通知另留 5 秒；Vercel 函数配置 60 秒。真实冷启动、资源使用和完成率仍需部署验证。
- 不提前 ACK，不在 HTTP 返回后执行工作。通知失败返回 503，让 Telegram 重试相同 update_id，由服务器去重防止重做。新消息之间没有顺序保证，等上一条结果再发下一条。
- SSH 输出限制内存，不打印原始异常、Token、私钥、主机输出或上游响应体。`TS_NO_LOGS_NO_SUPPORT=true` 禁止 Tailscale 调试日志上传。

## 文件和远端验证

`internal/bot/` 包含鉴权、命令、Telegram HTTPS、OAuth 和 SSH；`api/probe.go` 的 `/tsnet-probe` 使用独立密钥，只做入网和 SSH 登录，不创建 SSH session，不执行 Docker。`scripts/remote_control.py` 保持现有服务器协议。

CI 保留 Python 回滚测试和服务器去重测试，新增 Go 安全测试、race 检查、vet 和 Linux 构建。依赖元数据由远端生成后提交。按仓库约定不在本地编译或运行测试，只做格式和差异检查。

手动运行现有部署工作流时，feature 分支只部署 Preview，不注册 Telegram Webhook；main 分支保留生产部署流程。正式切换前必须按准备清单完成实际验收，失败则保留 Python。不要用常驻服务器绕开 Vercel 兼容性问题。

[旧版文档](旧版说明.md) 保留 Python 回滚方案。实现依据：[Vercel Go Runtime](https://vercel.com/docs/functions/runtimes/go)、[Tailscale tsnet](https://tailscale.com/docs/features/tsnet)、[固定版本 tsnet 源码](https://github.com/tailscale/tailscale/blob/v1.96.2/tsnet/tsnet.go)。
