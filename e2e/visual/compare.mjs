#!/usr/bin/env node
// UI visual harness -- compare two captured batches and write a report a
// reviewer can actually read: before | after | diff heatmap per surface, plus
// numeric diff percentages in compare.json.
//
// Entry point:  scripts/ui-visual.sh compare before after
// Direct:       node e2e/visual/compare.mjs --before before --after after
//
// Output: <out-root>/compare-<before>-<after>/compare.html, compare.json, diff/*.png

import { chromium } from 'playwright';
import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const here = path.dirname(fileURLToPath(import.meta.url));
const REPO_ROOT = path.resolve(here, '..', '..');
const DEFAULT_OUT_ROOT = path.join(REPO_ROOT, 'e2e', 'visual-output');
// Per-channel delta that counts a pixel as changed. Above the noise a
// compositor can introduce, below anything a designer would call a change.
const DEFAULT_THRESHOLD = 24;

let browser;

function fail(message) {
  console.error(`\n  error: ${message}\n`);
  process.exit(2);
}

function parseArgs(argv) {
  const args = { before: 'before', after: 'after', out: null, threshold: DEFAULT_THRESHOLD, outRoot: process.env.UI_VIS_OUT || DEFAULT_OUT_ROOT };
  for (let i = 0; i < argv.length; i++) {
    const flag = argv[i];
    const value = () => argv[++i];
    if (flag === '--before') args.before = value();
    else if (flag === '--after') args.after = value();
    else if (flag === '--out') args.out = value();
    else if (flag === '--threshold') args.threshold = Number(value());
    else if (flag === '--out-root') args.outRoot = value();
    else if (flag === '--help' || flag === '-h') {
      console.log(`
  compare.mjs -- side-by-side + pixel diff for two captured batches

    --before <label|dir>   default "before"
    --after  <label|dir>   default "after"
    --out    <dir>         default <out-root>/compare-<before>-<after>
    --threshold <0-255>    per-channel delta counted as changed (default ${DEFAULT_THRESHOLD})
`);
      process.exit(0);
    } else fail(`unknown argument ${flag}`);
  }
  return args;
}

// A batch is either a bare label (resolved under the output root) or a path.
function resolveBatch(value, outRoot, side) {
  const direct = path.isAbsolute(value) ? value : path.resolve(process.cwd(), value);
  const dir = fs.existsSync(path.join(direct, 'manifest.json')) ? direct : path.join(outRoot, value);
  const manifestFile = path.join(dir, 'manifest.json');
  if (!fs.existsSync(manifestFile)) {
    fail(`no batch "${value}" (${side}): ${manifestFile} does not exist\n  capture it first: scripts/ui-visual.sh capture ${value}`);
  }
  return { dir, manifest: JSON.parse(fs.readFileSync(manifestFile, 'utf8')) };
}

const key = (record) => `${record.name}@${record.viewport}`;

async function diffImages(page, beforeFile, afterFile, threshold) {
  const dataUrl = (file) => `data:image/png;base64,${fs.readFileSync(file).toString('base64')}`;
  return page.evaluate(
    async ({ aData, bData, threshold: limit }) => {
      const load = (src) =>
        new Promise((resolve, reject) => {
          const img = new Image();
          img.onload = () => resolve(img);
          img.onerror = () => reject(new Error('image decode failed'));
          img.src = src;
        });
      const [before, after] = await Promise.all([load(aData), load(bData)]);
      const width = Math.max(before.width, after.width);
      const height = Math.max(before.height, after.height);

      const sample = (img) => {
        const canvas = document.createElement('canvas');
        canvas.width = width;
        canvas.height = height;
        const ctx = canvas.getContext('2d');
        ctx.fillStyle = '#ffffff';
        ctx.fillRect(0, 0, width, height);
        ctx.drawImage(img, 0, 0);
        return ctx.getImageData(0, 0, width, height);
      };
      const a = sample(before);
      const b = sample(after);

      const heatmap = document.createElement('canvas');
      heatmap.width = width;
      heatmap.height = height;
      const ctx = heatmap.getContext('2d');
      const out = ctx.createImageData(width, height);
      let changed = 0;
      for (let i = 0; i < a.data.length; i += 4) {
        const delta = Math.max(
          Math.abs(a.data[i] - b.data[i]),
          Math.abs(a.data[i + 1] - b.data[i + 1]),
          Math.abs(a.data[i + 2] - b.data[i + 2]),
        );
        if (delta > limit) {
          changed += 1;
          out.data[i] = 220;
          out.data[i + 1] = 30;
          out.data[i + 2] = 30;
        } else {
          out.data[i] = 246;
          out.data[i + 1] = 246;
          out.data[i + 2] = 246;
        }
        out.data[i + 3] = 255;
      }
      ctx.putImageData(out, 0, 0);
      return {
        width,
        height,
        pixels: width * height,
        changed,
        sameSize: before.width === after.width && before.height === after.height,
        beforeSize: [before.width, before.height],
        afterSize: [after.width, after.height],
        diff: heatmap.toDataURL('image/png'),
      };
    },
    { aData: dataUrl(beforeFile), bData: dataUrl(afterFile), threshold },
  );
}

