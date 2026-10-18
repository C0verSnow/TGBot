# 本文档为4象限的任务清单，全文书写大白话

## 想做：
- [1792313883] 启用时按准备清单补齐 Infisical 和 Redis REST 配置，部署后在隔离环境做实际验收。

## 做完：
- [1792313170] 完成 issue #5：私聊更新 Infisical 的两个 Gate 密钥，补文档，提 PR 并使用远端 CI 验证。
- [1792313170] 已查看 issue、确认生产是 Go Webhook，本地工作区干净，已创建 feature 分支。
- [1792313170] 核对 Orders3 接入方式和共享状态配置；所有密钥不打印、不写入文档。
- [1792313208] 用户已同意 Redis REST；E:/git/.env 有六项 Infisical 配置，未读取或输出密钥值；正在实现共享等待状态、去重和目标级互斥。
- [1792313255] 用户说明已有 Supabase，改用 PostgreSQL 原子 RPC 保存等待状态、去重与锁，不再新增 Redis 服务；密钥仍不进入状态数据库。
- [1792313389] 已新增独立 Infisical 更新模块、Go 命令入口、Supabase 状态 RPC 和专用私有表；继续补远端安全回归与并发测试。
- [1792313420] 按用户最新要求撤销 Supabase 方案：删除未提交的 Supabase RPC、表结构与配置读取代码，恢复已授权的 Redis REST。此前只读检查，没有对 Supabase、Infisical 做写操作。
- [1792313514] 已补 Redis 真服务并发回归、Infisical HTTP 边界及安全测试，并更新 README、环境变量样例和准备清单；实际密钥未写入文件，准备提交远端 CI。
- [1792313581] 已创建 PR #6（https://github.com/C0verSnow/TGBot/pull/6），远端 Python 回归通过；Go race 和 Redis 实例测试运行中。复查补上等待期间不带字段名的错误输入删除，避免格式错误时残留敏感文本。
- [1792313626] 首轮远端真实 Redis 的 6 项回归全部通过（含重复并发、跨用户锁、超时、大编号和不确定锁）；敏感配置全文静态检查通过。补上错误输入删除成功/失败两条安全回归，继续验证最终提交。
- [1792313883] issue #5 的代码和文档已完成，PR #6：https://github.com/C0verSnow/TGBot/pull/6 。issue #5 已按要求关闭，PR 保持打开待审阅。
- [1792313883] 最终代码 78fc62e 的远端 CI 全绿：https://github.com/C0verSnow/TGBot/actions/runs/39113221369 。Go race、vet、Linux 构建、Vercel 入口构建、Python 回归与真实 Redis 的 6 项并发回归均通过；敏感配置静态检查通过。

## 没做：
- [1792313883] 按约定没有进行本地编译、构建或测试；没有配置或部署生产 Redis，没有改动真实 Gate 密钥，也没有宣称生产真实验收通过。

## 在做：
- [1792313883] 本次代码交付已完成，没有进行中的代码修改。

