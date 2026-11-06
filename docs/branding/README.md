# CICD 页面标识

连续交付环结合向右上方的发布箭头，使用与平台蓝色界面协调的蓝、白、青色。页头显示图形与站点名称，浏览器标签页使用同一图形。

- `cicd-logo-master.png`：生成的透明背景原图，1254 × 1254。
- `../../web/src/assets/cicd-logo.png`：页头资源，192 × 192；由 Vite 打包并生成资源哈希。
- `../../web/public/favicon-16.png`、`favicon-32.png`：浏览器 PNG 图标。
- `../../web/public/favicon.ico`：32 × 32 ICO 兼容图标。
- `../../web/public/apple-touch-icon.png`：180 × 180 触屏图标。

使用内置 image_gen 工具生成原图，图标仅作确定性的尺寸和格式导出，保留透明通道。未使用外部现有品牌素材。

生成提示词：

```text
Use case: logo-brand.
Asset type: production app logo and browser favicon for an existing Chinese CICD automatic-deployment web platform.
Primary request: Create one beautiful original compact geometric brand mark for continuous integration and deployment. Design a bold continuous pipeline loop that resolves into a forward/up-right arrow, suggesting reliable automated delivery and release. Make this ONE clear recognizable symbol, not a collection of separate icons.
Style: refined flat vector-like app icon, precise geometry, rounded corners, strong silhouette and balanced negative space. Clean professional developer-tool identity. Integrate with an existing royal-blue (#2563eb) interface, with a restrained cyan accent and a white or near-white main glyph on a deep blue rounded-square tile. Keep contrast excellent when reduced to 16 and 32 pixels. Use substantial shapes, no fine lines or tiny decorations.
Composition: square canvas, single centered icon occupying about 88% of the canvas; a small even transparent margin around the rounded-square tile. Genuine transparent alpha outside the icon. Front view, crisp edges.
Text: none. No letters, words, captions or wordmark.
Avoid: complex illustration, gear or server clipart, recognizable existing brand marks, mockup scenes, multiple variants, gradients that reduce legibility, bevels, 3D effects, cast shadows, glows, borders around the canvas, checkerboard background, watermarks.
```
