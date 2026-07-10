'use strict';
const test = require('node:test');
const assert = require('node:assert/strict');
const { randomUUID } = require('node:crypto');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');

const root = path.join(__dirname, '..', 'docs/bruno/collections/gO');
const source = fs.readFileSync(path.join(root, 'opencollection.yml'), 'utf8').replace(/\r\n/g, '\n');
const code = /request:\n  scripts:\n    - type: before-request\n      code: \|-\n([\s\S]*?)(?=\n\S|$)/.exec(source)[1]
  .split('\n').map(line => line.replace(/^        /, '')).join('\n');

function run(method, initial = {}) {
  const headers = {...initial};
  let generated = 0;
  vm.runInNewContext(code, {
    req: {
      getMethod: () => method,
      getHeader: name => headers[Object.keys(headers).find(key => key.toLowerCase() === name.toLowerCase())],
      setHeader: (name, value) => { headers[name] = value; },
    },
    bru: {interpolate: template => {assert.equal(template, '{{$guid}}'); generated++; return randomUUID();}},
  });
  return {headers, generated};
}

test('mutating requests without a manual key receive a fresh UUID on every send', () => {
  const keys = new Set();
  for (const method of ['POST', 'PUT', 'PATCH', 'DELETE', 'post']) {
    for (let attempt = 0; attempt < 2; attempt++) {
      const result = run(method);
      const key = result.headers['Idempotency-Key'];
      assert.match(key, /^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/);
      assert.equal(result.generated, 1);
      keys.add(key);
    }
  }
  assert.equal(keys.size, 10);
});

test('manual keys are preserved case-insensitively without generating another value', () => {
  for (const name of ['Idempotency-Key', 'idempotency-key', 'IDEMPOTENCY-KEY']) {
    for (const value of ['fixed-retry-key', '{{myManualKey}}', ' manual-value ']) {
      const result = run('POST', {[name]: value, Accept: 'application/json'});
      assert.deepEqual(result.headers, {[name]: value, Accept: 'application/json'});
      assert.equal(result.generated, 0);
    }
  }
});

test('null, empty and whitespace-only values are treated as missing', () => {
  for (const value of [undefined, null, '', '  \t ']) {
    assert.equal(run('POST', {'Idempotency-Key': value}).generated, 1);
  }
});

test('read-only methods are not given an automatic key', () => {
  for (const method of ['GET', 'HEAD', 'OPTIONS']) {
    assert.deepEqual(run(method), {headers: {}, generated: 0});
    assert.deepEqual(run(method, {'Idempotency-Key':'manual'}), {headers: {'Idempotency-Key':'manual'}, generated: 0});
  }
});
