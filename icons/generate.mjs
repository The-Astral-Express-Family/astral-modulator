#!/usr/bin/env node
/**
 * Astral 品牌 Logo 生成器 —— icons/ 下全部 SVG 的单一事实来源。
 *
 * 用法:  node generate.mjs
 * 产出:  4 个概念 x (白 / 黑 / 主题色) x (纯 icon / 带文字 lockup) x (透明 / 带底色 tile)
 *        = 每概念 12 个 SVG，共 48 个，另附 preview.html 预览页。
 *
 * 主题色取自 web/src/assets/index.css 的 --primary（oklch 0.218 0.008 223.9）。
 * 改主题只需动 PALETTE / TILE，重新运行本脚本即可全量重生成。
 */

import { writeFileSync, mkdirSync, rmSync, readdirSync, statSync } from 'node:fs';
import { join, dirname } from 'node:path';
import { fileURLToPath } from 'node:url';

const ROOT = dirname(fileURLToPath(import.meta.url));

/* ---------------------------------------------------------------- colors */

/** oklch -> #rrggbb（CSS Color 4 OKLab -> linear sRGB -> gamma） */
function oklchToHex(L, C, H) {
  const h = (H * Math.PI) / 180;
  const a = C * Math.cos(h);
  const b = C * Math.sin(h);
  const l_ = L + 0.3963377774 * a + 0.2158037573 * b;
  const m_ = L - 0.1055613458 * a - 0.0638541728 * b;
  const s_ = L - 0.0894841775 * a - 1.291485548 * b;
  const l = l_ ** 3, m = m_ ** 3, s = s_ ** 3;
  const r = 4.0767416621 * l - 3.3077115913 * m + 0.2309699292 * s;
  const g = -1.2684380046 * l + 2.6097574011 * m - 0.3413193965 * s;
  const bl = -0.0041960863 * l - 0.7034186147 * m + 1.707614701 * s;
  const enc = (c) => {
    c = Math.min(1, Math.max(0, c));
    return c <= 0.0031308 ? 12.92 * c : 1.055 * c ** (1 / 2.4) - 0.055;
  };
  const to2 = (c) => Math.round(enc(c) * 255).toString(16).padStart(2, '0');
  return '#' + to2(r) + to2(g) + to2(bl);
}

// web/src/assets/index.css —— :root（浅色）令牌
const THEME = {
  primary:   oklchToHex(0.218, 0.008, 223.9), // #161B1D 深墨蓝（品牌主色）
  secondary: oklchToHex(0.963, 0.002, 197.1), // #F1F3F3 浅雾灰
};

const PALETTE = {
  white: '#FFFFFF',
  black: '#000000',
  primary: THEME.primary, // 主题色（当前 mist 预设下接近墨黑）
};

/** tile 底色：白标放主题色底、黑标放白底、主题标放浅雾灰底 */
const TILE_BG = {
  white: PALETTE.primary,
  black: '#FFFFFF',
  primary: THEME.secondary,
};
/** 白底系 tile 加极细描边，避免白页上隐形 */
const tileEdge = (bg) =>
  bg === '#FFFFFF' || bg === THEME.secondary
    ? `<rect width="512" height="512" rx="115" fill="none" stroke="#000000" stroke-opacity="0.08" stroke-width="2"/>`
    : '';

/* ------------------------------------------------------- wordmark: astral */
/*
 * 手绘 monoline 小写 "astral"（无字体依赖，纯路径）。
 * 坐标系：y 向下，基线 y=100，x-height 带 y∈[0,100]，升部（t/l）顶 y=-40，
 * 笔画宽 20、圆头圆角 —— 与各概念 icon 的线性语言一致。
 * 每个字形以"左外缘=0"归一化，w 为外缘宽度。
 */
const WM = { stroke: 20, width: 0, paths: [] };

const GLYPHS = {
  a: {
    w: 100,
    d: ['M 50 10 A 40 40 0 0 1 50 90 A 40 40 0 0 1 50 10', 'M 90 10 L 90 90'],
  },
  s: {
    w: 70,
    d: ['M 61 26 C 53 10 35 6 23 13 C 10 21 12 37 26 45 C 42 54 58 58 56 74 C 54 89 32 95 16 85'],
  },
  t: {
    w: 60,
    d: ['M 30 -30 L 30 90', 'M 10 10 L 50 10'],
  },
  r: {
    w: 64,
    d: ['M 10 10 L 10 90', 'M 10 62 C 10 28 40 14 54 32'],
  },
  l: {
    w: 20,
    d: ['M 10 -30 L 10 90'],
  },
};
const WORD = 'astral';
const WM_GAP = 26; // 字距（外缘间距）

