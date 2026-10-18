# TGBot

通过 Telegram 私聊，用 `/dockerstart`、`/dockerstop` 控制一个预先配置的 Docker 容器。

```text
Telegram → Vercel Python Webhook（验证密钥、用户白名单）
         → GitHub Actions（再次检查白名单、临时加入 Tailscale）
         → OpenSSH / deploy（检查服务器主机公钥、专用受限密钥）
         → 固定 Docker 操作 → Telegram 最终结果
```

从 [准备清单.md](准备清单.md) 开始。默认白名单为空，默认不会操作任何服务器。

## 为什么这样连接

Vercel 函数和 Actions runner 是两台独立机器，Actions 的 Tailscale 连接不能供 Vercel 使用。本项目把需要私网和 SSH 的工作交给短期 Actions runner，Vercel 只做有超时的 HTTPS 调用，不启动后台线程、不运行 Polling、不在请求内等待 Docker。Tailscale 官方提供 runner 加入 Tailnet 的 Action：[官方说明](https://tailscale.com/docs/solutions/connect-github-cicd-workflows-to-private-infrastructure-without-public-exposure)。

这是一套异步控制方案：正常也可能等待几十秒至数分钟，Actions 排队时间没有保证。每发一条命令会使用 Actions 配额。需要秒级响应时，应换成 Tailnet 内常驻任务服务；本项目没有假装 Vercel 自带私网。

## 安全边界与使用限制

- 只接受白名单用户的私聊原始消息；群聊、频道、编辑消息都忽略。
- `X-Telegram-Bot-Api-Secret-Token` 使用常量时间比较；空配置拒绝请求。
- 消息中不能指定容器名、命令参数或 Bash；服务器公钥必须事先从可信控制台取得。
- Vercel 没有 SSH 私钥；Actions 用固定 `deploy` 用户和 RejectPolicy 验证主机。
- 专用 SSH 公钥必须带强制命令及 `restrict`。服务器脚本从环境变量读取固定容器名，用参数列表执行 Docker，禁用 Shell。
- 服务器 SQLite 持久记录 update_id，文件锁把重复消息检查和 Docker 执行串起来；Webhook 重试即使创建多个任务，也不会再次操作同一消息。不要删除 `~deploy/.local/state/tgbot`。数据库随消息数增长，应备份并监控磁盘。
- 为避免崩溃后重复操作，先记录消息再执行 Docker。任务崩溃、超时或回复失败时，结果可能不确定；先查看真实容器状态，再发送新消息。重复消息提示仅表示已领取，不证明之前成功。
- Actions 不保证不同消息的执行顺序；不要连续交替发送启动、停止，等上一条结果回来再发下一条。不配置会取消排队任务的 concurrency。
- Telegram 通知失败、Actions 服务故障或配额耗尽可能导致收不到回复；以 Actions 日志和服务器实际状态为准。代码不输出原始异常、SSH 输出或 Token。
- Docker 组基本等同宿主机管理员权限。仅给机器人专用密钥装受限入口，避免复用管理员密钥；有更严格隔离需求时应另行使用 rootless Docker。

## 文件和验证

`main.py` / `api/index.py` 是 Webhook；`scripts/worker.py` 是 Actions 执行器；`scripts/remote_control.py` 安装到服务器；`scripts/set_webhook.py` 注册 Webhook。`ci.yml` 不读取真实密钥，在远端运行自动测试；`deploy.yml` 在远端测试后使用 Vercel 远端构建部署；`docker-control.yml` 执行真实控制任务。

遵守本仓库约定，不做本地构建、编译、测试运行。远端测试覆盖鉴权、命令限制、Webhook 重试、主机校验设置、SSH 错误、超时及消息去重，使用模拟服务，不会启停真实容器。真实端到端验收步骤见准备清单。

依据：[Vercel Python 入口](https://vercel.com/docs/functions/runtimes/python)、[Telegram Webhook](https://core.telegram.org/bots/api#setwebhook)、[GitHub workflow dispatch](https://docs.github.com/en/rest/actions/workflows#create-a-workflow-dispatch-event)。
