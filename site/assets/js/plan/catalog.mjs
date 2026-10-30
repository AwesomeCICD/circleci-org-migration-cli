// The step catalog of the plan builder: every step a migration can contain, and the
// ordered list of step ids of each scenario.
//
// Pure data and string building, no DOM. The three runbooks in docs/migrations/ are the
// source of truth: each scenario below lists its steps in the runbook's order, each step
// carries the runbook step it comes from (RUNBOOK), and the commands are the runbook's
// commands. The test suite pins the order to the runbook headings and checks that every
// --flag emitted exists in docs/cli.
//
// A step builder receives the normalized context `c` (see plan.mjs, buildContext) and
// returns a step, or null when the chosen options leave the step out.

export const BIN = 'circleci-migrate';
const CONTEXT_NAME = 'migration-secrets';

// ---------------------------------------------------------------------------
// Command formatting
// ---------------------------------------------------------------------------

// The runbooks use plain shell variables for the orgs ($SOURCE_ORG) in both shells.
export function ref(c, name) {
  return `"$${name}"`;
}

function q(value) {
  return `"${value}"`;
}

// One command. `head` is e.g. "circleci-migrate sync"; `parts` are flag groups such as
// '--manifest manifest.json'. bash gets one flag group per continuation line when the
// command is long; PowerShell always gets a single line (no continuation characters).
export function fmt(c, head, parts) {
  const all = [head, ...parts.filter(Boolean)];
  const one = all.join(' ');
  if (c.shell === 'powershell' || one.length <= 76) return one;
  return all.map((p, i) => (i === 0 ? p : '  ' + p) + (i < all.length - 1 ? ' \\' : '')).join('\n');
}

function hostParts(c) {
  return c.hostUrl ? [`--host ${q(c.hostUrl)}`] : [];
}

function cli(c, sub, parts) {
  return fmt(c, `${BIN} ${sub}`, [...parts, ...hostParts(c)]);
}

// The runbooks' mapping.json forms: a here-doc in bash, ConvertTo-Json in PowerShell.
// github_org is written only when the repos move and the source GitHub org is known (a gh/ source).
function writeMapping(c) {
  const gh = c.o.repoMove && c.srcGithubOrg && c.destGithubOrg ? { from: c.srcGithubOrg, to: c.destGithubOrg } : null;
  if (c.shell === 'powershell') {
    const extra = gh ? `; github_org = @{ from = "${gh.from}"; to = "${gh.to}" }` : '';
    return `@{ org = @{ from = $SOURCE_ORG; to = $DEST_ORG }${extra} } | ConvertTo-Json | Set-Content mapping.json`;
  }
  const extra = gh ? `, "github_org": { "from": "${gh.from}", "to": "${gh.to}" }` : '';
  return `cat > mapping.json <<EOF\n{ "org": { "from": "$SOURCE_ORG", "to": "$DEST_ORG" }${extra} }\nEOF`;
}

// Tokens are environment variables in both shells; the org names are plain shell variables.
function setVar(c, name, value, { env = false } = {}) {
  if (c.shell === 'powershell') return env ? `$env:${name} = "${value}"` : `$${name} = "${value}"`;
  return `export ${name}="${value}"`;
}

// ---------------------------------------------------------------------------
// Shared flag groups
// ---------------------------------------------------------------------------

const destGh = (c) => (c.o.repoMove && c.destGithubOrg ? [`--dest-github-org ${q(c.destGithubOrg)}`] : []);

// The main sync: `--skip-orb --skip-runner` and the GitHub org follow the runbook step that uses it.
function syncParts(c, { skipPipelines = false, skipOrbRunner = false, githubOrg = false, webhooks = false, apply = false, tf = false } = {}) {
  const parts = ['--manifest manifest.json', '--mapping mapping.json'];
  if (skipPipelines) parts.push('--skip-pipelines');
  if (skipOrbRunner) parts.push('--skip-orb', '--skip-runner');
  if (!c.o.contexts) parts.push('--skip-contexts');
  if (githubOrg) parts.push(...destGh(c));
  if (tf) parts.push('--skip-terraform-managed');
  if (apply) parts.push('--apply');
  if (webhooks && c.o.webhooks) parts.push('--webhook-secrets-out webhook-secrets.json');
  return parts;
}

function transferCmd(c, { enableTrigger, apply }) {
  const parts = [
    '--manifest manifest.json',
    '--mapping mapping.json',
    `--dest-org-id ${ref(c, 'DEST_ORG_ID')}`,
    `--dest-token-context ${CONTEXT_NAME}`,
  ];
  if (!c.o.contexts) parts.push('--skip-contexts');
  if (c.o.projectVars) parts.push('--include-project-vars');
  if (c.o.sshKeys) parts.push('--include-ssh-keys');
  if (c.o.restrictions && c.o.contexts) parts.push('--remove-restrictions');
  if (enableTrigger) parts.push('--enable-trigger');
  if (apply) parts.push('--apply');
  if (c.hostUrl) parts.push(`--dest-host ${q(c.hostUrl)}`);
  return cli(c, 'secrets transfer', parts);
}

// Preview, then apply: the runbooks' pairs for cutover.
function cutoverPair(c, flags, { mapping = true } = {}) {
  const base = ['--manifest manifest.json', ...(mapping ? ['--mapping mapping.json'] : [])];
  return [cli(c, 'cutover', [...base, ...flags]), cli(c, 'cutover', [...base, ...flags, '--apply'])];
}

const undoCmd = (c) => cli(c, 'cutover', ['--manifest manifest.json', '--mapping mapping.json', '--undo', '--apply']);

const destType = (c) => (c.destKind === 'app' ? 'standalone' : 'oauth');

// ---------------------------------------------------------------------------
// Runbook anchors: where each plan step lives in docs/migrations/<type>.md.
// The anchors are the ids Hugo generates from the "#### N. Title" headings (checked by the tests).
// ---------------------------------------------------------------------------

