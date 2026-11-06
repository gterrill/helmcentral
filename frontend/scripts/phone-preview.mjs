// Screenshot a page in a touch-enabled phone context (default 390x844 at 2x).
//
//   node frontend/scripts/phone-preview.mjs <url> <out.png>
//        [--width N] [--height N] [--wait-for <selector>] [--full-page]
//
// Playwright is a pinned devDependency, so this needs no npx cache lookup.
// The viewport is set by the browser context, which is why it works where
// resizing a real Chrome window does not.
import { chromium } from 'playwright';

const args = process.argv.slice(2);
const positional = [];
const opts = { width: 390, height: 844, waitFor: '', fullPage: false };

for (let i = 0; i < args.length; i++) {
  const a = args[i];
  if (a === '--full-page') opts.fullPage = true;
  else if (a === '--width') opts.width = Number(args[++i]);
  else if (a === '--height') opts.height = Number(args[++i]);
  else if (a === '--wait-for') opts.waitFor = args[++i] ?? '';
  else if (a.startsWith('--')) fail(`unknown option ${a}`);
  else positional.push(a);
}

const [url, out] = positional;
if (!url || !out) fail('usage: phone-preview.mjs <url> <out.png> [--width N] [--height N] [--wait-for <selector>] [--full-page]');
if (!Number.isFinite(opts.width) || !Number.isFinite(opts.height)) fail('--width and --height must be numbers');

function fail(msg) {
  console.error(msg);
  process.exit(2);
}

const browser = await chromium.launch();
try {
  const context = await browser.newContext({
    viewport: { width: opts.width, height: opts.height },
    deviceScaleFactor: 2,
    isMobile: true,
    hasTouch: true,
  });
  const page = await context.newPage();
  // Not 'networkidle': the dashboard holds /api/stream open, so the network
  // never goes idle. Wait for a selector when given, else a fixed settle.
  await page.goto(url, { waitUntil: 'load' });
  if (opts.waitFor) await page.waitForSelector(opts.waitFor, { timeout: 15000 });
  else await page.waitForTimeout(3000);
  await page.screenshot({ path: out, fullPage: opts.fullPage });
  const scrollWidth = await page.evaluate(() => document.documentElement.scrollWidth);
  console.log(`wrote ${out} (viewport ${opts.width}x${opts.height}, page scrollWidth ${scrollWidth})`);
  if (scrollWidth > opts.width) console.log('warning: page is wider than the viewport (horizontal overflow)');
} finally {
  await browser.close();
}
