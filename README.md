# 奶龙群列表

Go + PostgreSQL + React / TypeScript。深浅色模式自动跟随系统。前端内嵌在 Go 二进制中，由同一服务提供页面和 API。

## Docker 部署

需要 Docker Compose。复制 `.env.example` 为 `.env`，设置数据库密码和站点地址（数据库密码用随机字母数字，避免连接串转义问题）：

```env
POSTGRES_PASSWORD=随机的长密码
PUBLIC_URL=https://群列表域名
QQ_APP_ID=QQ互联应用ID
QQ_APP_KEY=QQ互联应用密钥
APP_PORT=8080
```

启动全部服务：

```sh
docker compose up -d --build
docker compose logs -f app
```

应用只监听本机 `127.0.0.1:8080`，数据库不开放公网端口。将域名 A/AAAA 记录指向服务器，开放 80/443 端口并准备 TLS 证书。为域名配置 HTTPS 反向代理到 `127.0.0.1:8080`，并传递原始 Host 和协议头。以下配置放入已有 HTTPS Nginx `server` 块：

```nginx
location / {
    proxy_pass http://127.0.0.1:8080;
    proxy_set_header Host $host;
    proxy_set_header X-Real-IP $remote_addr;
    proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
    proxy_set_header X-Forwarded-Proto $scheme;
}
```

在 QQ 互联网站应用中，将回调地址设为 `https://群列表域名/auth/qq/callback`。`PUBLIC_URL` 必须是用户访问的 HTTPS 根地址。QQ 与 OIDC 均未配置时，列表仍可浏览，但不能登录管理。启动时会自动建表；更新版本执行 `docker compose up -d --build`。备份：`docker compose exec -T db pg_dump -U nailist nailist > backup.sql`。恢复：`docker compose exec -T db psql -U nailist nailist < backup.sql`（恢复到空数据库）。

## 二进制部署

需要 PostgreSQL 16+。从 GitHub Releases 下载 `nailist-X.Y.Z-linux-amd64`，重命名为 `nailist`，设置环境变量后直接运行，无需解压、Node.js 或单独部署前端：

```sh
chmod +x nailist
export DATABASE_URL='postgres://nailist:密码@127.0.0.1:5432/nailist?sslmode=disable'
export PUBLIC_URL='https://群列表域名'
export LISTEN_ADDR='127.0.0.1:8080'
./nailist
```

QQ/OIDC 环境变量见下方「登录方式与账号绑定」，HTTPS 反向代理配置同 Docker 部署。程序不会自动读取 `.env`；请通过 Shell 或服务管理器传入环境变量。启动时自动迁移数据库，更新时备份数据库、停止服务、替换二进制并重启即可。默认无需 `WEB_DIR`；仅需要覆盖内嵌前端时设置为外置资源目录。

## 本地开发

需要 Go 1.25+、Node.js 22.12+、pnpm 10.34.6、PostgreSQL 16+。创建数据库并设置环境变量：`DATABASE_URL`、`PUBLIC_URL`、`QQ_APP_ID` 和 `QQ_APP_KEY`。pnpm 版本固定在 `web/package.json`，可执行 `npm install -g pnpm@10.34.6` 安装。从仓库根目录构建前端并启动后端：

```sh
cd web && pnpm install --frozen-lockfile && pnpm run build && cd ..
go run ./cmd/server
```

运行 Go 构建、测试前，必须先构建前端（`web/dist` 不提交到仓库）。构建后端时前端资源会一并嵌入，修改前端后需要重新构建并启动 Go 服务。生成可部署二进制：

```sh
go build -trimpath -ldflags="-s -w" -o nailist ./cmd/server
```

访问 `PUBLIC_URL`。前端开发模式：后端设置 `PUBLIC_URL=http://localhost:5173`，在 `web` 目录运行 `pnpm run dev`，API 会代理到 `localhost:8080`；QQ 回调也须使用 5173 地址。`.env.example` 是配置参考，Go 程序直接读取进程环境变量。

## 功能与验证

首页完整加载群列表，实时搜索学校/单位、群号及隐藏关键词；支持复制群号和直达加群。登录用户可添加、编辑、删除本人群信息。

```sh
go test ./...
go vet ./...
cd web && pnpm test && pnpm run build
```

数据库集成测试可设置专用测试库 `TEST_DATABASE_URL` 后运行 `go test ./internal/server -run TestPostgres -v`。浏览器测试需运行服务并安装 Chrome，设置 `TEST_APP_URL=http://localhost:8080` 后，在 `web` 目录执行 `pnpm run test:e2e`。

## 自动检查与发版

Dependabot 自动更新 pnpm、Go、Docker 和 GitHub Actions。推送到 `master`/`main` 或提交 PR 时，自动运行格式、类型、单元/数据库/深浅色浏览器检查，并构建前端、后端和 Docker 镜像。**Build and Check** 的 Artifacts 提供 Linux x64 裸二进制 `nailist-dev-<commit>-linux-amd64`，前端已内嵌，保留 7 天；上传不添加 ZIP 或 tar.gz 包装。普通 Build 只检查 Docker 构建，不推送镜像。

发版在 GitHub **Actions → Release → Run workflow** 手动触发，选择默认分支和 `patch`（默认）、`minor` 或 `major`。版本基于最高稳定 `vX.Y.Z` 标签递增；无标签时从 `web/package.json` 的 `0.1.0` 起算，首次默认发 `v0.1.1`。工作流同步更新 `web/package.json`、`web/src/version.ts` 和 `cmd/server/version.go`，以 `chore: release vX.Y.Z` 提交，并原子推送默认分支与同名标签；主分支保护规则需允许工作流推送。随后检查、二进制和镜像均使用该发布提交。若默认分支在操作期间更新，推送会失败，需重新触发。

检查全部通过后，发布 Linux x64 裸二进制 `nailist-X.Y.Z-linux-amd64` 及 `SHA256SUMS.txt`，并推送 `ghcr.io/<owner>/<repo>:vX.Y.Z` 和 `:latest` 的 Linux x64 镜像。镜像仓库路径自动使用当前 GitHub 仓库的小写名称，使用内置 `GITHUB_TOKEN` 登录，无需 Docker Hub 账号或额外 Secret。仓库/组织需允许 Actions 及 `packages: write` 权限；镜像公开下载需将 GHCR 包设为公开。可通过 `docker pull ghcr.io/<owner>/<repo>:vX.Y.Z` 获取镜像。工作流仅在手动触发后创建标签和 Release，不会自动部署服务器。

发布任务需要 `contents: write`（工作流已配置）。Release 仅使用准备阶段已推送且指向发布提交的标签，避免发布时临时给旧提交创建标签触发权限限制。若旧版本工作流报 403，推送最新代码后重新 **Run workflow**；旧运行的 **Re-run jobs** 仍使用旧工作流。

后端版本可执行 `./nailist --version` 查看，前端版本写入页面 `<html data-version="X.Y.Z">`。

## 登录方式与账号绑定

账号独立于登录方式，支持 QQ 和一个 OIDC。同一账号可各绑定一个身份，管理页顶部可发起绑定；绑定后不支持解绑。身份被其他账号使用时拒绝绑定，不按邮箱/昵称自动合并账号。若已分别登录创建两个账号，也不能互相绑定；请先登录原账号再绑定新方式。

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

部署升级自动将旧 QQ 账号迁移到身份表，保留账号 ID、群归属和现有会话。首次登录原 QQ 应用会补齐身份所属应用。已有绑定后不要改 OIDC_ID / OIDC_ISSUER；显示名称和图标可调整。迁移前按部署章节备份数据库。
