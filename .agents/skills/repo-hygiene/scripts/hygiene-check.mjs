#!/usr/bin/env node
/**
 * 通用仓库卫生机械检查（repo-hygiene skill §A）。
 *
 * 自包含、零仓库依赖：不在目标仓库安装任何东西；阈值、豁免与忽略规则从
 * 仓库根的 .hygiene.config.json 读取（缺失时用内置默认值也能跑）。临时
 * 标记基线存 .hygiene-baseline.json，由 --update-baseline 在一道门通过后
 * 刷新。结构类检查（函数长度、嵌套、冗余、过度防御、补丁叠加）由 SKILL
 * §B 的 subagent 评审承担——不要试图用脚本猜这些。
 *
 * 用法（在目标仓库根执行）：
 *   node <本 skill 目录>/scripts/hygiene-check.mjs [--update-baseline]
 * 退出码：0 = 通过；1 = 有超限违规。
 */
import { execSync } from "node:child_process";
import { readdirSync, readFileSync, writeFileSync } from "node:fs";
import path from "node:path";

const ROOT = process.cwd();
const CONFIG_PATH = path.join(ROOT, ".hygiene.config.json");
const BASELINE_PATH = path.join(ROOT, ".hygiene-baseline.json");

// 起点默认值：首次在某仓库运行后按文件分布校准并写入 .hygiene.config.json。
const DEFAULT_LIMITS = {
  source: { warn: 600, fail: 900 },
  test: { warn: 1000, fail: 1500 },
  doc: { warn: 600, fail: 900 },
};

const SOURCE_EXT = new Set([
  ".ts", ".tsx", ".js", ".jsx", ".mjs", ".cjs",
  ".py", ".go", ".rs", ".java", ".rb", ".php", ".cs", ".swift", ".kt", ".scala",
  ".c", ".h", ".cpp", ".hpp",
]);
const MARKER_RE = /\b(TODO|FIXME|HACK)\b/g;

const DEFAULT_IGNORE = [
  "node_modules/", ".git/", "dist/", "build/", "out/",
  "vendor/", "third_party/", "coverage/",
];

function loadJson(file) {
  try {
    return JSON.parse(readFileSync(file, "utf8"));
  } catch {
    return null;
  }
}

const config = loadJson(CONFIG_PATH) ?? {};
const limits = {};
for (const cat of Object.keys(DEFAULT_LIMITS)) {
  limits[cat] = { ...DEFAULT_LIMITS[cat], ...(config.limits?.[cat] ?? {}) };
}
const allowlist = config.allowlist ?? {};
const ignore = [...DEFAULT_IGNORE, ...(config.ignore ?? [])];

function listFiles() {
  try {
    // 跟踪文件 + 未忽略的新文件：卫生检查恰好要抓新文件
    const out = execSync(
      "git ls-files --cached --others --exclude-standard -z",
      { cwd: ROOT, maxBuffer: 1 << 28, stdio: ["ignore", "pipe", "ignore"] },
    ).toString();
    return out.split("\0").filter(Boolean);
  } catch {
    // 非 git 仓库：退化为整树遍历
    return walk(".");
  }
}

function walk(dir, out = []) {
  for (const entry of readdirSync(path.join(ROOT, dir), { withFileTypes: true })) {
    const rel = dir === "." ? entry.name : `${dir}/${entry.name}`;
    if (entry.isDirectory()) walk(rel, out);
    else out.push(rel);
  }
  return out;
}

function ignored(rel) {
  if (rel.includes(".min.")) return true;
  return ignore.some((prefix) => rel === prefix.slice(0, -1) || rel.startsWith(prefix));
}

function category(rel) {
  const t = rel.replaceAll("\\", "/");
  if (t.endsWith(".md")) return "doc";
  const base = t.split("/").pop();
  if (
    /\.(test|spec)\.[^.]+$/.test(base) || // foo.test.ts / foo.spec.js
    /^test_/.test(base) || // test_foo.py
    /_test\.[^.]+$/.test(base) || // foo_test.go / foo_test.rs
    /(^|\/)(tests?|__tests__)\//.test(`${t}/`) // tests/ 目录
  ) return "test";
  if (SOURCE_EXT.has(path.extname(base))) return "source";
  return null;
}

function exemption(rel) {
  const t = rel.replaceAll("\\", "/");
  for (const [prefix, reason] of Object.entries(allowlist)) {
    if (t === prefix || t.startsWith(prefix)) return reason;
  }
  return null;
}

const violations = [];
const warnings = [];
const sizes = [];
let markers = 0;
let scanned = 0;

for (const rel of listFiles()) {
  if (ignored(rel)) continue;
  const cat = category(rel);
  if (cat === null) continue;
  let text;
  try {
    text = readFileSync(path.join(ROOT, rel), "utf8");
  } catch {
    continue;
  }
  scanned++;
  const lines = text.split("\n").length;
  sizes.push({ rel, cat, lines });
  markers += (text.match(MARKER_RE) ?? []).length;

  const limit = limits[cat];
  const exempt = exemption(rel);
  if (lines > limit.fail && !exempt) {
    violations.push(`${rel}: ${lines} 行 > ${limit.fail}（${cat}）`);
  } else if (lines > limit.fail && exempt) {
    warnings.push(`${rel}: ${lines} 行（豁免：${exempt}）`);
  } else if (lines >= limit.warn && !exempt) {
    warnings.push(`${rel}: ${lines} 行，接近 ${limit.fail} 阈值（${cat}）`);
  }
}

sizes.sort((a, b) => b.lines - a.lines);
console.log(`== 最大的 10 个文件（共扫描 ${scanned} 个源码/测试/文档文件）==`);
for (const s of sizes.slice(0, 10)) {
  console.log(`  ${String(s.lines).padStart(5)}  ${s.rel}  (${s.cat})`);
}

const baseline = loadJson(BASELINE_PATH);
const baselineNote = baseline
  ? `（上一基线 ${baseline.markers}，${
      markers > baseline.markers
        ? `+${markers - baseline.markers}，不得增加`
        : markers < baseline.markers
          ? `-${baseline.markers - markers}`
          : "持平"
    }）`
  : "（尚无基线；本门通过后用 --update-baseline 建立）";
console.log(`\nTODO/FIXME/HACK 标记总数：${markers}${baselineNote}`);

if (warnings.length > 0) {
  console.log("\n== 提醒 ==");
  for (const w of warnings) console.log(`  ${w}`);
}
if (violations.length > 0) {
  console.log("\n== 违规（必须修复或登记豁免+清偿任务）==");
  for (const v of violations) console.log(`  ${v}`);
}
if (process.argv.includes("--update-baseline")) {
  if (violations.length > 0) {
    console.log("\n存在违规，跳过 --update-baseline（先修复或登记豁免）。");
  } else {
    writeFileSync(
      BASELINE_PATH,
      `${JSON.stringify({ markers, updated: new Date().toISOString().slice(0, 10) }, null, 2)}\n`,
    );
    console.log(`\n已更新标记基线：${path.basename(BASELINE_PATH)}（记得提交）。`);
  }
}
if (violations.length > 0) process.exit(1);
console.log("\n卫生机械检查通过。");
