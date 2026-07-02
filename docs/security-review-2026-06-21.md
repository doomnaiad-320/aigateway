# 安全审查报告

审查日期：2026-06-21

审查范围：Go 后端、React 前端、安全相关路由、中间件、支付回调、SSRF 防护、初始化流程和部分本地 HTTP 行为检查。

审查限制：本次为静态审查加少量本地验证，未做完整依赖漏洞扫描、模糊测试、生产环境渗透测试或第三方支付平台端到端验证。

## 摘要

本项目的鉴权、支付回调、SSRF 防护和日志审计有一定安全基础，但存在几个需要优先处理的风险：

1. 公共内容渲染链路允许未经净化的 HTML/Markdown，形成存储型 XSS 风险。
2. 首次初始化接口如果在公网暴露，会被抢先创建 root 用户。
3. `ENABLE_PPROF=true` 时会在 `0.0.0.0:8005` 暴露未鉴权 pprof。
4. 少数管理功能绕过统一 SSRF 校验，直接使用 `http.Client` 访问配置 URL。
5. 多个支付 webhook 会记录完整签名和完整 body，存在敏感信息泄露风险。
6. Session Cookie、CORS 和 CSRF 机制需要按生产环境加固。

## 发现

### F-01 公共富文本渲染存在存储型 XSS 风险

严重性：高影响 / 中等可利用性

受影响位置：

- `web/default/src/components/ui/markdown.tsx:50` 到 `web/default/src/components/ui/markdown.tsx:52` 使用 `rehypeRaw` 渲染原始 HTML，未使用 sanitizer。
- `web/default/src/features/legal/legal-document.tsx:142` 到 `web/default/src/features/legal/legal-document.tsx:146` 使用 `dangerouslySetInnerHTML`。
- `web/default/src/features/about/index.tsx:173` 到 `web/default/src/features/about/index.tsx:177` 使用 `dangerouslySetInnerHTML`。
- `web/default/src/components/layout/components/footer.tsx:225` 到 `web/default/src/components/layout/components/footer.tsx:238` 使用 `dangerouslySetInnerHTML`。
- 公告、通知、FAQ 等公共组件通过 `Markdown` 渲染内容，例如 `web/default/src/components/notification-popover.tsx:187`、`web/default/src/components/notification-popover.tsx:241`、`web/default/src/features/dashboard/components/overview/faq-panel.tsx:64`、`web/default/src/features/dashboard/components/overview/announcement-detail-dialog.tsx:66`。
- 内容写入入口在 `/api/option`，路由要求 RootAuth：`router/api-router.go:184` 到 `router/api-router.go:188`。公告和 FAQ 只做结构和长度校验：`controller/option.go:317` 到 `controller/option.go:328`、`setting/console_setting/validation.go:142` 到 `setting/console_setting/validation.go:210`。
- 公共接口会返回相关内容：`controller/misc.go:173` 到 `controller/misc.go:209`，`controller/misc.go:126` 到 `controller/misc.go:135`。

可利用条件：

- 攻击者能控制 root 配置内容、导入配置、数据库内容，或未来新增了低权限内容写入路径。
- 普通用户访问公告、FAQ、关于页、用户协议、隐私政策或页脚等公共页面。

影响：

- 在同源页面执行任意脚本。
- 可代表受害用户调用同源 API、修改可访问设置、读取页面可见的敏感信息，并可诱导用户进行凭据或 API Key 操作。
- `HttpOnly` Cookie 能降低直接盗取 Cookie 的风险，但不能阻止同源 XSS 发起业务请求。

建议：

- 默认禁止 raw HTML，只保留 Markdown 基础语法。
- 如果必须支持 HTML，前端使用 DOMPurify 或 `rehype-sanitize`，并维护严格的标签和属性白名单。
- 服务端保存前也做同等净化，避免未来新增展示端绕过前端 sanitizer。
- 对公共页面增加 CSP，例如禁止 inline script，并限制 `frame-src`、`img-src`、`connect-src`。
- 对关于页的 URL iframe 增加 `sandbox`、`referrerPolicy` 和可信域名限制。

