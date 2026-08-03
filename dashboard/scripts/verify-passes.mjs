// verify-passes.mjs — keyboard, dialog-focus and async-state verification
// against a running dashboard.
//
//   npm run verify                       # localhost:8080 seeded, :8081 empty
//   node scripts/verify-passes.mjs <seededBase> <emptyBase>
//
// Requires two running instances: one with agents, one with none. Playwright is
// NOT a project dependency — install it where you run this:
//
//   npm i playwright && npx playwright install chromium
//
// Exits non-zero if any check fails.
//
// These checks exist because the properties they cover cannot be read off the
// source with any confidence. They have already earned it: the first run found
// that opening a workspace file tore the whole workbench down, because an
// $effect had picked up a dependency nobody intended.

import { chromium } from 'playwright';

const SEEDED = process.argv[2] ?? 'http://localhost:8080';
const EMPTY = process.argv[3] ?? 'http://localhost:8081';

let failures = 0;
const results = [];
function check(name, ok, detail = '') {
  if (!ok) failures++;
  results.push(`${ok ? 'ok  ' : 'FAIL'} ${name}${detail ? ` — ${detail}` : ''}`);
  console.log(results.at(-1));
}

const active = (page) =>
  page.evaluate(() => {
    const el = document.activeElement;
    if (!el) return null;
    return {
      tag: el.tagName.toLowerCase(),
      text: (el.textContent ?? '').trim().slice(0, 40),
      label: el.getAttribute('aria-label'),
      type: el.getAttribute('type'),
      id: el.id
    };
  });

// A focus ring must be *visible*: outline or box-shadow, not `outline: none`
// with nothing replacing it.
const focusVisible = (page) =>
  page.evaluate(() => {
    const el = document.activeElement;
    if (!el || el === document.body) return false;
    const s = getComputedStyle(el);
    const outlined = s.outlineStyle !== 'none' && parseFloat(s.outlineWidth) > 0;
    const shadowed = s.boxShadow !== 'none' && s.boxShadow !== '';
    return outlined || shadowed;
  });

async function tabTo(page, predicate, max = 60) {
  for (let i = 0; i < max; i++) {
    await page.keyboard.press('Tab');
    const el = await active(page);
    if (el && predicate(el)) return el;
  }
  return null;
}

const browser = await chromium.launch();
const ctx = await browser.newContext({ viewport: { width: 1600, height: 1000 } });
const page = await ctx.newPage();
const errors = [];
page.on('pageerror', (e) => errors.push(String(e)));
page.on('console', (m) => m.type() === 'error' && errors.push(m.text()));

// ---------------------------------------------------------------- 10.4 async
console.log('\n## 10.4 async states');

// Empty fleet: says nothing exists AND offers the create action.
await page.goto(EMPTY, { waitUntil: 'networkidle' });
const emptyText = await page.locator('body').innerText();
check('empty fleet names what is absent', /no agents yet/i.test(emptyText));
check(
  'empty fleet offers create',
  await page.getByRole('button', { name: /new agent/i }).first().isVisible()
);

// Filtered-empty is a *different* state from a genuinely empty fleet.
await page.goto(`${SEEDED}/?q=zzzznomatch`, { waitUntil: 'networkidle' });
const filteredText = await page.locator('body').innerText();
check('filtered-empty is distinct from empty', /no agents match/i.test(filteredText));
check('filtered-empty offers clear', /clear filters/i.test(filteredText));

// Loading must be distinguishable from empty: throttle the list response and
// assert a loading state appears before any empty state could.
const slow = await ctx.newPage();
await slow.route('**/v1/agents?*', async (route) => {
  await new Promise((r) => setTimeout(r, 1200));
  await route.continue();
});
await slow.goto(SEEDED, { waitUntil: 'domcontentloaded' });
await slow.waitForSelector('text=Fleet', { timeout: 5000 });
const midFlight = await slow.locator('body').innerText();
const busy = await slow.locator('[role="status"][aria-busy="true"]').count();
check('loading state shown while in flight', busy > 0, `${busy} busy region(s)`);
check('empty state not shown while loading', !/no agents yet/i.test(midFlight));
check('table not shown while loading', !/isolation/i.test(midFlight));
await slow.close();

