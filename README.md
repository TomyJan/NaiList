# NaiList 奶群与奶站列表

Go + PostgreSQL + React / TypeScript。深浅色模式自动跟随系统。前端内嵌在 Go 二进制中，由同一服务提供页面和 API。

公开 `/api/groups`、`/api/sites` 分别使用永久内存缓存，首次读取时查库并保存 JSON 快照；并发读取合并查询。公开读取不验证登录会话，即使携带 Cookie，缓存命中时也不会查询数据库。普通增删改、管理员修改或转移归属成功后，对应缓存失效，下次读取重新查库；校验、重复、配额等确定未写入的业务拒绝不影响缓存；数据库错误（包括取消、超时和连接中断）可能发生在提交之后，因此保守失效对应缓存，即使请求返回失败。个人和管理后台列表实时查库。缓存仅属于当前进程，多实例或直接修改数据库时需重启相关实例以重新加载；CDN 缓存仍按其配置刷新。

## 配置

准备已创建数据库的 PostgreSQL 16+，将 `.env.example` 复制为工作目录下的 `.env`，填写连接串、站点地址和登录方式。Docker 和裸跑使用相同字段，进程环境变量优先于 `.env`。

```env
DATABASE_URL=postgres://nailist:密码@数据库地址:5432/nailist?sslmode=verify-full
PUBLIC_URL=https://群列表域名
QQ_APP_ID=QQ互联应用ID
QQ_APP_KEY=QQ互联应用密钥
PORT=8080
```

数据库只使用 `DATABASE_URL`，连接串中的密码特殊字符需 URL 编码。服务监听 `PORT`（默认 8080），前端固定内嵌。QQ/OIDC 配置见下方「登录方式与账号绑定」。

外部数据库使用 `sslmode=verify-full`，校验证书和数据库主机名；证书须由受信任 CA 签发。私有 CA 在同一连接串追加 `&sslrootcert=/证书绝对路径/ca.pem`，Docker 需将该证书只读挂载到容器内相应路径。仅可信本机或隔离测试网络可使用 `sslmode=disable`，不要通过公网明文连接数据库。

Umami 在同一 `.env` 中配置：

```env
UMAMI_SCRIPT_URL=https://统计域名/script.js
UMAMI_WEBSITE_ID=Umami网站ID
```

两项同时留空则关闭访问追踪。修改后重启程序（Docker 执行 `docker compose up -d`）即可；启动时注入页面并设置对应 CSP，无需重新构建前端。MiSans 通过 CDN 加载，字体文件来自小米字体服务。

### 页脚访问统计

