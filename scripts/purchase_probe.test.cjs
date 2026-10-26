const { test } = require('node:test');
const assert = require('node:assert/strict');
const { storeURL, observedURL, endpoint, assertReplay, validateJSON, replay,
  failureSummary, MAX_AGE_MS } = require('./purchase_probe.cjs');

const now = 1000000;
const summaryURL = 'https://www.apple.com.cn/shop/updateSummary?product=MK2Q4CH%2FA&igt=private-token';
const record = (changes = {}) => ({ url: summaryURL, method: 'GET', at: now,
  status: 200, contentType: 'application/json;charset=utf-8', headers: {}, ...changes });
const body = JSON.stringify({ head: { status: 200 }, body: { response: {
  summarySection: { summary: { isBuyable: true } },
} } });

test('only exact Apple China HTTPS store hosts are accepted', () => {
  for (const host of ['www.apple.com.cn', 'secure.www.apple.com.cn', 'secure11.www.apple.com.cn']) {
    assert.equal(storeURL(`https://${host}/shop/bag`).hostname, host);
  }
  for (const url of ['http://www.apple.com.cn/shop/bag', 'https://www.apple.com.cn.evil.test/shop/bag',
    'https://apple.com.cn/shop/bag', 'https://user:secret@www.apple.com.cn/shop/bag',
    'https://www.apple.com.cn:8080/shop/bag', 'file:///shop/bag']) assert.throws(() => storeURL(url));
});

test('authentication, payment and checkout responses are not logged', () => {
  for (const suffix of ['/shop/signIn?password=secret', '/auth/verify', '/shop/checkout', '/shop/order/123']) {
    assert.equal(observedURL('https://secure11.www.apple.com.cn' + suffix), false);
  }
  assert.equal(observedURL(summaryURL), true);
  assert.equal(observedURL('https://www.apple.com.cn/shop/buy-iphone/iphone-duo?atbtoken=secret'), true);
});

test('endpoint summaries contain parameter names but never values', () => {
  const report = JSON.stringify(endpoint(summaryURL));
  assert.ok(report.includes('igt'));
  assert.ok(!report.includes('private-token'));
  assert.ok(!report.includes('MK2Q4CH'));
});

test('fresh observed read-only JSON requests are allowed', () => {
  assert.doesNotThrow(() => assertReplay(record(), now));
});

test('GET alone does not mean safe: add-to-bag and checkout are denied', () => {
  for (const url of ['https://www.apple.com.cn/shop/buy-iphone/iphone-duo?add-to-cart=add-to-cart',
    'https://secure11.www.apple.com.cn/shop/checkout',
    'https://www.apple.com.cn/shop/updateSummary?add-to-cart=1']) {
    assert.throws(() => assertReplay(record({ url }), now), /allowlisted/);
  }
  assert.throws(() => assertReplay(record({ method: 'POST' }), now), /allowlisted/);
});

test('failed, expired, future and non-JSON observations cannot be retried', () => {
  for (const status of [301, 403, 429, 541]) assert.throws(() => assertReplay(record({ status }), now));
  for (const at of [now - MAX_AGE_MS - 1, now + 1, NaN]) {
    assert.throws(() => assertReplay(record({ at }), now), /expired/);
  }
  assert.throws(() => assertReplay(record({ contentType: 'text/html' }), now), /JSON/);
});

test('HTTP 200, login HTML and application errors are not false success', () => {
  const api = '/shop/updateSummary';
  assert.throws(() => validateJSON(541, 'text/html', '<html>Page Not Found</html>', api), /541/);
  assert.throws(() => validateJSON(302, 'text/html', '', api), /302/);
  assert.throws(() => validateJSON(200, 'text/html', '<html>Login</html>', api), /JSON/);
  assert.throws(() => validateJSON(200, 'application/json', 'broken', api), /Invalid/);
  assert.throws(() => validateJSON(200, 'application/json', 'null', api), /envelope/);
  assert.throws(() => validateJSON(200, 'application/json', '{}', api), /summary/);
  assert.throws(() => validateJSON(200, 'application/json', '{"head":{"status":541}}', api), /application/);
  assert.throws(() => validateJSON(200, 'application/json', '{"body":{"errorMessage":"secret"}}', api), /application/);
  assert.ok(validateJSON(200, 'application/json', body, api));
});

