// One-time browser observation; replay is restricted to observed, read-only JSON APIs.
// No credentials, Cookie values, request bodies, or full URLs are written to disk.
const fs = require('node:fs');
const os = require('node:os');
const path = require('node:path');
const readline = require('node:readline');

const READ_PATHS = new Set([
  '/shop/updateSummary', '/shop/bag/status', '/shop/retail/pickup-message',
]);
const OBSERVE_PATHS = new Set([...READ_PATHS, '/shop/bag', '/shop/fulfillment-messages']);
const MAX_AGE_MS = 5 * 60 * 1000;

class ProbeError extends Error {
  constructor(kind, message, status, shape) {
    super(message);
    this.kind = kind;
    this.status = status;
    this.shape = shape;
  }
}

function failureSummary(error, record, index) {
  let target;
  try { target = endpoint(record.url); } catch { target = {}; }
  const known = error instanceof ProbeError;
  return { index, ...target, outcome: 'failed', kind: known ? error.kind : 'transport',
    status: known ? error.status : undefined, shape: known ? error.shape : undefined,
    retry: false };
}

function storeURL(raw) {
  const u = new URL(raw);
  if (u.protocol !== 'https:' || u.username || u.password || u.port ||
      !/^(www|secure\d*\.www)\.apple\.com\.cn$/.test(u.hostname)) {
    throw new Error('Only HTTPS Apple China store URLs are accepted');
  }
  return u;
}

function observedURL(raw) {
  try {
    const u = storeURL(raw);
    return OBSERVE_PATHS.has(u.pathname) || /^\/shop\/buy-iphone\//.test(u.pathname);
  } catch { return false; }
}

function endpoint(raw) {
  const u = storeURL(raw);
  return { host: u.hostname, path: u.pathname, queryKeys: [...u.searchParams.keys()] };
}

function assertReplay(record, now = Date.now()) {
  const u = storeURL(record.url);
  if (record.method !== 'GET' || !READ_PATHS.has(u.pathname) ||
      [...u.searchParams.keys()].some(k => /add.to.cart|checkout|submit|payment|order/i.test(k))) {
    throw new Error('Not an allowlisted read-only request; no request sent');
  }
  if (!Number.isFinite(record.at) || now < record.at || now - record.at > MAX_AGE_MS) {
    throw new Error('Observation expired; observe a fresh request in this session');
  }
  if (record.status !== 200) throw new Error('Failed observations cannot be replayed');
  if (!/application\/json\b/i.test(record.contentType || '')) {
    throw new Error('Observation was not JSON');
  }
}

function validateJSON(status, contentType, body, apiPath) {
  if (status !== 200) throw new ProbeError('http', `HTTP ${status}; stopped without retry`, status);
  if (!/application\/json\b/i.test(contentType || '')) throw new ProbeError('content_type', 'Expected JSON, not HTML', status);
  let data;
  try { data = JSON.parse(body); } catch { throw new ProbeError('invalid_json', 'Invalid JSON', status); }
  if (!data || typeof data !== 'object') throw new ProbeError('schema', 'Unexpected JSON envelope', status);
  if (data.head?.status != null && data.head.status !== 200) {
    throw new ProbeError('application', 'Apple returned a non-success application status', status);
  }
  if (data.body?.errorMessage || data.body?.error) throw new ProbeError('application', 'Apple returned an application error', status);
  if (apiPath === '/shop/updateSummary' && !data.body?.response?.summarySection?.summary) {
    throw new ProbeError('schema', 'Missing product summary; HTTP 200 alone is not success', status);
  }
  if (apiPath === '/shop/retail/pickup-message' &&
      !Array.isArray(data.body?.stores) && !Array.isArray(data.body?.content?.pickupMessage?.stores)) {
    throw new ProbeError('schema', 'Missing pickup stores; cannot infer availability', status);
  }
  // The observed bag-status API does not use the usual {head, body} envelope.
  if (apiPath === '/shop/bag/status' &&
      (!Number.isInteger(data.items) || data.items < 0 || !Number.isFinite(data.ttl) ||
       data.ttl < 0 || !data.api || typeof data.api !== 'object' || Array.isArray(data.api))) {
    throw new ProbeError('schema', 'Missing bag status fields', status);
  }
  return data;
}

async function replay(requestContext, record, now = Date.now()) {
  try { assertReplay(record, now); }
  catch { throw new ProbeError('precondition', 'Observation not eligible for read-only replay'); }
  const started = Date.now();
  const response = await requestContext.get(record.url, {
    headers: { accept: 'application/json', ...record.headers },
    timeout: 15000, maxRedirects: 0, maxRetries: 0,
  });
  try {
    const data = validateJSON(response.status(), response.headers()['content-type'], await response.text(),
      new URL(record.url).pathname);
    return { ...endpoint(record.url), status: response.status(),
      transport: 'http-request', elapsedMs: Date.now() - started,
      itemCount: new URL(record.url).pathname === '/shop/bag/status' ? data.items : undefined };
  } finally { await response.dispose(); }
}