### F-02 未初始化实例可被抢占 root

严重性：高，取决于部署窗口

受影响位置：

- `/api/setup` GET/POST 未鉴权：`router/api-router.go:22` 到 `router/api-router.go:23`。
- `PostSetup` 在 `constant.Setup == false` 且不存在 root 用户时创建 root：`controller/setup.go:54` 到 `controller/setup.go:174`。

可利用条件：

- 服务端启动后尚未完成初始化。
- 实例在初始化前暴露给公网、共享网络或不可信内网。

影响：

- 首个外部请求可创建 root 用户并接管实例。
- 后续合法管理员无法可信地完成初始化。

建议：

- 首次启动生成一次性 setup token，并要求 `/api/setup` POST 携带该 token。
- 未初始化状态默认只监听 `127.0.0.1`，或要求反向代理限制访问。
- 初始化流程放入事务或全局锁，确保 root 创建和 setup 状态写入原子完成。
- 在部署文档中明确要求先本地初始化，再暴露公网入口。

### F-03 pprof 调试端口可在生产误开后未鉴权暴露

严重性：中到高，取决于 `ENABLE_PPROF` 是否启用和网络暴露范围

受影响位置：

- `main.go:35` 导入 `net/http/pprof`。
- `ENABLE_PPROF=true` 时执行 `http.ListenAndServe("0.0.0.0:8005", nil)`：`main.go:142` 到 `main.go:147`。

可利用条件：

- 生产或测试环境设置了 `ENABLE_PPROF=true`。
- `8005` 端口对不可信网络开放。

影响：

- 访问 `/debug/pprof/goroutine`、`/debug/pprof/heap` 等接口可获取运行时、堆、goroutine、路径和部分业务上下文信息。
- 访问 CPU profile 端点可造成额外性能消耗。

建议：

- pprof 只绑定 `127.0.0.1`。
- 如需远程访问，放在 VPN、堡垒机或带鉴权的反向代理后。
- 将 pprof 使用独立 mux，并只注册必要 profile。
- 在启动日志中突出显示 pprof 暴露地址，并在生产配置模板中保持禁用。

### F-04 部分管理/配置型出站请求绕过统一 SSRF 校验

严重性：中，主要影响管理员或 root 可触发路径

已有正向设计：

- 默认 FetchSetting 开启 SSRF 防护、禁止私网 IP、限制端口并对域名解析后的 IP 应用过滤：`setting/system_setting/fetch_setting.go:16` 到 `setting/system_setting/fetch_setting.go:24`。
- 下载、Worker、用户通知等主要用户可控 URL 已调用 `common.ValidateURLWithFetchSetting`：`service/download.go:32` 到 `service/download.go:35`、`service/download.go:61` 到 `service/download.go:68`、`service/webhook.go:91` 到 `service/webhook.go:97`、`service/user_notify.go:156` 到 `service/user_notify.go:160`、`service/user_notify.go:250` 到 `service/user_notify.go:254`。
- 统一 HTTP client 也会校验重定向目标：`service/http_client.go:24` 到 `service/http_client.go:33`。

绕过点：

- Root-only 的模型拉取接口使用原生 `http.Client{}` 和用户传入 `base_url`：`controller/channel.go:1018` 到 `controller/channel.go:1095`，路由在 `router/api-router.go:252`。
- AdminAuth 下的 Ollama 管理操作使用原生 `http.Client{}` 请求渠道 BaseURL：`relay/channel/ollama/relay-ollama.go:281` 到 `relay/channel/ollama/relay-ollama.go:295`、`relay/channel/ollama/relay-ollama.go:321` 到 `relay/channel/ollama/relay-ollama.go:347`、`relay/channel/ollama/relay-ollama.go:362` 到 `relay/channel/ollama/relay-ollama.go:388`、`relay/channel/ollama/relay-ollama.go:439` 到 `relay/channel/ollama/relay-ollama.go:494`。路由在 `router/api-router.go:229` 到 `router/api-router.go:262`。
- 公开的 Uptime Kuma 状态接口会访问 root 配置的 URL，但仅做 URL 格式校验，未走 FetchSetting：`controller/uptime_kuma.go:38` 到 `controller/uptime_kuma.go:54`、`controller/uptime_kuma.go:131` 到 `controller/uptime_kuma.go:154`，配置校验在 `setting/console_setting/validation.go:237` 到 `setting/console_setting/validation.go:299`。