/** 生成 wordmark：<path> 组（stroke 用法），总宽、外框可直接取用 */
function wordmarkPaths(x0, y0, scale, color) {
  let x = x0;
  const els = [];
  for (const ch of WORD) {
    const g = GLYPHS[ch];
    for (const d of g.d) {
      els.push(
        `<path d="${d}" fill="none" stroke="${color}" stroke-width="${WM.stroke * scale}" stroke-linecap="round" stroke-linejoin="round" transform="translate(${x.toFixed(2)} ${y0}) scale(${scale})"/>`
      );
    }
    x += (g.w + WM_GAP) * scale;
  }
  WM.width = (x - WM_GAP * scale) - x0;
  return els.join('\n    ');
}
const wordmarkWidth = (scale) =>
  (WORD.length === 0 ? 0 : [...WORD].reduce((s, c) => s + GLYPHS[c].w, 0) + WM_GAP * (WORD.length - 1)) * scale;

/* ------------------------------------------------------------ svg 组装 */

const svg = (w, h, body) =>
  `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 ${w} ${h}" width="${w}" height="${h}">\n${body}\n</svg>`;

/* ------------------------------------------------------------- 概念 01 · Orbit */
/*
 * 行星 + 断口轨道环 + 卫星点：Astral 控制面居中，agents / 任务沿环运行。
 * 环与行星交叠处按"远端断开、近端实线"处理（透视深度），断口数值求解。
 */
function orbitIcon(color) {
  const cx = 256, cy = 256;
  const planetR = 96, planetW = 34;
  const rx = 208, ry = 142, rot = -30, ringW = 30;
  const rad = (rot * Math.PI) / 180;
  const pt = (t) => {
    const x = rx * Math.cos(t), y = ry * Math.sin(t);
    return [cx + x * Math.cos(rad) - y * Math.sin(rad), cy + x * Math.sin(rad) + y * Math.cos(rad)];
  };
  // 完整轨道环（与行星留净距，不穿插），卫星点贴环分布在右下 t=55°
  const ring = `M ${cx + rx} ${cy} A ${rx} ${ry} ${rot} 1 1 ${cx - rx} ${cy} A ${rx} ${ry} ${rot} 1 1 ${cx + rx} ${cy}`;
  const [sx, sy] = pt((55 / 180) * Math.PI);
  return {
    body: `
    <g fill="none" stroke="${color}" stroke-linecap="round">
      <circle cx="${cx}" cy="${cy}" r="${planetR}" stroke-width="${planetW}"/>
      <path d="${ring}" stroke-width="${ringW}"/>
    </g>
    <circle cx="${sx.toFixed(2)}" cy="${sy.toFixed(2)}" r="32" fill="${color}"/>`,
  };
}

/* ------------------------------------------------------------- 概念 02 · Nova */
/* 四角星芒（新星 / 灵感迸发），实底 + 内凹曲线，主星偏左下、伴星右上成对角动势 */
function starPath(cx, cy, R, k) {
  return (
    `M ${cx} ${cy - R} ` +
    `Q ${cx + k} ${cy - k} ${cx + R} ${cy} ` +
    `Q ${cx + k} ${cy + k} ${cx} ${cy + R} ` +
    `Q ${cx - k} ${cy + k} ${cx - R} ${cy} ` +
    `Q ${cx - k} ${cy - k} ${cx} ${cy - R} Z`
  );
}
function novaIcon(color) {
  return {
    body: `
    <path d="${starPath(232, 280, 168, 27)}" fill="${color}"/>
    <path d="${starPath(396, 140, 62, 10)}" fill="${color}"/>`,
  };
}

