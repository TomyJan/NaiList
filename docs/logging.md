# 日志与排障

应用使用 Go `slog`，输出到 stderr。只有 `LOG_LEVEL` 一个配置入口，支持 `debug`、`info`（默认）、`warn`、`error`；级别包含自身及更高等级，修改 `.env` 后重启生效。stdout 仅保留 `--version` 等显式命令输出。

## 等级

| 级别 | 用途 |
| --- | --- |
| DEBUG | 处理路径与分支：请求开始、会话状态、输入校验、数据库操作、OAuth 各阶段、账号匹配/事务、资源加载、健康检查、资源释放 |
| INFO | 运维与业务结果：启动/就绪/停机、普通 API 汇总、已提交的增改删/账号绑定、成功登录/退出、客户端取消、普通 404 汇总 |
| WARN | 可预期拒绝及退化：输入/权限/身份冲突、认证验证失败、超时、慢操作、响应中断、辅助清理失败 |
| ERROR | 需要排查的故障：启动失败、未恢复的数据库/上游/内部错误、panic、异常退出 |

普通 404、匿名/失效会话不视为服务故障。超时与客户端取消分别为 WARN 与 INFO。低层数据库/上游失败先保留 DEBUG 诊断，由业务层记录最终错误；某请求已记录 ERROR 后，其汇总为 WARN，避免同一故障重复 ERROR。账号和群信息只有数据库提交成功后才记录业务成功；响应投递失败另记 WARN，不会把已提交操作描述为回滚。

慢 HTTP 请求及数据库语句/池等待阈值为 1 秒，上游 HTTP 为 3 秒。阈值属于诊断策略，不增加部署配置。

## 字段

| 字段 | 含义 |
| --- | --- |
| `time` / `level` / `msg` | 含时区时间、等级、中文语义说明 |
| `service` / `version` / `pid` | 应用名、编译版本、进程 ID |
| `component` / `event` | 子系统及稳定事件名，用于聚合和检索 |
| `operation` | 请求意图，如 `groups.create`、`auth.callback` |
| `request_id` / `user_id` | 服务端生成的请求 ID、已认证的内部账号 ID |
| `account_id` | 账号解析候选或已提交的目标账号；与请求的当前 `user_id` 区分，便于定位绑定冲突 |
| `outcome` / `reason` | 处理结果及明确原因，如 `rejected` / `duplicate_qq_number` |
| `method` / `path` / `route` | HTTP 方法、去掉查询参数的路径、路由模板 |
| `peer_ip` | 实际 socket 对端；反向代理场景通常是代理地址，不信任客户端伪造的转发头 |
| `status` / `duration_ms` | 已选定响应状态码、毫秒耗时；不能代表客户端已完整接收 |
| `request_bytes` / `response_bytes` | 请求声明的 Content-Length（未知为 -1）、实际成功写出的响应字节数 |
| `response_started` | 是否开始发送最终响应；中断前未响应时状态可为 0 |
| `provider` / `intent` / `stage` | 登录方式、登录或绑定意图、失败/处理阶段 |
| `error_kind` / `error_type` / `error` | 错误分类、根因 Go 类型、经过脱敏的错误说明 |
| `sqlstate` / `constraint` | PostgreSQL 错误码及约束名称，省略含行数据的 Detail |
| `db_operation` / `statement_id` | 数据库动作名、SQL 文本 SHA-256 的前 12 位（定位同一语句） |
| `statement_kind` / `table` / `argument_count` / `rows_affected` | SQL 动词、允许记录的业务表名、参数数量、影响行数；不会记录 SQL/参数/结果行 |
| `upstream_host` / `upstream_path` / `upstream_status` | 上游端点及状态码，不含查询参数 |
| `dns_ms` / `connect_ms` / `tls_ms` / `first_byte_ms` | 上游 DNS/TCP/TLS/首字节时序；未发生的阶段为 0，例如复用连接 |
| `connection_reused` | 是否复用连接；`upstream.request.completed` 耗时截至收到响应头，`upstream.response.completed` 另含正文读取耗时及字节数 |

`X-Request-ID` 始终由服务器重新生成。可从浏览器网络面板取得响应头，再检索同一 ID；认证成功后的业务及最终汇总包含 `user_id`，此前事件仍可用同一请求 ID 关联。数据库后台建连没有所属请求时仅包含进程信息。

## 事件与覆盖范围