可利用条件：

- 攻击者拥有 Admin 或 Root 权限，或能影响对应渠道/控制台配置。
- Uptime Kuma 场景中，攻击者不能控制目标 URL，但普通访客可以反复触发服务端访问已配置目标。

影响：

- 管理员权限被滥用时，可探测或访问内网 HTTP 服务。
- 若低权限管理员不应拥有内网访问能力，Ollama 管理操作会扩大权限边界。
- Uptime Kuma 配置错误时，公共接口可能成为对内网目标的放大触发器。

建议：

- 所有出站 HTTP 请求统一通过封装 client，禁止业务代码直接 `&http.Client{}`。
- 在构造请求前统一调用 `ValidateURLWithFetchSetting`，并校验重定向后的目标。
- 对 AdminAuth 和 RootAuth 的 SSRF 策略分层：低权限管理员默认不得访问私网 IP。
- 对 Uptime Kuma URL 使用同一 FetchSetting，并为公共触发接口增加缓存和速率限制。

### F-05 支付 webhook 日志记录完整签名和完整 body

严重性：中

受影响位置：

- Stripe：`controller/topup_stripe.go:162` 到 `controller/topup_stripe.go:164`。
- Creem：`controller/topup_creem.go:244` 到 `controller/topup_creem.go:255`。
- Waffo：`controller/topup_waffo.go:341` 到 `controller/topup_waffo.go:347`。
- Waffo Pancake：`controller/topup_waffo_pancake.go:461` 到 `controller/topup_waffo_pancake.go:466`。
- 易支付：`controller/topup.go:337`、`controller/topup.go:355`。

可利用条件：

- 攻击者能读取应用日志、集中日志平台、容器 stdout、备份或运维采集系统。

影响：

- 泄露支付订单号、金额、邮箱、客户名、签名头和完整事件 body。
- 若第三方签名允许短时间重放，日志中的签名和 body 会提高重放风险。
- 日志中长期保留 PII，不利于最小化数据留存。

建议：

- 默认只记录事件类型、订单号、金额、状态、来源 IP 和验签结果。
- 签名只保留前后少量字符或 hash，不记录完整值。
- body 仅在本地 debug 模式下截断记录，并使用 `common.LocalLogPreview` 这类长度限制。
- 对集中日志设置更短保留期和更严格访问控制。

### F-06 Session、CORS 和 CSRF 防护需要生产加固

严重性：中

受影响位置：

- Session Cookie `Secure` 固定为 `false`：`main.go:173` 到 `main.go:180`。
- CORS 全域放开且同时声明 credentials：`middleware/cors.go:9` 到 `middleware/cors.go:15`。
- Dashboard session 鉴权依赖 `Max-Api-User` / `New-Api-User` header 与 session id 匹配：`middleware/auth.go:95` 到 `middleware/auth.go:125`。前端从本地状态附加该 header：`web/default/src/lib/api.ts:161` 到 `web/default/src/lib/api.ts:168`。

可利用条件：

- 站点同时存在 HTTP 和 HTTPS 入口，或反向代理没有强制 HTTPS。
- CORS 行为被代理、中间件升级或浏览器差异改变。
- 配合 XSS 时，攻击脚本可以读取同源状态并携带自定义 header 调用接口。

影响：

- `Secure=false` 允许 Cookie 在明文 HTTP 请求中发送。
- `AllowAllOrigins=true` 与 `AllowCredentials=true` 是危险组合；现代浏览器通常会拒绝 credentialed wildcard CORS，但该配置仍然容易在代理或后续代码修改中变成跨站读写风险。
- `Max-Api-User` 是用户 id，不是不可预测 CSRF token。

建议：