// A surface whose dimensions changed is never "identical", even when every
// pixel that overlaps is unchanged: the padding does not show up in a changed
// pixel count, and a reviewer still has to know the shot was resized.
// Screenshots live beside the manifest that describes them, so resolve there
// first. Trusting the stored (repo-relative) path instead silently compares
// the original batch when the directory you named is a copy -- measured: a
// mutated copy of a batch compared as identical to the original.
function imageIn(batchDir, record) {
  const beside = path.join(batchDir, path.basename(record.file));
  return fs.existsSync(beside) ? beside : path.join(REPO_ROOT, record.file);
}

function rowStatus(changed, sameSize) {
  if (!sameSize) return changed === 0 ? 'size mismatch' : 'changed (size mismatch)';
  return changed === 0 ? 'identical' : 'changed';
}

function percent(changed, pixels) {
  if (!pixels) return 0;
  return Math.round((changed / pixels) * 10_000) / 100;
}

function escapeHtml(value) {
  return String(value ?? '').replace(/[&<>"]/g, (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;' })[c]);
}

function renderHtml(context) {
  const { before, after, rows, missing, threshold, outDir } = context;
  const sorted = [...rows].sort((a, b) => b.diffPct - a.diffPct);
  const changedRows = sorted.filter((row) => row.status !== 'identical');
  const rel = (absolute) => path.relative(outDir, absolute).split(path.sep).join('/');

  const summary = sorted
    .map(
      (row) => `<tr class="${row.status === 'identical' ? '' : 'changed'}">
      <td>${escapeHtml(row.name)}</td>
      <td>${escapeHtml(row.viewport)}</td>
      <td class="num">${row.diffPct}%</td>
      <td class="num">${row.changed.toLocaleString()}</td>
      <td>${row.sameSize ? `${row.width}x${row.height}` : `size ${row.beforeSize.join('x')} -> ${row.afterSize.join('x')}`}</td>
      <td>${row.domChanged ? 'changed' : 'same'}</td>
      <td><a href="#${escapeHtml(row.id)}">jump</a></td>
    </tr>`,
    )
    .join('\n');

  const details = sorted
    .map(
      (row) => `<section id="${escapeHtml(row.id)}">
    <h3>${escapeHtml(row.name)} <span class="muted">${escapeHtml(row.viewport)}</span>
      <span class="badge ${row.status === 'identical' ? 'badge-same' : 'badge-changed'}">${row.diffPct}% \u00b7 ${row.status}</span>
      <span class="muted">dom ${row.domChanged ? 'changed' : 'same'}</span></h3>
    <div class="panes">
      <figure><figcaption>before</figcaption><img loading="lazy" src="${escapeHtml(rel(row.beforeFile))}" alt=""></figure>
      <figure><figcaption>after</figcaption><img loading="lazy" src="${escapeHtml(rel(row.afterFile))}" alt=""></figure>
      <figure><figcaption>diff (${row.changed.toLocaleString()} px)</figcaption><img loading="lazy" src="${escapeHtml(rel(row.diffFile))}" alt=""></figure>
    </div>
  </section>`,
    )
    .join('\n');

  const missingBlock = missing.length
    ? `<section><h3>Present in only one batch</h3><ul>${missing
        .map((item) => `<li>${escapeHtml(item.name)} <span class="muted">${escapeHtml(item.viewport)}</span> — ${item.side}</li>`)
        .join('')}</ul></section>`
    : '';

  return `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<title>UI diff: ${escapeHtml(before.manifest.label)} → ${escapeHtml(after.manifest.label)}</title>
<style>
  :root { color-scheme: light; }
  body { margin: 0; padding: 2rem; background: #f4f4f6; color: #17171b;
         font: 14px/1.5 ui-monospace, SFMono-Regular, Menlo, Consolas, monospace; }
  h1 { font-size: 1.35rem; margin: 0 0 .25rem; }
  h3 { font-size: 1rem; margin: 0 0 .6rem; }
  .muted { color: #6b6b76; font-weight: 400; }
  .meta { color: #4a4a55; margin-bottom: 1.5rem; }
  table { border-collapse: collapse; width: 100%; background: #fff; }
  th, td { text-align: left; padding: .4rem .6rem; border-bottom: 1px solid #e2e2e8; }
  td.num { text-align: right; font-variant-numeric: tabular-nums; }
  tr.changed { background: #fff5f5; }
  .badge { font-size: .78rem; padding: .1rem .45rem; border-radius: 999px; margin-left: .5rem; }
  .badge-changed { background: #fde2e2; color: #8c1c1c; }
  .badge-same { background: #e6f4ea; color: #1c5c2e; }
  section { margin: 2.5rem 0; }
  .panes { display: grid; grid-template-columns: repeat(3, 1fr); gap: 1rem; align-items: start; }
  figure { margin: 0; background: #fff; border: 1px solid #e2e2e8; }
  figcaption { padding: .35rem .6rem; border-bottom: 1px solid #e2e2e8; color: #4a4a55; }
  img { display: block; width: 100%; height: auto; }
  a { color: #1b4fd8; }
</style>
</head>
<body>
<h1>UI diff — ${escapeHtml(before.manifest.label)} → ${escapeHtml(after.manifest.label)}</h1>
<p class="meta">
  ${rows.length} surface/viewport pairs, ${changedRows.length} changed · threshold ${threshold}/255 per channel<br>
  before: ${escapeHtml(before.manifest.capturedAt)} @ ${escapeHtml(before.manifest.baseUrl)}
    (${escapeHtml(before.manifest.git?.rev || '?')}, dirty ${escapeHtml(before.manifest.git?.dirtyFiles ?? '?')}) as ${escapeHtml(before.manifest.identity || before.manifest.authMode)}<br>
  after: ${escapeHtml(after.manifest.capturedAt)} @ ${escapeHtml(after.manifest.baseUrl)}
    (${escapeHtml(after.manifest.git?.rev || '?')}, dirty ${escapeHtml(after.manifest.git?.dirtyFiles ?? '?')}) as ${escapeHtml(after.manifest.identity || after.manifest.authMode)}
</p>
<table>
<thead><tr><th>surface</th><th>viewport</th><th>diff</th><th>px changed</th><th>size</th><th>dom</th><th></th></tr></thead>
<tbody>
${summary}
</tbody>
</table>
${missingBlock}
${details}
</body>
</html>
`;
}

async function main() {
  const args = parseArgs(process.argv.slice(2));
  const before = resolveBatch(args.before, args.outRoot, 'before');
  const after = resolveBatch(args.after, args.outRoot, 'after');
  const outDir = args.out ? path.resolve(args.out) : path.join(args.outRoot, `compare-${before.manifest.label}-${after.manifest.label}`);
  const diffDir = path.join(outDir, 'diff');
  fs.mkdirSync(diffDir, { recursive: true });

  const beforeIndex = new Map(before.manifest.surfaces.map((record) => [key(record), record]));
  const afterIndex = new Map(after.manifest.surfaces.map((record) => [key(record), record]));
  const missing = [];
  for (const [k, record] of beforeIndex) if (!afterIndex.has(k)) missing.push({ name: record.name, viewport: record.viewport, side: 'before only' });
  for (const [k, record] of afterIndex) if (!beforeIndex.has(k)) missing.push({ name: record.name, viewport: record.viewport, side: 'after only' });

  browser = await chromium.launch();
  const page = await browser.newPage();

  const rows = [];
  for (const [k, beforeRecord] of beforeIndex) {
    const afterRecord = afterIndex.get(k);
    if (!afterRecord) continue;
    const beforeFile = imageIn(before.dir, beforeRecord);
    const afterFile = imageIn(after.dir, afterRecord);
    const result = await diffImages(page, beforeFile, afterFile, args.threshold);
    const diffFile = path.join(diffDir, `${beforeRecord.name}.${beforeRecord.viewport}.diff.png`);
    fs.writeFileSync(diffFile, Buffer.from(result.diff.split(',')[1], 'base64'));
    rows.push({
      id: `${beforeRecord.name}-${beforeRecord.viewport}`,
      name: beforeRecord.name,
      viewport: beforeRecord.viewport,
      beforeFile,
      afterFile,
      diffFile,
      beforeStatus: beforeRecord.httpStatus,
      afterStatus: afterRecord.httpStatus,
      imageHashBefore: beforeRecord.imageHash || beforeRecord.sha256 || null,
      imageHashAfter: afterRecord.imageHash || afterRecord.sha256 || null,
      domHashBefore: beforeRecord.domHash || null,
      domHashAfter: afterRecord.domHash || null,
      // Distinct from a pixel diff: markup that moved AND pixels that moved.
      // Data behind a surface changing (a scan adding tracks) shows up here
      // with no UI change at all, so a reviewer is not left guessing.
      domChanged: Boolean(beforeRecord.domHash && afterRecord.domHash && beforeRecord.domHash !== afterRecord.domHash),
      width: result.width,
      height: result.height,
      beforeSize: result.beforeSize,
      afterSize: result.afterSize,
      sameSize: result.sameSize,
      pixels: result.pixels,
      changed: result.changed,
      diffPct: percent(result.changed, result.pixels),
      status: rowStatus(result.changed, result.sameSize),
    });
  }
  await page.close();
  await browser.close();
  browser = null;

  const report = {
    before: { label: before.manifest.label, dir: before.dir, capturedAt: before.manifest.capturedAt, baseUrl: before.manifest.baseUrl, git: before.manifest.git },
    after: { label: after.manifest.label, dir: after.dir, capturedAt: after.manifest.capturedAt, baseUrl: after.manifest.baseUrl, git: after.manifest.git },
    threshold: args.threshold,
    generatedAt: new Date().toISOString(),
    surfaceCount: rows.length,
    changedCount: rows.filter((row) => row.changed > 0 || !row.sameSize).length,
    surfaces: rows.map((row) => ({ ...row, beforeFile: path.relative(outDir, row.beforeFile).split(path.sep).join('/'), afterFile: path.relative(outDir, row.afterFile).split(path.sep).join('/'), diffFile: path.relative(outDir, row.diffFile).split(path.sep).join('/') })),
    missing,
  };
  fs.writeFileSync(path.join(outDir, 'compare.json'), `${JSON.stringify(report, null, 2)}\n`);
  fs.writeFileSync(path.join(outDir, 'compare.html'), renderHtml({ before, after, rows, missing, threshold: args.threshold, outDir }));

  const sorted = [...rows].sort((a, b) => b.diffPct - a.diffPct);
  console.log(`\n  diff ${before.manifest.label} -> ${after.manifest.label}  (${rows.length} pairs, threshold ${args.threshold})`);
  for (const row of sorted) {
    const marker = row.changed === 0 ? '  ' : '≠ ';
    console.log(
      `   ${marker}${row.name.padEnd(22)} ${row.viewport.padEnd(8)} ${String(row.diffPct).padStart(6)}%  ${row.status.padEnd(24)} dom ${row.domChanged ? 'changed' : 'same'}`,
    );
  }
  if (missing.length) console.log(`\n   ${missing.length} pair(s) present in only one batch -- see compare.html`);
  console.log(`\n  report: ${path.relative(process.cwd(), path.join(outDir, 'compare.html')) || 'compare.html'}`);
  console.log(`  json:   ${path.relative(process.cwd(), path.join(outDir, 'compare.json'))}\n`);
}

try {
  await main();
} catch (error) {
  console.error(`\n  error: ${error?.stack || error}\n`);
  process.exitCode = 2;
} finally {
  await browser?.close().catch(() => {});
}