const PREP = 'before-you-start';
export const RUNBOOK = {
  'oauth-to-app': {
    'before-you-start': PREP, install: PREP,
    doctor: '1-run-preflight', export: '2-export-the-source', mapping: '3-write-mappingjson',
    'secrets-context': '4-create-the-migration-secrets-context', 'support-plan-ask': '5-ask-circleci-support-to-plan-the-moves',
    'dry-run': '6-dry-run-the-cutover-commands', 'sync-projects': '7-freeze-merges-and-create-the-destination-projects',
    transfer: '8-transfer-the-secrets', 'pause-source': '9-pause-the-source',
    'move-app': '10-move-the-github-app', 'support-plan-move': '10-move-the-github-app',
    'sync-definitions': '11-add-pipeline-definitions-and-triggers',
    'orb-support': '12-move-orbs-and-runners-only-if-you-use-them', 'orb-sync': '12-move-orbs-and-runners-only-if-you-use-them',
    'runner-ns-support': '12-move-orbs-and-runners-only-if-you-use-them', runners: '12-move-orbs-and-runners-only-if-you-use-them',
    'runner-register': '12-move-orbs-and-runners-only-if-you-use-them',
    'enable-dest': '13-enable-the-destination', smoke: 'verify', validate: 'verify',
    'webhook-handoff': 'verify', 'rotate-cleanup': 'verify', 'oidc-trust': 'verify', 'external-pins': 'verify',
  },
  'oauth-to-oauth': {
    'before-you-start': PREP, install: PREP,
    doctor: '1-run-preflight', export: '2-export-the-source', mapping: '3-write-mappingjson',
    'secrets-context': '4-create-the-migration-secrets-context', 'dry-run-sync': '5-dry-run-the-sync',
    'sync-apply': '6-create-the-destination-contexts-and-projects', 'mapping-generate': '7-match-source-projects-to-destination-projects',
    transfer: '8-freeze-merges-then-transfer-the-secrets', 'pause-source': '9-pause-the-source', 'follow-dest': '10-enable-the-destination',
    'orb-support': '11-move-orbs-only-if-you-publish-orbs', 'orb-sync': '11-move-orbs-only-if-you-publish-orbs',
    'runner-ns-support': '12-recreate-runners-only-if-you-use-them', runners: '12-recreate-runners-only-if-you-use-them',
    'runner-register': '12-recreate-runners-only-if-you-use-them',
    smoke: 'verify', validate: 'verify', 'webhook-handoff': 'verify', 'cleanup-extra': 'verify', 'rotate-cleanup': 'verify', 'external-pins': 'verify',
  },
  'app-to-app': {
    'before-you-start': PREP, install: PREP,
    doctor: '1-run-preflight', export: '2-export-the-source', mapping: '3-write-mappingjson',
    'secrets-context': '4-create-the-migration-secrets-context', 'dry-run': '5-dry-run-the-cutover-commands',
    'sync-projects': '6-freeze-merges-and-create-the-destination-projects', transfer: '7-transfer-the-secrets',
    'pause-source': '8-pause-the-source', 'move-app': '9-connect-the-repos-to-the-destination',
    'connect-repos': '9-connect-the-repos-to-the-destination', 'pause-enable': '9-connect-the-repos-to-the-destination',
    'sync-definitions': '10-add-pipeline-definitions-and-triggers', 'sync-apply': '9-connect-the-repos-to-the-destination',
    'orb-support': '11-move-orbs-and-runners-only-if-you-use-them', 'orb-sync': '11-move-orbs-and-runners-only-if-you-use-them',
    'runner-ns-support': '11-move-orbs-and-runners-only-if-you-use-them', runners: '11-move-orbs-and-runners-only-if-you-use-them',
    'runner-register': '11-move-orbs-and-runners-only-if-you-use-them', 'oidc-trust': '11-move-orbs-and-runners-only-if-you-use-them',
    'enable-dest': '12-enable-the-destination', smoke: 'verify', validate: 'verify',
    'webhook-handoff': 'verify', 'cleanup-extra': 'verify', 'rotate-cleanup': 'verify', 'external-pins': 'verify',
  },
};

const TERRAFORM_LINK = 'docs/reference/terraform/';

// Path of a step's link, relative to the site root (the UI prefixes "../" from /plan/).
export function stepLink(type, id) {
  if (id === 'terraform-generate' || id === 'terraform-apply') return { path: TERRAFORM_LINK, label: 'Terraform output' };
  const anchor = RUNBOOK[type]?.[id];
  if (!anchor) return null;
  const n = /^(\d+)-/.exec(anchor);
  const label = n ? `Runbook step ${n[1]}` : anchor === PREP ? 'Runbook: before you start' : 'Runbook: verify and clean up';
  return { path: `docs/migrations/${type}/#${anchor}`, label };
}

// ---------------------------------------------------------------------------
// Step builders
// ---------------------------------------------------------------------------

const S = {};