/* ---------------------------------------------------------- 概念 03 · Wave-A */
/* 调制波作字母 A 的横杠：monoline 双腿 + 高斯包络正弦，Astral 与 Modulator 一形双关 */
function waveAIcon(color) {
  const legs = 'M 84 436 L 256 110 L 428 436';
  const cx = 256, yBar = 348, half = 80, sigma = 36, amp = 32, cycles = 2;
  let d = '';
  for (let x = cx - half; x <= cx + half; x += 2) {
    const dx = x - cx;
    const env = amp * Math.exp(-(dx * dx) / (2 * sigma * sigma));
    const y = yBar - env * Math.sin((2 * Math.PI * cycles * dx) / (2 * half));
    d += `${d ? 'L' : 'M'} ${x.toFixed(1)} ${y.toFixed(1)} `;
  }
  return {
    body: `
    <path d="${legs}" fill="none" stroke="${color}" stroke-width="48" stroke-linecap="round" stroke-linejoin="round"/>
    <path d="${d}" fill="none" stroke="${color}" stroke-width="26" stroke-linecap="round" stroke-linejoin="round"/>`,
  };
}

/* ------------------------------------------------------------- 概念 04 · Peak */
/* 实底圆角山形 = 字母 A，负形（镂空）四角星居中：攀峰之上有北极星 */
function roundedPoly(pts, r) {
  const n = pts.length;
  let d = '';
  for (let i = 0; i < n; i++) {
    const p = pts[i], prev = pts[(i - 1 + n) % n], next = pts[(i + 1) % n];
    const v1 = [prev[0] - p[0], prev[1] - p[1]];
    const v2 = [next[0] - p[0], next[1] - p[1]];
    const l1 = Math.hypot(...v1), l2 = Math.hypot(...v2);
    const u1 = [v1[0] / l1, v1[1] / l1], u2 = [v2[0] / l2, v2[1] / l2];
    const ang = Math.acos(Math.max(-1, Math.min(1, u1[0] * u2[0] + u1[1] * u2[1])));
    const t = Math.min(r / Math.tan(ang / 2), l1 / 2 - 1, l2 / 2 - 1);
    const a = [p[0] + u1[0] * t, p[1] + u1[1] * t];
    const b = [p[0] + u2[0] * t, p[1] + u2[1] * t];
    const cross = u1[0] * u2[1] - u1[1] * u2[0];
    d += `${i ? 'L' : 'M'} ${a[0].toFixed(2)} ${a[1].toFixed(2)} A ${r} ${r} 0 0 ${cross > 0 ? 0 : 1} ${b[0].toFixed(2)} ${b[1].toFixed(2)} `;
  }
  return d + 'Z';
}
function peakIcon(color) {
  const tri = roundedPoly([[256, 84], [420, 428], [92, 428]], 34);
  const star = starPath(256, 312, 80, 12);
  return {
    body: `
    <path d="${tri} ${star}" fill="${color}" fill-rule="evenodd"/>`,
  };
}

/* ------------------------------------------------------------- 概念 05 · Pulse */
/* 高斯包络正弦波 = AM 调制载波，中心实心节点 = 调制器本体，直指 "Modulator" 本义 */
function pulseIcon(color) {
  const cx = 256, yMid = 256, half = 196, sigma = 112, amp = 144, cycles = 3;
  let d = '';
  for (let x = cx - half; x <= cx + half; x += 2) {
    const dx = x - cx;
    const env = amp * Math.exp(-(dx * dx) / (2 * sigma * sigma));
    const y = yMid - env * Math.sin((2 * Math.PI * cycles * dx) / (2 * half));
    d += `${d ? 'L' : 'M'} ${x.toFixed(1)} ${y.toFixed(1)} `;
  }
  return {
    body: `
    <path d="${d}" fill="none" stroke="${color}" stroke-width="34" stroke-linecap="round" stroke-linejoin="round"/>
    <circle cx="${cx}" cy="${yMid}" r="30" fill="${color}"/>`,
  };
}

/* ---------------------------------------------------------------- lockups */

/** 横排 lockup（透明底用）：icon 左、wordmark 右，x-height 带对 icon 居中 */
function lockupHorizontal(iconFn, color) {
  const H = 280, iconS = 232, pad = 24, gap = 64;
  const scale = 0.74; // x-height 100 -> 74
  const wmW = wordmarkWidth(scale);
  const wmX = pad + iconS + gap;
  const wmY = H / 2 - (50 * scale); // native x-height 中点 y=50 对齐画布中线
  const W = Math.ceil(wmX + wmW + 28);
  const { body } = iconFn(color);
  const icon = `<g transform="translate(${pad} ${(H - iconS) / 2}) scale(${iconS / 512})">${body}</g>`;
  return svg(W, H, `${icon}\n    ${wordmarkPaths(wmX, wmY, scale, color)}`);
}

