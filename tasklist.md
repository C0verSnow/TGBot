# 本文档为4象限的任务清单，全文书写大白话

## 想做：
- 完成 issue #1，让白名单用户通过 Telegram 启停指定 Docker 容器。

## 做完：
- 2026-10-17：从 GitHub 同步仓库，在 feature/issue-1-telegram-docker 分支工作。
- 确认 Vercel 不继承 Actions 的 Tailscale 网络，采用 Vercel 收消息、Actions 临时连服务器的方案。
- 完成 Webhook 验证、两端白名单检查、固定启动停止命令、SSH 主机身份检查和专用受限密钥入口。
- 完成服务器消息去重、文件锁、超时处理和中文结果回复。
- 补齐变量示例、中文准备清单、远端 CI、Vercel 部署及 Webhook 注册工作流。
- Python 静态语法、JSON 格式及差异检查通过，没有执行本地构建、编译或测试运行。
- GitHub 远端 CI 的 11 项模拟测试全部通过：https://github.com/C0verSnow/TGBot/actions/runs/39017219009 。
- 已按 issue 要求关闭 issue #1，代码与文档在 PR #2 中交付，生产配置和真实验收仍需按准备清单完成。
- 已提交 PR #2：https://github.com/C0verSnow/TGBot/pull/2 。PR 保持打开，供审核，不直接合并。

## 没做：
- 未配置真实 Telegram、Vercel、Tailscale 账号和服务器密钥，未上线生产或真实启停容器；真实端到端验收步骤已经写进准备清单。
- 按仓库约定，不进行本地编译验证；测试仅在 GitHub 上运行。

## 在做：
- 2026-10-17：通过 Chrome 确认 main 最新提交为 a01e73f，CI #8 成功；已有部署 #1 失败（https://github.com/C0verSnow/TGBot/actions/runs/39024546847），原因是 production 的 VERCEL_CLI_VERSION 填成 latest，未通过固定版本号检查，部署和 Webhook 注册被跳过。
- 已查看 Vercel 官方发布页，选定具体版本 58.4.4；尝试保存时 GitHub 提示 Re-authentication required，尚未保存成功，已请用户完成身份验证。继续检查线上入口，用户已补充机器人用户名为 @GatebyTroy_bot；再次刷新确认版本号仍为 latest，等待 GitHub 身份验证完成及允许停机验收的说明。
- 浏览器打开 https://tgbot1-chi.vercel.app/webhook 返回 {"ok": true}，入口可以访问；这只证明 HTTP 入口可用，尚未证明 Telegram、Actions 和服务器控制链路通过。
- 本次未读取或输出 .env 密钥，未进行本地构建、编译或测试运行。

- 2026-10-17 继续验收：用户确认已保存 CLI 58.4.4，明确要求先停止再启动；浏览器核对版本已生效，已重跑 main 提交 a01e73f 的部署任务 #1，等待远端结果。
- 浏览器确认部署任务第 2 次尝试成功（远端测试、Vercel 部署、Webhook 注册均成功，31 秒）；按用户要求改用 Telegram 网页端，在 @GatebyTroy_bot 私聊于 06:29 EDT 发送 /dockerstop，等待执行结果，随后再启动。
- 停止命令触发 Telegram 重试，Vercel 日志多次出现 POST /webhook 503，GitHub 控制任务仍为 0；尚未确认容器停止，未发送启动命令。用户确认仓库名、main 分支和派发 Token 的 Actions 读写权限。
- 为定位故障，只增加上游 HTTP 状态码日志，不输出 URL、请求头、响应体或 Token；补充日志脱敏测试，准备提交给远端 CI 执行。