S['before-you-start'] = (c) => {
  const common = [
    'Create one personal API token for the source org and one for the destination org (User Settings, Personal API Tokens). Each token\'s user must be an organization admin of the org it acts on.',
    'Paste each token over its <paste token> placeholder in the environment block, in the shell you will use for every command. This page never asks for, stores or sends a token.',
  ];
  const per = {
    'oauth-to-app': [
      'You can install and uninstall the CircleCI GitHub App in the GitHub org.',
      'The destination circleci/<id> org exists. Copy its Organization ID (Organization Settings, Overview) into DEST_ORG_ID.',
      'Create a GitHub personal access token with repo read access. Any OAuth project without a captured GitHub App pipeline needs it to look up the repo ID, and preflight fails without it.',
    ],
    'oauth-to-oauth': [
      'The destination org already exists. Copy its Organization ID (Organization Settings, Overview) into DEST_ORG_ID.',
      'Every source project has a checkout key (Project Settings, SSH Keys) or a GitHub App pipeline. The transfer pipeline needs one of them.',
      ...(c.o.repoMove ? ['Repos are moving to a new GitHub org: create a GitHub token with repo read access, and schedule the secrets transfer before the repos move.'] : []),
      'Agree a freeze window with the team.',
    ],
    'app-to-app': [
      'The destination circleci/<id> org exists. Copy its Organization ID (Organization Settings, Overview) into DEST_ORG_ID.',
      'Every user whose org role should move is already a member of the destination org. Roles apply only to existing members, matched by email. Pass --skip-ciam to sync to leave roles and groups alone.',
      ...(c.o.repoMove
        ? ['Repos are moving to a new GitHub org: create a GitHub token with repo read access, and install the CircleCI GitHub App in the new GitHub org from the destination CircleCI org.']
        : ['Same GitHub org: you can install and uninstall the CircleCI GitHub App in it. A GitHub org connects to one CircleCI org at a time.']),
    ],
  }[c.type];
  return {
    phase: 'prep',
    kind: 'ui',
    title: 'Before you start: tokens, orgs and access',
    why: 'These are the checklist items of the runbook; missing one costs you a failed step on cutover day.',
    details: [...common, ...per],
    commands: [],
    check: 'Every variable in the environment block is set in this shell, and you are an admin of both orgs.',
  };
};

S.install = (c) => ({
  phase: 'prep',
  kind: 'cli',
  title: 'Install the CLI',
  why: 'Every command in this plan is circleci-migrate; use the latest release.',
  details: ['Install from the Install page (Homebrew, a release download, or the install script), then confirm the version.'],
  commands: [`${BIN} version`],
  check: 'It prints v0.19.0 or later.',
});

S.doctor = (c) => ({
  phase: 'prep',
  kind: 'cli',
  title: 'Run preflight',
  why: 'Catch bad tokens and unreachable orgs before you touch anything. Doctor is read-only.',
  commands: [cli(c, 'doctor', [`--source-org ${ref(c, 'SOURCE_ORG')}`, `--dest-org ${ref(c, 'DEST_ORG')}`])],
  check: 'It exits 0 with no hard failures.',
});

S.export = (c) => ({
  phase: 'prep',
  kind: 'cli',
  title: 'Export the source',
  why:
    c.type === 'oauth-to-app'
      ? 'Export records names and structure, never secret values, and is read-only. Run it while the GitHub App is still installed on the source, so existing App pipelines are captured.'
      : c.type === 'app-to-app'
        ? 'Export records names and structure, never secret values, and is read-only. Run it while the source still has its pipelines, so the definitions are captured.'
        : 'Export records names and structure, never secret values, and is read-only.',
  details:
    c.type === 'oauth-to-oauth'
      ? ['Repos that were never followed in CircleCI are missing from the export. Add --follow-all (needs GITHUB_TOKEN) to follow them first.']
      : [],
  commands: [
    cli(c, 'export', [
      `--source-org ${ref(c, 'SOURCE_ORG')}`,
      c.type === 'oauth-to-app' ? `--dest-org ${ref(c, 'DEST_ORG')}` : '',
      '--output manifest.json',
      '--report migration-report.md',
      c.o.runners ? `--runner-namespace ${q(c.runnerNs)}` : '',
      c.o.orbs ? `--orb-namespace ${q(c.orbNs)}` : '',
    ]),
  ],
  check: 'Read migration-report.md in full and note the counts of contexts, projects and project env vars.',
});

S.mapping = (c) => ({
  phase: 'prep',
  kind: 'cli',
  title: 'Write mapping.json',
  why:
    c.type === 'oauth-to-app'
      ? 'The mapping tells sync which org to write to. The org line is all you need, because App projects are matched by name.'
      : c.type === 'app-to-app'
        ? 'Without org.to, sync writes to your source org. The org line is all you need, because projects are matched by name.'
        : 'Without org.to, sync writes to your source org. The mapping prevents that.',
  details:
    c.o.repoMove && !c.srcGithubOrg
      ? ['The repos move to a new GitHub org: you can also add "github_org": { "from": "<old GitHub org>", "to": "' + c.destGithubOrg + '" } to the file. The --dest-github-org flag below does the same.']
      : [],
  commands: [writeMapping(c)],
  check: 'mapping.json has org.to set to the destination.',
});

S['secrets-context'] = (c) => ({
  phase: 'prep',
  kind: 'ui',
  title: `Create the ${CONTEXT_NAME} context`,
  why: 'Secret values can only be read inside a pipeline. The transfer pipeline runs in the source org and gets the destination token from a context.',
  details: [
    `In the source org, go to Organization Settings, Contexts and create ${CONTEXT_NAME} with one variable, CIRCLECI_DEST_TOKEN, set to the destination token.`,
    'Anyone in the source org who can create pipelines can read this token. Use a token scoped to the destination org and delete it when you finish.',
  ],
  commands: [],
  check: 'The context exists in the source org with that one variable.',
});

S['support-plan-ask'] = (c) => ({
  phase: 'prep',
  kind: 'support',
  title: 'Book the plan move with CircleCI support',
  why: 'The plan, and any same-name orb namespace, cannot move through the API, so book support before cutover day.',
  details: [
    `Tell support the destination org (${c.dst}) and the date you will move the GitHub App.`,
    'The destination org must be on the Free plan and not share another org\'s plan.',
  ],
  commands: [],
  check: 'Support confirms they will run it after the App moves.',
});

