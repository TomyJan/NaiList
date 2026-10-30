# 素材来源

## 奶龙图标

`public/nailong.png` 来自七创社官网的奶龙角色头像，用于导航、PNG 图标和 Apple Touch Icon；`public/favicon.ico` 为其缩小版本。`public/nailong_100px.png` 是仓库保留的缩小图，当前页面未引用。

- 官网：https://www.nailoong.com/
- 原图：https://www.nailoong.com/_ipx/q_85/img/stars/btn_nl.png
- 获取日期：2026-10-12

奶龙角色及原图版权归原权利人所有。官网未提供该图的开源许可；本项目的代码许可不覆盖该素材。

## 奶蛙像素宠物

`public/naiwa.png` 取自开源项目 [codex-pet-naiwa](https://github.com/sandytruant/codex-pet-naiwa)（奶蛙，Codex 像素宠物），**MIT License, Copyright (c) 2026 sandytruant**，可随本项目一同分发，需保留版权及许可全文，见随素材分发的 [naiwa.LICENSE.txt](public/naiwa.LICENSE.txt)。

- 原始素材：`spritesheet.webp`（1536×2288，8×11 网格，单元格 192×208，透明背景）
- 取用范围：仅第 1 行第 1 格（待机姿态），裁去透明边后为 116×198
- 转换与缩放：保留原帧配色及透明背景，导出 PNG，使用浏览器默认平滑缩放
- 用途：主页背景装饰，整体 50% 不透明度，仅 `/` 显示

奶蛙的黄色同时作为本站品牌主色（取 `#f8c820`）。

## 外部资源

- QQ 登录图标：由 `web/src/Account.tsx` 直接加载 [QQ 官方图片](https://static-res.qq.com/static-res/imqq/qq-logo.png)，版权归原权利人，本仓库不分发副本。
- MiSans：由 `web/index.html` 引入 [CDN 样式](https://cdn.amoe.cc/web-static/public/font/misans/misans.css)，字体文件从小米字体服务加载，本仓库不分发字体副本。
- 随机音频：由 `web/src/audio.ts` 从 [CDN 索引](https://cdn.amoe.cc/web-static/private/tomyjan-website/nailong/audio/index.json) 加载，浏览器后台缓存，未打包进二进制。

二维码识别使用 `zxing-wasm/reader`；JS / WASM 随前端构建内嵌，按需加载。第三方代码许可见对应依赖包，不能套用到上述角色图像或音频。