/** 堆叠 lockup（tile 用）：icon 上、wordmark 下，整体在 512 方形内居中 */
function lockupStacked(iconFn, color) {
  const C = 512, iconS = 216, gap = 52;
  const scale = 0.62; // 宽 496 -> ~307
  const wmW = wordmarkWidth(scale);
  const blockH = iconS + gap + 140 * scale; // 140 = wordmark 总高（升部顶到基线）
  const top = (C - blockH) / 2;
  const { body } = iconFn(color);
  const icon = `<g transform="translate(${(C - iconS) / 2} ${top.toFixed(2)}) scale(${iconS / 512})">${body}</g>`;
  const wmX = (C - wmW) / 2;
  const wmY = top + iconS + gap + 40 * scale; // native 升部顶 -40 对齐块顶
  return svg(C, C, `${icon}\n    ${wordmarkPaths(wmX, wmY, scale, color)}`);
}

/* ------------------------------------------------------------------ tiles */

function tileWrap(inner, bg) {
  return svg(
    512,
    512,
    `<rect width="512" height="512" rx="115" fill="${bg}"/>
    ${tileEdge(bg)}
    <g transform="translate(58 58) scale(0.774)">${inner}</g>`
  );
}
// tile 内 icon 内容物缩放：icon 512 -> 396，居中 (58..454)

/* ------------------------------------------------------------------ 输出 */

const CONCEPTS = [
  { id: 'orbit', num: '01', name: 'Orbit 轨道', icon: orbitIcon },
  { id: 'nova', num: '02', name: 'Nova 星芒', icon: novaIcon },
  { id: 'wavea', num: '03', name: 'Wave-A 调制波 A', icon: waveAIcon },
  { id: 'peak', num: '04', name: 'Peak 山岳 A', icon: peakIcon },
  { id: 'pulse', num: '05', name: 'Pulse 调制波', icon: pulseIcon },
];

const OUT = [];
const written = [];
// 重新生成前清掉旧的概念目录（编号/命名可能变动）
for (const e of readdirSync(ROOT)) {
  if (/^\d{2}-/.test(e) && statSync(join(ROOT, e)).isDirectory()) {
    rmSync(join(ROOT, e), { recursive: true, force: true });
  }
}
for (const c of CONCEPTS) {
  const dirT = join(ROOT, `${c.num}-${c.id}`, 'transparent');
  const dirB = join(ROOT, `${c.num}-${c.id}`, 'tile');
  mkdirSync(dirT, { recursive: true });
  mkdirSync(dirB, { recursive: true });
  for (const colorName of ['white', 'black', 'primary']) {
    const color = PALETTE[colorName];
    // 透明底
    const iT = svg(512, 512, c.icon(color).body);
    const lT = lockupHorizontal(c.icon, color);
    write(join(dirT, `${c.id}-${colorName}-icon.svg`), iT);
    write(join(dirT, `${c.id}-${colorName}-lockup.svg`), lT);
    // 带底色 tile
    const bg = TILE_BG[colorName];
    const iB = tileWrap(c.icon(color).body, bg);
    const lB = svg(512, 512, `<rect width="512" height="512" rx="115" fill="${bg}"/>${tileEdge(bg)}\n    ${lockupStacked(c.icon, color).replace(/^<svg[^>]*>/, '').replace(/<\/svg>$/, '')}`);
    write(join(dirB, `${c.id}-${colorName}-icon.svg`), iB);
    write(join(dirB, `${c.id}-${colorName}-lockup.svg`), lB);
  }
  OUT.push(c);
}

function write(p, content) {
  writeFileSync(p, content.trimStart() + '\n');
  written.push(p);
}

/* ------------------------------------------------------------------ 预览 */