// oauth-to-app step 6 and app-to-app step 5: every cutover command as a dry run.
S['dry-run'] = (c) => {
  const newGh = c.type === 'app-to-app' && c.o.repoMove;
  const syncCmd = newGh
    ? cli(c, 'sync', syncParts(c, { skipOrbRunner: true, githubOrg: true }))
    : cli(c, 'sync', syncParts(c, { skipPipelines: true, skipOrbRunner: true, githubOrg: true }));
  const commands = [syncCmd];
  if (c.xfer.any) commands.push(transferCmd(c, { enableTrigger: false, apply: false }));
  if (c.o.pauseSource) commands.push(cli(c, 'cutover', ['--manifest manifest.json', '--mapping mapping.json', ...(newGh ? ['--pause-source', '--enable-dest'] : ['--pause-source'])]));
  return {
    phase: 'prep',
    kind: 'cli',
    title: 'Dry-run the cutover commands',
    why: 'Every command below is a dry run until you add --apply. Read each plan now.',
    commands,
    check: newGh
      ? 'No unexplained "error" lines. Project lines in the transfer plan can show SKIP until the destination projects exist; that is expected now.'
      : 'No unexplained "error" lines. Every project shows SKIP in the transfer plan because the destination projects do not exist yet. That is expected.',
  };
};

S['dry-run-sync'] = (c) => ({
  phase: 'prep',
  kind: 'cli',
  title: 'Dry-run the sync',
  why: 'Without --apply, nothing is written. Read the plan before you apply.',
  commands: [cli(c, 'sync', syncParts(c))],
  check: 'No unexpected "error" lines. "manual" lines for secret values are expected, because those move in the transfer step. Project counts match the report.',
});

S['terraform-generate'] = (c) => {
  const after = c.type !== 'oauth-to-oauth';
  const commands = [];
  if (after) {
    const snap = cli(c, 'sync', ['--manifest manifest.json', '--mapping mapping.json', '--json']);
    commands.push(c.shell === 'powershell' ? `${snap} | Out-File -FilePath sync-result.json -Encoding ascii` : `${snap} > sync-result.json`);
  }
  commands.push(
    cli(c, 'terraform generate', [
      '--manifest manifest.json',
      '--mapping mapping.json',
      `--dest-org-id ${ref(c, 'DEST_ORG_ID')}`,
      `--dest-org-type ${destType(c)}`,
      c.o.runners ? `--dest-runner-namespace ${q(c.destRunnerNs)}` : '',
      ...(after ? ['--import-existing', '--existing sync-result.json'] : []),
      '--out ./terraform/',
    ]),
  );
  return {
    phase: after ? 'after' : 'prep',
    kind: 'cli',
    title: after ? 'Generate Terraform for the destination you just built' : 'Generate Terraform for the destination',
    why: after
      ? 'The CLI already created the destination, so Terraform adopts those resources with import blocks instead of creating them twice.'
      : 'Terraform manages contexts, projects, webhooks and runner classes as code; the CLI then fills the gaps Terraform cannot manage.',
    commands,
    check: 'The ./terraform/ directory holds the .tf files and GAPS.md. Read GAPS.md: it lists what Terraform does not manage.',
  };
};

S['terraform-apply'] = (c) => {
  const after = c.type !== 'oauth-to-oauth';
  return {
    phase: after ? 'after' : 'cutover',
    kind: 'cli',
    title: after ? 'Apply the Terraform, then fill the gaps with the CLI' : 'Apply the Terraform',
    why: after
      ? 'Terraform takes ownership of the destination; the gap-fill sync skips what Terraform now manages.'
      : 'Terraform creates the contexts, projects and runner classes it manages. The sync after it fills the gaps.',
    commands: [
      'cd ./terraform/',
      setVar(c, 'TF_VAR_circleci_api_token', '<paste token>', { env: true }),
      'terraform init',
      'terraform plan',
      'terraform apply',
      'cd ..',
      ...(after ? [cli(c, 'sync', ['--manifest manifest.json', '--mapping mapping.json', '--apply', '--skip-terraform-managed'])] : []),
    ],
    check: after
      ? 'terraform plan shows only imports and no destroys, apply succeeds, and the sync reports no error lines. GAPS.md lists what stays manual.'
      : 'terraform plan shows only creates you expect, and apply succeeds.',
  };
};

// ---- cutover ----

// oauth-to-app step 7, app-to-app step 6.
S['sync-projects'] = (c) => ({
  phase: 'cutover',
  kind: 'cli',
  title: 'Freeze merges and create the destination projects',
  why: 'Project env vars are written into the destination projects, so they must exist first. This creates projects, contexts and settings, but no pipelines or triggers, and needs no GitHub token.',
  details: [
    'Stop merges in the source repos (branch protection, a merge freeze, or an announcement) so nothing lands between the transfer and the switch.',
    'If anything changed in the source org after you exported, run the export command again first.',
  ],
  commands: [cli(c, 'sync', syncParts(c, { skipPipelines: true, skipOrbRunner: true, githubOrg: true, apply: true }))],
  check: c.xfer.any
    ? 'Every project reports "skipped: --skip-pipelines", and the team confirms the freeze. Re-run the transfer dry run from the prep step: no project shows SKIP now.'
    : 'Every project reports "skipped: --skip-pipelines", and the team confirms the freeze.',
});

S.transfer = (c) => {
  const isOauth = c.type === 'oauth-to-oauth';
  const commands = isOauth
    ? [transferCmd(c, { enableTrigger: true, apply: false }), transferCmd(c, { enableTrigger: true, apply: true })]
    : [transferCmd(c, { enableTrigger: true, apply: true })];
  return {
    phase: 'cutover',
    kind: 'cli',
    title: isOauth ? 'Freeze merges, then transfer the secrets' : 'Transfer the secrets',
    why: 'This runs one pipeline per source project and writes each value straight to the destination over TLS. Nothing touches disk.',
    details: [
      ...(isOauth ? ['Freeze merges in the source repos first, so the values you copy are the final ones.'] : []),
      'It must run before you pause the source, because a paused project drops the pipeline.',
      ...(c.o.repoMove && c.type !== 'oauth-to-app' ? ['Repos are moving to a new GitHub org: run it before they move, because the pipelines check out code from the old org.'] : []),
    ],
    commands,
    check: isOauth
      ? 'The dry run lists no SKIP. The apply reports every pipeline succeeded.'
      : 'The log reports every context and project pipeline succeeded.',
  };
};

