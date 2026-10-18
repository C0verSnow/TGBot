# 本文档为4象限的任务清单，全文书写大白话

## 想做：
- 2026-10-17：完成 issue #3，在独立 feature 分支实现 Go + tsnet，增加 /id、/help，远端测试、预览验证后提交 PR。
- 完成 issue #1，让白名单用户通过 Telegram 启停指定 Docker 容器。

## 做完：
- 2026-10-17 issue #3 代码阶段：在 feature/issue-3-go-tsnet 实现 Go + tsnet 直连、/id、/help、请求内临时 OAuth 入网、严格 SSH 主机公钥校验、独立登录探针、超时和结果不确定提示。复用原服务器强制命令与 SQLite，旧 Python 生产入口和回滚链路保留。
- 代码提交 e171b5b 的远端 CI 全部通过：Python 测试、Go 安全测试和 race、gofmt、go vet、Linux 构建，https://github.com/C0verSnow/TGBot/actions/runs/39089837592 。没有执行本地编译或测试，只做本地格式、静态语法、JSON 和差异检查。
- 远端生成的 go.mod、go.sum 已校验 artifact SHA256 后提交；提交草稿 PR #4：https://github.com/C0verSnow/TGBot/pull/4 。README、准备清单、变量示例和旧版说明已更新。

- 最终核对：用户在服务器执行 docker inspect，确认 orders-dashboard 的 Running 为 true。带多余参数的 /dockerstart other-container 被拒绝并提示正确用法，GitHub 控制任务总数保持 6，没有新增操作任务。启停链路验收完成，容器最终保持运行。
- 2026-10-17 本次部署验收：已生成并保存最小权限的 Tailscale OAuth 凭据，修正 GitHub 和本地 .env 的服务器主机公钥，重新保存原专用私钥的正确多行格式。
- 用户完成 root 安装后，确认服务器控制脚本的属主、权限和内容正确；专用 SSH 公钥已限制为只能启停 orders-dashboard。
- Telegram 实际停止成功（08:39 EDT）：https://github.com/C0verSnow/TGBot/actions/runs/39033370518 。随后实际启动成功（08:40 EDT）：https://github.com/C0verSnow/TGBot/actions/runs/39033406821 。两条消息都已收到明确成功回复。
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
- issue #3：用户说 Preview 变量晚点配置，已把所有配置项和验证步骤写入准备清单。本次未部署验证 Vercel 入网、冷启动、并发、资源限制或真实 Docker 启停；未切换生产 Webhook，未合并 PR。没有把 Go 版标为可投入生产，issue #3 保持打开，等后续实际验收完成再关闭。

- 首次交付时未进行真实部署；本次已补齐 OAuth、SSH 主机公钥和服务器限制入口，真实启停结果见下面的新记录。
- 本次未使用其他账号复验非白名单拒绝，也未故意更改线上主机公钥来制造故障。
- 按仓库约定，不进行本地编译验证；测试仅在 GitHub 上运行。