在 Umami 的「网站 → 编辑 → 分享」中新建**网站分享链接**，最少只勾选「流量 → 概览」。无需勾选会话、实时、比较、行为或增长中的其他权限；当前 Umami 的汇总接口允许「概览 / 比较」任一权限，在线接口允许「概览 / 实时」任一权限，因此仅「概览」即可提供全部页脚数据。参见 [官方分享说明](https://docs.umami.is/docs/enable-share-url)、[汇总接口权限](https://github.com/umami-software/umami/blob/master/src/app/api/websites/%5BwebsiteId%5D/stats/route.ts)与[在线接口权限](https://github.com/umami-software/umami/blob/master/src/app/api/websites/%5BwebsiteId%5D/active/route.ts)。

把生成的完整链接写入同一 `.env`：

```env
UMAMI_SHARE_URL=https://统计域名/share/分享码
```

重启后页脚底部显示「今日 访客 123 浏览 456 · 总计 访客 999 浏览 1234 · 当前在线 66」，末尾文字按钮可手动刷新。支持带部署子路径及站点域名后缀的分享地址。此配置独立于上述追踪脚本：留空则完全隐藏该行且不请求统计；只配置分享链接也能展示 Umami 已有的数据。

- **今日**：北京时间（UTC+8）零点至查询时刻的访客与页面浏览数。
- **总计**：Umami 可查询的全部历史汇总，不是逐日访客数相加；清空数据、套餐保留期限等会影响总计。
- **当前在线**：Umami 最近五分钟内的活跃访客数，不是实时连接数。
- **刷新与故障**：后端统一缓存 15 秒、合并并发请求，失败退避 5 秒；短时间刷新可能仍显示缓存数字，不自动轮询。首次失败保留占位数字和刷新入口；手动刷新失败显示 Toast 并保留已有数字。

浏览器只向本站 GET `/api/traffic` 请求五个汇总数字及获取时间；后端换取分享 token 并查询 Umami，分享 URL、token 和原始统计响应不会注入页面。API 返回 `Cache-Control: no-store`，CDN 请绕过此接口，由程序管理缓存。原有访问追踪脚本仍按 `UMAMI_SCRIPT_URL` 配置向 Umami 上报访问，和汇总查询相互独立。

分享链接本身是 Umami 的公开只读入口，持有链接的人也能查看所勾选的概览；不需要提供管理员密码或 API key。部署环境须允许后端访问 Umami，建议使用 HTTPS。日志记录获取失败、HTTP 状态与缓存信息，不记录分享码、token 或上游响应正文。

## Docker 部署

镜像仅运行奶群主程序，PostgreSQL 自行提供；连接串中的数据库地址须能从容器访问。准备 `compose.yaml` 和 `.env` 后运行：

```sh
docker compose pull
docker compose up -d
docker compose logs -f app
```

Compose 将应用端口映射到本机 `127.0.0.1:PORT`。将域名 A/AAAA 记录指向服务器，开放 80/443 端口并准备 TLS 证书。为域名配置 HTTPS 反向代理到 `127.0.0.1:8080`（与 `PORT` 一致），并传递原始 Host 和协议头。以下配置放入已有 HTTPS Nginx `server` 块：

```nginx
location / {
    proxy_pass http://127.0.0.1:8080;
    proxy_set_header Host $host;
    proxy_set_header X-Real-IP $remote_addr;
    proxy_set_header X-Forwarded-For $remote_addr;
    proxy_set_header X-Forwarded-Proto $scheme;
}
```

在 QQ 互联网站应用中，将回调地址设为 `https://群列表域名/auth/qq/callback`。`PUBLIC_URL` 必须是用户访问的 HTTPS 根地址。QQ 与 OIDC 均未配置时，列表仍可浏览，但不能登录管理。启动时会自动建表；更新版本执行 `docker compose pull && docker compose up -d`。升级前备份外部 PostgreSQL 数据库，备份与恢复由数据库管理工具执行。

代理 / CDN 部署在 `.env` 设置 `CLIENT_IP_HEADER` 为传递访客 IP 的请求头，例如 `X-Forwarded-For`、`CF-Connecting-IP` 或 `X-Real-IP`，无需配置回源网段。程序只读取指定头；`X-Forwarded-For` 取第一个 IP，其他头须为单个 IP。留空使用 TCP 对端；请求头缺失、格式错误或过长时回退 TCP 对端，无效头名配置会拒绝启动。代理应覆盖该头以传递真实访客 IP，源站应通过代理访问。上面的 Nginx 示例适用于直接面向访客；前面还有 CDN 时，须保留 CDN 提供的真实 IP 头并配置对应名称。旧配置 `TRUSTED_PROXY_CIDRS` 已移除，请替换为 `CLIENT_IP_HEADER`。

## 请求与上传限制

会话验证仅用于账号信息、个人列表、管理 API、写操作和账号绑定回调。公开列表、统计、登录方式列表、静态文件及普通登录流程不读取现有会话；绑定回调在授权状态及流程验证通过后才读取会话。健康检查独立探测数据库可用性。

- 登录发起（含绑定）：每 IP 每分钟 20 次，全站每分钟 120 次；回调独立为每 IP 60 次、全站 240 次。频率检查在会话查询和授权流程写入前执行；发起与回调各自限制并发为 8 / 16。
- 账号写操作（含管理员变更）：每分钟 30 次、每小时 200 次，两者都满足才允许。退出登录保留身份与 CSRF 校验，使用独立并发限制，不消耗业务写入额度。API 超限返回 429 和 `Retry-After`；浏览器登录超限返回带提示的首页，仍附该响应头。
- 每账号奶群与奶站合计最多 100 条有效记录、累计最多 1000 条（包含软删除历史）。创建时在数据库事务中锁定账号并校验，两种类型及多个实例共享配额；超限返回 429 和具体提示，仍可编辑或删除。删除释放有效额度，累计额度保留；既有数据不自动清理，管理员可转移归属。

频率限制使用有界进程内存和固定时间窗口，重启会重置；多个应用实例需在反向代理 / WAF 统一限流。上传配额保存在数据库中，重启不重置。

## 二进制部署

需要 PostgreSQL 16+。从 GitHub Releases 下载 `nailist-X.Y.Z-linux-amd64`，重命名为 `nailist`，参照 `.env.example` 创建工作目录下的 `.env` 后直接运行：

```sh
chmod +x nailist
./nailist
```

程序自动加载 `.env`，文件不存在时使用环境变量；格式错误或未配置 `DATABASE_URL` 时拒绝启动。无需解压、Node.js 或单独部署前端，HTTPS 反向代理配置同 Docker 部署。启动时自动迁移数据库，更新时备份数据库、停止服务、替换二进制并重启即可。

## 日志

日志统一输出到标准错误（stderr），包含时间、级别和结构化字段；Docker 使用 `docker compose logs -f app`，裸跑可用 `./nailist 2>>nailist.log`。

在 `.env` 中设置 `LOG_LEVEL=info`（默认），支持 `debug`、`info`、`warn`、`error`，修改后重启生效：

- **DEBUG**：请求开始、会话命中/失效、表单与 CSRF 校验、数据库连接池/语句指纹/影响行数、QQ/OIDC 每阶段及上游 DNS/TCP/TLS 时序、身份匹配/事务分支、静态资源和健康检查。
- **INFO**：启动配置与数据库目标、服务就绪、普通 API 请求结果、已提交的奶群/账号变更、登录/绑定/退出、客户端取消及正常停机。普通 404 保留请求汇总。
- **WARN**：输入/权限/身份校验拒绝、超时、慢请求/慢数据库/慢上游、响应中断、辅助清理失败。内部错误已记录 ERROR 时，请求汇总为 WARN，避免重复报错。
- **ERROR**：启动失败、未恢复的数据库/上游/内部故障、panic（含堆栈）及异常退出；错误由负责最终处理的一层记录。

日志使用中文说明及稳定的 `event`、`operation`、`outcome`、`reason` 字段。请求携带服务端生成的 `request_id`（响应头 `X-Request-ID`），认证后关联 `user_id`，错误附带类型、原因及数据库 SQLSTATE。不记录请求正文、查询参数、Cookie、认证头、SQL 参数或上游响应正文。启动失败记录阶段、连接目标及排查方向，并以非零状态退出；只有数据库初始化完成才报告服务就绪。停止时最多等待 10 秒处理中的请求。

日志同步写出，不使用异步队列；文件轮转由 Docker/systemd 或日志采集器处理。完整字段、事件和排障示例见 [日志说明](docs/logging.md)。

## 本地开发

需要 Go 1.26.10+、Node.js 22.12+、pnpm 10.34.6、PostgreSQL 16+。创建数据库并在仓库根目录配置 `.env`。pnpm 版本固定在 `web/package.json`，可执行 `npm install -g pnpm@10.34.6` 安装。从仓库根目录构建前端并启动后端：

```sh
cd web && pnpm install --frozen-lockfile && pnpm run build && cd ..
go run ./cmd/server
```

运行 Go 构建、测试前，必须先构建前端（`web/dist` 不提交到仓库）。构建后端时前端资源会一并嵌入，修改前端后需要重新构建并启动 Go 服务。生成可部署二进制：

```sh
node scripts/build.mjs -o nailist
```

构建脚本将版本固定进二进制：首页页脚和 `--version` 一致。当前提交有对应 `vX.Y.Z` 标签时显示 `X.Y.Z`，否则显示 `X.Y.Z-短hash`；运行时无需 Git，移动二进制或修改配置不会改变版本。构建前须保留 Git 元数据和标签（CI 已配置完整检出）。直接 `go run` 用于开发，可能显示 `-dev`。

自行构建 Docker 镜像时传入同一编译版本：

```sh
docker build --build-arg BUILD_VERSION="$(node scripts/build-version.mjs)" -t nailist:local .
```

访问 `PUBLIC_URL`。前端开发模式：后端设置 `PUBLIC_URL=http://localhost:5173`，在 `web` 目录运行 `pnpm run dev`，API 会代理到 `localhost:8080`；QQ 回调也须使用 5173 地址。

## 功能与验证

首页提供「奶群列表 / 奶站列表」Tab，切换不改变地址，浏览器标题采用「页面名 - NaiList」。两个列表完整加载，每次加载后在客户端随机排序，兼容 API/CDN 缓存；搜索、复制和 Tab 切换保持已加载列表顺序。搜索栏右侧可刷新列表，重新请求数据并随机排序，保留搜索词。奶群搜索学校/单位、群号及隐藏关键词，支持复制群号和直达加群。「管理奶表」顶部保留账号绑定，下方按 Tab 添加、编辑、删除自己的奶群或奶站；管理列表仍按更新时间倒序。群号全站唯一。
导航栏喇叭按钮会在页面空闲后后台加载音频索引和音频缓存；点击后随机播放一段，音量固定为 36%，不会自动播放或叠加播放。音频来自固定 CDN，索引加载或播放失败时通过底部 Toast 提示，不影响其他页面功能。

奶群和奶站的添加、编辑弹窗自动保留当前标签页的草稿，关闭、取消或刷新后可恢复；草稿按账号、资源类型和个人 / 管理员作用域隔离，成功保存、删除、退出或检测到登录失效后清理。「撤销」将添加表单清空，或将编辑表单恢复到打开时的原值，操作不会写入服务器。

群链接支持粘贴 QQ 分享文本自动提取，或通过「从二维码解析」选择图片（不超过 10 MB）。使用 ZXing 支持彩色、反色、圆点等样式二维码，仅在浏览器处理，图片不会上传；识别失败保留原链接，并建议上传 QQ 默认样式的二维码。解码库及 WASM 按需加载，随应用内嵌，无需外部解码服务。

前后端均校验：学校/单位必填且最多 32 字，关键词最多 128 字（包含分隔符），群号为 5～11 位数字。群链接支持 `https://qm.qq.com/q/` 加有效短码，以及带查询参数的旧版 `https://qun.qq.com/universal-share/share?…` 链接，最多 2048 字符；粘贴、二维码识别和保存均完整保留原链接参数。文字长度按 Unicode 字符计算，支持表情。

奶站必填站点名称、学校/单位和域名，站名及单位分别最多 32 个 Unicode 字符。域名全站唯一，支持 `甘肃奶龙大学.com` 等中文域名，前后端统一转为小写 Punycode 后校验和保存，跳转也使用 Punycode；列表、编辑框和删除确认显示中文。中文与其 Punycode 写法视为同一域名，添加或编辑冲突时提示「此域名已存在, 请前往首页搜索查看」，由后端及数据库唯一索引保证。标准 DNS 格式限制：转换后标签最多 63 字符、总长最多 253 字符，后缀为至少两位英文字母；不接受 IP、端口、凭据或路径。输入框外固定显示 `https://`；粘贴完整网址或结束输入时自动删除协议、`www.`、路径及查询片段，访问固定使用 HTTPS。域名框下提示添加本站友链。奶站搜索匹配站名、学校/单位及域名，支持中文与 Punycode 两种写法。

首页页脚提供项目与反馈支持入口。奶群和奶站添加、编辑成功后提醒首页信息可能需一分钟更新。CDN 可分别缓存公开 GET `/api/groups` 和 `/api/sites`；登录、个人、管理和所有写入接口请勿缓存。

页脚提供 `/terms` 用户协议和 `/privacy` 隐私政策，支持直接访问及刷新。登录弹窗必须主动勾选同意两份政策后才能跳转，默认不勾选；勾选或取消保存在 `localStorage` 的 `nailist:agreement`。底部 Cookie 告知不阻塞浏览，确认状态独立保存在 `nailist:cookie-notice`，不会自动勾选协议，也不控制 Umami 脚本加载。两个记录以 `web/src/consent.ts` 中的 `POLICY_VERSION` 为版本值；政策修改时同步更新版本，新版本需重新确认。浏览器禁用存储时仅保留本次页面状态，确认不作为服务端签署记录。政策正文位于 `web/src/legal-content.ts`，更换运营者或调整数据处理方式时应同步修改。

操作结果和错误使用底部居中的 Toast，支持堆叠、自动消失、手动关闭与系统深浅色。保存成功的缓存提醒保留 8 秒；表单失败保留输入，弹窗关闭后通知继续显示。

弹窗、头像菜单、页面与 Tab 使用统一轻量动效，刷新图标随请求旋转；自动遵循系统「减少动态效果」设置。关闭动画期间禁止重复交互，结束后恢复焦点，草稿与保存规则保持一致。

```sh
go test ./...
go vet ./...
cd web && pnpm test && pnpm run build
```

数据库集成测试可设置专用测试库 `TEST_DATABASE_URL` 后运行 `go test ./internal/server -run TestPostgres -v`。浏览器测试需运行服务并安装 Chrome，设置 `TEST_APP_URL=http://localhost:8080` 后，在 `web` 目录执行 `pnpm run test:e2e`。

浏览器测试包含彩色圆点二维码回归用例。设置测试变量 `QQ_QR_SAMPLE` 为本地 QQ 原图路径，可额外运行实图用例（预期链接 `https://qm.qq.com/q/lnNAZvuNKS`）；原图无需提交到仓库。

## 自动检查与发版

Dependabot 自动更新 pnpm、Go、Docker 和 GitHub Actions。推送到 `master`/`main` 或提交 PR 时，自动运行格式、类型、单元/数据库/深浅色浏览器检查，并构建前端、后端和 Docker 镜像。**Build and Check** 的 Artifacts 提供 Linux x64 裸二进制 `nailist-X.Y.Z-短hash-linux-amd64`（标签提交省略 hash），前端已内嵌，保留 7 天；上传不添加 ZIP 或 tar.gz 包装。普通 Build 只检查 Docker 构建，不推送镜像。

发版在 GitHub **Actions → Release → Run workflow** 手动触发，选择默认分支和 `patch`（默认）、`minor` 或 `major`。版本基于最高稳定 `vX.Y.Z` 标签递增；无标签时从 `web/package.json` 的 `0.1.0` 起算，首次默认发 `v0.1.1`。工作流同步更新 `web/package.json`、`web/src/version.ts` 和 `cmd/server/version.go`，以 `chore: release vX.Y.Z` 提交，并原子推送默认分支与同名标签；主分支保护规则需允许工作流推送。随后检查、二进制和镜像均使用该发布提交。若默认分支在操作期间更新，推送会失败，需重新触发。

检查全部通过后，发布 Linux x64 裸二进制 `nailist-X.Y.Z-linux-amd64` 及 `SHA256SUMS.txt`，并推送 `ghcr.io/<owner>/<repo>:vX.Y.Z` 和 `:latest` 的 Linux x64 镜像。镜像仓库路径自动使用当前 GitHub 仓库的小写名称，使用内置 `GITHUB_TOKEN` 登录，无需 Docker Hub 账号或额外 Secret。仓库/组织需允许 Actions 及 `packages: write` 权限；镜像公开下载需将 GHCR 包设为公开。可通过 `docker pull ghcr.io/<owner>/<repo>:vX.Y.Z` 获取镜像。工作流仅在手动触发后创建标签和 Release，不会自动部署服务器。

发布任务需要 `contents: write`（工作流已配置）。Release 仅使用准备阶段已推送且指向发布提交的标签，避免发布时临时给旧提交创建标签触发权限限制。若旧版本工作流报 403，推送最新代码后重新 **Run workflow**；旧运行的 **Re-run jobs** 仍使用旧工作流。

后端版本可执行 `./nailist --version` 查看，同一编译版本注入页脚和 `<html data-version="X.Y.Z[-短hash]">`。

## 登录方式与账号绑定

账号独立于登录方式，支持 QQ 和一个 OIDC。同一账号可各绑定一个身份，管理页顶部可发起绑定；普通用户不支持解绑，站点管理员可协助解除绑定，但至少保留一种登录方式。身份被其他账号使用时拒绝绑定，不按邮箱/昵称自动合并账号。若已分别登录创建两个账号，也不能互相绑定；请先登录原账号再绑定新方式。

QQ 配置 `QQ_APP_ID`、`QQ_APP_KEY`，回调为 `/auth/qq/callback`。`QQ_USE_UNIONID=false` 默认使用应用内 OpenID；设为 `true` 会请求并使用 UnionID，需要 QQ 互联应用具有相应权限，未返回 UnionID 时拒绝登录。启用时先用原 QQ 应用登录一次，系统会补充 UnionID 并保留原账号；更换 App ID 前须完成此步骤。已保存的 UnionID 不会被配置切换删除。

OIDC 示例（省略则不启用）：

```env
OIDC_ID=campus
OIDC_NAME=校园账号
OIDC_ICON=https://id.example.com/icon.png
OIDC_ISSUER=https://id.example.com
OIDC_CLIENT_ID=nailist
OIDC_CLIENT_SECRET=应用密钥
OIDC_SCOPES=
```

`OIDC_ID` 是稳定的小写登录方式标识，不能为 `qq`；客户端 ID 是独立的 `OIDC_CLIENT_ID`。名称必填，图标可选（HTTPS URL 或站内路径）。Issuer 需为 HTTPS，支持标准 OIDC discovery、授权码模式及 PKCE S256；注册精确回调 `https://群列表域名/auth/campus/callback`（campus 替换为实际 ID）。密钥对公开客户端可留空，默认请求 `openid profile`，额外 scope 用空格分隔。验证 ID Token 签名、issuer、audience、过期时间和 nonce，使用 issuer + sub 识别身份，密钥和 token 不返回前端。

`profile` 已包含标准头像字段 `picture`，无需额外的头像 scope；登录服务仍需允许向该客户端提供此字段。ID Token 缺少昵称或有效头像时，会从 discovery 声明的 HTTPS UserInfo 接口补充，并核对 `sub` 与 ID Token 一致。头像仅接受不含用户名/密码的 HTTPS 地址；UserInfo 不可用时保留 ID Token 登录，身份不一致则拒绝登录。升级后退出并通过 OIDC 重新登录，可更新已保存的账号头像。若仍无头像，启用 DEBUG 查看 `userinfo` / `claims` 阶段的 `picture_present`、`avatar_accepted`；请求失败会产生 `auth.profile.unavailable` 警告。

部署升级自动将旧 QQ 账号迁移到身份表，保留账号 ID、群归属和现有会话。首次登录原 QQ 应用会补齐身份所属应用。已有绑定后不要改 OIDC_ID / OIDC_ISSUER；显示名称和图标可调整。迁移前备份数据库。

## 站点管理

先登录，在「管理奶表」的账号绑定区查看自己的内部账号 ID，在 `.env` 配置后重启：

```env
ADMIN_USER_IDS=1,2
```

只接受逗号分隔的正整数账号 ID，不能填写 QQ 群号、QQ 号或 OAuth 标识；留空关闭管理区，无效值拒绝启动。管理员从头像菜单进入「站点管理」（`/admin`）。权限由服务端检查；管理 API 必须绕过 CDN 缓存。

- **概览**：显示用户数、有效奶群数、有效奶站数、上传用户数和学校/单位数；提供近 7/30 天用户、奶群和奶站的每日新增趋势，可刷新统计。按北京时间统计现存用户与有效记录，已删除数据不计入；上传用户和学校/单位跨两类列表去重。
- **用户管理**：完整列出用户 ID、昵称、有效奶群 / 奶站数量及 QQ/OIDC 绑定资料，可搜索、解除指定绑定或删除用户。至少保留一种登录方式；解绑会使该用户的全部会话和待完成绑定失效，需重新登录。不能删除当前自己的账号。
- **奶群管理**：完整列出有效奶群的学校/单位、关键词、群号、链接和所属账号，可搜索、修改四项信息及转移归属。编辑复用校验、二维码解析、撤销和草稿，管理员草稿与个人草稿隔离；四项信息和归属一次保存。

- **奶站管理**（`/admin/sites`）：完整列出站名、学校/单位、HTTPS 域名链接和所属账号，可编辑三项信息并转移归属；复用校验、草稿与撤销。

删除用户前，须先把其所有有效奶群和奶站转移给其他用户；删除会清理该账号、绑定、会话、待完成绑定流程及历史软删除的奶群和奶站。解除绑定后，该身份再次登录可创建新的独立账号，不会继承旧账号的奶表。变更结果与首页 CDN 缓存最多可能相差一分钟。

升级自动创建奶站表和索引，不需要新增环境变量；保留原用户、绑定及奶群数据。升级前备份数据库，数据库账号须具有建表权限。