S['mapping-generate'] = (c) => ({
  phase: 'cutover',
  kind: 'cli',
  title: 'Match source projects to destination projects',
  why: 'Secret transfer routes project values by slug, and OAuth slugs need an explicit entry per project. This overwrites mapping.json and needs the destination token.',
  details: [...(c.o.repoMove ? ['Re-add the github_org line to mapping.json afterwards if you had one.'] : [])],
  commands: [cli(c, 'mapping generate', ['--manifest manifest.json', `--dest-org ${ref(c, 'DEST_ORG')}`, '--output mapping.json'])],
  check: 'The report lists each source project under "matched", with none "unmatched".',
});

S['sync-apply'] = (c) => {
  const oauth = c.type === 'oauth-to-oauth';
  // For OAuth the runbook adds --dest-github-org to this step only.
  const parts = oauth
    ? syncParts(c, { githubOrg: true, apply: true, webhooks: true, tf: c.o.terraform })
    : syncParts(c, { skipOrbRunner: true, githubOrg: true, apply: true, webhooks: true });
  return {
    phase: 'cutover',
    kind: 'cli',
    title: oauth ? 'Create the destination contexts and projects' : 'Create the destination projects, definitions and triggers',
    why: oauth
      ? 'This creates contexts, projects, settings, webhooks and schedules in the destination. OAuth projects are created paused: no webhook, no builds.'
      : 'The repos are connected to the destination GitHub App, so definitions can be created now. Triggers are created disabled.',
    details: [
      oauth
        ? 'Answer N at the "Enable builds" prompt.'
        : 'Answer N at the "Enable N trigger(s)" prompt: the cutover command enables them later.',
      ...(c.o.webhooks ? ['New webhook URLs and secrets go to webhook-secrets.json (mode 0600).'] : []),
      ...(oauth && c.o.repoMove ? ['The repos are in a new GitHub org, so --dest-github-org is set.'] : []),
    ],
    commands: [cli(c, 'sync', parts)],
    check: oauth
      ? 'Re-run the dry run from the prep step. Every line now says "exists".'
      : 'No line says "connect the repo to the GitHub App". If one does, connect the repo and re-run.',
  };
};

S['pause-source'] = (c) => ({
  phase: 'cutover',
  kind: 'cli',
  title: 'Pause the source',
  why:
    c.type === 'oauth-to-oauth'
      ? 'Pausing stops new builds in the source so both orgs never build the same push. Every change is recorded in cutover-state.json, so you can undo it.'
      : 'Pausing stops builds in the source and records every change in cutover-state.json, so you can undo it. Preview first.',
  details: [
    c.type === 'oauth-to-oauth' ? 'Add --pause-org to pause the whole source org.' : 'Keep cutover-state.json until the cutover is confirmed.',
    'The first command is a preview. Run the second to apply it.',
  ],
  commands: cutoverPair(c, ['--pause-source']),
  check: c.type === 'oauth-to-oauth'
    ? 'Every source project is listed as paused.'
    : 'Every source project is listed as paused, and cutover-state.json exists.',
  reversible: true,
  undo: [undoCmd(c)],
});

S['move-app'] = (c) => {
  if (c.type === 'oauth-to-app') {
    return {
      phase: 'cutover',
      kind: 'ui',
      title: 'Move the GitHub App',
      why: 'A GitHub org connects to one CircleCI org at a time, so the destination cannot see your repositories until the App moves.',
      details: [
        'Uninstall the CircleCI GitHub App from the source org\'s connection.',
        `Install it from the destination org on ${c.o.repoMove && c.destGithubOrg ? `the GitHub org ${c.destGithubOrg}` : 'the same GitHub org'}, with every migrating repo selected.`,
        'Each user opens the new org and clicks Authorize, then Refresh permissions if Projects is empty.',
      ],
      commands: [],
      check: 'The destination org lists every repo as connected. A repo that is not connected still gets a project, but its definition is created only after you connect it and re-run sync in the next step.',
      pointOfNoReturn: true,
      ponrNote:
        'Uninstalling deletes the source org\'s GitHub App pipelines and triggers, and the export is the only record of them. cutover --undo can no longer re-enable them.',
    };
  }
  // app-to-app, same GitHub org.
  return {
    phase: 'cutover',
    kind: 'ui',
    title: 'Connect the repos to the destination (move the GitHub App)',
    why: 'Definitions can only be created for repos the destination GitHub App can see, and a GitHub org connects to one CircleCI org at a time.',
    details: [
      'Uninstall the CircleCI GitHub App from the source org\'s connection, then install it from the destination org with every migrating repo selected.',
      'Users click Authorize, then Refresh permissions if Projects is empty.',
    ],
    commands: [],
    check: 'The destination org lists every repo as connected.',
    pointOfNoReturn: true,
    ponrNote:
      'Same GitHub org only. Uninstalling deletes the source org\'s GitHub App pipelines and triggers, and the export is the only record of them. cutover --undo can no longer re-enable them.',
  };
};

// app-to-app, repos in a new GitHub org: nothing is uninstalled.
S['connect-repos'] = (c) => ({
  phase: 'cutover',
  kind: 'ui',
  title: 'Connect the repos to the destination',
  why: 'Definitions can only be created for repos the destination GitHub App can see.',
  details: [
    'Confirm every repo is connected in the destination org\'s GitHub App installation, in the new GitHub org. Nothing is uninstalled.',
    'Users click Authorize, then Refresh permissions if Projects is empty.',
  ],
  commands: [],
  check: 'The destination org lists every repo as connected.',
});

