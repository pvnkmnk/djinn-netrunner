#!/usr/bin/env node
// Self-check for the comparator. Proves compare.mjs can see a change, stays
// quiet when nothing changed, and flags a size mismatch -- on synthetic images,
// so it needs no stack, no seeded data and no browser session.
//
//   node e2e/visual/self-check.mjs
//
// It drives compare.mjs through its real CLI and reads compare.json, so it
// tests the interface the UI pass actually uses rather than a private copy of
// the logic.

import { chromium } from 'playwright';
import { spawnSync } from 'node:child_process';
import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const here = path.dirname(fileURLToPath(import.meta.url));
const REPO_ROOT = path.resolve(here, '..', '..');
const SCRATCH = path.join(REPO_ROOT, 'e2e', 'visual-output', 'selfcheck');

const failures = [];
function check(label, condition, detail) {
  if (condition) {
    console.log(`    ok    ${label}`);
  } else {
    console.log(`    FAIL  ${label}${detail ? ` -- ${detail}` : ''}`);
    failures.push(label);
  }
}

// The subject: a black square on white, so the image carries no text and no
// font rendering that could differ between two runs.
const page = (square) => `<!DOCTYPE html>
<html><head><style>html,body{margin:0;height:100%}
body{background:#fff}
#mark{width:60px;height:60px;background:${square.color};position:absolute;left:${square.x}px;top:${square.y}px}
</style></head><body><div id="mark"></div></body></html>`;

const SUBJECT = { color: '#000000', x: 40, y: 40 };

async function shoot(browser, size, html, file) {
  const page = await browser.newPage({ viewport: size, deviceScaleFactor: 1 });
  await page.setContent(html);
  fs.writeFileSync(file, await page.screenshot());
  await page.close();
}

function writeManifest(batchDir, label, entries, filePaths) {
  fs.mkdirSync(batchDir, { recursive: true });
  const surfaces = entries.map((entry, index) => ({
    name: entry.name,
    kind: 'page',
    viewport: entry.viewport,
    viewportSize: { width: entry.width, height: entry.height },
    url: 'synthetic://self-check',
    httpStatus: 200,
    note: null,
    file: path.relative(REPO_ROOT, filePaths[index]).split(path.sep).join('/'),
    bytes: fs.statSync(filePaths[index]).size,
    sha256: 'synthetic',
  }));
  fs.writeFileSync(
    path.join(batchDir, 'manifest.json'),
    `${JSON.stringify(
      {
        label,
        capturedAt: new Date(0).toISOString(),
        baseUrl: 'synthetic://self-check',
        authMode: 'none',
        identity: null,
        git: { rev: 'self-check', branch: 'self-check', dirtyFiles: 0 },
        viewports: { desktop: { width: 320, height: 180 } },
        surfaceCount: surfaces.length,
        surfaces,
        failures: [],
      },
      null,
      2,
    )}\n`,
  );
}

function runCompare(beforeDir, afterDir, outDir) {
  const result = spawnSync(
    process.execPath,
    [path.join(here, 'compare.mjs'), '--before', beforeDir, '--after', afterDir, '--out', outDir],
    { encoding: 'utf8' },
  );
  if (result.status !== 0) {
    console.log(result.stdout || '');
    console.error(result.stderr || '');
    throw new Error(`compare.mjs exited ${result.status}`);
  }
  return JSON.parse(fs.readFileSync(path.join(outDir, 'compare.json'), 'utf8'));
}

const SIZE = { width: 320, height: 180 };
const NAME = 'selfcheck';

