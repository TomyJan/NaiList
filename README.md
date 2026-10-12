# 奶龙群列表

Go + PostgreSQL + React / TypeScript。深浅色模式自动跟随系统。

## 部署

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

在 QQ 互联网站应用中，将回调地址设为 `https://群列表域名/auth/qq/callback`。`PUBLIC_URL` 必须是用户访问的 HTTPS 根地址。未配置 QQ 凭据时，列表仍可浏览，但不能登录管理。启动时会自动建表；更新版本执行 `docker compose up -d --build`。备份：`docker compose exec -T db pg_dump -U nailist nailist > backup.sql`。恢复：`docker compose exec -T db psql -U nailist nailist < backup.sql`（恢复到空数据库）。

## 本地开发

需要 Go 1.25+、Node.js 22+、PostgreSQL 16+。创建数据库并设置环境变量：`DATABASE_URL`、`PUBLIC_URL`、`QQ_APP_ID` 和 `QQ_APP_KEY`。从仓库根目录构建前端并启动后端：

```sh
cd web && npm ci && npm run build && cd ..
go run ./cmd/server
```

访问 `PUBLIC_URL`。前端开发模式：后端设置 `PUBLIC_URL=http://localhost:5173`，在 `web` 目录运行 `npm run dev`，API 会代理到 `localhost:8080`；QQ 回调也须使用 5173 地址。`.env.example` 是配置参考，Go 程序直接读取进程环境变量。

## 功能与验证

首页完整加载群列表，实时搜索学校/单位、群号及隐藏关键词；支持复制群号和直达加群。登录用户可添加、编辑、删除本人群信息。

```sh
go test ./...
go vet ./...
cd web && npm test && npm run build
```

数据库集成测试可设置专用测试库 `TEST_DATABASE_URL` 后运行 `go test ./internal/server -run TestPostgres -v`。浏览器测试需运行服务并安装 Chrome，设置 `TEST_APP_URL=http://localhost:8080` 后，在 `web` 目录执行 `npm run test:e2e`。

## 自动检查与发版

Dependabot 自动更新 npm、Go、Docker 和 GitHub Actions。推送到 `master`/`main` 或提交 PR 时，自动运行格式、类型、单元/数据库/深浅色浏览器检查，并构建前端、后端和 Docker 镜像。**Build and Check** 的 Artifacts 提供六平台 `binary-<系统>-<架构>` 压缩包（含前端、配置示例和说明），保留 7 天；开发包名包含提交 SHA。普通 Build 只检查 Docker 构建，不推送镜像。

发版在 GitHub **Actions → Release → Run workflow** 手动触发，选择默认分支和 `patch`（默认）、`minor` 或 `major`。版本基于最高稳定 `vX.Y.Z` 标签递增；无标签时从 `web/package.json` 的 `0.1.0` 起算，首次默认发 `v0.1.1`。发版不修改前端私有包版本。

检查全部通过后，发布包含前端的 Linux/macOS/Windows（amd64、arm64）压缩包及 SHA256 校验文件，并推送 `ghcr.io/<owner>/<repo>:vX.Y.Z` 和 `:latest` 多架构镜像。镜像仓库路径自动使用当前 GitHub 仓库的小写名称，使用内置 `GITHUB_TOKEN` 登录，无需 Docker Hub 账号或额外 Secret。仓库/组织需允许 Actions 及 `packages: write` 权限；镜像公开下载需将 GHCR 包设为公开。可通过 `docker pull ghcr.io/<owner>/<repo>:vX.Y.Z` 获取镜像。工作流仅在手动触发后创建标签和 Release，不会自动部署服务器。

使用二进制包时，解压后设置 `DATABASE_URL`、`PUBLIC_URL` 和 QQ 环境变量，在解压目录运行 `./nailist`（Windows 为 `nailist.exe`），或通过 `WEB_DIR` 指定解压后的 `web/dist`。
