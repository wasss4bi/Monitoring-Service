'use strict';
const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');
const { randomUUID } = require('node:crypto');
const collection = fs.readFileSync(path.join(__dirname, '..', 'docs/bruno/collections/gO/opencollection.yml'), 'utf8').replace(/\r\n/g, '\n');
const collectionScript = /      code: \|-\n([\s\S]*?)(?=\n\S|$)/.exec(collection)[1]
  .split('\n').map(line => line.replace(/^        /, '')).join('\n');

const file = path.join(__dirname, '..', 'docs', 'bruno', 'collections', 'gO', 'Monitors', 'Create monitor.yml');
const source = fs.readFileSync(file, 'utf8').replace(/\r\n/g, '\n');
const rawBody = /    data: \|-\n([\s\S]*?)\n  auth:/.exec(source)[1].split('\n').map(line => line.replace(/^      /, '')).join('\n');
const script = /    - type: before-request\n      code: \|-\n([\s\S]*?)(?=\n    - type:|\nsettings:|\ndocs:|$)/.exec(source)[1]
  .split('\n').map(line => line.replace(/^        /, '')).join('\n');
const labels = ['DNS', 'OZON', 'WB', 'AVITO', 'LAMODA', 'LEMANAPRO'];

test('Bruno no longer offers custom upstream headers', () => {
  assert.equal(fs.existsSync(path.join(path.dirname(file), 'Create monitor with request headers.yml')), false);
  for (const name of fs.readdirSync(path.dirname(file)).filter(name => name.endsWith('.yml'))) {
    const text = fs.readFileSync(path.join(path.dirname(file), name), 'utf8');
    assert.doesNotMatch(text, /"headers"\s*:/, name);
  }
});

function uncomment(selected) {
  let active = false;
  return rawBody.split('\n').map(line => {
    const marker = /^\/\/ (DNS|OZON|WB|AVITO|LAMODA|LEMANAPRO)$/.exec(line);
    if (marker) { active = selected.includes(marker[1]); return line; }
    return active ? line.replace(/^\/\/ ?/, '') : line;
  }).join('\n');
}
const fixture = { version: 1, userAgent: 'fixture-only', cookieJar: { cookies: [] } };
function run(body, variables = {}, initialHeaders = {}) {
  const headers = { ...initialHeaders }, reads = [];
  let sent;
  const context = {
    req: { getBody: options => { assert.equal(options.raw, true); return body; },
      getMethod: () => "POST",
      getHeader: name => headers[Object.keys(headers).find(key => key.toLowerCase() === name.toLowerCase())],
      setBody: value => { sent = value; }, setHeader: (name, value) => { headers[name] = value; } },
    bru: { getEnvVar: name => { reads.push(name); return variables[name]; },
      interpolate: value => { assert.equal(value, '{{$guid}}'); return randomUUID(); } },
  };
  vm.runInNewContext(collectionScript, context);
  vm.runInNewContext(script, context);
  return { body: JSON.parse(JSON.stringify(sent)), headers, reads };
}

test('all six bodies are disabled by default and sending fails before HTTP', () => {
  assert.equal(rawBody.split('\n').filter(line => line.trim() && !line.trimStart().startsWith('//')).length, 0);
  for (const label of labels) assert.equal(rawBody.split('\n').filter(line => line === '// ' + label).length, 1);
  assert.throws(() => run(rawBody), /Uncomment exactly one/);
});

test('each marketplace produces a valid body and gets a fresh automatic idempotency key', () => {
  const keys = new Set();
  const names = { DNS: 'dnsSession', OZON: 'ozonSession', WB: 'wildberriesSession', AVITO: 'avitoSession' };
  for (const label of labels) {
    const values = { idempotencyKey: 'fixture-batch' };
    if (names[label]) values[names[label]] = JSON.stringify(fixture, null, 2);
    const result = run(uncomment([label]), values);
    assert.ok(result.body.url.startsWith('https://'));
    assert.ok(result.body.rules.length > 0);
    assert.ok(result.body.page_url.startsWith('https://'));
    assert.ok(result.body.rules.every(rule => typeof rule.description === 'string' && rule.description.trim().length > 0));
    if (['DNS','WB'].includes(label)) assert.equal(result.body.browser_page_url, result.body.page_url);
    else assert.equal(result.body.browser_page_url, undefined);
    keys.add(result.headers['Idempotency-Key']);
    assert.notEqual(result.headers['Idempotency-Key'], run(uncomment([label]), values).headers['Idempotency-Key']);
    assert.equal(result.body.session_mode, 'managed');
    assert.equal(result.body.session, undefined);
    assert.deepEqual(result.reads, []);
  }
  assert.equal(keys.size, 6);
});

