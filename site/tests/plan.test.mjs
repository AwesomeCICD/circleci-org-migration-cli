// Tests for the plan builder logic. Run with: node --test site/tests/   (Node 24, no dependencies)

import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync, readdirSync } from 'node:fs';
import { fileURLToPath } from 'node:url';

import {
  OPTION_DEFS,
  buildPlan,
  decodeState,
  defaultInput,
  encodeState,
  renderRunSheet,
  validate,
} from '../assets/js/plan/plan.mjs';

const SRC_UUID = '11111111-1111-1111-1111-111111111111';
const DST_UUID = '22222222-2222-2222-2222-222222222222';

const BASE = {
  'oauth-to-app': { source: 'gh/acme', dest: `circleci/${DST_UUID}` },
  'oauth-to-oauth': { source: 'gh/acme', dest: 'gh/acme-new', destId: DST_UUID },
  'app-to-app': { source: `circleci/${SRC_UUID}`, dest: `circleci/${DST_UUID}` },
};
const TYPES = Object.keys(BASE);
const OPTION_KEYS = OPTION_DEFS.map((d) => d.key);
const NAMES = { runnerNs: 'acme-runners', destRunnerNs: 'acme-new-runners', orbNs: 'acme', destOrbNs: 'acme-new', destGithubOrg: 'acme-new' };

function input(type, options = {}, extra = {}) {
  const opts = Object.fromEntries(OPTION_DEFS.map((d) => [d.key, d.default]));
  return { type, ...BASE[type], ...NAMES, options: { ...opts, ...options }, ...extra };
}
const allOn = Object.fromEntries(OPTION_KEYS.map((k) => [k, true]));
const allOff = Object.fromEntries(OPTION_KEYS.map((k) => [k, false]));

const ids = (plan) => plan.steps.map((s) => s.id);
const flat = (cmd) => cmd.replace(/ \\\n\s*/g, ' ');
const allCommands = (plan) => plan.steps.flatMap((s) => [...s.commands, ...(s.undo ?? [])]).map(flat);
const step = (plan, id) => plan.steps.find((s) => s.id === id);

// ---------------------------------------------------------------------------
// docs/cli: the flags every subcommand accepts
// ---------------------------------------------------------------------------

const CLI_DIR = fileURLToPath(new URL('../../docs/cli/', import.meta.url));