- 生产环境根据 `SERVER_ADDRESS` 或显式环境变量设置 Cookie `Secure=true`。
- 部署层强制 HTTPS 和 HSTS。
- Dashboard/Admin API 使用精确 origin allowlist；Relay API 如需跨域，可单独使用宽 CORS 策略。
- 对 state-changing session API 增加真正的 CSRF token，token 应不可预测、绑定 session，并在登录后轮换。

### F-07 OAuth state 使用非密码学安全随机数

严重性：低到中

受影响位置：

- OAuth state 由 `common.GetRandomString(12)` 生成：`controller/oauth.go:22` 到 `controller/oauth.go:40`。
- `common.GetRandomString` 调用 `lo.RandomString`：`common/str.go:40` 到 `common/str.go:45`。
- 当前依赖的 `lo.RandomString` 使用 `math/rand/v2` 默认源：`github.com/samber/lo@v1.52.0/string.go:67` 到 `github.com/samber/lo@v1.52.0/string.go:70`、`github.com/samber/lo@v1.52.0/internal/xrand/ordered_go122.go:19` 到 `github.com/samber/lo@v1.52.0/internal/xrand/ordered_go122.go:22`。
- 回调会校验 session 中的 state：`controller/oauth.go:57` 到 `controller/oauth.go:65`。

可利用条件：

- 攻击者需要预测或影响 state，并诱导受害者完成 OAuth 回调。

影响：

- 当前有 session 绑定和 CriticalRateLimit，实际利用难度较高。
- 但 OAuth CSRF state 属于安全令牌，应避免伪随机数。

建议：

- 用 `crypto/rand` 生成至少 128 bit 随机值，再 base64url 编码。
- OAuth 回调成功或失败后删除 session 中的 `oauth_state`，防止重复使用。
- 将该能力封装为 `common.GenerateSecureToken`，供 setup token、OAuth state 等安全场景复用。

## 已检查但未列为当前可利用漏洞的点

- Creem 的 `verifyCreemSignature` 在 `CreemTestMode` 且 secret 为空时会返回 true：`controller/topup_creem.go:36` 到 `controller/topup_creem.go:43`。但当前 webhook 入口先要求 `CreemWebhookSecret` 非空：`controller/payment_webhook_availability.go:41` 到 `controller/payment_webhook_availability.go:47`，所以该绕过逻辑在当前路由下不可达。建议移除该分支，防止未来改动引入回归。
- 用户自定义通知 webhook、Bark、Gotify 在非 Worker 和 Worker 模式都会先走 FetchSetting 校验：`service/webhook.go:91` 到 `service/webhook.go:97`、`service/user_notify.go:156` 到 `service/user_notify.go:160`、`service/user_notify.go:250` 到 `service/user_notify.go:254`、`service/download.go:32` 到 `service/download.go:35`。
- 未发现明显的用户可控命令执行路径。`common/utils.go` 中的系统打开命令未在审查范围内发现可由远程用户直接触发。
- Stripe 自定义成功/取消 URL 有可信域名校验：`controller/topup_stripe.go:78` 到 `controller/topup_stripe.go:85`、`common/url_validator.go:19` 到 `common/url_validator.go:38`。
- 默认 `SESSION_SECRET` 为启动时 UUID，且显式拒绝 `random_string`：`common/constants.go:68`、`common/init.go:49` 到 `common/init.go:57`。

## 优先修复清单

1. 立即修复富文本 XSS：移除 raw HTML 或引入严格 sanitizer，并补 CSP。
2. 初始化接口增加一次性 setup token，并限制未初始化状态的访问范围。
3. pprof 改为 localhost 绑定，并加鉴权或网络隔离。
4. 出站 HTTP 请求统一接入 FetchSetting，先覆盖 Root/Admin 可配置 URL。
5. 支付 webhook 日志脱敏，禁止默认记录完整 body 和完整签名。
6. 生产环境启用 Cookie Secure、HTTPS/HSTS、精确 CORS allowlist 和真实 CSRF token。
7. 将 OAuth state 改为 `crypto/rand`。