S['support-plan-move'] = (c) => ({
  phase: 'cutover',
  kind: 'support',
  title: `Ask CircleCI support to move the plan to ${c.dst}`,
  why: 'Plan and billing move through support, not the API. Carry on with the next step while they work.',
  details: ['Reply on the request you opened earlier, and say the GitHub App move is done.'],
  commands: [],
  check: 'Support confirms the plan request.',
});

S['sync-definitions'] = (c) => ({
  phase: 'cutover',
  kind: 'cli',
  title: 'Add pipeline definitions and triggers',
  why: c.type === 'oauth-to-app'
    ? 'The App is connected, so definitions can be created. Triggers are created disabled.'
    : 'Triggers are created disabled.',
  details: [
    c.type === 'oauth-to-app'
      ? 'Answer N at the "Enable N trigger(s)" prompt. cutover enables them in the last step.'
      : 'Answer N at the "Enable N trigger(s)" prompt, because cutover enables them in the last step.',
    ...(c.o.webhooks ? ['New webhook URLs and secrets go to webhook-secrets.json (mode 0600).'] : []),
  ],
  commands: [cli(c, 'sync', syncParts(c, { skipOrbRunner: true, githubOrg: true, apply: true, webhooks: true }))],
  check: 'No line says "connect the repo to the GitHub App". If one does, connect the repo and re-run.',
});

S['orb-support'] = (c) => ({
  phase: 'cutover',
  kind: 'support',
  title: `Ask CircleCI support to move the orb namespace "${c.orbNs}" to ${c.dst}`,
  why: 'A namespace belongs to one org, so it is moved, not recreated; the orbs move with it and no config reference changes.',
  details: ['Ask for a move of ownership, not a rename: a rename does not free the old name.'],
  commands: [],
  check: 'Support confirms the namespace now belongs to the destination org.',
});

S['orb-sync'] = (c) => ({
  phase: 'cutover',
  kind: 'cli',
  title: `Republish the orbs into the "${c.destOrbNs}" namespace`,
  why: 'For a different namespace, sync republishes every stable version, and you rewrite your orb references.',
  commands: [
    cli(c, 'sync', ['--manifest manifest.json', '--mapping mapping.json', '--only orb', `--dest-orb-namespace ${q(c.destOrbNs)}`, '--apply']),
  ],
  check: 'validate lists the orbs (see the verify step). Rewrite the orb references in your configs to the new namespace.',
});

S['runner-ns-support'] = (c) => ({
  phase: 'cutover',
  kind: 'support',
  title: `Ask CircleCI support to move the runner namespace "${c.runnerNs}" to ${c.dst}`,
  why: 'Keeping the namespace name means the namespace itself moves; support does that, not the CLI.',
  details: ['After a namespace move the classes stay attributed to the source: delete the copies, re-run the runner sync, then issue new tokens.'],
  commands: [],
  check: 'Support confirms the namespace now belongs to the destination org.',
});

S.runners = (c) => ({
  phase: 'cutover',
  kind: 'cli',
  title: 'Recreate the runner resource classes',
  why: 'Resource classes are recreated, but they do not follow a namespace move: each stays attributed to the org that created it.',
  details: [
    c.destRunnerNs === c.runnerNs
      ? 'A class that sync reports as "exists but is attributed to another org" must be deleted from the source (or removed by support) before this re-run can create it.'
      : 'Your configs name resource classes as <namespace>/<class>. Rewrite every resource_class: value to the new namespace.',
  ],
  commands: [
    cli(c, 'sync', ['--manifest manifest.json', '--mapping mapping.json', '--only runner', `--dest-runner-namespace ${q(c.destRunnerNs)}`, '--apply']),
  ],
  check: 'Each resource class exists in the destination namespace, with no 409 or "attributed to another org" lines left.',
});

S['runner-register'] = (c) => ({
  phase: 'cutover',
  kind: 'cli',
  title: 'Register the runners with new tokens',
  why: 'Runner instances never move, and registration tokens do not transfer; only the resource-class definitions do.',
  details: [
    'Run the token command once per resource class, then put the new token in each runner\'s config and restart the agent.',
    'The same token can be created in the web app: Organization Settings, Self-hosted Runners, your resource class, Add Token.',
  ],
  commands: [`circleci runner token create <resource-class> "<nickname>"`],
  check: 'Runners register against the destination classes and show online.',
});

S['oidc-trust'] = (c) => ({
  phase: c.type === 'oauth-to-app' ? 'after' : 'cutover',
  kind: 'ui',
  title: 'Update cloud trust policies, allow-lists and SSO',
  why: 'Anything that names the source org\'s ID or addresses will reject the destination until you update it. Skip what you do not use.',
  details: [
    'OIDC: update cloud trust policies for the new org ID.',
    'Allow-lists: update any IP allowlists.',
    'SSO: recreate it in the destination org; it cannot be automated. New SSO users default to Viewer: set Contributor for people who trigger pipelines.',
  ],
  commands: [],
  check: 'A job in the destination can assume its cloud role.',
});

S['enable-dest'] = (c) => ({
  phase: 'cutover',
  kind: 'cli',
  title: 'Enable the destination',
  why: c.o.pauseSource
    ? 'This enables only the triggers that were enabled in the source.'
    : 'This enables the destination triggers that already exist. The source is not paused, so both orgs build until you stop the source.',
  details: [
    c.o.pauseSource
      ? 'Do not run sync --apply --yes here: it enables triggers without pausing the source, so both orgs build.'
      : 'Without a pause, a sync re-run enables the triggers that are still disabled.',
    ...(c.o.pauseSource ? ['The first command is a preview. Run the second to apply it.'] : []),
  ],
  commands: c.o.pauseSource
    ? cutoverPair(c, ['--enable-dest'])
    : [cli(c, 'sync', ['--manifest manifest.json', '--mapping mapping.json', '--apply', '--yes', '--enable-existing-triggers'])],
  check: 'No project reports "manual". If one does, enable that trigger in Project Settings, Triggers.',
  reversible: c.o.pauseSource,
  undo: c.o.pauseSource ? [undoCmd(c)] : undefined,
});

