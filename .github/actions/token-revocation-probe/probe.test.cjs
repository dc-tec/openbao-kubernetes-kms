'use strict';
const { test } = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const os = require('node:os');
const path = require('node:path');
const { register, verify, checkRevocation, receipt } = require('./probe.cjs');

const token = 'synthetic-test-token';
const repo = 'dc-tec/openbao-kubernetes-kms';
function fixture(t) {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), 'kms-probe-test-'));
  t.after(() => fs.rmSync(root, { recursive: true, force: true }));
  const env = { RUNNER_TEMP: root, GITHUB_STATE: path.join(root, 'state'), GITHUB_OUTPUT: path.join(root, 'output') };
  return { root, env, directory: register(env) };
}
function api(replies) {
  return async (url, options) => {
    assert.equal(options.method, 'GET');
    assert.equal(options.redirect, 'error');
    assert.equal(options.headers.Authorization, `Bearer ${token}`);
    const expected = replies.shift();
    assert.ok(expected, 'unexpected API request');
    assert.equal(url, `https://api.github.com${expected[0]}`);
    return { status: expected[1], json: async () => expected[2] };
  };
}
function scopeResponse(overrides = {}) {
  return { total_count: 1, repositories: [{ full_name: repo, permissions: { pull: true, push: false, admin: false } }], ...overrides };
}
test('records private cleanup state without putting the token in action outputs', async t => {
  const { env, directory } = fixture(t);
  const replies = [
    ['/installation/repositories', 200, scopeResponse()],
    [`/repos/${repo}/contents/README.md`, 200, { type: 'file' }],
    [`/repos/${repo}/actions/secrets`, 403, { message: 'Resource not accessible by integration' }],
  ];
  const result = await verify(directory, token, api(replies));
  assert.equal(replies.length, 0);
  assert.equal(fs.statSync(directory).mode & 0o777, 0o700);
  assert.equal(fs.statSync(path.join(directory, 'token')).mode & 0o777, 0o600);
  assert.ok(!JSON.stringify(result).includes(token));
  for (const file of [env.GITHUB_OUTPUT, env.GITHUB_STATE]) assert.ok(!fs.readFileSync(file, 'utf8').includes(token));
});
for (const [label, body] of [
  ['extra repositories', scopeResponse({ total_count: 2 })],
  ['wrong repository', scopeResponse({ repositories: [{ full_name: 'other/repository' }] })],
  ['write access', scopeResponse({ repositories: [{ full_name: repo, permissions: { push: true } }] })],
]) {
  test(`rejects ${label}`, async t => {
    const { directory } = fixture(t);
    await assert.rejects(verify(directory, token, api([['/installation/repositories', 200, body]])));
  });
}
test('does not confuse rate limiting with a denied token permission', async t => {
  const { directory } = fixture(t);
  await assert.rejects(verify(directory, token, api([
    ['/installation/repositories', 200, scopeResponse()],
    [`/repos/${repo}/contents/README.md`, 200, { type: 'file' }],
    [`/repos/${repo}/actions/secrets`, 403, { message: 'API rate limit exceeded' }],
  ])));
});
for (const [label, status, message, success] of [
  ['revoked token', 401, 'Bad credentials', true],
  ['still valid token', 200, 'OK', false],
  ['API failure', 503, 'Unavailable', false],
]) {
  test(`checks ${label} and removes token material`, async t => {
    const { directory } = fixture(t);
    fs.writeFileSync(path.join(directory, 'token'), token, { mode: 0o600 });
    const result = checkRevocation(directory, api([['/installation/repositories', status, { message }]]));
    if (success) assert.equal((await result).postActionRevocation, 'verified');
    else await assert.rejects(result);
    assert.equal(fs.existsSync(directory), false);
  });
}
test('cleans token material after a network failure without logging the token', async t => {
  const { directory } = fixture(t);
  fs.writeFileSync(path.join(directory, 'token'), token, { mode: 0o600 });
  await assert.rejects(checkRevocation(directory, async () => { throw new Error('network failure'); }));
  assert.equal(fs.existsSync(directory), false);
});
test('sanitized receipt contains no credential', t => {
  const { root } = fixture(t);
  const summary = path.join(root, 'summary');
  receipt({ repository: repo, status: 401 }, { GITHUB_STEP_SUMMARY: summary });
  assert.ok(!fs.readFileSync(summary, 'utf8').includes(token));
});