## 在做：
- 2026-10-17 PR #4：首轮修复后远端发现 Vercel 把函数模块改名为 handler，Go internal 访问规则因此拒绝入口导入。改用 pkg 目录，并让远端回归检查同时模拟模块改名。用户指定 Go 采用 TS_AUTHKEY，已切换 Preview 配置，未改旧生产 OAuth。
- 2026-10-17 PR #4：确认最新提交 e90e071 的远端 CI 成功，但 Vercel Preview 构建失败：根目录白名单包与生成的 main__vc__go__.go 冲突。把唯一白名单移动到 pkg/allowlist，Python 和 Go 共用该文件；新增远端生成入口编译回归检查。Preview 当前缺配置，优先使用已有 OAuth，避免与 TS_AUTHKEY 同时设置。
- 2026-10-17 本次继续 PR #4：用户确认完成 Go + tsnet PR，并允许短暂停止 orders-dashboard 后启动恢复。先核对远端 PR、CI、Vercel Preview 配置和 SSH 探针，全部过程不做本地编译。
- 2026-10-17：按用户要求通过浏览器核对准备清单所需配置，与本地 .env 同步；只记录变量名称和状态，不记录密钥，不切换生产、不运行本地编译。
- issue #3 本次代码工作已交付，等待用户按准备清单配置 Preview 后继续实际验收。下方保留处理过程。
- issue #3：Go 入口、临时 tsnet、请求内 OAuth、严格 SSH、独立探针及 /id、/help 已实现；首轮远端 CI 已触发，Python 测试通过，Go 正在准备工具链。用户说明 Preview 变量晚点配置，已把配置表、探针步骤、切换与回滚写进准备清单；本次不切生产，也不做真实容器启停。
- issue #3：已读取完整需求和现有实现，创建 feature/issue-3-go-tsnet；核对 Vercel 官方 Go Runtime 和 tsnet 生命周期。先保留 Python 生产入口，新增隔离的 Go 入口和无 Docker 操作的 SSH 探针。
- 本次启动与启停链路验收已完成，当前没有等待中的操作；下方保留排查过程。
- 2026-10-17：通过 Chrome 确认 main 最新提交为 a01e73f，CI #8 成功；已有部署 #1 失败（https://github.com/C0verSnow/TGBot/actions/runs/39024546847），原因是 production 的 VERCEL_CLI_VERSION 填成 latest，未通过固定版本号检查，部署和 Webhook 注册被跳过。
- 已查看 Vercel 官方发布页，选定具体版本 58.4.4；尝试保存时 GitHub 提示 Re-authentication required，尚未保存成功，已请用户完成身份验证。继续检查线上入口，用户已补充机器人用户名为 @GatebyTroy_bot；再次刷新确认版本号仍为 latest，等待 GitHub 身份验证完成及允许停机验收的说明。
- 浏览器打开 https://tgbot1-chi.vercel.app/webhook 返回 {"ok": true}，入口可以访问；这只证明 HTTP 入口可用，尚未证明 Telegram、Actions 和服务器控制链路通过。
- 本次未读取或输出 .env 密钥，未进行本地构建、编译或测试运行。

- 2026-10-17 继续验收：用户确认已保存 CLI 58.4.4，明确要求先停止再启动；浏览器核对版本已生效，已重跑 main 提交 a01e73f 的部署任务 #1，等待远端结果。
- 浏览器确认部署任务第 2 次尝试成功（远端测试、Vercel 部署、Webhook 注册均成功，31 秒）；按用户要求改用 Telegram 网页端，在 @GatebyTroy_bot 私聊于 06:29 EDT 发送 /dockerstop，等待执行结果，随后再启动。
- 停止命令触发 Telegram 重试，Vercel 日志多次出现 POST /webhook 503，GitHub 控制任务仍为 0；尚未确认容器停止，未发送启动命令。用户确认仓库名、main 分支和派发 Token 的 Actions 读写权限。
- 为定位故障，只增加上游 HTTP 状态码日志，不输出 URL、请求头、响应体或 Token；补充日志脱敏测试，准备提交给远端 CI 执行。
- 用户指示参考本地 .env：仅核对普通配置和变量是否填写，不输出密钥。仓库名、main 分支、生产入口均符合预期。诊断提交 bb5f337 已通过远端部署任务 https://github.com/C0verSnow/TGBot/actions/runs/39025207257 。
- 新日志确认上游 HTTP 403。浏览器检查 GitHub 中 C0verSnow 组织的 fine-grained Token“1”，发现其未选仓库且没有仓库权限。已在表单中准备只选 TGBot、Actions 读写和必需 Metadata 只读，尚未保存；已请用户确认是否为 .env 使用的同一 Token，并当场确认权限扩大。
- 用户确认 Token“1”就是 .env 中的 GITHUB_DISPATCH_TOKEN，并明确批准保存最小权限；已保存，GitHub 显示 Your personal access token has been updated。继续等待原停止消息重试，不重复发送停止命令。

