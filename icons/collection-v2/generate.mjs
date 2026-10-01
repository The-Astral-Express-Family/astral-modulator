#!/usr/bin/env node
/**
 * Astral brand logo collection v2
 *
 * This directory is intentionally isolated from the older icons/01-* collection.
 * Run from this directory with:
 *   node generate.mjs
 *
 * Output: 3 concepts x 3 colors x 2 forms x 2 surfaces = 36 SVG files.
 * The CSS theme colors are read from web/src/assets/index.css and resolved to
 * six-digit sRGB hex values so the SVGs remain portable in design tools.
 */

import {
  mkdirSync,
  readFileSync,
  readdirSync,
  rmSync,
  statSync,
  writeFileSync,
} from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';

const ROOT = dirname(fileURLToPath(import.meta.url));
const CSS_PATH = join(ROOT, '..', '..', 'web', 'src', 'assets', 'index.css');

/* ------------------------------------------------------------------ colors */

// CSS Color 4 OKLCH -> OKLab -> linear sRGB -> encoded sRGB.
function oklchToHex(L, C, H) {
  const h = (H * Math.PI) / 180;
  const a = C * Math.cos(h);
  const b = C * Math.sin(h);
  const l_ = L + 0.3963377774 * a + 0.2158037573 * b;
  const m_ = L - 0.1055613458 * a - 0.0638541728 * b;
  const s_ = L - 0.0894841775 * a - 1.291485548 * b;
  const l = l_ ** 3;
  const m = m_ ** 3;
  const s = s_ ** 3;
  const r = 4.0767416621 * l - 3.3077115913 * m + 0.2309699292 * s;
  const g = -1.2684380046 * l + 2.6097574011 * m - 0.3413193965 * s;
  const blue = -0.0041960863 * l - 0.7034186147 * m + 1.707614701 * s;
  const encode = (value) => {
    const clipped = Math.min(1, Math.max(0, value));
    return clipped <= 0.0031308
      ? 12.92 * clipped
      : 1.055 * clipped ** (1 / 2.4) - 0.055;
  };
  const byte = (value) => Math.round(encode(value) * 255).toString(16).padStart(2, '0');
  return `#${byte(r)}${byte(g)}${byte(blue)}`;
}

function parseUnit(value, unit, label) {
  const normalized = value.trim().toLowerCase();
  const numericText = unit
    ? (normalized.endsWith(unit) ? normalized.slice(0, -unit.length) : normalized)
    : normalized;
  const numeric = Number(numericText);
  if (!Number.isFinite(numeric)) throw new Error(`Invalid ${label} in CSS color: ${value}`);
  return numeric;
}