test('session variables accept an object and reject missing or malformed exports without disclosing values', () => {
  const supplied = '{"url":"https://example.com/","session":{{dnsSession}}}';
  assert.deepEqual(run(supplied, { dnsSession: fixture, idempotencyKey: 'batch' }).body.session, fixture);
  assert.throws(() => run(supplied, { idempotencyKey: 'batch' }), /Fill dnsSession/);
  assert.throws(() => run(supplied, { dnsSession: 'sensitive-fixture' }), error =>
    !error.message.includes('sensitive-fixture') && error.message.includes('v1 JSON'));
  assert.throws(() => run(supplied, { dnsSession: '{}' }), /version=1/);
});

test('two active bodies and malformed JSON never become a request; no environment key is needed', () => {
  assert.throws(() => run(uncomment(['LAMODA', 'LEMANAPRO']), { idempotencyKey: 'batch' }), /exactly one complete/);
  assert.throws(() => run('{ invalid JSON }'), /exactly one complete/);
  assert.ok(run(uncomment(['LAMODA'])).headers['Idempotency-Key']);
});

test('manual idempotency header survives the monitor body script unchanged', () => {
  const result = run(uncomment(['WB']), {}, {'idempotency-key':'manual-retry-key'});
  assert.equal(result.headers['idempotency-key'], 'manual-retry-key');
  assert.equal(result.headers['Idempotency-Key'], undefined);
});

test('price units and existing endpoint source settings are preserved', () => {
  const values = { wildberriesSession: fixture, dnsSession: fixture, idempotencyKey: 'batch' };
  assert.equal(run(uncomment(['WB']), values).body.rules[0].value, 7033600);
  assert.equal(run(uncomment(['DNS']), values).body.rules[0].value, 5799);
  const lamoda = run(uncomment(['LAMODA']), values).body;
  assert.equal(lamoda.json_source.object_key, 'product');
  assert.equal(lamoda.rules[1].path, 'sizes[9].stock_quantity');
  assert.ok(run(uncomment(['LEMANAPRO']), values).body.rules[0].path.endsWith('/@value'));
});

test('Login stores the service token only after a successful response', () => {
  const login = fs.readFileSync(path.join(__dirname, '..', 'docs', 'bruno', 'collections', 'gO', 'Authentication', 'Login.yml'), 'utf8').replace(/\r\n/g, '\n');
  const after = /    - type: after-response\n      code: \|-\n([\s\S]*?)(?=\nsettings:|\ndocs:|$)/.exec(login)[1]
    .split('\n').map(line => line.replace(/^        /, '')).join('\n');
  const updates = [];
  const bru = { setEnvVar: (...args) => updates.push(args) };
  vm.runInNewContext(after, { bru, res: { status: 200, getBody: () => ({ access_token: 'fixture-only-token' }) } });
  vm.runInNewContext(after, { bru, res: { status: 401, getBody: () => ({ error: 'unauthorized' }) } });
  assert.deepEqual(updates, [['accessToken', 'fixture-only-token']]);
});

test('monitor creation response updates monitorId without treating an error as success', () => {
  const after = /    - type: after-response\n      code: \|-\n([\s\S]*?)(?=\nsettings:|\ndocs:|$)/.exec(source)[1]
    .split('\n').map(line => line.replace(/^        /, '')).join('\n');
  const updates = [];
  const bru = { setEnvVar: (...args) => updates.push(args) };
  vm.runInNewContext(after, { bru, res: { status: 201, getBody: () => ({ id: 42 }) } });
  vm.runInNewContext(after, { bru, res: { status: 400, getBody: () => ({ error: 'bad request' }) } });
  assert.deepEqual(updates, [['monitorId', '42']]);
});
