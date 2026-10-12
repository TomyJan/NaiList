# 单文件发布实现计划

目标：Build 和 Release 上传 Linux amd64 裸二进制，前端内嵌并由后端提供。

- [x] 测试：空工作目录下首页、管理页和 JS/CSS/图标正常，未知路径返回 404。
- [x] `web/assets.go` 嵌入构建后的 `dist`；始终使用内嵌资源。
- [x] CI 先构建前端，再检查 Go 和构建二进制；使用 `archive: false` 上传，Release 下载并发布原文件。
- [x] Docker 在 Go 构建阶段嵌入前端，运行镜像仅复制二进制。
- [x] README 补充裸二进制部署、源码构建顺序和更新说明。
- [x] 验证 Go、前端构建、工作流语法及独立运行后提交。

验证：Go 测试（含 PostgreSQL）、vet、前端构建、10 项深浅色浏览器测试、actionlint、Compose 配置校验通过。Linux amd64 裸二进制构建成功；本机二进制在仅有可执行文件的目录运行，首页与内嵌脚本正常响应。Docker 镜像构建交由 CI 验证。
