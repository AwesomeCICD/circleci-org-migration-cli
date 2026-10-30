// Plan builder logic: validate the inputs and turn them into an ordered migration plan.
//
// Pure ES module with no DOM access, so it runs in the browser and under `node --test`.
// The step catalog (every command and manual step) lives in catalog.mjs.

import { SCENARIOS, buildSteps, envBlock } from './catalog.mjs';

// ---------------------------------------------------------------------------
// Static definitions the UI renders
// ---------------------------------------------------------------------------

export const MIGRATION_TYPES = [
  {
    id: 'oauth-to-app',
    label: 'GitHub OAuth to GitHub App',
    description: 'A gh/ org becomes a circleci/ GitHub App org. The GitHub App moves with it.',
    sourceHint: 'gh/acme',
    destHint: 'circleci/<org-id>',
  },
  {
    id: 'oauth-to-oauth',
    label: 'OAuth to OAuth',
    description: 'A gh/ or bb/ org moves to another gh/ or bb/ org: a rename, or repos moving to a new GitHub org.',
    sourceHint: 'gh/acme',
    destHint: 'gh/acme-new',
  },
  {
    id: 'app-to-app',
    label: 'GitHub App to GitHub App',
    description: 'A circleci/ org (GitHub App, standalone or GitLab) moves to another circleci/ org.',
    sourceHint: 'circleci/<org-id>',
    destHint: 'circleci/<org-id>',
  },
];

export const OPTION_DEFS = [
  { key: 'contexts', label: 'Contexts and their variables', hint: 'Recreated in the destination; values move by the secrets transfer.', default: true },
  { key: 'projectVars', label: 'Project environment variables', hint: 'Values move by the secrets transfer, project by project.', default: true },
  { key: 'sshKeys', label: 'Additional SSH keys', hint: 'Private keys move inside the transfer pipeline, never to your disk.', default: true },
  { key: 'restrictions', label: 'Some contexts have project or expression restrictions', hint: 'The transfer lifts them temporarily and restores them afterwards (--remove-restrictions).', default: true },
  { key: 'runners', label: 'Self-hosted runners', hint: 'Recreates resource classes in the destination namespace; runners re-register.', default: false },
  { key: 'orbs', label: 'Private orbs (an orb namespace)', hint: 'A same-name namespace is moved by CircleCI support; a new name is republished.', default: false },
  { key: 'webhooks', label: 'Webhooks', hint: 'Writes new signing secrets and inbound URLs to a file for you to hand over.', default: true },
  { key: 'repoMove', label: 'The repositories are moving to a new GitHub org', hint: 'For example an Enterprise Managed Users move. Needs a GitHub token to look up repository IDs.', default: false },
  { key: 'pauseSource', label: 'Pause source builds at cutover (recommended)', hint: 'Stops the source building before the destination goes live, and can be undone.', default: true },
  { key: 'terraform', label: 'Manage the destination with Terraform', hint: 'Generates Terraform, then the CLI fills in what Terraform cannot manage.', default: false },
];

export const PHASES = [
  { id: 'prep', label: 'Prepare', note: 'Days before. Nothing here changes the source or builds anything.' },
  { id: 'cutover', label: 'Cutover day', note: 'Run these in order, in one sitting.' },
  { id: 'after', label: 'After the cutover', note: 'Clean up once the destination is healthy.' },
];

export const KIND_LABELS = { cli: 'CLI', ui: 'In the CircleCI UI', support: 'Ask support' };

export function defaultOptions() {
  return Object.fromEntries(OPTION_DEFS.map((o) => [o.key, o.default]));
}

export function defaultInput() {
  return {
    type: 'oauth-to-app',
    host: 'cloud',
    hostUrl: '',
    source: '',
    dest: '',
    destId: '',
    options: defaultOptions(),
    runnerNs: '',
    destRunnerNs: '',
    orbNs: '',
    destOrbNs: '',
    destGithubOrg: '',
    shell: 'bash',
  };
}

// ---------------------------------------------------------------------------
// Slug and value shapes
// ---------------------------------------------------------------------------