test('pickup must contain one of the two known store envelopes', () => {
  const api = '/shop/retail/pickup-message';
  assert.throws(() => validateJSON(200, 'application/json', '{}', api), /stores/);
  for (const value of [{ body: { stores: [] } }, { body: { content: { pickupMessage: { stores: [] } } } }]) {
    assert.ok(validateJSON(200, 'application/json', JSON.stringify(value), api));
  }
});

test('bag status uses its observed items/ttl/api schema, not an invented body envelope', () => {
  const api = '/shop/bag/status';
  assert.ok(validateJSON(200, 'application/json', '{"items":0,"ttl":60,"api":{}}', api));
  for (const value of [{ body: {} }, { items: -1, ttl: 60, api: {} },
    { items: '0', ttl: 60, api: {} }, { items: 0, ttl: -1, api: {} },
    { items: 0, ttl: 60, api: [] }]) {
    assert.throws(() => validateJSON(200, 'application/json', JSON.stringify(value), api), /status fields/);
  }
});

test('replay uses the supplied live context with no redirects or retries', async () => {
  let calls = 0, disposed = false;
  const context = { get: async (url, options) => {
    calls++;
    assert.equal(url, summaryURL);
    assert.equal(options.maxRedirects, 0);
    assert.equal(options.maxRetries, 0);
    assert.ok(!('cookie' in options.headers));
    return { status: () => 200, headers: () => ({ 'content-type': 'application/json' }),
      text: async () => body, dispose: async () => { disposed = true; } };
  } };
  const result = await replay(context, record(), now);
  assert.equal(calls, 1);
  assert.equal(disposed, true);
  assert.equal(result.transport, 'http-request');
  assert.ok(!JSON.stringify(result).includes('private-token'));
});

test('a 541 stops after exactly one request, disposes the response and never retries', async () => {
  let calls = 0, disposed = false;
  const context = { get: async () => {
    calls++;
    return { status: () => 541, headers: () => ({ 'content-type': 'text/html' }),
      text: async () => 'Page Not Found', dispose: async () => { disposed = true; } };
  } };
  await assert.rejects(replay(context, record(), now), /541/);
  assert.equal(calls, 1);
  assert.equal(disposed, true);
});

test('unsafe request preconditions fail before any network access', async () => {
  const context = { get: () => { assert.fail('must not send'); } };
  await assert.rejects(replay(context, record({ method: 'POST' }), now));
  await assert.rejects(replay(context, record({ status: 541 }), now));
});

test('failure diagnostics distinguish 541 and stale observations without exposing URLs or tokens', async () => {
  const context = { get: async () => ({ status: () => 541,
    headers: () => ({ 'content-type': 'text/html' }), text: async () => 'private-token',
    dispose: async () => {} }) };
  let httpError, preflightError;
  try { await replay(context, record(), now); } catch (e) { httpError = e; }
  try { await replay(context, record({ at: 0 }), now); } catch (e) { preflightError = e; }
  const http = failureSummary(httpError, record(), 1);
  const preflight = failureSummary(preflightError, record(), 2);
  assert.equal(http.kind, 'http');
  assert.equal(http.status, 541);
  assert.equal(preflight.kind, 'precondition');
  assert.equal(http.retry, false);
  for (const value of [http, preflight, failureSummary(new Error(summaryURL), record(), 3)]) {
    assert.ok(!JSON.stringify(value).includes('private-token'));
    assert.ok(!JSON.stringify(value).includes('https://'));
  }
});
