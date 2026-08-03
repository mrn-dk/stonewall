// contrast.mjs — WCAG 2.1 contrast audit for the token layer in src/app.css.
//
// The AA ratios recorded in app.css's header comment are produced by this
// script, so a token edit can be re-checked instead of eyeballed:
//
//   node scripts/contrast.mjs
//
// Exits non-zero if any pair falls below its threshold. Every foreground is
// checked against both surfaces it actually appears on — the page background
// and the muted/card panel — because a token that passes on the background can
// still fail on a panel.

import { readFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import { dirname, join } from 'node:path';

const cssPath = join(dirname(fileURLToPath(import.meta.url)), '..', 'src', 'app.css');

/** Parse `oklch(L C H)` / `oklch(L C H / A%)` into components. */
function parseOklch(token) {
  const m = /oklch\(\s*([\d.]+)\s+([\d.]+)\s+([\d.]+)\s*(?:\/\s*([\d.]+)(%?)\s*)?\)/.exec(token);
  if (!m) throw new Error(`not an oklch() color: ${token}`);
  const alpha = m[4] === undefined ? 1 : m[5] === '%' ? Number(m[4]) / 100 : Number(m[4]);
  return { L: Number(m[1]), C: Number(m[2]), H: Number(m[3]), alpha };
}

/** OKLCH -> linear-light sRGB, compositing over `over` when the color has alpha. */
function toLinearSrgb({ L, C, H, alpha }, over = [1, 1, 1]) {
  const h = (H * Math.PI) / 180;
  const a = C * Math.cos(h);
  const b = C * Math.sin(h);
  const l = (L + 0.3963377774 * a + 0.2158037573 * b) ** 3;
  const m = (L - 0.1055613458 * a - 0.0638541728 * b) ** 3;
  const s = (L - 0.0894841775 * a - 1.291485548 * b) ** 3;
  const rgb = [
    4.0767416621 * l - 3.3077115913 * m + 0.2309699292 * s,
    -1.2684380046 * l + 2.6097574011 * m - 0.3413193965 * s,
    -0.0041960863 * l - 0.7034186147 * m + 1.707614701 * s
  ];
  return rgb.map((v, i) => {
    const clamped = Math.min(1, Math.max(0, v));
    return clamped * alpha + over[i] * (1 - alpha);
  });
}

const luminance = ([r, g, b]) => 0.2126 * r + 0.7152 * g + 0.0722 * b;

function contrast(fgToken, bgToken) {
  const bg = toLinearSrgb(parseOklch(bgToken));
  const fg = toLinearSrgb(parseOklch(fgToken), bg);
  const [hi, lo] = [luminance(fg), luminance(bg)].sort((x, y) => y - x);
  return (hi + 0.05) / (lo + 0.05);
}

/**
 * Read the custom properties of one block from app.css. `selector` is matched
 * literally against the rule's prelude.
 */
function readTokens(css, selector) {
  const start = css.indexOf(selector + ' {');
  if (start === -1) throw new Error(`block not found: ${selector}`);
  const open = css.indexOf('{', start);
  const end = css.indexOf('\n}', open);
  const body = css.slice(open, end);
  const tokens = {};
  for (const [, name, value] of body.matchAll(/(--[\w-]+):\s*([^;]+);/g)) {
    tokens[name] = value.trim();
  }
  return tokens;
}

const css = readFileSync(cssPath, 'utf8');
const light = readTokens(css, ':root');
const dark = { ...light, ...readTokens(css, "[data-theme='dark']") };

// Normal-size text needs 4.5:1; non-text UI boundaries (borders, focus ring)
// need 3:1. Panels are the second surface every foreground has to survive.
const foregrounds = [
  '--foreground',
  '--muted-foreground',
  '--destructive',
  '--state-running',
  '--state-parked',
  '--state-pending',
  '--state-terminal',
  '--state-failed',
  '--diff-added',
  '--diff-changed',
  '--diff-removed'
];

let failures = 0;
for (const [themeName, tokens, panel] of [
  ['light', light, '--muted'],
  ['dark', dark, '--card']
]) {
  console.log(`\n${themeName}`);
  for (const fg of foregrounds) {
    for (const bg of ['--background', panel]) {
      const ratio = contrast(tokens[fg], tokens[bg]);
      const ok = ratio >= 4.5;
      if (!ok) failures++;
      console.log(
        `  ${ok ? 'ok  ' : 'FAIL'} ${ratio.toFixed(2).padStart(5)}:1  ${fg} on ${bg}`
      );
    }
  }
  const ring = contrast(tokens['--ring'], tokens['--background']);
  const ringOk = ring >= 3;
  if (!ringOk) failures++;
  console.log(`  ${ringOk ? 'ok  ' : 'FAIL'} ${ring.toFixed(2).padStart(5)}:1  --ring on --background (needs 3)`);
}

console.log(failures === 0 ? '\nAll pairs meet WCAG 2.1 AA.' : `\n${failures} pair(s) below threshold.`);
process.exit(failures === 0 ? 0 : 1);