const UUID = '[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}';
export const OAUTH_SLUG_RE = /^(gh|bb|github|bitbucket)\/[A-Za-z0-9_.-]+$/;
// circleci/<org-id>: the UUID shown in Organization Settings, Overview, or the 20-24 character short ID of the app URL.
export const APP_SLUG_RE = new RegExp(`^circleci/(?:${UUID}|[A-Za-z0-9]{20,24})$`);
const APP_SLUG_UUID_RE = new RegExp(`^circleci/(${UUID})$`);
export const UUID_RE = new RegExp(`^${UUID}$`);
const NAMESPACE_RE = /^[A-Za-z0-9][A-Za-z0-9_.-]*$/;
const GITHUB_ORG_RE = /^[A-Za-z0-9](?:[A-Za-z0-9-]{0,37}[A-Za-z0-9])?$/;
const HOST_RE = /^https?:\/\/[A-Za-z0-9.-]+(?::[0-9]{1,5})?(?:\/[A-Za-z0-9._~/-]*)?$/;

// The CLI accepts github/ and bitbucket/ as aliases of gh/ and bb/; plans use the short forms.
export function normalizeSlug(value) {
  const s = String(value ?? '').trim().replace(/\/+$/, '');
  if (s.startsWith('github/')) return 'gh/' + s.slice('github/'.length);
  if (s.startsWith('bitbucket/')) return 'bb/' + s.slice('bitbucket/'.length);
  return s;
}