// A failed panel retries in isolation: fail only the activations request and
// confirm the transcript/timeline still render.
const agents = await (await fetch(`${SEEDED}/v1/agents`)).json();
const agentId = agents.agents.find((a) => a.last_turn > 0)?.id ?? agents.agents[0].id;

const iso = await ctx.newPage();
let activationCalls = 0;
let allowActivations = false;
let otherCalls = 0;
await iso.route('**/activations', async (route) => {
  activationCalls++;
  if (allowActivations) return route.continue();
  return route.fulfill({
    status: 500,
    contentType: 'application/json',
    body: '{"message":"boom","request_id":"req-test-1"}'
  });
});
await iso.route('**/v1/agents/*/workspace*', async (route) => {
  otherCalls++;
  return route.continue();
});
await iso.goto(`${SEEDED}/agents/${agentId}`, { waitUntil: 'networkidle' });
await iso.waitForTimeout(600);
const isoText = await iso.locator('body').innerText();
check('failed panel shows its own error', /could not load activations/i.test(isoText));
check('other panes still render', /timeline/i.test(isoText) && /workspace/i.test(isoText));
const retry = iso.getByRole('button', { name: /retry/i }).first();
if (await retry.isVisible().catch(() => false)) {
  check('failed panel carries the request id', /req-test-1/.test(isoText));
  allowActivations = true;
  const before = activationCalls;
  const otherBefore = otherCalls;
  await retry.click();
  await iso.waitForTimeout(800);
  check(
    'retry re-issues that request',
    activationCalls > before,
    `activations ${before} -> ${activationCalls}`
  );
  check(
    'retry does not re-issue other panels',
    otherCalls === otherBefore,
    `workspace ${otherBefore} -> ${otherCalls}`
  );
  check(
    'panel recovers after retry',
    !/could not load activations/i.test(await iso.locator('body').innerText())
  );
} else {
  check('retry control present on failed panel', false);
}
await iso.close();

// ------------------------------------------------------------- 10.1 keyboard
console.log('\n## 10.1 keyboard-only');

await page.goto(SEEDED, { waitUntil: 'networkidle' });
await page.keyboard.press('Tab');
check('first tab stop has a visible focus ring', await focusVisible(page));

const search = await tabTo(page, (el) => el.type === 'search');
check('search box reachable by keyboard', !!search);

const rowLink = await tabTo(page, (el) => el.tag === 'a' && /^agt_/.test(el.text));
check('agent row link reachable by keyboard', !!rowLink, rowLink?.text);
check('row link focus ring visible', await focusVisible(page));