// app-to-app, repos in a new GitHub org: pause and enable in one reversible command.
S['pause-enable'] = (c) => ({
  phase: 'cutover',
  kind: 'cli',
  title: 'Pause the source and enable the destination',
  why: 'The definitions and triggers already exist, so one reversible command turns the source off and the destination on, and records both in cutover-state.json.',
  details: ['The first command is a preview. Run the second to apply it. Run the transfer first.'],
  commands: cutoverPair(c, ['--pause-source', '--enable-dest']),
  check: 'Source projects report paused and destination triggers report enabled; a trigger that was disabled in the source stays disabled.',
  reversible: true,
  undo: [undoCmd(c)],
});

// oauth-to-oauth: pause first, then follow.
S['follow-dest'] = (c) => ({
  phase: 'cutover',
  kind: 'cli',
  title: 'Enable the destination',
  why: 'Following each project installs the GitHub webhook and deploy key, so pushes start building in the destination.',
  details: [
    '--yes skips the confirmation prompt. cutover --enable-dest does nothing for an OAuth destination, which is why the source is paused first.',
    ...(c.o.pauseSource ? [] : ['The source is not paused, so both orgs build every push until you stop the source.']),
  ],
  commands: [cli(c, 'sync', ['--manifest manifest.json', '--mapping mapping.json', '--apply', '--yes'])],
  check: 'The destination Projects page shows every project as followed.',
});

S.smoke = (c) => ({
  phase: 'cutover',
  kind: 'ui',
  title: 'Smoke test: push a commit',
  why: 'A real pipeline in the destination proves the trigger, the context and the secrets work together.',
  details: ['Push a commit to a migrated repo.'],
  commands: [],
  check: 'A pipeline starts in the destination org, runs green, and a job can read a context variable.',
});

S.validate = (c) => ({
  phase: 'cutover',
  kind: 'cli',
  title: 'Validate parity',
  why: 'Validate exports both orgs read-only and reports what matched, what is missing and what needs a person.',
  commands: [
    cli(c, 'validate', [
      `--source-org ${ref(c, 'SOURCE_ORG')}`,
      `--dest-org ${ref(c, 'DEST_ORG')}`,
      '--mapping mapping.json',
      c.xfer.any ? `--dest-token-context ${CONTEXT_NAME}` : '',
      c.o.runners ? `--dest-runner-namespace ${q(c.destRunnerNs)}` : '',
      c.o.orbs ? `--dest-orb-namespace ${q(c.destOrbNs)}` : '',
    ]),
  ],
  check: 'A clean report exits 0 and lists only expected "manual" items: SSO, role bindings and, where they apply, group context restrictions or per-project grants.',
});

// ---- after ----

S['webhook-handoff'] = (c) => ({
  phase: 'after',
  kind: 'ui',
  title: 'Give senders and receivers the new webhook values',
  why: 'CircleCI never returns a webhook secret, so every recreated webhook has a new one that the other side must learn.',
  details: [
    'Open webhook-secrets.json. For each outbound webhook, set the new signing secret in the system that receives it.',
    'For each inbound webhook trigger, give the sender the new URL.',
    'Delete webhook-secrets.json once every value is handed over: it holds credentials, and re-runs merge into it.',
  ],
  commands: [],
  check: 'A test event is delivered and verified in each receiving system.',
});

S['cleanup-extra'] = (c) => {
  if (c.type === 'oauth-to-oauth') {
    return {
      phase: 'after',
      kind: 'cli',
      title: 'Finish the settings that did not move',
      why: 'Group context restrictions and the danger flags are left for after validation, because they can freeze or break builds.',
      details: [
        'Recreate group context restrictions in the destination UI.',
        'If the source had drop_all_build_requests or require_context_group_restriction on, set them now with this command.',
      ],
      commands: [cli(c, 'sync', ['--manifest manifest.json', '--mapping mapping.json', '--include-danger-flags', '--apply'])],
      check: 'The destination org shows the restrictions and flags the source had.',
    };
  }
  return {
    phase: 'after',
    kind: 'ui',
    title: 'Recreate per-project role grants',
    why: 'Per-project role grants do not move, because the destination project IDs cannot be matched reliably.',
    details: ['Recreate each user and group grant on the destination project.'],
    commands: [],
    check: 'The people who could reach a project in the source can reach it in the destination.',
  };
};

S['rotate-cleanup'] = (c) => ({
  phase: 'after',
  kind: 'ui',
  title: 'Rotate secrets and remove what the migration left behind',
  why: 'Every value that moved was readable by a pipeline, so treat it as exposed and replace it.',
  details: [
    'Rotate every transferred secret, in the system that issued it, and update it in the destination.',
    ...(c.xfer.any ? [`Rotate the destination token and delete the ${CONTEXT_NAME} context.`] : []),
    'Revoke the personal API tokens you created for this migration.',
    'Delete cutover-state.json once you are sure you will not undo.',
  ],
  commands: [],
  check: 'No migration token is still active, and destination builds still pass with the rotated values.',
});

S['external-pins'] = (c) => ({
  phase: 'after',
  kind: 'ui',
  title: 'Update everything that points at the old org',
  why: 'Project slugs and IDs changed, so references to the old ones go stale.',
  details: [
    c.type === 'oauth-to-oauth'
      ? 'Update badges, Slack apps and required status checks to the new org.'
      : 'Update service catalogs, badges, Slack apps and required status checks for the new project slugs.',
    ...(c.type === 'oauth-to-app' ? ['Tell contributors that fork-PR builds no longer run on GitHub App projects.'] : []),
  ],
  commands: [],
  check: 'Nothing you rely on still references the source org.',
});

// ---------------------------------------------------------------------------
// Scenarios: which steps, in which order. The order is the runbook's.
// ---------------------------------------------------------------------------

