# 开发与验证

部署及运行配置见 [README](../README.md)，环境变量以 [.env.example](../.env.example) 为准。下列环境变量赋值示例使用 POSIX shell；PowerShell 请使用 `$env:变量名='值'`。

## 目录与接口

| 目录                                                        | 职责                                             |
| ----------------------------------------------------------- | ------------------------------------------------ |
| `cmd/server`                                                | 配置、编译版本、数据库初始化及 HTTP 服务生命周期 |
| `internal/server`                                           | 路由、会话、CSRF、限流、列表缓存和 HTTP 处理     |
| `internal/accounts` / `internal/admin`                      | 账号身份、管理事务及统计                         |
| `internal/groups` / `internal/sites` / `internal/resources` | 字段校验、列表存储和跨类型上传配额               |
| `internal/logging` / `internal/traffic`                     | 结构化日志、Umami 汇总代理与缓存                 |
| `migrations`                                                | 内嵌 SQL 迁移，启动时自动执行                    |
| `web/src` / `web/e2e`                                       | React 页面、浏览器端逻辑及深浅色交互回归         |
| `scripts` / `.github/workflows`                             | 版本生成、构建和 CI / 发版                       |

路由以 `internal/server/server.go` 为准：

| 访问范围         | 接口                                                                                                                                            |
| ---------------- | ----------------------------------------------------------------------------------------------------------------------------------------------- |
| 公开，无会话查询 | `GET /api/groups`、`GET /api/sites`、`GET /api/auth/providers`、`GET /api/traffic`、`GET /api/health`                                           |
| 当前账号         | `GET /api/me`（未登录返回 null）、`GET /api/my/groups`、`GET /api/my/sites`                                                                     |
| 登录账号写入     | `POST /api/groups`、`POST /api/sites`、`PUT` / `DELETE /api/groups/{id}`、`PUT` / `DELETE /api/sites/{id}`                                      |
| 登录与绑定       | `GET /auth/{provider}`、`GET /auth/{provider}/callback`、`POST /api/auth/bind/{provider}`、`POST /api/auth/logout`                              |
| 管理员查询       | `GET /api/admin/stats`、`GET /api/admin/users`、`GET /api/admin/groups`、`GET /api/admin/sites`                                                 |
| 管理员写入       | `PUT /api/admin/groups/{id}`、`PUT /api/admin/sites/{id}`、`DELETE /api/admin/users/{id}`、`DELETE /api/admin/users/{id}/identities/{provider}` |

写接口使用 JSON（需要正文时），验证会话、Origin 和 `X-CSRF-Token`；前端统一通过 `web/src/api.ts` 发起。公开列表完整返回有效条目及搜索字段，不返回所属账号 ID，搜索和随机排序在前端进行。个人写入校验归属，管理员编辑额外接收 `ownerId` 并原子保存信息与归属；管理响应不得被 CDN 缓存。

## 单元与构建检查

使用 README 中要求的 Go、Node.js、pnpm 版本。先安装依赖、构建前端，再执行 Go 检查；`web/assets.go` 必须能嵌入 `web/dist`：

```sh
cd web
pnpm install --frozen-lockfile
pnpm exec prettier --check src e2e '*.ts' package.json tsconfig.json index.html
pnpm test
pnpm run build
cd ..
node --test scripts/*.test.mjs
go test -count=1 ./...
go vet ./...
```

前端构建包含 TypeScript 检查。仅修改前端后也要重新编译 Go 服务，浏览器才会使用新的内嵌资源。Git 工作区换行规则应与 CI 一致，避免把 CRLF 差异误认为代码格式问题。

## 数据库集成测试

使用独立 PostgreSQL 测试数据库，账号需要建表、建 schema 权限。测试会执行迁移、插入及清理数据，不要连接生产库。`go test` 不会自动加载应用的 `.env`；须显式设置测试连接：

```sh
export TEST_DATABASE_URL='postgres://用户:密码@127.0.0.1:5432/nailist_test?sslmode=disable'
go test -count=1 ./...
```

未设置 `TEST_DATABASE_URL` 时，数据库集成用例会跳过，不能将此结果当作集成验证通过。集成测试分布在 `internal/accounts`、`internal/admin`、`internal/server`，仅运行 server 包不能覆盖全部事务和身份行为。CI 额外运行 `go test -race -count=1 ./...`；本机运行 race 检测需要支持 CGO 的 C 编译环境。

## 浏览器回归

在独立测试配置中启动已构建的 Go 服务。让 `PUBLIC_URL` 和 `TEST_APP_URL` 使用同一地址；Playwright 默认 `http://127.0.0.1:8088`，不负责启动应用。不要将用例运行在生产服务。

完整覆盖启动时注入 Umami 的用例，服务启动前设置以下测试值；测试会拦截该地址，不需要真实统计服务或 OAuth 密钥：

```env
PUBLIC_URL=http://127.0.0.1:8080
PORT=8080
UMAMI_SCRIPT_URL=http://umami.test/script.js
UMAMI_WEBSITE_ID=00000000-0000-0000-0000-000000000001
```

确认 `/api/health` 成功后，在另一个终端运行：

```sh
export TEST_APP_URL=http://127.0.0.1:8080
cd web
pnpm exec playwright install chrome
pnpm run test:e2e
```

Linux 安装浏览器及系统依赖可用 `pnpm exec playwright install --with-deps chrome`。用例自动分别运行浅色和深色；音频、登录资料、列表等场景使用本地 fixture / 请求拦截。数据库事务由 Go / PostgreSQL 集成测试验证，OAuth 协议流程使用模拟上游；真实登录应用的授权与回调仍需部署后联调。

彩色圆点二维码自带回归素材；可额外将 `QQ_QR_SAMPLE` 设为本地 QQ 原图路径，验证预期链接 `https://qm.qq.com/q/lnNAZvuNKS`，未设置时仅跳过该可选用例。原图无需提交。

日常 CI 见 [.github/workflows/code-check.yml](../.github/workflows/code-check.yml)，包含格式、类型、测试、依赖安全检查、二进制与镜像构建。[Prepare Release](../.github/workflows/release-prepare.yml) 手动准备提交和标签，[Release](../.github/workflows/release.yml) 响应标签推送，独立验证、构建并发布，不嵌套调用其他工作流。配置 `RELEASE_TOKEN` 及发版步骤见 [自动检查与发版](../README.md#自动检查与发版)。
