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
- 无。