if (rowLink) {
  await page.keyboard.press('Enter');
  await page.waitForURL(/\/agents\//, { timeout: 5000 });
  check('Enter on a row opens the workbench', /\/agents\//.test(page.url()));
}

await page.waitForTimeout(800);
// Select a turn from the keyboard.
const turnBtn = await tabTo(page, (el) => /^Turn \d+/.test(el.label ?? ''), 80);
check('timeline turn reachable by keyboard', !!turnBtn, turnBtn?.label);
if (turnBtn) {
  await page.keyboard.press('Enter');
  await page.waitForTimeout(700);
  check(
    'selecting a turn syncs the workspace',
    await page.getByText(/Workspace/i).first().isVisible()
  );
}

// Open a workspace file from the keyboard.
const fileBtn = await tabTo(page, (el) => /\.(txt|md|json|go|js)\b/.test(el.text), 90);
check('workspace file reachable by keyboard', !!fileBtn, fileBtn?.text);
if (fileBtn) {
  await page.keyboard.press('Enter');
  await page.waitForTimeout(600);
  check('opening a file renders it', await page.getByLabel('Close file').first().isVisible());
}

// Send input from the keyboard.
await page.goto(`${SEEDED}/agents/${agentId}`, { waitUntil: 'networkidle' });
const msg = await tabTo(page, (el) => el.label === 'Message body', 60);
check('message box reachable by keyboard', !!msg);
if (msg) {
  await page.keyboard.type('keyboard pass');
  await page.keyboard.press('Enter');
  await page.waitForTimeout(600);
  check('sending input reports an outcome', /input sent/i.test(await page.locator('body').innerText()));
}

// -------------------------------------------------------------- 10.2 dialogs
console.log('\n## 10.2 dialog focus');

async function dialogPass(name, open, { destructiveOf = null } = {}) {
  await page.goto(`${SEEDED}/agents/${agentId}`, { waitUntil: 'networkidle' });
  await page.waitForTimeout(400);
  const opener = await open();
  if (!opener) return check(`${name}: trigger found`, false);

  const openerBox = await page.evaluate(() => document.activeElement?.outerHTML?.slice(0, 80));
  await page.keyboard.press('Enter');
  await page.waitForTimeout(500);

  const dialog = page.locator('[role="dialog"], [role="alertdialog"]').first();
  const visible = await dialog.isVisible().catch(() => false);
  check(`${name}: opens`, visible);
  if (!visible) return;

  // Focus must be inside the dialog.
  const inside = await page.evaluate(() => {
    const d = document.querySelector('[role="dialog"], [role="alertdialog"]');
    return !!d && d.contains(document.activeElement);
  });
  check(`${name}: focus moves inside`, inside);

  // Tabbing must not escape it.
  let escaped = false;
  for (let i = 0; i < 12; i++) {
    await page.keyboard.press('Tab');
    const still = await page.evaluate(() => {
      const d = document.querySelector('[role="dialog"], [role="alertdialog"]');
      return !!d && d.contains(document.activeElement);
    });
    if (!still) {
      escaped = true;
      break;
    }
  }
  check(`${name}: focus is trapped`, !escaped);

  const before = destructiveOf ? await destructiveOf() : null;
  await page.keyboard.press('Escape');
  await page.waitForTimeout(400);
  check(`${name}: Escape closes`, !(await dialog.isVisible().catch(() => false)));

  const returned = await page.evaluate(
    (html) => document.activeElement?.outerHTML?.slice(0, 80) === html,
    openerBox
  );
  check(`${name}: focus returns to opener`, returned);

  if (destructiveOf) {
    const after = await destructiveOf();
    check(`${name}: Escape performs no action`, before === after, `${before} -> ${after}`);
  }
}

const agentState = async () => (await (await fetch(`${SEEDED}/v1/agents/${agentId}`)).json()).state;

await dialogPass(
  'cancel',
  async () => tabTo(page, (el) => /^Cancel$/i.test(el.text), 80),
  { destructiveOf: agentState }
);
await dialogPass('delete', async () => tabTo(page, (el) => /^Delete$/i.test(el.text), 80), {
  destructiveOf: agentState
});
await dialogPass('restore', async () => tabTo(page, (el) => /^Restore$/i.test(el.text), 80), {
  destructiveOf: agentState
});

// Command palette: opens on the shortcut, closes on Escape.
await page.goto(SEEDED, { waitUntil: 'networkidle' });
await page.keyboard.press('Control+k');
await page.waitForTimeout(400);
const palette = page.locator('[role="dialog"]').first();
check('palette: opens on Ctrl-K', await palette.isVisible().catch(() => false));
await page.keyboard.type('index');
await page.waitForTimeout(800);
const paletteText = await page.locator('body').innerText();
check('palette: finds agents by goal', /agt_/.test(paletteText));
await page.keyboard.press('Escape');
await page.waitForTimeout(300);
check('palette: Escape closes', !(await palette.isVisible().catch(() => false)));

check('no uncaught page errors', errors.length === 0, errors.slice(0, 2).join(' | '));

await browser.close();
console.log(`\n${failures === 0 ? 'ALL PASSES GREEN' : `${failures} FAILURE(S)`}`);
process.exit(failures === 0 ? 0 : 1);
