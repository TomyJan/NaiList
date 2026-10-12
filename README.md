# 奶龙群列表

Go + PostgreSQL + React / TypeScript。深浅色模式自动跟随系统。前端内嵌在 Go 二进制中，由同一服务提供页面和 API。

## 配置

准备已创建数据库的 PostgreSQL 16+，将 `.env.example` 复制为工作目录下的 `.env`，填写连接串、站点地址和登录方式。Docker 和裸跑使用相同字段，进程环境变量优先于 `.env`。

```env
DATABASE_URL=postgres://nailist:密码@数据库地址:5432/nailist?sslmode=disable
PUBLIC_URL=https://群列表域名
QQ_APP_ID=QQ互联应用ID
QQ_APP_KEY=QQ互联应用密钥
PORT=8080
```

数据库只使用 `DATABASE_URL`，连接串中的密码特殊字符需 URL 编码。服务监听 `PORT`（默认 8080），前端固定内嵌。QQ/OIDC 配置见下方「登录方式与账号绑定」。

Umami 在同一 `.env` 中配置：

```env
UMAMI_SCRIPT_URL=https://统计域名/script.js
UMAMI_WEBSITE_ID=Umami网站ID
```

两项同时留空则关闭统计。修改后重启程序（Docker 执行 `docker compose up -d`）即可；启动时注入页面并设置对应 CSP，无需重新构建前端。MiSans 通过 CDN 加载，字体文件来自小米字体服务。

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
    proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
    proxy_set_header X-Forwarded-Proto $scheme;
}
```

在 QQ 互联网站应用中，将回调地址设为 `https://群列表域名/auth/qq/callback`。`PUBLIC_URL` 必须是用户访问的 HTTPS 根地址。QQ 与 OIDC 均未配置时，列表仍可浏览，但不能登录管理。启动时会自动建表；更新版本执行 `docker compose pull && docker compose up -d`。升级前备份外部 PostgreSQL 数据库，备份与恢复由数据库管理工具执行。

## 二进制部署

需要 PostgreSQL 16+。从 GitHub Releases 下载 `nailist-X.Y.Z-linux-amd64`，重命名为 `nailist`，参照 `.env.example` 创建工作目录下的 `.env` 后直接运行：

```sh
chmod +x nailist
./nailist
```

程序自动加载 `.env`，文件不存在时使用环境变量；格式错误或未配置 `DATABASE_URL` 时拒绝启动。无需解压、Node.js 或单独部署前端，HTTPS 反向代理配置同 Docker 部署。启动时自动迁移数据库，更新时备份数据库、停止服务、替换二进制并重启即可。

## 本地开发

需要 Go 1.25+、Node.js 22.12+、pnpm 10.34.6、PostgreSQL 16+。创建数据库并在仓库根目录配置 `.env`。pnpm 版本固定在 `web/package.json`，可执行 `npm install -g pnpm@10.34.6` 安装。从仓库根目录构建前端并启动后端：

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

首页完整加载群列表，实时搜索学校/单位、群号及隐藏关键词；支持复制群号和直达加群。登录用户可添加、编辑、删除本人群信息。群号全站唯一，添加或编辑重复群号时提示前往首页搜索查看。

添加、编辑弹窗自动保留当前标签页的草稿，关闭、取消或刷新后可恢复；草稿按账号和奶群隔离，成功保存、删除或退出登录后清理。「撤销」将添加表单清空，或将编辑表单恢复到打开时的原值，操作不会写入服务器。

群链接支持粘贴 QQ 分享文本自动提取，或通过「从二维码解析」选择图片（不超过 10 MB）。二维码仅在浏览器处理，图片不会上传；识别失败保留原链接，解码库按需加载。

前后端均校验：学校/单位必填且最多 32 字，关键词最多 128 字（包含分隔符），群号为 5～11 位数字，群链接为 `https://qm.qq.com/q/` 加有效短码。文字长度按 Unicode 字符计算，支持表情。

首页页脚提供项目与反馈支持入口。添加、编辑成功后提醒首页群信息可能需一分钟更新，以适配 `/api/groups` 的 CDN 缓存；登录、管理与写入接口请勿缓存。

```sh
go test ./...
go vet ./...
cd web && pnpm test && pnpm run build
```

数据库集成测试可设置专用测试库 `TEST_DATABASE_URL` 后运行 `go test ./internal/server -run TestPostgres -v`。浏览器测试需运行服务并安装 Chrome，设置 `TEST_APP_URL=http://localhost:8080` 后，在 `web` 目录执行 `pnpm run test:e2e`。

## 自动检查与发版

Dependabot 自动更新 pnpm、Go、Docker 和 GitHub Actions。推送到 `master`/`main` 或提交 PR 时，自动运行格式、类型、单元/数据库/深浅色浏览器检查，并构建前端、后端和 Docker 镜像。**Build and Check** 的 Artifacts 提供 Linux x64 裸二进制 `nailist-X.Y.Z-短hash-linux-amd64`（标签提交省略 hash），前端已内嵌，保留 7 天；上传不添加 ZIP 或 tar.gz 包装。普通 Build 只检查 Docker 构建，不推送镜像。

发版在 GitHub **Actions → Release → Run workflow** 手动触发，选择默认分支和 `patch`（默认）、`minor` 或 `major`。版本基于最高稳定 `vX.Y.Z` 标签递增；无标签时从 `web/package.json` 的 `0.1.0` 起算，首次默认发 `v0.1.1`。工作流同步更新 `web/package.json`、`web/src/version.ts` 和 `cmd/server/version.go`，以 `chore: release vX.Y.Z` 提交，并原子推送默认分支与同名标签；主分支保护规则需允许工作流推送。随后检查、二进制和镜像均使用该发布提交。若默认分支在操作期间更新，推送会失败，需重新触发。

检查全部通过后，发布 Linux x64 裸二进制 `nailist-X.Y.Z-linux-amd64` 及 `SHA256SUMS.txt`，并推送 `ghcr.io/<owner>/<repo>:vX.Y.Z` 和 `:latest` 的 Linux x64 镜像。镜像仓库路径自动使用当前 GitHub 仓库的小写名称，使用内置 `GITHUB_TOKEN` 登录，无需 Docker Hub 账号或额外 Secret。仓库/组织需允许 Actions 及 `packages: write` 权限；镜像公开下载需将 GHCR 包设为公开。可通过 `docker pull ghcr.io/<owner>/<repo>:vX.Y.Z` 获取镜像。工作流仅在手动触发后创建标签和 Release，不会自动部署服务器。

发布任务需要 `contents: write`（工作流已配置）。Release 仅使用准备阶段已推送且指向发布提交的标签，避免发布时临时给旧提交创建标签触发权限限制。若旧版本工作流报 403，推送最新代码后重新 **Run workflow**；旧运行的 **Re-run jobs** 仍使用旧工作流。

后端版本可执行 `./nailist --version` 查看，同一编译版本注入页脚和 `<html data-version="X.Y.Z[-短hash]">`。

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

部署升级自动将旧 QQ 账号迁移到身份表，保留账号 ID、群归属和现有会话。首次登录原 QQ 应用会补齐身份所属应用。已有绑定后不要改 OIDC_ID / OIDC_ISSUER；显示名称和图标可调整。迁移前备份数据库。