export const SCENARIOS = {
  // docs/migrations/oauth-to-app.md, "Steps" 1-13, then "Verify".
  'oauth-to-app': {
    label: 'GitHub OAuth to GitHub App',
    blurb: 'A gh/ OAuth org moves to a circleci/ GitHub App org. The GitHub App moves with it.',
    steps: [
      'before-you-start', 'install',
      'doctor', 'export', 'mapping', 'secrets-context', 'support-plan-ask', 'dry-run',
      'sync-projects', 'transfer', 'pause-source', 'move-app', 'support-plan-move', 'sync-definitions',
      'orb-support', 'orb-sync', 'runner-ns-support', 'runners', 'runner-register', 'enable-dest',
      'smoke', 'validate',
      'terraform-generate', 'terraform-apply', 'webhook-handoff', 'rotate-cleanup', 'oidc-trust', 'external-pins',
    ],
  },
  // docs/migrations/oauth-to-oauth.md, "Steps" 1-12, then "Verify".
  'oauth-to-oauth': {
    label: 'GitHub or Bitbucket OAuth to OAuth',
    blurb: 'A gh/ or bb/ org moves to another gh/ or bb/ org, such as a rename, or repos moving to a new GitHub org.',
    steps: [
      'before-you-start', 'install',
      'doctor', 'export', 'mapping', 'secrets-context', 'terraform-generate', 'dry-run-sync',
      'terraform-apply', 'sync-apply', 'mapping-generate', 'transfer', 'pause-source', 'follow-dest',
      'orb-support', 'orb-sync', 'runner-ns-support', 'runners', 'runner-register',
      'smoke', 'validate',
      'webhook-handoff', 'cleanup-extra', 'rotate-cleanup', 'external-pins',
    ],
  },
  // docs/migrations/app-to-app.md, "Steps" 1-12, then "Verify". Same GitHub org: the phased order.
  // With repos in a NEW GitHub org (the repoMove option) the runbook's step 9 tip applies: see APP_NEW_GITHUB_ORG.
  'app-to-app': {
    label: 'GitHub App to GitHub App',
    blurb: 'A circleci/ org moves to another circleci/ org: standalone, GitHub App or GitLab.',
    steps: [
      'before-you-start', 'install',
      'doctor', 'export', 'mapping', 'secrets-context', 'dry-run',
      'sync-projects', 'transfer', 'pause-source', 'move-app', 'sync-definitions',
      'orb-support', 'orb-sync', 'runner-ns-support', 'runners', 'runner-register', 'oidc-trust', 'enable-dest',
      'smoke', 'validate',
      'terraform-generate', 'terraform-apply', 'webhook-handoff', 'cleanup-extra', 'rotate-cleanup', 'external-pins',
    ],
  },
};

// app-to-app with the repos in a new GitHub org (docs/migrations/app-to-app.md, tip under step 9): the App is
// connected up front, definitions and triggers are created with the projects, and one cutover command pauses
// the source and enables the destination. Nothing is uninstalled, so there is no point of no return.
const APP_NEW_GITHUB_ORG = [
  'before-you-start', 'install',
  'doctor', 'export', 'mapping', 'secrets-context', 'dry-run',
  'connect-repos', 'sync-apply', 'transfer',
  'orb-support', 'orb-sync', 'runner-ns-support', 'runners', 'runner-register', 'oidc-trust', 'pause-enable',
  'smoke', 'validate',
  'terraform-generate', 'terraform-apply', 'webhook-handoff', 'cleanup-extra', 'rotate-cleanup', 'external-pins',
];

// Steps that only exist for some option combinations.
const GUARDS = {
  'support-plan-ask': (c) => c.type === 'oauth-to-app',
  'support-plan-move': (c) => c.type === 'oauth-to-app',
  'secrets-context': (c) => c.xfer.any,
  transfer: (c) => c.xfer.any,
  'terraform-generate': (c) => c.o.terraform,
  'terraform-apply': (c) => c.o.terraform,
  'pause-source': (c) => c.o.pauseSource,
  'orb-support': (c) => c.o.orbs && c.orbSame,
  'orb-sync': (c) => c.o.orbs && !c.orbSame,
  'runner-ns-support': (c) => c.o.runners && c.runnerSame,
  runners: (c) => c.o.runners,
  'runner-register': (c) => c.o.runners,
  'webhook-handoff': (c) => c.o.webhooks,
};

export function stepIds(c) {
  const scenario = SCENARIOS[c.type];
  if (!scenario) return [];
  let list = c.type === 'app-to-app' && c.o.repoMove ? APP_NEW_GITHUB_ORG : scenario.steps;
  // Without a pause there is nothing to switch with cutover: a sync re-run enables the triggers.
  if (!c.o.pauseSource) list = list.map((id) => (id === 'pause-enable' ? 'enable-dest' : id));
  return list.filter((id) => {
    const guard = GUARDS[id];
    return guard ? Boolean(guard(c)) : true;
  });
}

export function buildSteps(c) {
  return stepIds(c)
    .map((id) => {
      const step = S[id](c);
      if (!step) return null;
      return { id, ...step, link: stepLink(c.type, id) ?? undefined };
    })
    .filter(Boolean);
}

export function envBlock(c) {
  const lines = [
    ['SOURCE_ORG', c.src, false],
    ['DEST_ORG', c.dst, false],
  ];
  if (c.destId) lines.push(['DEST_ORG_ID', c.destId, false]);
  lines.push(['CIRCLECI_SOURCE_TOKEN', '<paste token>', true], ['CIRCLECI_DEST_TOKEN', '<paste token>', true]);
  if (c.needsGithubToken) lines.push(['GITHUB_TOKEN', '<paste token>', true]);
  return lines.map(([k, v, isEnv]) => setVar(c, k, v, { env: isEnv })).join('\n');
}