function resolveCssColor(value) {
  const raw = value.trim();
  const hex = raw.match(/^#([\da-f]{3}|[\da-f]{6})$/i);
  if (hex) {
    const expanded = hex[1].length === 3
      ? [...hex[1]].map((char) => char + char).join('')
      : hex[1];
    return `#${expanded.toLowerCase()}`;
  }

  // Keep the parser deliberately strict: silently copying an unsupported CSS
  // function into a standalone SVG is worse than failing during generation.
  const match = raw.match(/^oklch\(\s*([^\s]+)\s+([^\s]+)\s+([^\s]+)\s*\)$/i);
  if (!match) {
    throw new Error(`Unsupported CSS color token: ${raw}`);
  }
  const L = match[1].endsWith('%')
    ? parseUnit(match[1], '%', 'OKLCH lightness') / 100
    : parseUnit(match[1], '', 'OKLCH lightness');
  const C = match[2].endsWith('%')
    ? parseUnit(match[2], '%', 'OKLCH chroma') / 100
    : parseUnit(match[2], '', 'OKLCH chroma');
  let H;
  if (match[3].toLowerCase().endsWith('deg')) H = parseUnit(match[3], 'deg', 'OKLCH hue');
  else if (match[3].toLowerCase().endsWith('turn')) H = parseUnit(match[3], 'turn', 'OKLCH hue') * 360;
  else if (match[3].toLowerCase().endsWith('rad')) H = parseUnit(match[3], 'rad', 'OKLCH hue') * 180 / Math.PI;
  else if (match[3].toLowerCase().endsWith('grad')) H = parseUnit(match[3], 'grad', 'OKLCH hue') * 0.9;
  else H = parseUnit(match[3], '', 'OKLCH hue');
  if (L < 0 || L > 1 || C < 0 || !Number.isFinite(H)) {
    throw new Error(`Out-of-range OKLCH token: ${raw}`);
  }
  return oklchToHex(L, C, H);
}

function rootToken(css, name) {
  const root = css.match(/:root\s*\{([\s\S]*?)\}/)?.[1];
  if (!root) throw new Error(`Could not find :root in ${CSS_PATH}`);
  const value = root
    .split(';')
    .map((declaration) => declaration.trim())
    .find((declaration) => declaration.startsWith(`--${name}:`))
    ?.slice(name.length + 3)
    .trim();
  if (!value) throw new Error(`Could not find --${name} in ${CSS_PATH}`);
  return value;
}

const css = readFileSync(CSS_PATH, 'utf8');
const THEME = {
  primary: resolveCssColor(rootToken(css, 'primary')),
  secondary: resolveCssColor(rootToken(css, 'secondary')),
};
const PALETTE = {
  white: '#ffffff',
  black: '#000000',
  primary: THEME.primary,
};
const TILE_BG = {
  white: PALETTE.primary,
  black: '#ffffff',
  primary: THEME.secondary,
};

/* --------------------------------------------------------------- primitives */

const esc = (value) => String(value)
  .replaceAll('&', '&amp;')
  .replaceAll('<', '&lt;')
  .replaceAll('>', '&gt;')
  .replaceAll('"', '&quot;')
  .replaceAll("'", '&apos;');

function svg(width, height, body, title, description, id) {
  const titleId = `${id}-title`;
  const descId = `${id}-description`;
  return `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 ${width} ${height}" width="${width}" height="${height}" role="img" aria-labelledby="${titleId} ${descId}">
  <title id="${titleId}">${esc(title)}</title>
  <desc id="${descId}">${esc(description)}</desc>
${body}
</svg>`;
}

function starPath(cx, cy, radius, inner) {
  return `M ${cx} ${cy - radius} L ${cx + inner} ${cy - inner} L ${cx + radius} ${cy} L ${cx + inner} ${cy + inner} L ${cx} ${cy + radius} L ${cx - inner} ${cy + inner} L ${cx - radius} ${cy} L ${cx - inner} ${cy - inner} Z`;
}

/* ----------------------------------------------------------- concept 01 */

/**
 * Constellation / 星群
 *
 * A four-ray astronomical spark sits inside a deliberately broken, node-based
 * constellation frame. The open corners preserve air at small sizes and make
 * this direction feel precise rather than like a generic enclosed badge.
 */
function constellationIcon(color) {
  return `
  <g fill="none" stroke="${color}" stroke-width="16" stroke-linecap="round" stroke-linejoin="round">
    <path d="M 114 206 L 146 142 L 214 108"/>
    <path d="M 298 108 L 366 142 L 398 206"/>
    <path d="M 398 306 L 366 370 L 298 404"/>
    <path d="M 214 404 L 146 370 L 114 306"/>
  </g>
  <g fill="${color}">
    <circle cx="114" cy="206" r="18"/>
    <circle cx="398" cy="206" r="18"/>
    <circle cx="398" cy="306" r="18"/>
    <circle cx="114" cy="306" r="18"/>
    <circle cx="214" cy="108" r="12"/>
    <circle cx="298" cy="108" r="12"/>
    <circle cx="214" cy="404" r="12"/>
    <circle cx="298" cy="404" r="12"/>
    <path d="${starPath(256, 256, 112, 25)}"/>
  </g>`;
}

/* ----------------------------------------------------------- concept 02 */

/**
 * Prism / 折面
 *
 * A solid, angular A assembled from four planes. Small intentional seams at
 * the crown and between the legs keep the mark from collapsing into a plain
 * triangle, while the broad cross-plane gives it a strong app-icon silhouette.
 */
function prismIcon(color) {
  return `
  <g fill="${color}">
    <path d="M 238 62 L 250 62 L 181 418 L 86 418 Z"/>
    <path d="M 262 62 L 274 62 L 426 418 L 331 418 Z"/>
    <path d="M 238 62 L 274 62 L 306 136 L 207 136 Z"/>
    <path d="M 150 260 L 362 260 L 389 322 L 123 322 Z"/>
  </g>`;
}

/* ----------------------------------------------------------- concept 03 */

/**
 * Modwave / 调制波
 *
 * A smooth, low-frequency wave is the visual center of a split A. All curves
 * are native cubic Béziers (not point-sampled polylines), so the mark stays
 * clean at large sizes and communicates the product's modulation theme.
 */
function modwaveIcon(color) {
  return `
  <g fill="none" stroke="${color}" stroke-width="38" stroke-linecap="round" stroke-linejoin="round">
    <path d="M 170 192 C 184 116 211 72 256 72 C 301 72 328 116 342 192"/>
    <path d="M 190 330 L 150 440"/>
    <path d="M 322 330 L 362 440"/>
  </g>
  <path d="M 68 260 C 120 216 170 216 210 250 C 236 273 276 273 302 250 C 342 216 392 216 444 260" fill="none" stroke="${color}" stroke-width="44" stroke-linecap="round" stroke-linejoin="round"/>
  <circle cx="256" cy="268" r="21" fill="${color}"/>`;
}

const CONCEPTS = [
  {
    id: 'constellation',
    number: '01',
    name: 'Constellation · 星群',
    shortName: 'Constellation',
    description: '节点星群与四向星芒，精确、开放、具有天体导航感。',
    icon: constellationIcon,
  },
  {
    id: 'prism',
    number: '02',
    name: 'Prism · 折面',
    shortName: 'Prism',
    description: '由四个平面组成的几何 A，厚重、锐利，适合产品图标。',
    icon: prismIcon,
  },
  {
    id: 'modwave',
    number: '03',
    name: 'Modwave · 调制波',
    shortName: 'Modwave',
    description: '平滑 Bézier 调制波穿过解构 A，最直接回应 Modulator 与草图。',
    icon: modwaveIcon,
  },
];

/* ---------------------------------------------------------- wordmark */

// Font-independent monoline lowercase "astral". Coordinates are normalized
// to a 0..100-ish cap/x-height box; every output therefore renders identically
// in browsers, design tools, and rasterizers without installing a font.
const GLYPHS = {
  a: {
    width: 74,
    paths: [
      'M 63 58 C 63 36 49 21 32 21 C 15 21 5 37 5 59 C 5 81 16 97 32 97 C 46 97 57 88 63 77',
      'M 63 21 L 63 97',
    ],
  },
  s: {
    width: 64,
    paths: [
      'M 57 32 C 50 23 40 20 29 20 C 15 20 6 28 6 40 C 6 52 15 58 30 62 C 46 66 57 72 57 84 C 57 96 46 102 31 102 C 18 102 9 98 3 90',
    ],
  },
  t: {
    width: 58,
    paths: [
      'M 28 4 L 28 91 C 28 99 33 102 40 102 C 45 102 50 100 53 97',
      'M 10 27 L 50 27',
    ],
  },
  r: {
    width: 63,
    paths: [
      'M 8 21 L 8 98',
      'M 8 55 C 9 34 21 21 39 21 C 48 21 55 26 59 33',
    ],
  },
  l: {
    width: 20,
    paths: ['M 10 4 L 10 98'],
  },
};
const WORD = 'astral';
const WORD_GAP = 14;
const WORD_STROKE = 14;

function wordmarkWidth(scale) {
  const native = [...WORD].reduce((sum, character) => sum + GLYPHS[character].width, 0);
  return (native + WORD_GAP * (WORD.length - 1)) * scale;
}

function wordmarkPaths(x, y, scale, color) {
  let cursor = x;
  const output = [];
  for (const character of WORD) {
    const glyph = GLYPHS[character];
    for (const path of glyph.paths) {
      output.push(`<path d="${path}" fill="none" stroke="${color}" stroke-width="${(WORD_STROKE * scale).toFixed(2)}" stroke-linecap="round" stroke-linejoin="round" transform="translate(${cursor.toFixed(2)} ${y.toFixed(2)}) scale(${scale})"/>`);
    }
    cursor += (glyph.width + WORD_GAP) * scale;
  }
  return output.join('\n    ');
}

function horizontalLockup(concept, color, colorName) {
  const height = 240;
  const iconSize = 196;
  const iconX = 28;
  const iconY = (height - iconSize) / 2;
  const wordScale = 0.78;
  const wordX = iconX + iconSize + 48;
  const wordY = 68;
  const width = Math.ceil(wordX + wordmarkWidth(wordScale) + 28);
  const body = `
  <g transform="translate(${iconX} ${iconY}) scale(${(iconSize / 512).toFixed(6)})">
${concept.icon(color)}
  </g>
  <g aria-label="astral wordmark">
    ${wordmarkPaths(wordX, wordY, wordScale, color)}
  </g>`;
  return svg(
    width,
    height,
    body,
    `Astral ${concept.shortName} logo · ${colorName} · icon with wordmark`,
    `${concept.description} Horizontal icon and custom path wordmark.`,
    `${concept.id}-${colorName}-horizontal`,
  );
}

function stackedLockupBody(concept, color) {
  const canvas = 512;
  const iconSize = 236;
  const iconX = (canvas - iconSize) / 2;
  const iconY = 38;
  const wordScale = 0.62;
  const wordWidth = wordmarkWidth(wordScale);
  const wordX = (canvas - wordWidth) / 2;
  const wordY = 322;
  return `
  <g transform="translate(${iconX.toFixed(2)} ${iconY}) scale(${(iconSize / 512).toFixed(6)})">
${concept.icon(color)}
  </g>
  <g aria-label="astral wordmark">
    ${wordmarkPaths(wordX, wordY, wordScale, color)}
  </g>`;
}

function tileLockup(concept, color, colorName) {
  const background = TILE_BG[colorName];
  const body = `
  <rect width="512" height="512" rx="112" fill="${background}"/>${tileEdge(background)}
${stackedLockupBody(concept, color)}`;
  return svg(
    512,
    512,
    body,
    `Astral ${concept.shortName} logo tile · ${colorName} · icon with wordmark`,
    `${concept.description} Stacked icon and custom path wordmark on a rounded tile.`,
    `${concept.id}-${colorName}-tile-lockup`,
  );
}

/* --------------------------------------------------------------- surfaces */

function tileEdge(background) {
  if (background !== '#ffffff' && background !== THEME.secondary) return '';
  return '\n  <rect x="1" y="1" width="510" height="510" rx="111" fill="none" stroke="#000000" stroke-opacity="0.08" stroke-width="2"/>';
}

function tileIcon(concept, color, colorName) {
  const background = TILE_BG[colorName];
  const body = `
  <rect width="512" height="512" rx="112" fill="${background}"/>${tileEdge(background)}
  <g transform="translate(20 20) scale(0.921875)">
${concept.icon(color)}
  </g>`;
  return svg(
    512,
    512,
    body,
    `Astral ${concept.shortName} logo tile · ${colorName} · icon`,
    `${concept.description} Pure icon on a rounded ${colorName} color tile.`,
    `${concept.id}-${colorName}-tile-icon`,
  );
}

function transparentIcon(concept, color, colorName) {
  return svg(
    512,
    512,
    concept.icon(color),
    `Astral ${concept.shortName} logo · ${colorName} · icon`,
    `${concept.description} Pure transparent icon artwork.`,
    `${concept.id}-${colorName}-icon`,
  );
}

/* --------------------------------------------------------------- generation */

function writeFile(path, content) {
  mkdirSync(dirname(path), { recursive: true });
  writeFileSync(path, `${content.trim()}\n`, 'utf8');
}

// Only this collection's numbered concept folders are removed. The older
// icons/01-* folders are outside ROOT and are never touched by this script.
for (const entry of readdirSync(ROOT)) {
  if (/^\d{2}-/.test(entry) && statSync(join(ROOT, entry)).isDirectory()) {
    rmSync(join(ROOT, entry), { recursive: true, force: true });
  }
}

const files = [];
for (const concept of CONCEPTS) {
  const transparentDir = join(ROOT, `${concept.number}-${concept.id}`, 'transparent');
  const tileDir = join(ROOT, `${concept.number}-${concept.id}`, 'tile');
  mkdirSync(transparentDir, { recursive: true });
  mkdirSync(tileDir, { recursive: true });

  for (const colorName of ['white', 'black', 'primary']) {
    const color = PALETTE[colorName];
    const variants = [
      [transparentDir, `${concept.id}-${colorName}-icon.svg`, transparentIcon(concept, color, colorName)],
      [transparentDir, `${concept.id}-${colorName}-lockup.svg`, horizontalLockup(concept, color, colorName)],
      [tileDir, `${concept.id}-${colorName}-icon.svg`, tileIcon(concept, color, colorName)],
      [tileDir, `${concept.id}-${colorName}-lockup.svg`, tileLockup(concept, color, colorName)],
    ];
    for (const [directory, filename, content] of variants) {
      const path = join(directory, filename);
      writeFile(path, content);
      files.push(path.slice(ROOT.length + 1).replaceAll('\\', '/'));
    }
  }
}

const manifest = {
  collection: 'Astral Brand Logo Collection v2',
  generator: 'generate.mjs',
  sourceTheme: '../../web/src/assets/index.css',
  theme: {
    primary: THEME.primary,
    secondary: THEME.secondary,
    palette: PALETTE,
    tileBackground: TILE_BG,
  },
  concepts: CONCEPTS.map(({ id, number, name, description }) => ({ id, number, name, description })),
  variantsPerConcept: 12,
  svgCount: files.length,
  naming: '<concept>-<white|black|primary>-<icon|lockup>.svg',
  surfaces: {
    transparent: 'No canvas background; place on a controlled surface.',
    tile: '512x512 rounded background with the foreground color paired for contrast.',
  },
  files,
};
writeFile(join(ROOT, 'manifest.json'), JSON.stringify(manifest, null, 2));

function previewHtml() {
  const swatches = [
    ['white', PALETTE.white],
    ['black', PALETTE.black],
    ['primary', PALETTE.primary],
    ['secondary tile', THEME.secondary],
  ].map(([label, color]) => `<div class="swatch" style="background:${color};color:${label === 'white' || label === 'secondary tile' ? '#161b1d' : '#fff'}"><strong>${label}</strong><small>${color}</small></div>`).join('');

  const sections = CONCEPTS.map((concept) => {
    const transparent = ['white', 'black', 'primary'].flatMap((colorName) => [
      `<figure class="card checker"><img src="${concept.number}-${concept.id}/transparent/${concept.id}-${colorName}-icon.svg" alt="${concept.name} ${colorName} icon"/><figcaption>transparent · ${colorName} · icon</figcaption></figure>`,
      `<figure class="card checker"><img src="${concept.number}-${concept.id}/transparent/${concept.id}-${colorName}-lockup.svg" alt="${concept.name} ${colorName} lockup"/><figcaption>transparent · ${colorName} · lockup</figcaption></figure>`,
    ]).join('');
    const tiles = ['white', 'black', 'primary'].flatMap((colorName) => [
      `<figure class="card tile-card"><img src="${concept.number}-${concept.id}/tile/${concept.id}-${colorName}-icon.svg" alt="${concept.name} ${colorName} tile icon"/><figcaption>tile · ${colorName} · icon</figcaption></figure>`,
      `<figure class="card tile-card"><img src="${concept.number}-${concept.id}/tile/${concept.id}-${colorName}-lockup.svg" alt="${concept.name} ${colorName} tile lockup"/><figcaption>tile · ${colorName} · lockup</figcaption></figure>`,
    ]).join('');
    const small = ['white', 'black', 'primary'].flatMap((colorName) => [24, 48, 96].map((size) =>
      `<figure class="small-card ${colorName === 'white' ? 'dark-surface' : ''}"><img src="${concept.number}-${concept.id}/transparent/${concept.id}-${colorName}-icon.svg" style="width:${size}px;height:${size}px" alt=""/><figcaption>${colorName} · ${size}px</figcaption></figure>`
    )).join('');
    return `<section>
  <div class="heading"><span class="number">${concept.number}</span><div><h2>${esc(concept.name)}</h2><p>${esc(concept.description)}</p></div></div>
  <h3>透明底 · checkerboard（白标）</h3><div class="grid">${transparent}</div>
  <h3>带底色 · rounded tile</h3><div class="grid">${tiles}</div>
  <h3>小尺寸 · 24 / 48 / 96 px</h3><div class="small-grid">${small}</div>
</section>`;
  }).join('\n');

  return `<!doctype html>
<html lang="zh-CN">
<head>
<meta charset="utf-8"/>
<meta name="viewport" content="width=device-width, initial-scale=1"/>
<title>Astral Brand Logo Collection v2</title>
<style>
:root { color-scheme: light; --ink:#161b1d; --muted:#68777a; --line:#dfe5e6; }
* { box-sizing:border-box; }
body { margin:0; padding:32px 24px 80px; max-width:1260px; margin-inline:auto; color:var(--ink); background:#f8faf9; font:14px/1.5 Inter, ui-sans-serif, system-ui, sans-serif; }
h1 { margin:0 0 8px; font-size:30px; letter-spacing:-.03em; }
.intro { max-width:760px; color:var(--muted); margin:0 0 20px; }
.swatches { display:flex; flex-wrap:wrap; gap:10px; margin:22px 0 50px; }
.swatch { min-width:142px; padding:12px 14px; border:1px solid var(--line); border-radius:12px; box-shadow:0 2px 8px #13252b0a; }
.swatch strong,.swatch small { display:block; } .swatch small { margin-top:3px; opacity:.7; font-size:11px; }
section { margin-top:62px; }
.heading { display:flex; align-items:flex-start; gap:16px; border-bottom:1px solid var(--line); padding-bottom:14px; }
.number { display:grid; place-items:center; width:38px; height:38px; border-radius:12px; background:var(--ink); color:#fff; font-weight:700; }
h2 { margin:0; font-size:22px; } h3 { margin:24px 0 10px; font-size:12px; color:var(--muted); text-transform:uppercase; letter-spacing:.08em; }
.heading p { margin:4px 0 0; color:var(--muted); }
.grid { display:grid; grid-template-columns:repeat(6,minmax(0,1fr)); gap:12px; }
.card { margin:0; min-width:0; padding:12px; border:1px solid var(--line); border-radius:14px; background:#fff; text-align:center; }
.card img { display:block; width:100%; height:136px; object-fit:contain; }
.card:nth-child(2n) img { height:90px; margin-block:23px; }
.card figcaption,.small-card figcaption { margin-top:8px; color:var(--muted); font-size:11px; }
.checker { background-color:#fff; background-image:linear-gradient(45deg,#e8eded 25%,transparent 25%),linear-gradient(-45deg,#e8eded 25%,transparent 25%),linear-gradient(45deg,transparent 75%,#e8eded 75%),linear-gradient(-45deg,transparent 75%,#e8eded 75%); background-size:16px 16px; background-position:0 0,0 8px,8px -8px,-8px 0; }
.tile-card { background:#fff; } .tile-card img { height:180px; } .tile-card:nth-child(2n) img { height:180px; margin:0; }
.small-grid { display:flex; flex-wrap:wrap; gap:10px; }
.small-card { width:112px; min-height:136px; padding:12px; border:1px solid var(--line); border-radius:12px; background:#fff; text-align:center; display:flex; flex-direction:column; justify-content:center; align-items:center; }
.small-card.dark-surface { background:var(--ink); border-color:var(--ink); } .small-card.dark-surface figcaption { color:#b8c2c4; }
.small-card img { display:block; object-fit:contain; max-width:96px; max-height:96px; }
@media (max-width:900px) { .grid { grid-template-columns:repeat(3,minmax(0,1fr)); } }
@media (max-width:560px) { body { padding-inline:14px; } .grid { grid-template-columns:repeat(2,minmax(0,1fr)); } }
</style>
</head>
<body>
<h1>Astral · Brand Logo Collection v2</h1>
<p class="intro">三套独立设计方向。每套固定输出 3 色 × 纯 icon / 带文字 lockup × 透明 / 圆角底色，共 12 个 SVG。此页同时用于检查透明白标、对比度与小尺寸可读性。</p>
<div class="swatches">${swatches}</div>
${sections}
</body>
</html>`;
}
writeFile(join(ROOT, 'preview.html'), previewHtml());

console.log(`OK — ${files.length} SVG + manifest.json + preview.html`);
console.log(`theme primary: ${THEME.primary}; secondary: ${THEME.secondary}`);