- 2026-10-17：用户确认相关环境变量已部署，要求通过浏览器启动和验收。开始核对当前线上代码、部署及控制任务；只参考本地配置，不运行本地编译或测试。
- 浏览器核对：远端部署 #2 已成功；历史停止任务 #1/#2 虽为绿色，但 Telegram 均回复连接服务器失败，不能算停止验收通过。
- 本次已在 @GatebyTroy_bot 私聊发送 /dockerstart（08:29 EDT），触发控制任务 #3：https://github.com/C0verSnow/TGBot/actions/runs/39032705485 。任务通过输入和白名单检查，在加入 Tailscale 时提示 backend error: invalid key: unable to validate API key，尚未执行 Docker 启动。已请用户核对 bot-control 中是否填写同一组有效的 Tailscale OAuth client ID 和 secret。
- 用户授权自行修改后，确认本地 TS_OAUTH_SECRET 实际是设备入网密钥，现有工作流误走 OAuth 路径；官方固定版本 Action 支持 authkey 输入。修改工作流按密钥类型选择输入，保留原 OAuth 支持和原 tag，文档补充有效期、重复使用及 tag 要求。只做差异检查，接下来提交远端 CI 并重试真实启动。
- 按用户最新要求改为在 Tailscale 寻找正确 OAuth 凭据；管理台 Trust credentials 列表为空。已撤回未提交的 authkey 兼容修改，保留原工作流；新建表单已准备为 TGBot GitHub Actions，仅 Auth Keys 写入并限定 tag:tgbot-ci。生成前按浏览器规则请用户当场确认，尚未生成或保存任何新凭据。
- 用户当场确认后，已在 Tailscale 生成 TGBot GitHub Actions OAuth 凭据，权限仅 Auth Keys 写入并限定 tag:tgbot-ci。用户完成 GitHub 身份验证后，已保存 bot-control 的 TS_OAUTH_CLIENT_ID 和 TS_OAUTH_SECRET，页面更新时间均为 08:33 EDT；已在编辑器同步本地 .env 两项并保存。08:34 EDT 重新发送 /dockerstart，等待真实结果。
- 更新凭据后的控制任务 #4：https://github.com/C0verSnow/TGBot/actions/runs/39033061359 ，Tailscale 步骤耗时 8 秒后继续执行 worker，Telegram 仍回复连接服务器失败，不能算启动成功。
- 对本地 SSH 配置做静态检查：私钥可解析；SSH_KNOWN_HOSTS 中的公钥与该登录私钥对应的公钥完全相同，疑似误填了登录公钥而不是服务器主机公钥。已请用户提供服务器管理控制台入口，便于从可信服务器读取正确主机公钥；未关闭主机身份检查，未做本地编译或测试。
- 按用户给定的 deploy@192.168.1.245，用 .env 专用私钥和本机已有可信 known_hosts 连接成功。读取到真实服务器主机公钥，确认原 SSH_KNOWN_HOSTS 误填了登录公钥；已修正本地 .env。检查发现 orders-dashboard 正在运行，但 /usr/local/lib/tgbot-control.py 不存在，authorized_keys 未设置受限入口，继续补齐服务器部署。
- 已从可信 LAN SSH 连接读取真实主机公钥，并保存 GitHub bot-control 的 SSH_KNOWN_HOSTS；同时重新保存原 SSH 私钥为完整多行内容，不带 .env 的外围引号。
- 控制脚本已上传到 /home/deploy/tgbot-control.py，SHA256 与仓库脚本相同。deploy 无法免密 sudo，已请用户在服务器执行 sudo install -o root -g root -m 755 /home/deploy/tgbot-control.py /usr/local/lib/tgbot-control.py。安装完成前暂不配置会拦截日常命令的受限入口。
- 用户确认已执行 sudo install；SSH 核对系统脚本为 root 所有、不可被普通用户修改，内容与仓库一致。已为原专用公钥设置 restrict 和固定 orders-dashboard 控制命令，原 authorized_keys 备份在 /home/deploy/.ssh/authorized_keys.before-tgbot。08:39 EDT 从 Telegram 发送 /dockerstop，收到结果后再启动恢复运行。
- Telegram 正常启停链路已收到成功回复；用户随后独立确认最终 Running=true，多余参数拒绝验收也已完成。