const browser = await chromium.launch();
try {
  fs.rmSync(SCRATCH, { recursive: true, force: true });

  const baseDir = path.join(SCRATCH, 'base');
  const sameDir = path.join(SCRATCH, 'same');
  const changedDir = path.join(SCRATCH, 'changed');
  const biggerDir = path.join(SCRATCH, 'bigger');
  const biggerChangedDir = path.join(SCRATCH, 'bigger-changed');

  for (const dir of [baseDir, sameDir, changedDir, biggerDir, biggerChangedDir]) fs.mkdirSync(dir, { recursive: true });

  const baseFile = path.join(baseDir, `${NAME}.desktop.png`);
  const sameFile = path.join(sameDir, `${NAME}.desktop.png`);
  const changedFile = path.join(changedDir, `${NAME}.desktop.png`);
  const biggerFile = path.join(biggerDir, `${NAME}.desktop.png`);
  const biggerChangedFile = path.join(biggerChangedDir, `${NAME}.desktop.png`);

  await shoot(browser, SIZE, page(SUBJECT), baseFile);
  // Identical by construction, not by re-rendering: the trap this guards is a
  // comparator that reports a diff where the bytes are equal.
  fs.copyFileSync(baseFile, sameFile);
  // Same size, visibly changed: the square moved and changed colour.
  await shoot(browser, SIZE, page({ color: '#c8102e', x: 120, y: 70 }), changedFile);
  // Same width, taller: the size-mismatch path.
  await shoot(browser, { width: 320, height: 240 }, page(SUBJECT), biggerFile);
  // Taller and changed: the size-mismatch path with real pixel changes on top.
  await shoot(browser, { width: 320, height: 240 }, page({ color: '#c8102e', x: 120, y: 70 }), biggerChangedFile);

  writeManifest(baseDir, 'base', [{ name: NAME, viewport: 'desktop', ...SIZE }], [baseFile]);
  writeManifest(sameDir, 'same', [{ name: NAME, viewport: 'desktop', ...SIZE }], [sameFile]);
  writeManifest(changedDir, 'changed', [{ name: NAME, viewport: 'desktop', ...SIZE }], [changedFile]);
  writeManifest(biggerDir, 'bigger', [{ name: NAME, viewport: 'desktop', width: 320, height: 240 }], [biggerFile]);
  writeManifest(biggerChangedDir, 'bigger-changed', [{ name: NAME, viewport: 'desktop', width: 320, height: 240 }], [biggerChangedFile]);

  console.log('\n  comparator self-check');

  const quiet = runCompare(baseDir, sameDir, path.join(SCRATCH, 'out-same'));
  check(
    'identical batches report no change',
    quiet.changedCount === 0 && quiet.surfaces.every((row) => row.status === 'identical' && row.changed === 0),
    `changedCount=${quiet.changedCount}`,
  );

  const seen = runCompare(baseDir, changedDir, path.join(SCRATCH, 'out-changed'));
  const seenRow = seen.surfaces[0];
  check('a changed image is reported as changed', seen.changedCount === 1 && seenRow.status === 'changed' && seenRow.changed > 0, `status=${seenRow?.status} changed=${seenRow?.changed}`);
  check('the diff percentage is non-zero', seenRow.diffPct > 0, `diffPct=${seenRow?.diffPct}`);
  check('a diff heatmap is written', fs.existsSync(path.join(SCRATCH, 'out-changed', seenRow.diffFile)), seenRow.diffFile);
  check('compare.html is written', fs.existsSync(path.join(SCRATCH, 'out-changed', 'compare.html')));

  // A copied batch must be compared against its own screenshots. The copy's
  // manifest still names the original's changed image, while its own file is
  // the unchanged one: trusting the manifest reports a change that is not there.
  const copyDir = path.join(SCRATCH, 'changed-copy');
  fs.cpSync(changedDir, copyDir, { recursive: true });
  fs.copyFileSync(baseFile, path.join(copyDir, `${NAME}.desktop.png`));
  const copied = runCompare(baseDir, copyDir, path.join(SCRATCH, 'out-copy'));
  check(
    'a copied batch is read from its own directory',
    copied.surfaces[0].changed === 0 && copied.surfaces[0].status === 'identical',
    `changed=${copied.surfaces[0].changed} status=${copied.surfaces[0].status}`,
  );

  const resized = runCompare(baseDir, biggerDir, path.join(SCRATCH, 'out-bigger'));
  const resizedChanged = runCompare(baseDir, biggerChangedDir, path.join(SCRATCH, 'out-bigger-changed'));
  check(
    'a size mismatch is reported as one',
    resized.surfaces[0].sameSize === false && resized.surfaces[0].status === 'size mismatch' && resized.surfaces[0].changed === 0,
    `status=${resized.surfaces[0].status} changed=${resized.surfaces[0].changed}`,
  );
  check(
    'a resize with changed pixels says both',
    resizedChanged.surfaces[0].sameSize === false && resizedChanged.surfaces[0].status === 'changed (size mismatch)',
    `status=${resizedChanged.surfaces[0].status}`,
  );
} finally {
  await browser.close();
}

console.log(`\n  ${failures.length ? `${failures.length} check(s) failed` : 'all checks passed'}\n`);
process.exitCode = failures.length ? 1 : 0;
