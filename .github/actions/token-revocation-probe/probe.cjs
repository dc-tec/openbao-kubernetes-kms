'use strict';

const fs = require('node:fs');
const path = require('node:path');
const repository = 'dc-tec/openbao-kubernetes-kms';

function requireCondition(value) {
  if (!value) throw new Error('App token rehearsal assertion failed');
}

async function request(endpoint, token, fetcher = fetch) {
  const response = await fetcher(`https://api.github.com${endpoint}`, {
    method: 'GET',
    headers: {
      Authorization: `Bearer ${token}`,
      Accept: 'application/vnd.github+json',
      'X-GitHub-Api-Version': '2022-11-28',
    },
    redirect: 'error',
    signal: AbortSignal.timeout(10000),
  });
  return { status: response.status, body: await response.json() };
}

function register(environment = process.env) {
  requireCondition(environment.RUNNER_TEMP && environment.GITHUB_STATE && environment.GITHUB_OUTPUT);
  const directory = fs.mkdtempSync(path.join(environment.RUNNER_TEMP, 'kms-token-probe-'));
  fs.chmodSync(directory, 0o700);
  fs.appendFileSync(environment.GITHUB_STATE, `directory=${directory}\n`);
  fs.appendFileSync(environment.GITHUB_OUTPUT, `directory=${directory}\n`);
  return directory;
}

async function verify(directory, token, fetcher = fetch) {
  requireCondition(directory && token);
  // Keep the token off action outputs, logs, summaries and artifacts. The post
  // hook removes this private file even when an API assertion fails.
  fs.writeFileSync(path.join(directory, 'token'), token, { mode: 0o600, flag: 'wx' });
  const installation = await request('/installation/repositories', token, fetcher);
  requireCondition(installation.status === 200 && installation.body.total_count === 1);
  requireCondition(installation.body.repositories.length === 1);
  requireCondition(installation.body.repositories[0].full_name === repository);
  const permissions = installation.body.repositories[0].permissions;
  if (permissions) {
    requireCondition(!permissions.admin && !permissions.maintain && !permissions.push && !permissions.triage);
  }
  const contents = await request(`/repos/${repository}/contents/README.md`, token, fetcher);
  requireCondition(contents.status === 200 && contents.body.type === 'file');
  // A read-only request proves an unrelated permission is absent. Never try a
  // write to discover whether it would succeed.
  const secrets = await request(`/repos/${repository}/actions/secrets`, token, fetcher);
  requireCondition(secrets.status === 403 && secrets.body.message === 'Resource not accessible by integration');
  return { repository, contents: 'read', unrelatedSecretsRead: 'denied', repositories: 1 };
}

async function checkRevocation(directory, fetcher = fetch) {
  requireCondition(directory);
  try {
    const token = fs.readFileSync(path.join(directory, 'token'), 'utf8');
    const result = await request('/installation/repositories', token, fetcher);
    requireCondition(result.status === 401 && result.body.message === 'Bad credentials');
    return { repository, postActionRevocation: 'verified', status: 401 };
  } finally {
    fs.rmSync(directory, { recursive: true, force: true });
  }
}

function receipt(value, environment = process.env) {
  const text = JSON.stringify(value);
  console.log(text);
  if (environment.GITHUB_STEP_SUMMARY) fs.appendFileSync(environment.GITHUB_STEP_SUMMARY, `${text}\n`);
}

module.exports = { register, verify, checkRevocation, receipt };

if (require.main === module) {
  verify(process.env.PROBE_DIRECTORY, process.env.REHEARSAL_TOKEN)
    .then(receipt)
    .catch(() => { console.error('Read-only App token scope verification failed'); process.exitCode = 1; });
}
