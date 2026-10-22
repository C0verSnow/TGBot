# Go 安全更新核查（issue #9）

核查日期：2026-10-22。基线：`9c15b1e80437ab339ed00af8a28278e79ebe39c2`。

## 版本和修复决定

原声明 Go 1.26.1，改为 **Go 1.26.10**。它包含下表五项标准库修复；沿用 1.26 分支，避免无关的大版本迁移。官方依据：[Go 发布记录](https://go.dev/doc/devel/release#go1.26.10)、[Go 安全发布公告](https://groups.google.com/g/golang-announce/c/66pojueaQek)。

`golang.org/x/net v0.48.0` 单独携带 HTTP/2 实现，也受影响，因此升级到 **v0.60.0**。其 go.mod 要求同步升级 x/crypto v0.57.0、x/sys v0.48.0、x/term v0.46.0、x/text v0.42.0，最终依赖和校验和由远端 Go 工具链整理。Tailscale 保持 v1.96.2，是否兼容以远端测试、构建和扫描结果为准。没有通过关闭 TLS、禁用 HTTP/2 或跳过鉴权来规避漏洞。

## 五项漏洞逐项核查

“原版本受影响”不等于“生产环境已被利用”。下面分别记录版本、代码路径和触发条件；业务级测试不复现标准库内部漏洞。

| 漏洞与官方依据 | 受影响及修复版本 | 本仓库调用路径及判断 | 处置与限制 |
| --- | --- | --- | --- |
| [CVE-2026-111943 / GO-2026-6809](https://pkg.go.dev/vuln/GO-2026-6809) | stdlib <1.26.10；1.27 预发行至 <1.27.3 | Telegram、Tailscale OAuth、Infisical 和 Redis REST 的 HTTPS 请求使用 Go 证书验证。旧版本没有新加入的中间证书拒绝名单；能否利用还取决于信任库、证书链和攻击者的网络位置。SSH known_hosts 不属于 X.509。 | 升级 Go；保留证书链及主机名校验。测试用独立 CA 验证不可信证书在发送应用凭据前被拒绝，也验证可信证书正常工作。没有模拟真实被撤信的中间证书，也没有声称生产此前已遭攻击。 |
| [CVE-2026-94439 / GO-2026-6613](https://pkg.go.dev/vuln/GO-2026-6613) | stdlib <1.26.9；1.27 预发行至 <1.27.2 | 触发要求 HTTP/1 CONNECT 获得 2xx，处理器未接管连接而继续处理 HTTP。Webhook 仅 GET 健康检查、POST 业务；Probe 仅 GET 已认证版本检查、POST 探针，其余方法先返回 405，因此这两个处理器不满足 2xx CONNECT 条件。 | 升级 Go；增加两个处理器 CONNECT 均为 405 且不进入业务的回归测试。结论只覆盖本仓库处理器，不替平台内部路由或依赖的监听服务作保证。 |
| [CVE-2026-78663 / GO-2026-6612](https://pkg.go.dev/vuln/GO-2026-6612) | stdlib <1.26.9；1.27 预发行至 <1.27.2；x/net <0.60.0 | HTTP/2 服务端在重置流和读取缓冲数据时重复退还连接流控额度。直接暴露受影响 HTTP/2 服务端是前提。64 KiB 请求体限制不能修复协议层记账。Vercel 函数适配器及 tsnet 依赖链需要单独考虑。 | 同时升级 Go 和 x/net。生产入站 HTTP/2 是否到达 Go 解析器尚未核实，不因平台前置代理就宣称不适用。 |
| [CVE-2026-56866 / GO-2026-6605](https://pkg.go.dev/vuln/GO-2026-6605) | stdlib <1.26.9；1.27 预发行至 <1.27.2 | 触发要求带非空 Body 的 HTTP/1 CONNECT、非 2xx keep-alive 响应及连接池复用。Telegram/OAuth 固定 POST；Infisical 固定 GET/POST/PATCH；Redis REST 固定 POST。入站方法不透传到这些出站请求，也未在业务中创建 ReverseProxy。HTTPS 代理生成的建隧道 CONNECT 不携带业务 Body。 | 升级 Go；业务路径不满足所述带 Body CONNECT 条件。依赖和平台内部路径不能仅靠业务代码排除，仍扫描全模块。 |
| [CVE-2026-78669 / GO-2026-6611](https://pkg.go.dev/vuln/GO-2026-6611) | stdlib <1.26.9；1.27 预发行至 <1.27.2；x/net <0.60.0 | 恶意 HTTP/2 对端打开大量流并频繁改变初始窗口，可能消耗客户端或服务端 CPU。出站 HTTPS 可协商 HTTP/2，所以不能只调查 Vercel 入站终止位置，也不能仅靠请求超时宣称无风险。 | 同时升级 Go 和 x/net。源码扫描给出可达性线索；实际对端、协议协商、并发条件仍取决于部署。 |

## 工具链与 Vercel 边界

- 本机 PATH 没有 Go，`GOTOOLCHAIN=local go version` 返回 command not found。按仓库约定不安装工具链做本地编译、不跑本地测试。本机版本不是 1.26.10 的验证证据。
- CI 从 go.mod 安装精确版本，设置 `GOTOOLCHAIN=local`，记录 `go version`、GOOS/GOARCH 并断言实际 GOVERSION 与声明一致，禁止隐式换用另一工具链。
- [Vercel 官方 Go 文档](https://vercel.com/docs/functions/runtimes/go#go-version)说明从根 go.mod 选择版本；本项目保留 `/api/*.go` 函数模式，不迁移框架。部署工作流在远端测试后交给 Vercel 远端构建，不能把 Actions 中的版本当成 Vercel 版本。
- 本次连接能找到 `tgbot1` 项目，但读取其 production 部署时返回 403（团队范围未授权）。没有读取到生产构建日志，没有进行本次生产部署，也没有验证生产 Go 版本或平台的 HTTP/2 终止方式。
- 为后续验收，`GET /tsnet-probe` 在正确 `X-TGBot-Probe-Secret` 下返回原有 `{"ok":true}` 结构和 `X-TGBot-Go-Version` 响应头。该请求不创建 Tailnet 节点、不登录 SSH；无密钥返回 403，不暴露版本。原 POST 探针继续只做连接验证，并携带版本头。版本来自 `runtime.Version()`，代表正在执行的 Go 二进制版本，不代表边缘代理版本。
- 合并后先运行 feature Preview 远端部署，保存两个 Go 函数的构建日志及来源提交 SHA；用上述 GET 核对实际二进制版本，再执行已认证 POST 探针。正式发布时对 production 重复核对。不要把密钥放 URL、终端命令历史或公开日志；使用现有安全环境配置。Webhook GET 仅检查存活，不执行 Docker 或真实密钥更新。
- 升级不会自动替换已上线的旧二进制。确认两个 Go 函数均由新工具链构建前，生产版本风险仍未结清。入站协议终止边界需要部署日志、运行时适配器源码或平台确认；公网协商到 HTTP/2 本身不能证明 Go 收到了 HTTP/2 帧。出站 TLS 证书验证在 Go 客户端执行。

## 远端检查与证据

CI 保留 Python 和真实 Redis 测试，Go job 执行版本核验、`go mod tidy`、`go mod verify`、gofmt 检查、`go test -race -v -timeout 3m ./...`、`go vet ./...`、Linux 构建和生成入口的 Vercel 兼容性构建。所有带 tee 的命令通过 bash pipefail 传播失败。

扫描使用当次最新 `govulncheck`：保存工具自身的构建版本、模块版本关联 JSON、源码调用链 JSON，以及会阻断 CI 的人类可读扫描。报告在失败时也上传为 `go-security-results`（30 天保留）；完整 job 日志保留执行环境和状态。每周及手动触发重新扫描，避免只检查修改当天的漏洞库。模块关联结果用于审查，不能直接推断业务可利用；源码扫描通过也不能证明平台或未来版本没有风险。

新增回归测试覆盖 CONNECT 拒绝、版本检查鉴权与无副作用、Telegram/OAuth/Infisical 可信及不可信 TLS、重定向拒绝；原测试继续覆盖 Webhook 请求体限制/非法输入/权限、Telegram 通知、OAuth 密钥范围、Infisical 读取/更新/错误脱敏、SSH 主机密钥和真实 Redis 去重并发。使用模拟凭据，未触发生产 Docker 或真实 Gate 密钥更新。

最终执行结果及运行链接在 PR 和 tasklist.md 记录。生产部署兼容性、真实外部服务和 HTTP/2 平台边界仍是独立验收项。PR 等待人工审查；合并且验收完成后再关闭 issue #9。