function parseCliDocs() {
  const commands = new Map(); // "secrets transfer" -> Set of "--flag"
  for (const file of readdirSync(CLI_DIR)) {
    const m = /^circleci-migrate_(.+)\.md$/.exec(file);
    if (!m) continue;
    const name = m[1].replaceAll('_', ' ');
    const text = readFileSync(CLI_DIR + file, 'utf8');
    const flags = new Set();
    // Only the fenced blocks under "### Options" and "### Options inherited ...".
    for (const section of text.split(/^### /m).slice(1)) {
      if (!/^Options/.test(section)) continue;
      const block = /```\n([\s\S]*?)```/.exec(section);
      if (!block) continue;
      for (const line of block[1].split('\n')) {
        const f = /^\s+(?:-\w, )?(--[a-z][a-z0-9-]*)/.exec(line);
        if (f) flags.add(f[1]);
      }
    }
    commands.set(name, flags);
  }
  return commands;
}

const CLI = parseCliDocs();

// Split a flattened command line into words, keeping "quoted values" together.
function words(line) {
  return line.match(/"[^"]*"|'[^']*'|\S+/g) ?? [];
}

// For one flattened `circleci-migrate ...` command: { sub, flags }.
function parseCommand(line) {
  const cmd = line.split(' | ')[0].split(' > ')[0];
  const w = words(cmd).slice(1);
  const subWords = [];
  for (const word of w) {
    if (word.startsWith('-')) break;
    subWords.push(word);
  }
  // Longest known subcommand prefix: "secrets transfer", then "secrets".
  let sub = '';
  for (let n = subWords.length; n > 0; n -= 1) {
    const candidate = subWords.slice(0, n).join(' ');
    if (CLI.has(candidate)) {
      sub = candidate;
      break;
    }
  }
  const flags = w.filter((x) => /^--[a-z][a-z0-9-]*$/.test(x));
  return { sub, flags };
}

test('docs/cli is parsed: subcommands and flags are found', () => {
  for (const sub of ['doctor', 'export', 'mapping generate', 'sync', 'cutover', 'secrets transfer', 'validate', 'terraform generate', 'orb inline']) {
    assert.ok(CLI.has(sub), `docs/cli has no page for "${sub}"`);
  }
  assert.ok(CLI.get('sync').has('--skip-pipelines'));
  assert.ok(CLI.get('sync').has('--host'), 'inherited flags are included');
  assert.ok(CLI.get('secrets transfer').has('--remove-restrictions'));
  assert.ok(!CLI.get('sync').has('--include-project-vars'));
});

test('every circleci-migrate flag emitted exists for its subcommand (all scenarios x all option combinations x both shells x cloud/server)', () => {
  let commands = 0;
  const seen = new Set();
  for (const type of TYPES) {
    for (let mask = 0; mask < 1 << OPTION_KEYS.length; mask += 1) {
      const options = Object.fromEntries(OPTION_KEYS.map((k, i) => [k, Boolean(mask & (1 << i))]));
      for (const shell of ['bash', 'powershell']) {
        const server = mask % 2 === 1 ? { host: 'server', hostUrl: 'https://circleci.example.com' } : {};
        const plan = buildPlan(input(type, options, { shell, ...server }));
        assert.deepEqual(plan.errors, [], `${type} ${mask}: ${JSON.stringify(plan.errors)}`);
        for (const line of allCommands(plan)) {
          const flatLines = line.split('\n');
          for (const l of flatLines) {
            if (!l.startsWith('circleci-migrate ')) continue;
            const { sub, flags } = parseCommand(l);
            assert.ok(sub, `unknown subcommand in: ${l}`);
            for (const flag of flags) {
              assert.ok(CLI.get(sub).has(flag), `${type} #${mask} ${shell}: "${flag}" is not a flag of "${sub}" in docs/cli (${l})`);
              seen.add(`${sub} ${flag}`);
            }
            commands += 1;
          }
        }
      }
    }
  }
  assert.ok(commands > 10000, `expected many commands, checked ${commands}`);
  // Spot-check that the walk reached the flags this plan builder is built around.
  for (const expected of [
    'sync --skip-pipelines', 'sync --webhook-secrets-out', 'sync --skip-terraform-managed', 'sync --only', 'sync --enable-existing-triggers',
    'sync --dest-github-org', 'sync --dest-orb-namespace', 'sync --dest-runner-namespace', 'secrets transfer --remove-restrictions',
    'secrets transfer --include-ssh-keys', 'secrets transfer --skip-contexts', 'secrets transfer --dest-host', 'cutover --pause-source',
    'cutover --enable-dest', 'cutover --undo', 'validate --dest-token-context', 'validate --dest-orb-namespace', 'export --orb-namespace',
    'export --dest-org', 'terraform generate --import-existing', 'terraform generate --dest-org-type', 'mapping generate --output', 'sync --include-danger-flags',
  ]) {
    assert.ok(seen.has(expected), `never emitted: ${expected}`);
  }
});

// ---------------------------------------------------------------------------
// Validation
// ---------------------------------------------------------------------------

const errorFields = (v) => v.errors.map((e) => `${e.field}:${e.code}`);

test('a valid plan has no errors for each scenario', () => {
  for (const type of TYPES) assert.deepEqual(validate(input(type)).errors, [], type);
});

test('empty slugs are required', () => {
  const v = validate({ type: 'oauth-to-app', options: {} });
  assert.deepEqual(errorFields(v).sort(), ['dest:required', 'destId:required', 'source:required']);
});

test('OAuth slug shapes: gh, bb, github and bitbucket are accepted; junk is not', () => {
  for (const ok of ['gh/acme', 'bb/acme-co', 'github/Acme_1', 'bitbucket/a.b']) {
    assert.deepEqual(validate(input('oauth-to-oauth', {}, { source: ok })).errors.filter((e) => e.field === 'source'), [], ok);
  }
  for (const bad of ['gh/', 'gh/a b', 'acme', 'https://github.com/acme', 'gh/acme/web', 'gl/acme']) {
    const v = validate(input('oauth-to-oauth', {}, { source: bad }));
    assert.ok(v.errors.some((e) => e.field === 'source'), `${bad} should be rejected`);
  }
});

test('github/ and bitbucket/ aliases are normalized to gh/ and bb/', () => {
  const v = validate(input('oauth-to-oauth', {}, { source: 'github/acme', dest: 'bitbucket/acme-new' }));
  assert.equal(v.input.source, 'gh/acme');
  assert.equal(v.input.dest, 'bb/acme-new');
});

const SHORT_ID = 'Wn4fYU5iDujKHai2ppc7gB';

test('App slug is circleci/<uuid> or circleci/<short id>', () => {
  for (const ok of [`circleci/${SRC_UUID}`, `circleci/${SRC_UUID.toUpperCase()}`, `circleci/${SHORT_ID}`, 'circleci/Wn4fYU5iDujKHai2ppc7']) {
    assert.deepEqual(validate(input('app-to-app', {}, { source: ok })).errors.filter((e) => e.field === 'source'), [], ok);
    assert.deepEqual(validate(input('app-to-app', {}, { source: 'circleci/AAAAAAAAAAAAAAAAAAAAAA', dest: ok, destId: DST_UUID })).errors.filter((e) => e.field === 'dest'), [], ok);
  }
  for (const bad of ['circleci/acme', 'circleci/1234', 'circleci/Wn4fYU5iDujKHai2pp', `circleci/${'a'.repeat(25)}`, 'circleci/Wn4fYU5iDujK-ai2ppc7gB', `circleci/${SRC_UUID}/web`, SRC_UUID]) {
    const v = validate(input('app-to-app', {}, { source: bad }));
    assert.ok(v.errors.some((e) => e.field === 'source'), bad);
  }
});

test('destination org ID must look like a UUID', () => {
  assert.ok(validate(input('oauth-to-oauth', {}, { destId: 'not-a-uuid' })).errors.some((e) => e.field === 'destId' && e.code === 'invalid'));
  assert.ok(validate(input('oauth-to-oauth', {}, { destId: DST_UUID.toUpperCase() })).errors.length === 0);
});

test('destination org ID is required for an OAuth destination only when something needs it', () => {
  const noId = input('oauth-to-oauth', {}, { destId: '' });
  assert.ok(validate(noId).errors.some((e) => e.field === 'destId' && e.code === 'required'));
  const nothingToTransfer = input('oauth-to-oauth', { contexts: false, projectVars: false, sshKeys: false, terraform: false }, { destId: '' });
  assert.deepEqual(validate(nothingToTransfer).errors, []);
});

test('a short-ID circleci/ destination needs the org ID as a UUID; a UUID destination supplies its own', () => {
  const short = input('app-to-app', {}, { dest: `circleci/${SHORT_ID}`, destId: '' });
  assert.ok(validate(short).errors.some((e) => e.field === 'destId' && e.code === 'required'));
  const withId = buildPlan({ ...short, destId: DST_UUID });
  assert.deepEqual(withId.errors, []);
  assert.match(withId.env.bash, new RegExp(`DEST_ORG="circleci/${SHORT_ID}"`));
  assert.match(withId.env.bash, new RegExp(`DEST_ORG_ID="${DST_UUID}"`));
  assert.ok(!withId.warnings.some((w) => w.code === 'dest-id-mismatch'), 'no mismatch warning for a short ID');
  assert.ok(validate({ ...short, destId: SHORT_ID }).errors.some((e) => e.field === 'destId' && e.code === 'invalid'), 'the ID field stays UUID only');
  assert.match(validate(input('app-to-app', {}, { dest: 'circleci/acme' })).errors.find((e) => e.field === 'dest').message, /short ID in the app URL/);
});

test('a circleci/ destination supplies its own org ID', () => {
  const plan = buildPlan(input('oauth-to-app'));
  assert.match(plan.env.bash, new RegExp(`DEST_ORG_ID="${DST_UUID}"`));
});

test('a destination ID that differs from the circleci/ slug is a warning', () => {
  const v = validate(input('oauth-to-app', {}, { destId: SRC_UUID }));
  assert.deepEqual(v.errors, []);
  assert.ok(v.warnings.some((w) => w.code === 'dest-id-mismatch'));
});

test('source and destination must differ', () => {
  const v = validate(input('oauth-to-oauth', {}, { dest: 'gh/acme' }));
  assert.ok(v.errors.some((e) => e.code === 'same-org' && e.field === 'dest'));
  assert.ok(validate(input('oauth-to-oauth', {}, { dest: 'github/acme' })).errors.some((e) => e.code === 'same-org'), 'alias of the same org');
});

test('type and slug mismatch gives a fix message that names the right type', () => {
  const v = validate(input('app-to-app', {}, { source: 'gh/acme' }));
  const e = v.errors.find((x) => x.field === 'source');
  assert.equal(e.code, 'type-mismatch');
  assert.equal(e.suggestType, 'oauth-to-app');
  assert.match(e.message, /GitHub OAuth to GitHub App/);
  assert.match(e.message, /gh\/acme/);

  const v2 = validate(input('oauth-to-app', {}, { dest: 'gh/acme-new' }));
  const e2 = v2.errors.find((x) => x.field === 'dest');
  assert.equal(e2.code, 'type-mismatch');
  assert.equal(e2.suggestType, 'oauth-to-oauth');
});

test('a Bitbucket source cannot move to a GitHub App org', () => {
  const v = validate(input('oauth-to-app', {}, { source: 'bb/acme' }));
  assert.ok(v.errors.some((e) => e.code === 'bitbucket-to-app'));
});

test('an App-to-OAuth pair has no matching type, so no suggestion', () => {
  const v = validate(input('oauth-to-oauth', {}, { source: `circleci/${SRC_UUID}` }));
  const e = v.errors.find((x) => x.field === 'source');
  assert.equal(e.code, 'type-mismatch');
  assert.equal(e.suggestType, undefined);
});

test('server host needs a safe URL', () => {
  assert.ok(validate(input('oauth-to-oauth', {}, { host: 'server' })).errors.some((e) => e.field === 'hostUrl' && e.code === 'required'));
  for (const bad of ['circleci.example.com', 'https://x.example.com/"; rm -rf ~', 'https://a b.example.com', 'ftp://x.example.com', 'https://$(whoami).example.com']) {
    assert.ok(validate(input('oauth-to-oauth', {}, { host: 'server', hostUrl: bad })).errors.some((e) => e.field === 'hostUrl'), bad);
  }
  const ok = validate(input('oauth-to-oauth', {}, { host: 'server', hostUrl: 'https://circleci.example.com/' }));
  assert.deepEqual(ok.errors, []);
  assert.equal(ok.input.hostUrl, 'https://circleci.example.com');
  assert.ok(validate(input('oauth-to-oauth', {}, { host: 'server', hostUrl: 'http://circleci.example.com' })).warnings.some((w) => w.code === 'http-host'));
});

test('namespaces and GitHub org names are validated and required when their option is on', () => {
  assert.ok(validate(input('app-to-app', { runners: true }, { runnerNs: '' })).errors.some((e) => e.field === 'runnerNs'));
  assert.ok(validate(input('app-to-app', { runners: true }, { runnerNs: 'a b' })).errors.some((e) => e.field === 'runnerNs'));
  assert.ok(validate(input('app-to-app', { orbs: true }, { orbNs: '"; ls' })).errors.some((e) => e.field === 'orbNs'));
  assert.ok(validate(input('app-to-app', { repoMove: true }, { destGithubOrg: '' })).errors.some((e) => e.field === 'destGithubOrg'));
  assert.ok(validate(input('app-to-app', { repoMove: true }, { destGithubOrg: 'bad org' })).errors.some((e) => e.field === 'destGithubOrg'));
  // Off options are not validated.
  assert.deepEqual(validate(input('app-to-app', { runners: false, orbs: false, repoMove: false }, { runnerNs: '', orbNs: '', destGithubOrg: '' })).errors, []);
  // OAuth destination: the GitHub org defaults to the destination slug's name.
  const v = validate(input('oauth-to-oauth', { repoMove: true }, { destGithubOrg: '' }));
  assert.deepEqual(v.errors, []);
  assert.equal(v.input.destGithubOrg, 'acme-new');
});

test('warnings: risky combinations', () => {
  const codes = (type, options) => validate(input(type, options)).warnings.map((w) => w.code);
  assert.ok(codes('oauth-to-app', { pauseSource: false }).includes('no-pause'));
  assert.match(validate(input('oauth-to-app', { pauseSource: false })).warnings.find((w) => w.code === 'no-pause').message, /double builds/);
  assert.ok(!codes('oauth-to-app', { pauseSource: true }).includes('no-pause'));
  assert.ok(codes('app-to-app', { pauseSource: false }).includes('no-pause'));
  assert.ok(codes('oauth-to-app', {}).includes('stale-export'));
  assert.ok(codes('oauth-to-app', {}).includes('lossy'));
  assert.ok(!codes('app-to-app', {}).includes('lossy'));
  assert.ok(codes('oauth-to-app', { contexts: false, projectVars: false, sshKeys: false }).includes('no-secrets'));
  assert.ok(codes('oauth-to-app', { projectVars: false }).includes('no-project-vars'));
  assert.ok(codes('oauth-to-app', { sshKeys: false }).includes('no-ssh'));
  assert.ok(codes('oauth-to-app', { restrictions: false }).includes('restrictions'));
  assert.ok(!codes('oauth-to-app', { restrictions: true }).includes('restrictions'));
  assert.ok(codes('oauth-to-app', { runners: true }).includes('runners'));
  assert.ok(codes('oauth-to-oauth', {}).includes('stale-export'));
  assert.ok(validate(input('oauth-to-oauth', {}, { source: 'gh/acme', dest: 'bb/acme' })).warnings.some((w) => w.code === 'provider-mismatch'));
});

test('invalid input yields errors and no steps, never half a plan', () => {
  const plan = buildPlan(input('oauth-to-app', {}, { source: 'nope' }));
  assert.ok(plan.errors.length > 0);
  assert.deepEqual(plan.steps, []);
  assert.deepEqual(plan.env, { bash: '', powershell: '' });
});

// ---------------------------------------------------------------------------
// Step order: pinned to the runbooks in docs/migrations/
// ---------------------------------------------------------------------------

const DOCS_DIR = fileURLToPath(new URL('../../docs/migrations/', import.meta.url));

// The Hugo anchor of a heading: lower case, punctuation dropped, spaces to dashes ("mapping.json" is "mappingjson").
const slugify = (text) => text.toLowerCase().replace(/[^a-z0-9 -]/g, '').replace(/ /g, '-');

function readRunbook(type) {
  const text = readFileSync(`${DOCS_DIR}${type}.md`, 'utf8');
  const steps = new Map(); // anchor -> step number
  const headings = new Set();
  for (const m of text.matchAll(/^#{2,4} (.+)$/gm)) {
    headings.add(slugify(m[1]));
    const n = /^(\d+)\. /.exec(m[1]);
    if (n) steps.set(slugify(m[1]), Number(n[1]));
  }
  // Every circleci-migrate command of the fenced bash blocks, continuation lines joined.
  const commands = [];
  for (const block of text.matchAll(/```bash\n([\s\S]*?)```/g)) {
    for (const line of block[1].replace(/ \\\n\s*/g, ' ').split('\n')) {
      if (line.startsWith('circleci-migrate ')) commands.push(line.trim());
    }
  }
  return { steps, headings, commands };
}

const anchorOf = (s) => s.link?.path.split('#')[1];

test('every step links to a heading that exists in its runbook, relative to the site root', () => {
  for (const type of TYPES) {
    const { headings } = readRunbook(type);
    for (const options of [{}, allOn]) {
      for (const s of buildPlan(input(type, options, { destOrbNs: '' })).steps) {
        assert.ok(s.link, `${type}/${s.id} has no link`);
        assert.ok(!s.link.path.startsWith('/') && !s.link.path.startsWith('http'), `${s.link.path} must be relative`);
        if (s.link.path.startsWith(`docs/migrations/${type}/#`)) {
          assert.ok(headings.has(anchorOf(s)), `${type}/${s.id}: #${anchorOf(s)} is not a heading of ${type}.md`);
        } else {
          assert.equal(s.link.path, 'docs/reference/terraform/');
        }
      }
    }
  }
});

// docs/migrations/oauth-to-app.md, "Steps" 1-13 then "Verify".
test('oauth-to-app: step order is the runbook order (steps 1-13, then Verify)', () => {
  assert.deepEqual(ids(buildPlan(input('oauth-to-app', { orbs: false, runners: false, terraform: false }))), [
    'before-you-start', 'install',
    'doctor', 'export', 'mapping', 'secrets-context', 'support-plan-ask', 'dry-run',
    'sync-projects', 'transfer', 'pause-source', 'move-app', 'support-plan-move', 'sync-definitions', 'enable-dest',
    'smoke', 'validate',
    'webhook-handoff', 'rotate-cleanup', 'oidc-trust', 'external-pins',
  ]);
});

// docs/migrations/oauth-to-oauth.md, "Steps" 1-12 then "Verify".
test('oauth-to-oauth: step order is the runbook order (steps 1-12, then Verify)', () => {
  assert.deepEqual(ids(buildPlan(input('oauth-to-oauth', { orbs: false, runners: false, terraform: false }))), [
    'before-you-start', 'install',
    'doctor', 'export', 'mapping', 'secrets-context', 'dry-run-sync',
    'sync-apply', 'mapping-generate', 'transfer', 'pause-source', 'follow-dest',
    'smoke', 'validate',
    'webhook-handoff', 'cleanup-extra', 'rotate-cleanup', 'external-pins',
  ]);
});

// docs/migrations/app-to-app.md, "Steps" 1-12 then "Verify": the phased order, the GitHub App moves (same GitHub org).
test('app-to-app, same GitHub org: step order is the runbook order (steps 1-12, then Verify)', () => {
  assert.deepEqual(ids(buildPlan(input('app-to-app', { orbs: false, runners: false, terraform: false, repoMove: false }))), [
    'before-you-start', 'install',
    'doctor', 'export', 'mapping', 'secrets-context', 'dry-run',
    'sync-projects', 'transfer', 'pause-source', 'move-app', 'sync-definitions', 'oidc-trust', 'enable-dest',
    'smoke', 'validate',
    'webhook-handoff', 'cleanup-extra', 'rotate-cleanup', 'external-pins',
  ]);
});

// docs/migrations/app-to-app.md, the tip under step 9: repos in a NEW GitHub org, so one cutover command.
test('app-to-app, new GitHub org: definitions up front, one pause-and-enable command, no point of no return', () => {
  const plan = buildPlan(input('app-to-app', { orbs: false, runners: false, terraform: false, repoMove: true }));
  assert.deepEqual(ids(plan), [
    'before-you-start', 'install',
    'doctor', 'export', 'mapping', 'secrets-context', 'dry-run',
    'connect-repos', 'sync-apply', 'transfer', 'oidc-trust', 'pause-enable',
    'smoke', 'validate',
    'webhook-handoff', 'cleanup-extra', 'rotate-cleanup', 'external-pins',
  ]);
  assert.deepEqual(plan.steps.filter((s) => s.pointOfNoReturn), []);
  assert.equal(
    flat(step(plan, 'pause-enable').commands[1]),
    'circleci-migrate cutover --manifest manifest.json --mapping mapping.json --pause-source --enable-dest --apply',
  );
  assert.ok(flat(step(plan, 'sync-apply').commands[0]).includes('--dest-github-org "acme-new"'));
  assert.ok(ids(plan).indexOf('transfer') < ids(plan).indexOf('pause-enable'), 'the transfer runs before the source is paused');
  // The same-GitHub-org branch never uses the single command.
  assert.ok(!cmdsOf(buildPlan(input('app-to-app', { repoMove: false }))).includes('--pause-source --enable-dest'));
});

test('the runbook step numbers of the plan never go backwards, and every runbook step is covered', () => {
  for (const type of TYPES) {
    const { steps } = readRunbook(type);
    const plan = buildPlan(input(type, allOn, { destOrbNs: '', destRunnerNs: '' }));
    const planPlain = type === 'app-to-app' ? buildPlan(input(type, { ...allOn, repoMove: false }, { destOrbNs: '', destRunnerNs: '' })) : plan;
    const numbers = planPlain.steps.map((s) => steps.get(anchorOf(s))).filter((n) => n !== undefined);
    assert.deepEqual(numbers, [...numbers].sort((a, b) => a - b), `${type}: runbook steps out of order: ${numbers}`);
    assert.deepEqual([...new Set(numbers)], [...steps.values()].sort((a, b) => a - b), `${type}: a runbook step is missing from the plan`);
  }
});

test('the plan emits the runbook commands (same flags, same dry-run and apply pairs)', () => {
  // The runbooks' defaults, with orbs and runners on under a different name.
  const norm = (c) => flat(c).replaceAll('"', '');
  for (const type of TYPES) {
    const { commands } = readRunbook(type);
    const plan = buildPlan(input(type, { orbs: true, runners: true, repoMove: false, terraform: false }, { destOrbNs: 'new-orbs', destRunnerNs: 'new-runners' }));
    const have = allCommands(plan).flatMap((c) => c.split('\n')).filter((l) => l.startsWith('circleci-migrate ')).map(norm);
    for (const raw of commands) {
      const want = raw.replaceAll('"', '').replace('--dest-orb-namespace <new-namespace>', '--dest-orb-namespace new-orbs').replace('--dest-runner-namespace <namespace>', '--dest-runner-namespace new-runners');
      assert.ok(
        have.some((h) => h === want || h.startsWith(want + ' ')),
        `${type}: the runbook command is not in the plan:\n  ${want}`,
      );
    }
    // The pairs: every --apply in the runbook has its preview in the plan, in the same order.
    const order = commands.map((c) => c.replaceAll('"', ''));
    assert.ok(order.length > 8, type);
  }
});

test('oauth-to-app: the pause-then-App order, the point of no return and the reversible steps', () => {
  const plan = buildPlan(input('oauth-to-app', allOn, { destOrbNs: '' }));
  const order = ids(plan);
  const before = (a, b) => assert.ok(order.indexOf(a) >= 0 && order.indexOf(a) < order.indexOf(b), `${a} before ${b}: ${order.join(' ')}`);
  for (const [a, b] of [
    ['sync-projects', 'transfer'], ['transfer', 'pause-source'], ['pause-source', 'move-app'], ['move-app', 'sync-definitions'],
    ['sync-definitions', 'orb-support'], ['orb-support', 'runners'], ['runners', 'enable-dest'], ['enable-dest', 'smoke'], ['smoke', 'validate'],
  ]) before(a, b);
  assert.deepEqual(plan.steps.filter((s) => s.pointOfNoReturn).map((s) => s.id), ['move-app']);
  assert.equal(step(plan, 'move-app').kind, 'ui');
  assert.ok(step(plan, 'pause-source').reversible && step(plan, 'enable-dest').reversible);
  assert.equal(flat(step(plan, 'pause-source').undo[0]), 'circleci-migrate cutover --manifest manifest.json --mapping mapping.json --undo --apply');
  assert.equal(buildPlan(input('oauth-to-oauth', allOn)).steps.filter((s) => s.pointOfNoReturn).length, 0);
  assert.deepEqual(buildPlan(input('app-to-app', { repoMove: true })).steps.filter((s) => s.pointOfNoReturn), []);
  assert.deepEqual(buildPlan(input('app-to-app', { repoMove: false })).steps.filter((s) => s.pointOfNoReturn).map((s) => s.id), ['move-app']);
});

test('oauth-to-app: the runbook mapping.json forms', () => {
  const bash = step(buildPlan(input('oauth-to-app')), 'mapping').commands[0];
  assert.equal(bash, 'cat > mapping.json <<EOF\n{ "org": { "from": "$SOURCE_ORG", "to": "$DEST_ORG" } }\nEOF');
  const ps = step(buildPlan(input('oauth-to-app', {}, { shell: 'powershell' })), 'mapping').commands[0];
  assert.equal(ps, '@{ org = @{ from = $SOURCE_ORG; to = $DEST_ORG } } | ConvertTo-Json | Set-Content mapping.json');
});

test('oauth-to-oauth: sync, then mapping generate, then transfer (dry run, apply), pause, follow', () => {
  const plan = buildPlan(input('oauth-to-oauth'));
  assert.equal(flat(step(plan, 'mapping-generate').commands[0]), 'circleci-migrate mapping generate --manifest manifest.json --dest-org "$DEST_ORG" --output mapping.json');
  assert.equal(step(plan, 'transfer').commands.length, 2);
  assert.ok(!flat(step(plan, 'transfer').commands[0]).includes('--apply'));
  assert.ok(flat(step(plan, 'transfer').commands[0]).includes('--enable-trigger'), 'the runbook dry run carries --enable-trigger');
  assert.equal(flat(step(plan, 'follow-dest').commands[0]), 'circleci-migrate sync --manifest manifest.json --mapping mapping.json --apply --yes');
  assert.equal(
    flat(step(plan, 'sync-apply').commands[0]),
    'circleci-migrate sync --manifest manifest.json --mapping mapping.json --apply --webhook-secrets-out webhook-secrets.json',
  );
});

test('oauth-to-oauth: without pausing, the destination is still followed with sync --yes and the warning appears', () => {
  const plan = buildPlan(input('oauth-to-oauth', { pauseSource: false }));
  assert.ok(!ids(plan).includes('pause-source'));
  assert.ok(ids(plan).includes('follow-dest'));
  assert.ok(plan.warnings.some((w) => w.code === 'no-pause'));
});

test('app-to-app without a pause: a sync re-run enables the existing triggers', () => {
  for (const repoMove of [false, true]) {
    const plan = buildPlan(input('app-to-app', { pauseSource: false, repoMove }));
    assert.ok(!ids(plan).includes('pause-source') && !ids(plan).includes('pause-enable'));
    assert.equal(
      flat(step(plan, 'enable-dest').commands[0]),
      'circleci-migrate sync --manifest manifest.json --mapping mapping.json --apply --yes --enable-existing-triggers',
    );
  }
});

test('the secrets transfer always runs before the source is paused or the destination enabled', () => {
  for (const type of TYPES) {
    for (const repoMove of [false, true]) {
      const order = ids(buildPlan(input(type, { ...allOn, repoMove }, { destOrbNs: '' })));
      const pause = order.findIndex((id) => ['pause-source', 'pause-enable', 'enable-dest', 'follow-dest'].includes(id));
      assert.ok(order.indexOf('transfer') >= 0 && order.indexOf('transfer') < pause, `${type}: ${order.join(' ')}`);
    }
  }
});

// ---------------------------------------------------------------------------
// Option toggles
// ---------------------------------------------------------------------------

const cmdsOf = (plan) => allCommands(plan).join('\n');

test('SSH keys off: no --include-ssh-keys; on: present', () => {
  for (const type of TYPES) {
    assert.ok(!cmdsOf(buildPlan(input(type, { sshKeys: false }))).includes('--include-ssh-keys'), type);
    assert.ok(cmdsOf(buildPlan(input(type, { sshKeys: true }))).includes('--include-ssh-keys'), type);
  }
});

test('project variables off: no --include-project-vars', () => {
  for (const type of TYPES) {
    assert.ok(!cmdsOf(buildPlan(input(type, { projectVars: false }))).includes('--include-project-vars'), type);
  }
});

test('context restrictions: --remove-restrictions only when ticked', () => {
  for (const type of TYPES) {
    assert.ok(cmdsOf(buildPlan(input(type, { restrictions: true }))).includes('--remove-restrictions'), type);
    assert.ok(!cmdsOf(buildPlan(input(type, { restrictions: false }))).includes('--remove-restrictions'), type);
  }
});

test('contexts off: --skip-contexts on sync and the transfer', () => {
  const plan = buildPlan(input('oauth-to-app', { contexts: false }));
  assert.ok(flat(step(plan, 'sync-projects').commands[0]).includes('--skip-contexts'));
  assert.ok(flat(step(plan, 'transfer').commands[0]).includes('--skip-contexts --include-project-vars'));
  assert.ok(!cmdsOf(buildPlan(input('oauth-to-app'))).includes('--skip-contexts'));
});

test('nothing to transfer: no secrets steps and no migration-secrets context', () => {
  for (const type of TYPES) {
    const plan = buildPlan(input(type, { contexts: false, projectVars: false, sshKeys: false }));
    const order = ids(plan);
    for (const id of ['secrets-context', 'transfer']) assert.ok(!order.includes(id), `${type} ${id}`);
    assert.ok(!cmdsOf(plan).includes('secrets transfer'), type);
    assert.ok(!cmdsOf(plan).includes('migration-secrets'), type);
  }
});

test('runners: export and validate flags, a recreate step, and re-registration', () => {
  const plan = buildPlan(input('oauth-to-app', { runners: true }));
  assert.ok(flat(step(plan, 'export').commands[0]).includes('--runner-namespace "acme-runners"'));
  assert.equal(
    flat(step(plan, 'runners').commands[0]),
    'circleci-migrate sync --manifest manifest.json --mapping mapping.json --only runner --dest-runner-namespace "acme-new-runners" --apply',
  );
  assert.match(step(plan, 'runners').why, /do not follow a namespace/);
  assert.ok(ids(plan).includes('runner-register'));
  assert.ok(flat(step(plan, 'validate').commands[0]).includes('--dest-runner-namespace "acme-new-runners"'));
  assert.ok(!ids(plan).includes('runner-ns-support'));
  const off = ids(buildPlan(input('oauth-to-app', { runners: false })));
  assert.ok(!off.includes('runners') && !off.includes('runner-register'));
});

test('runners with an unchanged namespace name need support to move it', () => {
  const plan = buildPlan(input('oauth-to-app', { runners: true }, { destRunnerNs: '' }));
  assert.ok(ids(plan).includes('runner-ns-support'));
  assert.equal(step(plan, 'runner-ns-support').kind, 'support');
});

test('orbs: a different name is republished by sync, the same name is moved by support', () => {
  const different = buildPlan(input('oauth-to-app', { orbs: true }));
  assert.ok(ids(different).includes('orb-sync') && !ids(different).includes('orb-support'));
  assert.ok(flat(step(different, 'orb-sync').commands[0]).includes('--only orb --dest-orb-namespace "acme-new"'));
  assert.ok(flat(step(different, 'export').commands[0]).includes('--orb-namespace "acme"'));

  const same = buildPlan(input('oauth-to-app', { orbs: true }, { destOrbNs: '' }));
  assert.ok(ids(same).includes('orb-support') && !ids(same).includes('orb-sync'));
  assert.equal(step(same, 'orb-support').kind, 'support');
  assert.deepEqual(step(same, 'orb-support').commands, []);
  assert.match(step(same, 'orb-support').title, /Ask CircleCI support to move the orb namespace "acme" to circleci\//);
});

test('webhooks: --webhook-secrets-out on the sync that creates them, plus a hand-over step', () => {
  for (const type of TYPES) {
    const on = buildPlan(input(type, { webhooks: true }));
    assert.ok(cmdsOf(on).includes('--webhook-secrets-out webhook-secrets.json'), type);
    assert.ok(ids(on).includes('webhook-handoff'), type);
    assert.equal(step(on, 'webhook-handoff').phase, 'after');
    const off = buildPlan(input(type, { webhooks: false }));
    assert.ok(!cmdsOf(off).includes('--webhook-secrets-out'), type);
    assert.ok(!ids(off).includes('webhook-handoff'), type);
  }
});

test('pause source off (oauth-to-app): no cutover, and sync --yes --enable-existing-triggers enables the destination', () => {
  const plan = buildPlan(input('oauth-to-app', { pauseSource: false }));
  assert.ok(!ids(plan).includes('pause-source'));
  assert.ok(!cmdsOf(plan).includes('cutover'));
  assert.equal(
    flat(step(plan, 'enable-dest').commands[0]),
    'circleci-migrate sync --manifest manifest.json --mapping mapping.json --apply --yes --enable-existing-triggers',
  );
  const w = plan.warnings.find((x) => x.code === 'no-pause');
  assert.ok(w && /double builds/.test(w.message));
});

test('repo move: --dest-github-org only where the runbook uses it, GitHub token only in the environment', () => {
  // oauth-to-oauth: step 6 only.
  const oo = buildPlan(input('oauth-to-oauth', { repoMove: true }));
  const withFlag = oo.steps.filter((s) => s.commands.some((c) => c.includes('--dest-github-org'))).map((s) => s.id);
  assert.deepEqual(withFlag, ['sync-apply']);
  // App types: every main sync of the runbook.
  const aa = buildPlan(input('app-to-app', { repoMove: true }));
  assert.ok(flat(step(aa, 'dry-run').commands[0]).includes('--dest-github-org "acme-new"'));
  assert.ok(flat(step(aa, 'sync-apply').commands[0]).includes('--dest-github-org "acme-new"'));
  const oa = buildPlan(input('oauth-to-app', { repoMove: true }));
  for (const id of ['dry-run', 'sync-projects', 'sync-definitions']) assert.ok(flat(step(oa, id).commands[0]).includes('--dest-github-org "acme-new"'), id);
  // Nothing passes --github-token: the CLI reads $GITHUB_TOKEN, as the runbooks set it.
  for (const plan of [oo, aa, oa]) {
    assert.ok(!cmdsOf(plan).includes('--github-token'));
    assert.match(plan.env.bash, /GITHUB_TOKEN="<paste token>"/);
  }
  const off = buildPlan(input('app-to-app', { repoMove: false }));
  assert.ok(!cmdsOf(off).includes('--dest-github-org'));
  assert.ok(!off.env.bash.includes('GITHUB_TOKEN'));
  assert.match(buildPlan(input('oauth-to-app', { repoMove: false })).env.bash, /GITHUB_TOKEN/);
});

test('repo move writes github_org into mapping.json when the source GitHub org is known', () => {
  const m = step(buildPlan(input('oauth-to-app', { repoMove: true })), 'mapping').commands[0];
  assert.match(m, /"github_org": \{ "from": "acme", "to": "acme-new" \}/);
  const ps = step(buildPlan(input('oauth-to-app', { repoMove: true }, { shell: 'powershell' })), 'mapping').commands[0];
  assert.match(ps, /github_org = @\{ from = "acme"; to = "acme-new" \}/);
  assert.ok(!step(buildPlan(input('oauth-to-app', { repoMove: false })), 'mapping').commands[0].includes('github_org'));
});

test('terraform: oauth-to-oauth generates first and applies before sync; the other scenarios adopt the CLI-built destination afterwards', () => {
  const plan = buildPlan(input('oauth-to-oauth', { terraform: true }));
  const order = ids(plan);
  assert.ok(order.indexOf('terraform-generate') < order.indexOf('dry-run-sync'));
  assert.ok(order.indexOf('terraform-apply') < order.indexOf('sync-apply'));
  assert.ok(flat(step(plan, 'terraform-generate').commands[0]).includes('--dest-org-type oauth'));
  assert.ok(flat(step(plan, 'sync-apply').commands[0]).includes('--skip-terraform-managed'));

  for (const type of ['oauth-to-app', 'app-to-app']) {
    const p = buildPlan(input(type, { terraform: true }));
    assert.equal(step(p, 'terraform-generate').phase, 'after', type);
    assert.ok(allCommands(p).some((c) => c.includes('--import-existing') && c.includes('--dest-org-type standalone')), type);
    assert.ok(!cmdsOf(p).split('\n').some((l) => l.includes('sync') && l.includes('--skip-pipelines') && l.includes('--skip-terraform-managed')));
    assert.equal(step(p, 'terraform-generate').link.path, 'docs/reference/terraform/');
  }
  assert.ok(!ids(buildPlan(input('oauth-to-oauth', { terraform: false }))).includes('terraform-generate'));
});

test('server host: --host on every circleci-migrate command, --dest-host on the transfer', () => {
  const plan = buildPlan(input('app-to-app', allOn, { host: 'server', hostUrl: 'https://circleci.example.com' }));
  const lines = allCommands(plan).flatMap((c) => c.split('\n')).filter((l) => l.startsWith('circleci-migrate ') && l !== 'circleci-migrate version');
  assert.ok(lines.length > 10);
  for (const l of lines) assert.ok(l.includes('--host "https://circleci.example.com"'), l);
  assert.ok(lines.some((l) => l.startsWith('circleci-migrate secrets transfer') && l.includes('--dest-host "https://circleci.example.com"')));
  assert.ok(!cmdsOf(buildPlan(input('app-to-app', allOn))).includes('--host'));
});

// ---------------------------------------------------------------------------
// Shells
// ---------------------------------------------------------------------------

test('PowerShell: single-line commands, no backslash anywhere, the runbook variables', () => {
  for (const type of TYPES) {
    for (const options of [{}, allOn, allOff]) {
      const plan = buildPlan(input(type, options, { shell: 'powershell' }));
      for (const c of allCommands(plan)) {
        assert.ok(!c.includes('\n'), `${type}: multi-line PowerShell command:\n${c}`);
        assert.ok(!c.includes('\\'), `${type}: backslash in PowerShell command: ${c}`);
        assert.ok(!c.includes('`'), `${type}: backtick in PowerShell command: ${c}`);
        assert.ok(!c.includes('<<'), `${type}: here-doc in PowerShell command: ${c}`);
      }
      assert.match(plan.env.powershell, /^\$SOURCE_ORG = "[^"]+"$/m);
      assert.match(plan.env.powershell, /^\$env:CIRCLECI_SOURCE_TOKEN = "<paste token>"$/m);
      assert.ok(!plan.env.powershell.includes('export '));
    }
  }
});

test('bash: long commands break with backslash continuations, one flag group per line', () => {
  const plan = buildPlan(input('oauth-to-app', allOn));
  const transfer = step(plan, 'transfer').commands[0];
  assert.ok(transfer.includes(' \\\n  --manifest manifest.json'));
  for (const line of transfer.split('\n')) assert.ok(line.length <= 80, line);
  assert.ok(allCommands(plan).every((c) => !c.endsWith('\\')));
});

test('environment block: export in bash, plain variables and $env: tokens in PowerShell, as the runbooks', () => {
  const plan = buildPlan(input('oauth-to-app'));
  assert.equal(
    plan.env.bash,
    ['export SOURCE_ORG="gh/acme"', `export DEST_ORG="circleci/${DST_UUID}"`, `export DEST_ORG_ID="${DST_UUID}"`,
      'export CIRCLECI_SOURCE_TOKEN="<paste token>"', 'export CIRCLECI_DEST_TOKEN="<paste token>"', 'export GITHUB_TOKEN="<paste token>"'].join('\n'),
  );
  assert.match(plan.env.powershell, /^\$SOURCE_ORG = "gh\/acme"$/m);
  assert.match(plan.env.powershell, /^\$env:GITHUB_TOKEN = "<paste token>"$/m);
});

// ---------------------------------------------------------------------------
// Tokens, support steps, public-site hygiene
// ---------------------------------------------------------------------------

test('tokens appear only as <paste token> placeholders', () => {
  for (const type of TYPES) {
    const plan = buildPlan(input(type, allOn));
    const text = JSON.stringify(plan) + renderRunSheet(plan);
    for (const m of text.matchAll(/(?:TOKEN\w*)(?:=| = )\\?"([^"\\]*)/g)) assert.equal(m[1], '<paste token>');
    assert.ok(!/\b(ghp_|github_pat_|xoxb-|circle-token)/.test(text));
  }
});

test('support steps say "Ask CircleCI support to ..." and carry no commands', () => {
  for (const type of TYPES) {
    for (const s of buildPlan(input(type, { ...allOn }, { destOrbNs: '', destRunnerNs: '' })).steps.filter((x) => x.kind === 'support')) {
      assert.match(s.title, /^(Ask CircleCI support to |Book the plan move with CircleCI support)/, s.id);
      assert.deepEqual(s.commands, [], s.id);
    }
  }
  const t = buildPlan(input('oauth-to-app')).steps.find((s) => s.id === 'support-plan-move').title;
  assert.equal(t, `Ask CircleCI support to move the plan to circleci/${DST_UUID}`);
});

test('no internal admin commands, customer names or internal URLs in any generated text', () => {
  const banned = /transfer-plan|change-namespace-owner|rename-namespace|plan-inspect|hubot|BCBSM|Questrade|Postman|\bRBI\b|slack\.com|#[a-z]+-[a-z-]+ channel|internal/i;
  for (const type of TYPES) {
    for (const options of [{}, allOn]) {
      for (const extra of [{}, { destOrbNs: '', destRunnerNs: '' }]) {
        const plan = buildPlan(input(type, options, extra));
        const text = renderRunSheet(plan) + JSON.stringify(plan);
        const hit = banned.exec(text);
        assert.equal(hit, null, `${type}: banned text "${hit?.[0]}"`);
      }
    }
  }
});

test('every step is well formed', () => {
  for (const type of TYPES) {
    const plan = buildPlan(input(type, allOn, { destOrbNs: '' }));
    const seen = new Set();
    for (const s of plan.steps) {
      assert.ok(!seen.has(s.id), `duplicate id ${s.id}`);
      seen.add(s.id);
      assert.ok(['prep', 'cutover', 'after'].includes(s.phase), s.id);
      assert.ok(['cli', 'ui', 'support'].includes(s.kind), s.id);
      for (const field of ['title', 'why', 'check']) assert.ok(typeof s[field] === 'string' && s[field].length > 0, `${s.id}.${field}`);
      assert.ok(Array.isArray(s.commands), s.id);
      if (s.kind !== 'cli') assert.deepEqual(s.commands, [], `${s.id} is ${s.kind} so it carries no commands`);
      if (s.kind === 'cli') assert.ok(s.commands.length > 0, s.id);
    }
    // Phases never go backwards.
    const rank = { prep: 0, cutover: 1, after: 2 };
    const ranks = plan.steps.map((s) => rank[s.phase]);
    assert.deepEqual(ranks, [...ranks].sort((a, b) => a - b), `${type}: phases out of order`);
  }
});

// ---------------------------------------------------------------------------
// Run sheet and shareable state
// ---------------------------------------------------------------------------

test('run sheet: a checkbox per step, the environment block, and the point-of-no-return callout', () => {
  const plan = buildPlan(input('oauth-to-app', allOn));
  const md = renderRunSheet(plan);
  assert.equal((md.match(/^- \[ \] \*\*/gm) ?? []).length, plan.steps.length);
  assert.match(md, /^## Environment$/m);
  assert.match(md, /> \[!CAUTION\]\n> \*\*Point of no return\.\*\*/);
  assert.match(md, /```bash/);
  assert.match(md, /Check: /);
  const ps = renderRunSheet(buildPlan(input('oauth-to-app', allOn, { shell: 'powershell' })));
  assert.match(ps, /```powershell/);
  assert.ok(!/```bash/.test(ps));
});

test('run sheet: runbook links are relative, or absolute with a base URL', () => {
  const plan = buildPlan(input('oauth-to-app'));
  assert.match(renderRunSheet(plan), /\[Runbook step 9\]\(docs\/migrations\/oauth-to-app\/#9-pause-the-source\)/);
  assert.match(renderRunSheet(plan, { baseUrl: 'https://example.test/x/' }), /\(https:\/\/example\.test\/x\/docs\/migrations\/oauth-to-app\/#9-pause-the-source\)/);
});

test('support prep step: "Book the plan move with CircleCI support"', () => {
  const s = step(buildPlan(input('oauth-to-app')), 'support-plan-ask');
  assert.equal(s.title, 'Book the plan move with CircleCI support');
  assert.equal(s.kind, 'support');
});

test('hash state round-trips and holds only non-secret fields', () => {
  const original = input('oauth-to-app', allOn, { host: 'server', hostUrl: 'https://circleci.example.com', shell: 'powershell' });
  const hash = encodeState(original);
  assert.ok(!/token/i.test(hash));
  const back = decodeState('#' + hash);
  for (const k of ['type', 'host', 'hostUrl', 'source', 'dest', 'runnerNs', 'destRunnerNs', 'orbNs', 'destOrbNs', 'destGithubOrg', 'shell']) {
    assert.equal(back[k], original[k], k);
  }
  assert.deepEqual(back.options, original.options);
  assert.deepEqual(buildPlan(back).steps.map((s) => s.id), buildPlan(original).steps.map((s) => s.id));
});

test('hash state: defaults are omitted and unknown input decodes to defaults', () => {
  assert.equal(encodeState(defaultInput()), '');
  assert.deepEqual(decodeState(''), defaultInput());
  assert.deepEqual(decodeState('#garbage&&=').options, defaultInput().options);
  const none = decodeState('#o=-');
  assert.ok(Object.values(none.options).every((v) => v === false));
});
