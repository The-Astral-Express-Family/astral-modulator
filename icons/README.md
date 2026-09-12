# Astral 品牌 Logo 组

全部由 [`generate.mjs`](./generate.mjs) 参数化生成的 SVG 品牌资产：**5 个概念 × 12 个变体 = 60 个 SVG**，
另附 [`preview.html`](./preview.html) 预览页（直接双击打开或任意静态服务器均可）。

## 目录结构与命名

```text
icons/
  generate.mjs            生成器（唯一事实来源，改设计/改色都在这里）
  preview.html            全量预览页（浅底/深底/tile/小尺寸阶梯）
  01-orbit/               概念目录（编号-名称）
    transparent/          透明底
      orbit-white-icon.svg      纯 icon
      orbit-white-lockup.svg    icon + astral 字标（横排）
      orbit-black-icon.svg
      ...
      orbit-primary-lockup.svg
    tile/                 带底色（圆角 app tile，512×512）
      orbit-white-icon.svg      白标 → 主题色底
      orbit-white-lockup.svg    带字 → icon 上、字标下堆叠
      orbit-black-icon.svg      黑标 → 白底（带 8% 黑细边防隐形）
      orbit-primary-icon.svg    主题标 → 浅雾灰底
      ...
  02-nova/  03-wavea/  04-peak/  05-pulse/   同构
```

命名规则：`<概念>-<white|black|primary>-<icon|lockup>.svg`，所在子目录决定透明/带底。

## 变体矩阵（每概念 12 个）

| | 纯 icon | 带文字 lockup |
|---|---|---|
| **透明底** | 3 色 × 1 | 3 色 × 1 |
| **带底色 tile** | 3 色 × 1 | 3 色 × 1（堆叠排版） |

## 色板（主题色读自 `web/src/assets/index.css`）

| 名称 | 值 | 来源 |
|---|---|---|
| white | `#FFFFFF` | — |
| black | `#000000` | — |
| primary | `#161B1D` | `--primary: oklch(0.218 0.008 223.9)`（mist 预设，深墨蓝黑） |
| tile 浅底 | `#F1F3F3` | `--secondary: oklch(0.963 0.002 197.1)` |

当前主题是单色系（primary 接近墨黑），因此 primary 与 black 变体差异细微属预期；
将来若换成彩色 primary，只需改 `generate.mjs` 顶部 `PALETTE` 后重新运行。

## 五个概念

| # | 名称 | 意象 | 适用 |
|---|---|---|---|
| 01 | **Orbit 轨道** | 行星 + 轨道环 + 环上卫星：控制面居中，agents/任务沿环运行 | 最「工具/航天」感，适合 favicon、app icon |
| 02 | **Nova 星芒** | 新星迸发（主星 + 伴星对角动势） | 亲和、现代 SaaS 感 |
| 03 | **Wave-A 调制波 A** | 解构式 A：圆拱 + 横贯画面的宽波 + 两根分离外撇的腿，波即横杠（用户草图定型） | 品牌叙事最强，推荐作主标 |
| 04 | **Peak 山岳 A** | 实底圆角山形 A，负形镂空四角星 | 稳健、有分量，深浅底都稳 |
| 05 | **Pulse 调制波** | 高斯包络正弦载波 + 中心节点，"Modulator" 的直译意象 | 信号/语音场景素材、装饰纹样 |

## 字标（wordmark）

lockup 中的小写 `astral` 为**手绘 monoline 路径**（圆头圆角，笔画几何与各 icon 同族），
无字体依赖，任何环境渲染一致。如需换字体方案，Inter（web 端在用）是最贴近的替代。

## 重新生成

```bash
cd icons && node generate.mjs     # 无依赖，纯 Node ≥ 18
```

会先清掉 `NN-*` 旧目录再全量重写（含 preview.html）。改几何 → 各 concept 函数；
改色 → 顶部 `PALETTE` / `TILE_BG`；加概念 → 新 icon 函数 + `CONCEPTS` 登记。

## 许可

与仓库一致，MIT。
