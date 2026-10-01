# Astral Brand Logo Collection v2

一批重新整理、完全由 SVG 路径构成的 Astral logo 方向。这里是**独立候选集**，不会覆盖或触碰上级 `icons/01-*` 至 `icons/06-*` 的既有资产。

## 交付概览

- **3 套设计方向**：Constellation、Prism、Modwave
- **每套 12 个 SVG**：白 / 黑 / 当前主题色 × 纯 icon / 带 `astral` 文字 × 透明 / 圆角底色
- **共 36 个 SVG**，另含生成器、清单、预览页和无依赖校验脚本
- 所有 artwork、字标均为内嵌路径；不依赖字体、外部 CSS、渐变或滤镜

## 目录

```text
collection-v2/
├── generate.mjs       # 唯一事实来源：颜色、几何、字标与批量输出
├── validate.py        # Python 标准库 XML / 矩阵 / 颜色 / 底色校验
├── manifest.json      # 输出清单、主题色解析结果与设计说明
├── preview.html       # checkerboard、浅底、tile、小尺寸预览
├── 01-constellation/
│   ├── transparent/   # 无画布底色
│   └── tile/          # 512×512 圆角底色
├── 02-prism/
└── 03-modwave/
```

文件名固定为：

```text
<concept>-<white|black|primary>-<icon|lockup>.svg
```

`lockup` 在透明目录中是横向 icon + `astral` 字标，在 tile 目录中是方形内上下堆叠的 icon + 字标。

## 设计方向

### 01 · Constellation / 星群

四角开放的节点星群包围一颗四向星芒。断开的外框保留呼吸感，圆点表达 agent / task 节点与天体导航；适合 favicon、控制台入口和需要轻盈精确感的场景。

### 02 · Prism / 折面

四个实心几何平面组成一个厚重的 A。与线性方案拉开距离，负空间和横向折面让它在 app tile、启动页和小尺寸图标中保持清晰。

### 03 · Modwave / 调制波

平滑的 cubic Bézier 波形穿过解构 A，中心节点强调调制器。它直接吸收草图中“拱顶 + 波 + 分离双腿”的构图意图，但将不规则手绘线条改成可缩放、可复用的精确曲线。

## 颜色来源与配对

生成器只读取 `web/src/assets/index.css` 的 `:root` 令牌：

| token / 变体 | 当前解析值 |
|---|---|
| `white` | `#ffffff` |
| `black` | `#000000` |
| `primary` | `#161b1d`（来自 `--primary: oklch(0.218 0.008 223.9)`） |
| `secondary`（tile 辅助底色） | `#f1f3f3`（来自 `--secondary: oklch(0.963 0.002 197.1)`） |

底色配对规则：

- white artwork → `primary` 深色 tile
- black artwork → 白色 tile
- primary artwork → `secondary` 浅雾灰 tile

虽然当前 `primary` 接近黑色，`black` 与 `primary` 在视觉上会很接近；这是 CSS 主题本身的结果，而不是 SVG 使用了错误颜色。所有输出均序列化为六位 hex，不会把 `oklch(...)`、`var(...)` 或 `currentColor` 留在文件中。

## 生成与校验

在仓库根目录执行：

```bash
node icons/collection-v2/generate.mjs
python icons/collection-v2/validate.py
node --check icons/collection-v2/generate.mjs
```

生成器会先删除**本目录内**的 `01-*`、`02-*`、`03-*` 目录，再重建；不会删除旧的上级概念目录。

`validate.py` 会确认：

- 恰好 36 个 SVG，矩阵无缺失或多余文件；
- XML、`viewBox`、尺寸有效；
- 透明变体没有全画布背景，tile 变体恰有一个 `512×512 rx=112` 圆角底；
- 颜色为显式六位 hex；没有 `<text>`、`NaN`、`Infinity`、`oklch`、`var` 或 `currentColor`；
- `manifest.json` 存在。

打开 `preview.html` 可检查 checkerboard 上的透明白标、三种底色以及 24 / 48 / 96 px 小尺寸表现。白色透明标在白底上故意不可见，应以 checkerboard 或深色背景判断。