| 事件 | 内容 |
| --- | --- |
| `app.starting` / `config.*` | 版本、Go/系统/架构、配置来源、站点、登录方式启用状态 |
| `http.listener.bound` / `db.pool.*` / `db.migration.*` / `app.ready` | 绑定地址、数据库目标/池参数、连接验证、迁移锁等待/提交、启动耗时 |
| `app.startup.failed` / `app.runtime.failed` | 失败阶段、原因、监听地址和排查提示 |
| `http.request.started` / `http.request.completed` | 请求上下文、实际状态/字节数、结果/原因、耗时、业务计数 |
| `http.request.panic` / `http.response.write_failed` | 异常堆栈、响应写入/刷新中断；已开始响应时不追加错误 JSON |
| `groups.list.*` / `groups.save.*` / `groups.delete.*` | 查询范围/数量、校验拒绝、群 ID/学校/群号、保存或软删除结果；链接只记格式 |
| `auth.session.lookup` / `auth.write.*` / `auth.access.rejected` | 会话缺失/命中/未知/过期/读取失败、来源与 CSRF 校验结果 |
| `auth.flow.*` / `auth.provider.*` / `auth.failed` | 授权流程创建与消费、QQ token/身份/资料、OIDC discovery/token/签名/nonce/userinfo/claims、明确失败阶段 |
| `auth.profile.unavailable` | WARN：OIDC UserInfo 补充资料失败，记录固定原因和错误类别，继续使用已验证的 ID Token 登录 |
| `account.resolve.*` / `account.created` / `account.identity.bound` | 匹配/锁/复用或创建/身份更新/提交或回滚；身份值不写日志 |
| `auth.login.completed` / `auth.binding.completed` / `auth.logout.*` | 登录会话写入成功、身份绑定成功、会话撤销 |
| `db.query.*` / `db.connection.*` / `db.pool.acquired` / `db.pool.slow` | 语句指纹、执行耗时、连接与池等待、错误分类 |
| `upstream.request.*` / `upstream.response.completed` | 登录服务 HTTP 端点、状态、网络时序、连接复用、正文读取完成/中断；保留 SDK 可能丢失的原始网络错误类型 |
| `app.shutdown.*` / `http.shutdown.*` / `app.stopped` | 停止原因、等待期限、强制关闭、请求结束、数据库池释放 |

## 排查方法

`security.rate_limited` 为 WARN，包含 `scope`（`auth_start` / `auth_callback` / `user_write`）及 `retry_after_seconds`；限额与 `CLIENT_IP_HEADER` 配置见 README。`peer_ip` 始终是 TCP 对端，不会被转发头覆盖。启动 DEBUG 事件 `http.server.configured` 记录选用的 IP 请求头（空表示 TCP 对端）。`resources.quota.checked` 为 DEBUG，记录锁内核对的所属账号、有效 / 含历史条数及上限；达到上传配额时业务拒绝为 WARN，`reason=resource_quota_exceeded`，不视为数据库故障。

启动失败先找 `app.startup.failed` 的 `stage` 和 `hint`：`http_bind` 查端口占用/监听权限，`database_connect` 查目标/凭据/TLS/网络，`database_migrate` 查建表权限与迁移锁。启动未出现 `app.ready` 表示尚未就绪。

登录问题按 `request_id` 检索 `auth.failed`，查看 `provider`、`stage`、`reason`；临时启用 DEBUG，沿 `auth.flow.*` → `auth.provider.*` → `upstream.request.*` / `db.query.*` 定位。OIDC token 校验仅记录固定拒绝原因，不写原始 claims；QQ 返回值记录错误码和字段是否存在，不写 OpenID/UnionID。

OIDC 头像排查查看 `userinfo` 与 `claims` 阶段：`picture_present=false` 表示服务未返回头像字段，`avatar_accepted=false` 表示未取得有效 HTTPS 头像。UserInfo 请求最多等待 5 秒，不跟随重定向；`userinfo_subject_mismatch` 会拒绝登录。日志不记录 UserInfo 原文、头像 URL、昵称或 token。

慢请求先看汇总 `duration_ms`，再比较数据库执行/池等待与上游网络时序；`statement_id` 可聚合同一语句。保存群号重复会出现 `reason=duplicate_qq_number`，属于业务拒绝。客户端取消以 `reason=client_canceled` 记录，通常无需按数据库故障处理。

以下为省略时间和公共字段的格式示例，数值仅作说明：

```text
level=INFO msg="创建奶群成功，数据库已保存" request_id=ABC operation=groups.create user_id=8 event=groups.save.completed group_id=42 organization=兰州理工大学 qq_number=123456789 duration_ms=3.7
level=WARN msg="登录或绑定未完成，检查失败阶段及原因" request_id=DEF operation=auth.callback event=auth.failed provider=qq stage=state_validation reason=state_mismatch outcome=rejected
level=ERROR msg="服务启动失败，未进入可用状态" event=app.startup.failed stage=http_bind listen_address=:8080 error_kind=network hint="检查端口占用、监听权限及 PORT 配置"
```

## 输出与敏感信息

每条记录同步写出，互斥保证并发记录不交错；短写重试，输出失败时向 stderr 直接报告 `logging.write_failed` 并补写已脱敏的未写入内容。若 stderr 本身不可写，进程无法保证外部持久化，应由运行环境收集并监控日志。

Docker 使用 `docker compose logs -f app`；裸跑可使用 `./nailist 2>>nailist.log`。轮转与持久化交给 Docker 日志驱动、systemd/journald 或现有采集器，应用不另设日志文件配置。

不记录请求正文、Cookie、认证头、OAuth code/state/nonce/verifier、token、OpenID/UnionID/sub、SQL 参数/结果行、二维码图片或上游响应正文。密钥、URL 凭据/查询参数、嵌套敏感字段统一脱敏；群号、学校名、内部账号 ID 属于诊断业务字段，按部署方的日志权限和保留期限管理。

## 站点管理审计

管理员身份仍由请求的 `user_id` 标识，目标账号使用 `account_id`。概览统计在 DEBUG 记录时区、统计范围、天数、各项总数与耗时（`admin.stats.started/completed`）；用户列表和管理奶群查询在 DEBUG 记录读取范围与数量；权限、输入及约束拒绝记录明确原因。解绑、删除、群信息更新和归属转移仅在数据库提交后记录 INFO，包括目标账号、群 ID、登录方式、旧/新所有者与撤销数量等相关字段。DEBUG 补充账号锁、约束检查、会话撤销、事务提交/回滚阶段。OAuth 身份标识、会话令牌、密钥及完整群链接不进入日志。