// 'oauth' | 'app' for a well-formed slug; 'oauth?' | 'app?' for a known prefix with a bad tail.
export function slugKind(slug) {
  if (OAUTH_SLUG_RE.test(slug)) return 'oauth';
  if (APP_SLUG_RE.test(slug)) return 'app';
  if (/^(gh|bb)\//.test(slug)) return 'oauth?';
  if (slug.startsWith('circleci/')) return 'app?';
  return '';
}

const KIND_OF = { 'oauth-to-app': ['oauth', 'app'], 'oauth-to-oauth': ['oauth', 'oauth'], 'app-to-app': ['app', 'app'] };

function typeForKinds(src, dst) {
  if (src === 'oauth' && dst === 'app') return 'oauth-to-app';
  if (src === 'oauth' && dst === 'oauth') return 'oauth-to-oauth';
  if (src === 'app' && dst === 'app') return 'app-to-app';
  return '';
}

const typeLabel = (id) => MIGRATION_TYPES.find((t) => t.id === id)?.label ?? id;

// ---------------------------------------------------------------------------
// Normalizing raw input
// ---------------------------------------------------------------------------

const str = (v) => String(v ?? '').trim();

export function normalizeInput(raw = {}) {
  const base = defaultInput();
  const options = { ...base.options };
  for (const key of Object.keys(options)) {
    if (raw.options && typeof raw.options[key] === 'boolean') options[key] = raw.options[key];
  }
  return {
    type: str(raw.type) || base.type,
    host: raw.host === 'server' ? 'server' : 'cloud',
    hostUrl: str(raw.hostUrl).replace(/\/+$/, ''),
    source: normalizeSlug(raw.source),
    dest: normalizeSlug(raw.dest),
    destId: str(raw.destId),
    options,
    runnerNs: str(raw.runnerNs),
    destRunnerNs: str(raw.destRunnerNs),
    orbNs: str(raw.orbNs),
    destOrbNs: str(raw.destOrbNs),
    destGithubOrg: str(raw.destGithubOrg),
    shell: raw.shell === 'powershell' ? 'powershell' : 'bash',
  };
}

// ---------------------------------------------------------------------------
// Validation
// ---------------------------------------------------------------------------

// Whether the plan needs the destination org ID as a separate value.
function needsDestId(input) {
  const o = input.options;
  return o.contexts || o.projectVars || o.sshKeys || o.terraform;
}

function err(errors, field, code, message, extra = {}) {
  errors.push({ field, code, message, ...extra });
}

function warn(warnings, code, level, message) {
  warnings.push({ code, level, message });
}

// validate(input) -> { errors: [{field, code, message, suggestType?}], warnings: [{code, level, message}], input }
// `input` is the normalized input, with defaults filled in (repoMove dropped where it cannot apply).
export function validate(raw) {
  const input = normalizeInput(raw);
  const errors = [];
  const warnings = [];
  const o = input.options;

  if (!SCENARIOS[input.type]) {
    err(errors, 'type', 'unknown-type', 'Choose a migration type.');
    return { errors, warnings, input };
  }
  const [wantSrc, wantDst] = KIND_OF[input.type];
  const srcKind = slugKind(input.source);
  const dstKind = slugKind(input.dest);
  const suggested = srcKind.length && !srcKind.endsWith('?') && dstKind.length && !dstKind.endsWith('?')
    ? typeForKinds(srcKind, dstKind)
    : '';

  // Host
  if (input.host === 'server') {
    if (!input.hostUrl) err(errors, 'hostUrl', 'required', 'Enter the base URL of your CircleCI server, such as https://circleci.example.com.');
    else if (!HOST_RE.test(input.hostUrl)) err(errors, 'hostUrl', 'invalid', 'Use a base URL such as https://circleci.example.com, with no spaces or quotes.');
    else if (input.hostUrl.startsWith('http://')) warn(warnings, 'http-host', 'warn', 'That URL is plain http, so API tokens cross the network unencrypted. Use https if your server supports it.');
  }

  // Source and destination slugs
  const checkSlug = (field, value, kind, want, role) => {
    if (!value) return err(errors, field, 'required', `Enter the ${role} org slug.`);
    if (kind === 'app?') return err(errors, field, 'invalid', 'A circleci/ slug is circleci/ followed by the org ID shown in Organization Settings, Overview, or the short ID in the app URL.');
    if (kind === 'oauth?') return err(errors, field, 'invalid', 'An OAuth slug is gh/<org> or bb/<org>, using letters, digits, dot, dash and underscore.');
    if (!kind) return err(errors, field, 'invalid', `"${value}" is not an org slug. Use ${want === 'oauth' ? 'gh/<org> or bb/<org>' : 'circleci/<org-id>'}.`);
    if (kind !== want) {
      const found = kind === 'oauth' ? 'a GitHub or Bitbucket OAuth org' : 'a GitHub App or standalone org';
      const expect = want === 'oauth' ? 'a gh/ or bb/ slug' : 'a circleci/<org-id> slug';
      if (suggested && suggested !== input.type) {
        return err(errors, field, 'type-mismatch', `${value} is ${found}, but "${typeLabel(input.type)}" needs ${expect} here. Your two slugs describe "${typeLabel(suggested)}": switch the type, or change the slug.`, { suggestType: suggested });
      }
      return err(errors, field, 'type-mismatch', `${value} is ${found}, but "${typeLabel(input.type)}" needs ${expect} for the ${role} org. Enter ${expect}.`);
    }
    return undefined;
  };
  checkSlug('source', input.source, srcKind, wantSrc, 'source');
  checkSlug('dest', input.dest, dstKind, wantDst, 'destination');

  if (!errors.some((e) => e.field === 'source') && input.type === 'oauth-to-app' && /^bb\//.test(input.source)) {
    err(errors, 'source', 'bitbucket-to-app', 'Bitbucket orgs cannot move to a GitHub App org. Use "OAuth to OAuth" with a bb/ destination, or start from a gh/ org.');
  }
  if (input.source && input.dest && input.source.toLowerCase() === input.dest.toLowerCase()) {
    err(errors, 'dest', 'same-org', 'The destination is the same org as the source. A migration needs two different orgs.');
  }

  // Destination org ID
  const slugId = APP_SLUG_UUID_RE.exec(input.dest)?.[1] ?? '';
  if (input.destId && !UUID_RE.test(input.destId)) {
    err(errors, 'destId', 'invalid', 'The org ID is a UUID such as 22222222-2222-2222-2222-222222222222 (Organization Settings, Overview).');
  } else if (!input.destId && !slugId && needsDestId(input)) {
    err(errors, 'destId', 'required', 'Enter the destination org ID (Organization Settings, Overview). The secrets transfer and Terraform need it.');
  } else if (input.destId && slugId && input.destId.toLowerCase() !== slugId.toLowerCase()) {
    warn(warnings, 'dest-id-mismatch', 'warn', 'The destination org ID differs from the UUID in the destination slug. For a circleci/ org they are normally the same value; check both.');
  }

  // Namespaces
  const ns = (field, value, required, label) => {
    if (!value) return required ? err(errors, field, 'required', `Enter the ${label}.`) : undefined;
    if (!NAMESPACE_RE.test(value)) return err(errors, field, 'invalid', `A namespace uses letters, digits, dot, dash and underscore, such as "acme-runners".`);
    return undefined;
  };
  if (o.runners) {
    ns('runnerNs', input.runnerNs, true, 'source runner namespace');
    ns('destRunnerNs', input.destRunnerNs, false, 'destination runner namespace');
  }
  if (o.orbs) {
    ns('orbNs', input.orbNs, true, 'source orb namespace');
    ns('destOrbNs', input.destOrbNs, false, 'destination orb namespace');
  }

  // Repo move
  if (o.repoMove) {
    if (/^bb\//.test(input.dest)) {
      o.repoMove = false;
      warn(warnings, 'repo-move-bb', 'warn', 'The repo-move option is for GitHub organizations; it is ignored for a Bitbucket destination.');
    } else if (!input.destGithubOrg && dstKind === 'oauth') {
      input.destGithubOrg = input.dest.split('/')[1];
    }
    if (o.repoMove) {
      if (!input.destGithubOrg) err(errors, 'destGithubOrg', 'required', 'Enter the name of the new GitHub org (the part after github.com/).');
      else if (!GITHUB_ORG_RE.test(input.destGithubOrg)) err(errors, 'destGithubOrg', 'invalid', 'A GitHub org name uses letters, digits and dashes, such as "acme-new".');
    }
  }

  // Warnings that need no valid slugs
  const [sk, dk] = [srcKind, dstKind];
  if (sk === 'oauth' && dk === 'oauth' && input.source.slice(0, 2) !== input.dest.slice(0, 2)) {
    warn(warnings, 'provider-mismatch', 'warn', 'The source and destination use different providers (GitHub and Bitbucket). The CLI does not move repositories between them; they must already exist at the destination.');
  }
  if (input.host === 'server' && input.type !== 'oauth-to-oauth') {
    warn(warnings, 'server-app', 'warn', 'Run the doctor step first: check that your CircleCI server supports a circleci/<id> org as the source or destination before you build on this plan.');
  }
  addPlanWarnings(input, warnings);
  return { errors, warnings, input };
}

function addPlanWarnings(input, warnings) {
  const o = input.options;
  const any = o.contexts || o.projectVars || o.sshKeys;

  if (!o.pauseSource) {
    warn(
      warnings,
      'no-pause',
      'warn',
      input.type === 'oauth-to-app'
        ? 'Without pausing the source, OAuth projects in the source org keep building after the destination goes live, so every push builds in both orgs: double builds and double billing. Leave "Pause source builds" ticked unless nothing builds in the source.'
        : 'Without pausing the source, both orgs build every push until you stop the source by hand. Leave "Pause source builds" ticked unless nothing builds in the source.',
    );
  }
  warn(warnings, 'stale-export', 'info', 'The export is a snapshot. If anything changes in the source after you export, run the export command again before cutover day.');
  if (input.type === 'oauth-to-app') {
    warn(warnings, 'lossy', 'info', 'This migration is lossy. Fork-PR builds, the OSS flag, pr_only_branch_overrides and group context restrictions have no GitHub App equivalent. Tell contributors before the switch.');
  }
  if (!any) {
    warn(warnings, 'no-secrets', 'warn', 'No secret values will move: contexts and projects are created with variable names only, and you must set every value by hand.');
  } else {
    if (!o.projectVars) warn(warnings, 'no-project-vars', 'warn', 'Project environment variable values will not move; only the names are recreated.');
    if (!o.sshKeys) warn(warnings, 'no-ssh', 'info', 'Additional SSH keys will not move. Add them again in each destination project (Project Settings, SSH Keys).');
    if (o.contexts && !o.restrictions) {
      warn(warnings, 'restrictions', 'info', 'If any context has a project or expression restriction, the transfer stops with an error. Tick the restrictions option and the CLI lifts them temporarily and restores them afterwards.');
    }
  }
  if (o.runners) {
    warn(warnings, 'runners', 'info', 'Runner resource classes do not follow a namespace move, and runner tokens do not transfer. Plan to re-register every runner.');
  }
  if (o.terraform) {
    warn(
      warnings,
      'terraform',
      'info',
      input.type === 'oauth-to-app'
        ? 'Terraform creates pipelines and triggers, which need the repositories connected to the destination GitHub App. This plan builds the destination with the CLI first, then adopts it into Terraform with import blocks.'
        : 'Terraform creates the contexts, projects and runner classes; the CLI then fills the gaps with --skip-terraform-managed. Read GAPS.md before you apply.',
    );
  }
  if (input.type === 'app-to-app' && !o.repoMove) {
    warn(warnings, 'one-app', 'warn', 'With the repos in the same GitHub org, the GitHub App has to move from the source org to the destination org, because a GitHub org connects to one CircleCI org at a time. That step is the point of no return. If the repos are moving to a new GitHub org, tick that option and nothing is uninstalled.');
  }
}

// ---------------------------------------------------------------------------
// Building a plan
// ---------------------------------------------------------------------------

function buildContext(input) {
  const o = input.options;
  const [srcKind, dstKind] = KIND_OF[input.type];
  const destId = input.destId || (APP_SLUG_UUID_RE.exec(input.dest)?.[1] ?? '');
  const destRunnerNs = input.destRunnerNs || input.runnerNs;
  const destOrbNs = input.destOrbNs || input.orbNs;
  return {
    type: input.type,
    shell: input.shell,
    hostUrl: input.host === 'server' ? input.hostUrl : '',
    src: input.source,
    dst: input.dest,
    destId,
    srcKind,
    destKind: dstKind,
    o,
    runnerNs: input.runnerNs,
    destRunnerNs,
    runnerSame: destRunnerNs === input.runnerNs,
    orbNs: input.orbNs,
    destOrbNs,
    orbSame: destOrbNs === input.orbNs,
    destGithubOrg: input.destGithubOrg,
    srcGithubOrg: input.source.startsWith('gh/') ? input.source.slice(3) : '',
    xfer: {
      any: o.contexts || o.projectVars || o.sshKeys,
      projects: o.projectVars || o.sshKeys,
    },
    needsGithubToken: input.type === 'oauth-to-app' || o.repoMove,
  };
}

// buildPlan(input) -> { errors, warnings, env: {bash, powershell}, steps, scenario, input }
export function buildPlan(raw) {
  const { errors, warnings, input } = validate(raw);
  const scenario = SCENARIOS[input.type] ?? null;
  if (errors.length > 0) {
    return { errors, warnings, env: { bash: '', powershell: '' }, steps: [], scenario, input };
  }
  const c = buildContext(input);
  const env = {
    bash: envBlock({ ...c, shell: 'bash' }),
    powershell: envBlock({ ...c, shell: 'powershell' }),
  };
  return { errors, warnings, env, steps: buildSteps(c), scenario, input };
}

// ---------------------------------------------------------------------------
// Run sheet (Markdown)
// ---------------------------------------------------------------------------

const fence = (shell) => (shell === 'powershell' ? 'powershell' : 'bash');

// `baseUrl` turns the relative runbook links into absolute ones for a file that lives outside the site.
export function renderRunSheet(plan, { baseUrl = '' } = {}) {
  const { input, steps, warnings, env } = plan;
  const lang = fence(input.shell);
  const out = [];
  out.push(`# Migration run sheet: ${plan.scenario.label}`);
  out.push('');
  out.push(`- Source: \`${input.source}\``);
  out.push(`- Destination: \`${input.dest}\``);
  if (input.host === 'server') out.push(`- CircleCI server: \`${input.hostUrl}\``);
  out.push(`- Shell: ${input.shell === 'powershell' ? 'PowerShell' : 'bash or zsh'}`);
  out.push('');
  out.push('Generated by the Plan builder. It holds no tokens: paste your own over each `<paste token>` placeholder.');
  out.push('');

  const real = warnings.filter((w) => w.level === 'warn');
  const info = warnings.filter((w) => w.level !== 'warn');
  if (warnings.length) {
    out.push('## Read first');
    out.push('');
    for (const w of [...real, ...info]) out.push(`- ${w.level === 'warn' ? '**Warning.** ' : ''}${w.message}`);
    out.push('');
  }

  out.push('## Environment');
  out.push('');
  out.push('```' + lang);
  out.push(input.shell === 'powershell' ? env.powershell : env.bash);
  out.push('```');
  out.push('');

  let n = 0;
  for (const phase of PHASES) {
    const group = steps.filter((s) => s.phase === phase.id);
    if (!group.length) continue;
    out.push(`## ${phase.label}`);
    out.push('');
    out.push(phase.note);
    out.push('');
    for (const s of group) {
      n += 1;
      out.push(`- [ ] **${n}. ${s.title}** (${KIND_LABELS[s.kind]})`);
      out.push('');
      if (s.pointOfNoReturn) {
        out.push('> [!CAUTION]');
        out.push(`> **Point of no return.** ${s.ponrNote}`);
        out.push('');
      }
      out.push(`  ${s.why}`);
      out.push('');
      if (s.link) {
        out.push(`  [${s.link.label}](${baseUrl}${s.link.path})`);
        out.push('');
      }
      for (const d of s.details ?? []) out.push(`  - ${d}`);
      if (s.details?.length) out.push('');
      if (s.commands.length) {
        out.push('  ```' + lang);
        for (const cmd of s.commands) out.push(cmd.split('\n').map((l) => '  ' + l).join('\n'));
        out.push('  ```');
        out.push('');
      }
      out.push(`  Check: ${s.check}`);
      out.push('');
      if (s.undo?.length) {
        out.push('  To undo this step:');
        out.push('');
        out.push('  ```' + lang);
        for (const cmd of s.undo) out.push(cmd.split('\n').map((l) => '  ' + l).join('\n'));
        out.push('  ```');
        out.push('');
      }
    }
  }
  return out.join('\n').replace(/\n{3,}/g, '\n\n').trimEnd() + '\n';
}

// ---------------------------------------------------------------------------
// Shareable state: non-secret fields only, as URL hash parameters
// ---------------------------------------------------------------------------

const STATE_KEYS = [
  ['type', 't'], ['host', 'h'], ['hostUrl', 'u'], ['source', 's'], ['dest', 'd'], ['destId', 'i'],
  ['runnerNs', 'rn'], ['destRunnerNs', 'drn'], ['orbNs', 'on'], ['destOrbNs', 'don'], ['destGithubOrg', 'gh'], ['shell', 'sh'],
];

export function encodeState(raw) {
  const input = { ...defaultInput(), ...raw, options: { ...defaultOptions(), ...(raw.options ?? {}) } };
  const params = new URLSearchParams();
  const base = defaultInput();
  for (const [field, short] of STATE_KEYS) {
    const value = String(input[field] ?? '');
    if (value && value !== String(base[field])) params.set(short, value);
  }
  const on = OPTION_DEFS.filter((d) => input.options[d.key]).map((d) => d.key);
  const different = OPTION_DEFS.some((d) => input.options[d.key] !== d.default);
  if (different) params.set('o', on.join(',') || '-');
  return params.toString();
}

export function decodeState(hash) {
  const params = new URLSearchParams(String(hash ?? '').replace(/^#/, ''));
  const input = defaultInput();
  for (const [field, short] of STATE_KEYS) {
    if (params.has(short)) input[field] = params.get(short);
  }
  if (params.has('o')) {
    const on = new Set(params.get('o').split(',').filter(Boolean));
    for (const d of OPTION_DEFS) input.options[d.key] = on.has(d.key);
  }
  return input;
}