async function main() {
  if (process.argv.includes('--help')) {
    console.log('node scripts/purchase_probe.cjs\nCommands: requests | replay <index> | status | exit\n' +
      'Manual browser login/observation only. No automatic add-to-bag, checkout, order or payment.\n' +
      'Uses locally installed Playwright (ASH_PLAYWRIGHT_MODULE can override its module path).\n' +
      'ASH_BROWSER_PROFILE and ASH_CAPTURE_DIR can override the private profile / report directory.');
    return;
  }
  const modulePath = process.env.ASH_PLAYWRIGHT_MODULE ||
    path.join(os.homedir(), '.local/share/apple-store-helper-browser/node_modules/playwright');
  const { chromium } = require(modulePath);
  const profile = process.env.ASH_BROWSER_PROFILE ||
    path.join(os.homedir(), '.local/share/apple-store-helper-browser/profile');
  const output = process.env.ASH_CAPTURE_DIR || path.resolve('.omx/purchase-capture');
  fs.mkdirSync(output, { recursive: true, mode: 0o700 });
  fs.mkdirSync(profile, { recursive: true, mode: 0o700 });
  const context = await chromium.launchPersistentContext(profile, {
    channel: 'chrome', headless: false, locale: 'zh-CN', viewport: { width: 1360, height: 900 },
  });
  const records = [], tasks = new Set();
  const report = path.join(output, `requests-${Date.now()}.ndjson`);
  const capture = response => {
    if (!observedURL(response.url())) return;
    const task = (async () => {
      const request = response.request();
      const h = await request.allHeaders();
      // Only these non-credential headers may be reused; Cookies remain in the live context.
      const headers = Object.fromEntries(['referer', 'x-aos-ui-fetch-call-1', 'x-skip-redirect']
        .filter(k => h[k] && (k !== 'referer' || !h[k].includes('?'))).map(k => [k, h[k]]));
      const record = { url: response.url(), method: request.method(), at: Date.now(),
        status: response.status(), contentType: response.headers()['content-type'] || '', headers };
      const index = records.push(record) - 1;
      const summary = { index, ...endpoint(record.url), method: record.method,
        at: record.at, status: record.status, contentType: record.contentType };
      fs.appendFileSync(report, JSON.stringify(summary) + '\n', { mode: 0o600 });
      console.log(JSON.stringify(summary));
    })().catch(() => console.error('Capture unavailable; no raw response or error URL logged'));
    tasks.add(task);
    task.finally(() => tasks.delete(task));
  };
  const attach = page => page.on('response', capture);
  context.pages().forEach(attach);
  context.on('page', attach);
  const rl = readline.createInterface({ input: process.stdin, output: process.stdout });
  try {
    const page = context.pages()[0] || await context.newPage();
    await page.goto('https://www.apple.com.cn/shop/bag', { waitUntil: 'domcontentloaded', timeout: 60000 });
    console.log('READY: operate this browser once; type requests, replay <index>, status, or exit.');
    for await (const line of rl) {
      const command = line.trim();
      if (command === 'exit') break;
      if (command === 'requests') {
        console.log(JSON.stringify(records.map((r, index) => ({ index, ...endpoint(r.url),
          method: r.method, status: r.status }))));
      } else if (command === 'status') {
        const cookies = await context.cookies('https://www.apple.com.cn/');
        console.log(JSON.stringify({ cookieNames: cookies.map(c => c.name), observations: records.length,
          note: 'Cookie presence does not prove login or checkout readiness' }));
      } else if (/^replay \d+$/.test(command)) {
        const index = Number(command.split(' ')[1]);
        if (!records[index]) { console.log('Unknown observation index'); continue; }
        // Consume before sending: transport failure or an ambiguous response must not trigger retries.
        const record = records[index];
        if (record.attempted) { console.log('Already attempted; not retrying'); continue; }
        record.attempted = true;
        try {
          const result = await replay(context.request, record);
          fs.appendFileSync(report, JSON.stringify({ replay: result }) + '\n', { mode: 0o600 });
          console.log(JSON.stringify(result));
        } catch (e) {
          // Network exceptions can contain token-bearing URLs. Never print those exceptions.
          const failure = failureSummary(e, record, index);
          fs.appendFileSync(report, JSON.stringify({ replay: failure }) + '\n', { mode: 0o600 });
          console.log(JSON.stringify(failure));
        }
      } else console.log('Commands: requests | replay <index> | status | exit');
    }
  } finally {
    rl.close();
    await Promise.allSettled([...tasks]);
    await context.close();
  }
}

module.exports = { storeURL, observedURL, endpoint, assertReplay, validateJSON, replay,
  failureSummary, ProbeError, MAX_AGE_MS };
if (require.main === module) main().catch(() => {
  console.error('Browser probe failed. Check Chrome, Playwright and whether the private profile is already open.');
  process.exitCode = 1;
});
