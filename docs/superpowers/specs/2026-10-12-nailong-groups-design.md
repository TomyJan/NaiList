# 奶龙群列表设计

## 目标
公开浏览群列表；QQ 互联登录后管理本人提交的奶群。

## 技术方案
- 前端：React + TypeScript + Vite + 原生 CSS，响应式单列列表，不引入重型组件库。
- 后端：Go + chi，HTTP/认证与群数据校验、PostgreSQL 数据访问分离。
- 数据库：PostgreSQL。
- 登录：QQ 互联 OAuth，服务端 session（HttpOnly、SameSite）。

## 页面
- `/`：顶部导航、搜索框、本地过滤、完整群列表；群号复制，群链接直达。
- `/manage`：登录用户的新增、编辑、软删除列表。
- `/auth/qq`、`/auth/qq/callback`：QQ 登录流程。
- 未登录访问管理页显示 QQ 登录入口。

## 数据模型
`groups(id, owner_id, organization, keywords, qq_number, join_url, created_at, updated_at, deleted_at)`。
原有三个字段必填，关键词选填，支持中英文逗号分隔的简称和 emoji；完整列表接口返回关键词供前端本地搜索，但公开页面不展示。搜索实时匹配学校/单位（显示名称）、群号及任一关键词。群号唯一；链接仅允许 http/https；列表默认过滤 deleted_at。

## API
- `GET /api/groups`：返回全部有效群（包含搜索关键词），前端本地过滤，无分页，无服务端搜索参数。
- `GET /api/my/groups`：登录用户本人提交的有效群。
- `POST /api/groups`、`PUT /api/groups/:id`、`DELETE /api/groups/:id`。
- `GET /api/me`、`POST /api/auth/logout`。

## 交互与错误
复制成功显示短提示；表单使用必填/格式校验与服务端校验；网络错误显示可重试提示；删除使用确认弹窗；OAuth 失败回到首页并提示。

## 安全
所有写操作校验 session 与 owner_id；CSRF token 与 Origin；参数化 SQL；限制登录回调和写接口并发，生产代理配置请求速率限制；不记录 OAuth 密钥。

## 验证
后端单元测试覆盖校验、权限、软删除；API 集成测试覆盖登录态和 CRUD；前端测试覆盖搜索、复制、表单和响应式关键断点。
