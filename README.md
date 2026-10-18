# TGBot

Go + tsnet 版本，处理 issue #3。已完成 Vercel 入网、SSH、并发探针和 Telegram 真实启停验收，用户独立确认容器已恢复运行。生产 Webhook 使用 `/webhook-go`，Go 按用户要求采用 `TS_AUTHKEY`。证据见 [验收记录](验收记录.md)，配置见 [准备清单](准备清单.md)。

```text
Telegram → Vercel Go Function → 临时 tsnet 节点 → Tailnet SSH
         → 现有受限 remote_control.py → 固定 Docker 容器 → Telegram
```

Go 路径不调用 GitHub API，也不派发控制任务。GitHub Actions 负责远端测试和部署；旧 Python `/webhook` 和控制 workflow 保留，供回滚。

## 命令

| 私聊命令 | 作用 | 白名单 |
| --- | --- | --- |
| `/id` | 返回发送者自己的数字 ID | 不需要 |
| `/help` | 返回命令说明 | 不需要 |
| `/dockerstart` | 启动服务器预先固定的容器 | 需要 |
| `/dockerstop` | 停止服务器预先固定的容器 | 需要 |
| `/updatesecret` | 更新 Infisical 中已有的两个 Gate 密钥 | 需要 |

群聊、频道、编辑消息、机器人消息都忽略；命令不能带参数。`/id` 仅返回发送者自己的 ID，不泄露白名单。

## 私聊更新 Infisical 密钥

仅 Go `/webhook-go` 支持。白名单用户发送 `/updatesecret`，收到提示后在 5 分钟内发送一条两行文本：

```text
GATE_API_KEY=新的API_KEY
GATE_API_SECRET=新的API_SECRET
```

两个字段必须各出现一次，顺序可换；值不带引号、空格、换行或非 ASCII 字符，每项 1–512 字符。这里只验证输入格式，不调用 Gate 交易接口验证密钥是否有效或具有交易权限。普通命令仍可使用，不会作为密钥输入。

Bot 尽快调用 `deleteMessage` 删除带字段名的原消息；等待期间的格式错误文本也会删除。删除失败就不开始新的更新，并提醒手动删除。Telegram 私聊机器人消息不具备端到端加密，删除不能撤回通知预览、截图或平台副本，不应把“已删除”理解为从未传输过。

随后使用与 Orders3 一致的 Universal Auth 认证方式，按固定项目、环境、路径读取两个已有密钥，再分别 PATCH 更新并重新读取核对。不会创建缺失密钥，不提交注释、标签、名称或其他密钥配置。任一写入失败或验证不一致都不报成功。两次 PATCH 不是跨密钥事务，存在部分更新的可能；这时保留目标锁，由管理员核对后处理，不自动回滚或重试。

Redis REST 是此功能必需的共享状态服务；只保存用户/聊天/消息编号、等待截止时间、固定结果提示和处理锁。原始消息、Gate 密钥、Infisical 令牌、Machine Identity 凭据均不入库、不记录日志。Lua 脚本原子领取消息，跨请求、跨实例、跨用户共享同一目标锁。重复 Webhook 重放固定结果，不重新写密钥；进程被强杀或结果记录失败时保留锁并报告结果待确认。事件和顺序记录不自动清理，防止旧消息重新生效；等待会在 5 分钟后逻辑失效。仅接收带有效消息时间且距接收不超过 5 分钟的输入。

需要填写的六项 Infisical 变量和两项 Redis REST 变量、权限、开通步骤、解除不确定锁的方法见 [准备清单](准备清单.md)。没有配置 Redis 时，此命令安全失败，原来的 Docker 命令不依赖 Redis。此实现不读取 Supabase，也不修改其配置或数据库。

接口依据：[Universal Auth](https://infisical.com/docs/api-reference/endpoints/universal-auth/login)、[读取已有密钥](https://infisical.com/docs/api-reference/endpoints/secrets/read)、[更新接口](https://infisical.com/docs/api-reference/endpoints/secrets/update)、[Redis REST](https://upstash.com/docs/redis/features/restapi)。

## 连接与安全

- Vercel 官方 Go Runtime，`api/go.go` 导出 Handler；`/webhook-go` 是新入口，`/webhook` 保留 Python 生产链路。
- 白名单通过 Go embed 随函数打包。Webhook 密钥常量时间比较，空配置或损坏白名单拒绝请求，请求体最多 64 KiB。
- 每次控制请求独立创建 ephemeral 节点、内存状态和临时目录，不依赖跨请求文件、连接或后台进程。请求完成关闭 SSH、tsnet 并清理目录；被平台强杀时依赖 ephemeral 节点过期清理，不保证瞬时消失。
- OAuth 在请求超时上下文中申请单次使用的预批准 ephemeral 入网密钥，限定现有 `tag:tgbot-ci`。也可提供可重复使用的 ephemeral `TS_AUTHKEY`；两种方式只选一种。目标仅允许 Tailscale IPv4/IPv6，不使用公网或 LAN SSH 回退。
- 专用 SSH 私钥和可信 known_hosts 在 Vercel 环境变量中保存，固定 deploy 用户、22 端口，不使用 agent，不跳过主机身份验证。服务端 restrict、强制命令和固定容器配置保持原样。
- 服务器 SQLite 和文件锁继续负责持久去重。重复消息只表示已领取，不能证明之前成功。超时、非零退出、未知响应均不会报成功；连接中断后先查容器状态，再发送新命令。不要清理服务器去重数据库。
- 控制执行共用 48 秒预算（入网含 OAuth 最多 20 秒、TCP 连接最多 10 秒），结果通知另留 5 秒；Vercel 函数配置 60 秒。真实单次、重复和双请求并发 SSH 探针均在 4.8–7.1 秒完成；尚未做长期负载测试或单独测量内存峰值。
- 不提前 ACK，不在 HTTP 返回后执行工作。通知失败返回 503，让 Telegram 重试相同 update_id，由服务器去重防止重做。新消息之间没有顺序保证，等上一条结果再发下一条。
- SSH 输出限制内存，不打印原始异常、Token、私钥、主机输出或上游响应体。`TS_NO_LOGS_NO_SUPPORT=true` 禁止 Tailscale 调试日志上传。

## 文件和远端验证

`pkg/bot/` 包含鉴权、命令、Telegram HTTPS、OAuth 和 SSH；`api/probe.go` 的 `/tsnet-probe` 使用独立密钥，只做入网和 SSH 登录，不创建 SSH session，不执行 Docker。`scripts/remote_control.py` 保持现有服务器协议。

CI 保留 Python 回滚测试和服务器去重测试，新增 Go 安全测试、race 检查、vet 和 Linux 构建。依赖元数据由远端生成后提交。按仓库约定不在本地编译或运行测试，只做格式和差异检查。

手动运行现有部署工作流时，feature 分支只部署 Preview，不注册 Telegram Webhook；main 分支保留生产部署流程。本次已完成实际验收并启用 Go；后续更改仍须复验，失败则按准备清单回滚。不要用常驻服务器绕开 Vercel 兼容性问题。

[旧版文档](旧版说明.md) 保留 Python 回滚方案。实现依据：[Vercel Go Runtime](https://vercel.com/docs/functions/runtimes/go)、[Tailscale tsnet](https://tailscale.com/docs/features/tsnet)、[固定版本 tsnet 源码](https://github.com/tailscale/tailscale/blob/v1.96.2/tsnet/tsnet.go)。