function preview() {
  const esc = (s) => s.replace(/&/g, '&amp;');
  const sections = OUT.map((c) => {
    const cell = (src, label, bg) => `
      <figure class="cell" style="background:${bg}">
        <img src="${src}" alt="${esc(label)}"/>
        <figcaption>${esc(label)}</figcaption>
      </figure>`;
    const transparent = ['white', 'black', 'primary']
      .flatMap((cn) => [
        cell(`${c.num}-${c.id}/transparent/${c.id}-${cn}-icon.svg`, `icon · ${cn}`, '#fff'),
        cell(`${c.num}-${c.id}/transparent/${c.id}-${cn}-lockup.svg`, `lockup · ${cn}`, '#fff'),
      ])
      .join('');
    const onDark = ['white', 'black', 'primary']
      .flatMap((cn) => [
        cell(`${c.num}-${c.id}/transparent/${c.id}-${cn}-icon.svg`, `icon · ${cn}`, 'var(--dark)'),
        cell(`${c.num}-${c.id}/transparent/${c.id}-${cn}-lockup.svg`, `lockup · ${cn}`, 'var(--dark)'),
      ])
      .join('');
    const tiles = ['white', 'black', 'primary']
      .flatMap((cn) => [
        cell(`${c.num}-${c.id}/tile/${c.id}-${cn}-icon.svg`, `tile icon · ${cn}`, '#fff'),
        cell(`${c.num}-${c.id}/tile/${c.id}-${cn}-lockup.svg`, `tile lockup · ${cn}`, '#fff'),
      ])
      .join('');
    const sizes = ['white', 'primary']
      .flatMap((cn) =>
        [96, 48, 24].map(
          (s) =>
            `<figure class="cell sz" style="background:#fff"><img src="${c.num}-${c.id}/transparent/${c.id}-${cn}-icon.svg" style="width:${s}px;height:${s}px" alt=""/><figcaption>${s}px · ${cn}</figcaption></figure>` +
            `<figure class="cell sz" style="background:var(--dark)"><img src="${c.num}-${c.id}/transparent/${c.id}-${cn}-icon.svg" style="width:${s}px;height:${s}px" alt=""/><figcaption>${s}px · ${cn}</figcaption></figure>`
        )
      )
      .join('');
    return `
  <section>
    <h2>${c.num} · ${esc(c.name)}</h2>
    <h3>透明底 · 浅色页面</h3>
    <div class="grid">${transparent}</div>
    <h3>透明底 · 深色页面</h3>
    <div class="grid">${onDark}</div>
    <h3>带底色（app tile）</h3>
    <div class="grid">${tiles}</div>
    <h3>小尺寸检查</h3>
    <div class="grid">${sizes}</div>
  </section>`;
  }).join('\n');

  const html = `<!doctype html>
<html lang="zh-CN">
<head>
<meta charset="utf-8"/>
<title>Astral Brand Logos — Preview</title>
<style>
  :root { --dark: #161B1D; }
  body { font-family: Inter, system-ui, sans-serif; margin: 32px auto; max-width: 1180px; padding: 0 20px; color: #161B1D; background: #fafafa; }
  h1 { font-size: 26px; } h2 { margin-top: 56px; border-bottom: 2px solid #e5e7eb; padding-bottom: 8px; }
  h3 { margin: 20px 0 8px; color: #67787c; font-size: 14px; font-weight: 600; }
  .grid { display: flex; flex-wrap: wrap; gap: 12px; }
  .cell { margin: 0; border: 1px solid #e5e7eb; border-radius: 10px; padding: 10px; width: 170px; text-align: center; }
  .cell img { width: 150px; height: auto; display: block; margin: 0 auto; }
  .cell.sz { width: auto; } .cell.sz img { width: auto; }
  figcaption { font-size: 11px; color: #67787c; margin-top: 6px; }
  .swatches { display: flex; gap: 16px; margin: 16px 0; }
  .sw { width: 120px; border-radius: 10px; padding: 12px; font-size: 12px; color: #fff; }
  .sw.light { color: #161B1D; border: 1px solid #e5e7eb; }
</style>
</head>
<body>
<h1>Astral 品牌 Logo 组 · 预览</h1>
<p>生成于 icons/generate.mjs —— 每概念 3 色 × 2 形 × 2 底 = 12 个 SVG。</p>
<div class="swatches">
  <div class="sw light" style="background:#FFFFFF">white<br>#FFFFFF</div>
  <div class="sw" style="background:#000000">black<br>#000000</div>
  <div class="sw" style="background:${THEME.primary}">primary<br>${THEME.primary}</div>
  <div class="sw light" style="background:${THEME.secondary}">tile bg<br>${THEME.secondary}</div>
</div>
${sections}
</body>
</html>`;
  write(join(ROOT, 'preview.html'), html);
}
preview();

console.log(`OK — ${written.length} 个 SVG + preview.html`);
