# 本文档为4象限的任务清单，全文书写大白话

## 想做：
- 完成 issue #1，让白名单用户通过 Telegram 启停指定 Docker 容器。

## 做完：
- 2026-10-17：从 GitHub 同步空仓库，在 feature/issue-1-telegram-docker 分支工作。
- 确认 Vercel 不继承 Actions 的 Tailscale 网络，采用 Vercel 收消息、Actions 连服务器的方案。

## 没做：
- 按仓库约定，不进行本地编译、构建或测试运行。测试放在 GitHub Actions。
- 真实账号、服务器和生产部署需要使用者按准备清单配置。

## 在做：
- 编写 Webhook、Actions 执行器、服务器限制脚本、远端测试和中文部署文档。
