// Plan builder UI: renders the form, validates as you type, and shows the generated plan.
// No network calls, no external scripts. The only thing kept anywhere is the non-secret form
// state in the URL hash, so a plan link can be shared. Tokens are never asked for.

import {
  KIND_LABELS,
  MIGRATION_TYPES,
  OPTION_DEFS,
  PHASES,
  buildPlan,
  decodeState,
  defaultInput,
  encodeState,
  renderRunSheet,
} from './plan.mjs';

// ---------------------------------------------------------------------------
// Tiny DOM helper
// ---------------------------------------------------------------------------

function h(tag, props = {}, ...kids) {
  const node = document.createElement(tag);
  for (const [key, value] of Object.entries(props)) {
    if (value == null || value === false) continue;
    if (key === 'class') node.className = value;
    else if (key === 'text') node.textContent = value;
    else if (key.startsWith('on') && typeof value === 'function') node.addEventListener(key.slice(2), value);
    else node.setAttribute(key, value === true ? '' : String(value));
  }
  for (const kid of kids.flat()) {
    if (kid == null || kid === false) continue;
    node.append(kid.nodeType ? kid : document.createTextNode(String(kid)));
  }
  return node;
}

async function copyText(text) {
  try {
    await navigator.clipboard.writeText(text);
    return true;
  } catch {
    // Fall through to the legacy path (older browsers, or a page without clipboard permission).
  }
  try {
    const area = h('textarea', { 'aria-hidden': 'true', tabindex: '-1', style: 'position:fixed;left:-9999px;top:0' });
    area.value = text;
    document.body.append(area);
    area.select();
    const ok = document.execCommand('copy');
    area.remove();
    return ok;
  } catch {
    return false;
  }
}

function flash(button, ok, idle) {
  button.textContent = ok ? 'Copied' : 'Copy failed: select and copy by hand';
  button.dataset.state = ok ? 'done' : 'failed';
  setTimeout(() => {
    button.textContent = idle;
    delete button.dataset.state;
  }, 1800);
}

function copyButton(label, getText, extraClass = '') {
  const button = h('button', { type: 'button', class: `pb-btn pb-btn-small ${extraClass}`.trim() }, label);
  button.addEventListener('click', async () => flash(button, await copyText(getText()), label));
  return button;
}

function download(filename, text) {
  const url = URL.createObjectURL(new Blob([text], { type: 'text/markdown;charset=utf-8' }));
  const link = h('a', { href: url, download: filename, style: 'display:none' });
  document.body.append(link);
  link.click();
  link.remove();
  setTimeout(() => URL.revokeObjectURL(url), 1000);
}

// ---------------------------------------------------------------------------
// Mount
// ---------------------------------------------------------------------------

export function mount(root) {
  const state = { input: defaultInput(), touched: {} };
  const fields = {}; // field name -> { input, error, row }
  let uid = 0;
  const nextId = (p) => `pb-${p}-${++uid}`;

  // ---- form pieces ----

  function textField(field, label, { hint = '', placeholder = '', required = false, mono = true } = {}) {
    const id = nextId(field);
    const errId = `${id}-err`;
    const hintId = `${id}-hint`;
    const input = h('input', {
      id,
      type: 'text',
      class: `pb-input${mono ? ' pb-mono' : ''}`,
      autocomplete: 'off',
      autocapitalize: 'off',
      spellcheck: 'false',
      placeholder,
      'aria-describedby': hint ? `${hintId} ${errId}` : errId,
    });
    const error = h('p', { id: errId, class: 'pb-error', hidden: true });
    const suggest = h('span', { class: 'pb-suggest' });
    const row = h(
      'div',
      { class: 'pb-field' },
      h('label', { for: id, class: 'pb-label' }, label, required ? h('span', { class: 'pb-req' }, ' (required)') : ''),
      hint ? h('p', { id: hintId, class: 'pb-hint' }, hint) : null,
      input,
      error,
      suggest,
    );
    input.addEventListener('input', () => {
      state.input[field] = input.value;
      update();
    });
    input.addEventListener('blur', () => {
      state.touched[field] = true;
      update();
    });
    fields[field] = { input, error, row, suggest };
    return row;
  }

  function checkbox(def) {
    const id = nextId(def.key);
    const box = h('input', { id, type: 'checkbox', class: 'pb-check' });
    box.addEventListener('change', () => {
      state.input.options[def.key] = box.checked;
      update();
    });
    fields[`opt:${def.key}`] = { input: box };
    return h(
      'div',
      { class: 'pb-option' },
      box,
      h('label', { for: id }, h('span', { class: 'pb-option-label' }, def.label), h('span', { class: 'pb-hint' }, def.hint)),
    );
  }

  const typeRadios = MIGRATION_TYPES.map((type) => {
    const id = nextId('type');
    const radio = h('input', { id, type: 'radio', name: 'pb-type', value: type.id, class: 'pb-radio' });
    radio.addEventListener('change', () => {
      state.input.type = type.id;
      update();
    });
    fields[`type:${type.id}`] = { input: radio };
    return h(
      'label',
      { class: 'pb-card', for: id },
      radio,
      h('span', { class: 'pb-card-body' }, h('span', { class: 'pb-card-title' }, type.label), h('span', { class: 'pb-hint' }, type.description)),
    );
  });

  const hostSelect = h(
    'select',
    { id: nextId('host'), class: 'pb-input' },
    h('option', { value: 'cloud' }, 'CircleCI cloud (circleci.com)'),
    h('option', { value: 'server' }, 'CircleCI server (your own install)'),
  );
  hostSelect.addEventListener('change', () => {
    state.input.host = hostSelect.value;
    update();
  });

  const shellRadios = [
    ['bash', 'bash or zsh (macOS, Linux, WSL)'],
    ['powershell', 'PowerShell (Windows)'],
  ].map(([value, label]) => {
    const id = nextId('shell');
    const radio = h('input', { id, type: 'radio', name: 'pb-shell', value, class: 'pb-radio' });
    radio.addEventListener('change', () => {
      state.input.shell = value;
      update();
    });
    fields[`shell:${value}`] = { input: radio };
    return h('label', { class: 'pb-inline', for: id }, radio, label);
  });

  const sourceRow = textField('source', 'Source org slug', { required: true, hint: 'Organization Settings, Overview, in the org you are leaving.' });
  const destRow = textField('dest', 'Destination org slug', { required: true, hint: 'The org you are moving to.' });
  const destIdRow = textField('destId', 'Destination org ID', { hint: 'A UUID, from the destination org\'s Organization Settings, Overview.' });
  const hostUrlRow = textField('hostUrl', 'Server URL', { placeholder: 'https://circleci.example.com', hint: 'Added to every command as --host.' });
  const runnerRows = [
    textField('runnerNs', 'Source runner namespace', { placeholder: 'acme-runners', required: true }),
    textField('destRunnerNs', 'Destination runner namespace', { placeholder: 'same as the source', hint: 'Leave blank to keep the same name; support then moves the namespace.' }),
  ];
  const orbRows = [
    textField('orbNs', 'Source orb namespace', { placeholder: 'acme', required: true }),
    textField('destOrbNs', 'Destination orb namespace', { placeholder: 'same as the source', hint: 'Leave blank to keep the same name; support then moves the namespace.' }),
  ];
  const ghOrgRow = textField('destGithubOrg', 'New GitHub org', { placeholder: 'acme-new', hint: 'The GitHub org the repositories now live in.' });

  const optionBoxes = new Map(OPTION_DEFS.map((def) => [def.key, checkbox(def)]));
  const optionList = (keys) => keys.map((k) => optionBoxes.get(k));

  const status = h('p', { class: 'pb-status', role: 'status', 'aria-live': 'polite' });
  const planArea = h('div', { class: 'pb-plan', id: nextId('plan') });

  const form = h(
    'form',
    { class: 'pb-form', novalidate: true, onsubmit: (e) => e.preventDefault(), 'aria-label': 'Migration plan inputs' },
    h(
      'fieldset',
      { class: 'pb-fieldset' },
      h('legend', {}, h('span', { class: 'pb-step-n' }, '1'), 'Migration type'),
      h('div', { class: 'pb-cards' }, typeRadios),
      h(
        'p',
        { class: 'pb-hint' },
        'A GitHub org with both an OAuth and a GitHub App record has two CircleCI orgs. Build one plan for each record: run this builder twice, once for the gh/ org and once for the circleci/ org.',
      ),
    ),
    h(
      'fieldset',
      { class: 'pb-fieldset' },
      h('legend', {}, h('span', { class: 'pb-step-n' }, '2'), 'Orgs'),
      h('div', { class: 'pb-field' }, h('label', { for: hostSelect.id, class: 'pb-label' }, 'Where do the orgs live?'), hostSelect),
      hostUrlRow,
      sourceRow,
      destRow,
      destIdRow,
    ),
    h(
      'fieldset',
      { class: 'pb-fieldset' },
      h('legend', {}, h('span', { class: 'pb-step-n' }, '3'), 'What you are moving'),
      h('div', { class: 'pb-options' }, optionList(['contexts', 'projectVars', 'sshKeys', 'restrictions'])),
      h('div', { class: 'pb-options' }, optionList(['runners'])),
      h('div', { class: 'pb-subfields', 'data-for': 'runners' }, runnerRows),
      h('div', { class: 'pb-options' }, optionList(['orbs'])),
      h('div', { class: 'pb-subfields', 'data-for': 'orbs' }, orbRows),
      h('div', { class: 'pb-options' }, optionList(['webhooks', 'repoMove'])),
      h('div', { class: 'pb-subfields', 'data-for': 'repoMove' }, ghOrgRow),
      h('div', { class: 'pb-options' }, optionList(['pauseSource', 'terraform'])),
    ),
    h(
      'fieldset',
      { class: 'pb-fieldset' },
      h('legend', {}, h('span', { class: 'pb-step-n' }, '4'), 'Environment'),
      h('div', { class: 'pb-inline-group', role: 'radiogroup', 'aria-label': 'Shell' }, shellRadios),
      h(
        'p',
        { class: 'pb-hint pb-secure' },
        'There is no token field on this page, on purpose. Tokens appear only as <paste token> placeholders that you fill in your own shell.',
      ),
    ),
  );

  root.replaceChildren(form, status, planArea);

  const subfieldBlocks = [...form.querySelectorAll('.pb-subfields')];

  // ---- state <-> form ----

  function syncFormFromState() {
    const i = state.input;
    for (const t of MIGRATION_TYPES) fields[`type:${t.id}`].input.checked = i.type === t.id;
    for (const s of ['bash', 'powershell']) fields[`shell:${s}`].input.checked = i.shell === s;
    hostSelect.value = i.host;
    for (const name of ['source', 'dest', 'destId', 'hostUrl', 'runnerNs', 'destRunnerNs', 'orbNs', 'destOrbNs', 'destGithubOrg']) {
      fields[name].input.value = i[name] ?? '';
    }
    for (const def of OPTION_DEFS) fields[`opt:${def.key}`].input.checked = Boolean(i.options[def.key]);
  }

  function writeHash() {
    try {
      const encoded = encodeState(state.input);
      const url = location.pathname + location.search + (encoded ? `#${encoded}` : '');
      history.replaceState(null, '', url);
    } catch {
      // Sandboxed previews can refuse history changes; the plan still works.
    }
  }

  // ---- update ----

  function update() {
    const plan = buildPlan(state.input);
    const i = state.input;
    const type = MIGRATION_TYPES.find((t) => t.id === i.type) ?? MIGRATION_TYPES[0];

    fields.source.input.placeholder = type.sourceHint;
    fields.dest.input.placeholder = type.destHint;

    // Conditional fields.
    hostUrlRow.hidden = i.host !== 'server';
    for (const block of subfieldBlocks) block.hidden = !i.options[block.dataset.for];

    // Derived hint for the destination org ID.
    const slugId = /^circleci\/([0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12})$/i.exec(i.dest.trim());
    fields.destId.input.placeholder = slugId ? `${slugId[1]} (from the destination slug)` : '';

    // Field errors. A "required" message waits until the field has been visited.
    const byField = new Map();
    for (const e of plan.errors) {
      if (e.code === 'required' && !state.touched[e.field]) continue;
      if (!byField.has(e.field)) byField.set(e.field, e);
    }
    for (const [name, f] of Object.entries(fields)) {
      if (!f.error) continue;
      const e = byField.get(name);
      f.error.hidden = !e;
      f.error.textContent = e ? e.message : '';
      f.input.setAttribute('aria-invalid', e ? 'true' : 'false');
      f.suggest.replaceChildren();
      if (e?.suggestType) {
        const target = MIGRATION_TYPES.find((t) => t.id === e.suggestType);
        f.suggest.append(
          h('button', {
            type: 'button',
            class: 'pb-btn pb-btn-small',
            onclick: () => {
              state.input.type = e.suggestType;
              syncFormFromState();
              update();
            },
          }, `Switch to "${target.label}"`),
        );
      }
    }

    // Status line (live region).
    if (plan.errors.length === 0) {
      status.textContent = `Your plan is ready: ${plan.steps.length} steps.`;
      status.dataset.state = 'ok';
    } else if (byField.size > 0) {
      status.textContent = `Fix ${byField.size} ${byField.size === 1 ? 'field' : 'fields'} above to see your plan.`;
      status.dataset.state = 'error';
    } else {
      status.textContent = 'Enter your source and destination orgs to see your plan.';
      status.dataset.state = 'idle';
    }

    renderPlan(plan);
    writeHash();
  }

  // ---- plan output ----

  function renderPlan(plan) {
    if (plan.errors.length > 0) {
      planArea.replaceChildren();
      return;
    }
    const { input } = plan;
    const envText = input.shell === 'powershell' ? plan.env.powershell : plan.env.bash;
    const stepsByPhase = new Map(PHASES.map((p) => [p.id, plan.steps.filter((s) => s.phase === p.id)]));

    // "Copy all commands": the environment block, then every CLI step with a comment heading.
    let n = 0;
    const lines = [envText, ''];
    for (const phase of PHASES) {
      for (const s of stepsByPhase.get(phase.id)) {
        n += 1;
        if (s.commands.length) {
          lines.push(`# ${n}. ${s.title}`, ...s.commands, '');
        } else {
          lines.push(`# ${n}. (${KIND_LABELS[s.kind]}, not a command) ${s.title}`, '');
        }
      }
    }
    const allCommands = lines.join('\n').trimEnd() + '\n';
    const slug = input.type;

    const warnings = plan.warnings.length
      ? h(
          'section',
          { class: 'pb-section', 'aria-labelledby': 'pb-h-warn' },
          h('h3', { id: 'pb-h-warn', class: 'pb-h' }, 'Read this first'),
          h(
            'ul',
            { class: 'pb-warnings' },
            [...plan.warnings]
              .sort((a, b) => (a.level === b.level ? 0 : a.level === 'warn' ? -1 : 1))
              .map((w) => h('li', { class: `pb-warning pb-${w.level}` }, h('strong', {}, w.level === 'warn' ? 'Warning. ' : 'Note. '), w.message)),
          ),
        )
      : null;

    const toolbar = h(
      'div',
      { class: 'pb-toolbar' },
      copyButton('Copy all commands', () => allCommands, 'pb-btn-primary'),
      h('button', { type: 'button', class: 'pb-btn', onclick: () => download(`migration-run-sheet-${slug}.md`, renderRunSheet(plan, { baseUrl: new URL('../', location.href).href })) }, 'Download run sheet (.md)'),
      copyButton('Copy link to this plan', () => location.href),
    );

    const envSection = h(
      'section',
      { class: 'pb-section', 'aria-labelledby': 'pb-h-env' },
      h('h3', { id: 'pb-h-env', class: 'pb-h' }, 'Set up your shell'),
      h('p', { class: 'pb-hint' }, 'Run this first, in the shell you will use for every command. Replace each <paste token> yourself: this page never sees a token.'),
      codeBlock(envText, input.shell),
    );

    const phaseSections = [];
    let counter = 0;
    for (const phase of PHASES) {
      const group = stepsByPhase.get(phase.id);
      if (!group.length) continue;
      const hid = `pb-h-${phase.id}`;
      const start = counter + 1;
      const items = group.map((s) => {
        counter += 1;
        return stepItem(s, counter, input.shell);
      });
      phaseSections.push(
        h(
          'section',
          { class: 'pb-section', 'aria-labelledby': hid },
          h('h3', { id: hid, class: 'pb-h' }, phase.label),
          h('p', { class: 'pb-hint' }, phase.note),
          h('ol', { class: 'pb-steps', start }, items),
        ),
      );
    }

    planArea.replaceChildren(
      h('h2', { class: 'pb-title' }, `Your plan: ${plan.scenario.label}`),
      h('p', { class: 'pb-summary pb-mono' }, `${input.source}  ->  ${input.dest}`),
      toolbar,
      warnings,
      envSection,
      ...phaseSections,
    );
  }

  function codeBlock(text, shell) {
    return h(
      'div',
      { class: 'pb-code' },
      h('pre', { class: 'pb-pre', tabindex: '0', 'aria-label': `${shell === 'powershell' ? 'PowerShell' : 'bash'} commands` }, h('code', {}, text)),
      copyButton('Copy', () => text, 'pb-code-copy'),
    );
  }

  function stepItem(step, number, shell) {
    const kindClass = `pb-kind pb-kind-${step.kind}`;
    return h(
      'li',
      { class: `pb-step${step.pointOfNoReturn ? ' pb-step-ponr' : ''}`, id: `step-${number}` },
      h(
        'div',
        { class: 'pb-step-head' },
        h('span', { class: 'pb-num', 'aria-hidden': 'true' }, String(number)),
        h('h4', { class: 'pb-step-title' }, h('span', { class: 'pb-sr' }, `Step ${number}: `), step.title),
        h('span', { class: kindClass }, KIND_LABELS[step.kind]),
        step.reversible ? h('span', { class: 'pb-kind pb-kind-rev' }, 'Reversible') : null,
      ),
      step.pointOfNoReturn
        ? h('div', { class: 'pb-ponr', role: 'note' }, h('strong', {}, 'Point of no return. '), step.ponrNote)
        : null,
      h('p', { class: 'pb-why' }, step.why),
      step.link ? h('p', { class: 'pb-link' }, h('a', { href: `../${step.link.path}` }, step.link.label)) : null,
      step.details?.length ? h('ul', { class: 'pb-details' }, step.details.map((d) => h('li', {}, d))) : null,
      step.commands.length ? codeBlock(step.commands.join('\n'), shell) : null,
      step.undo?.length
        ? h('div', { class: 'pb-undo' }, h('p', { class: 'pb-hint' }, 'To undo this step:'), codeBlock(step.undo.join('\n'), shell))
        : null,
      h('p', { class: 'pb-check-line' }, h('strong', {}, 'Check: '), step.check),
    );
  }

  // ---- start ----

  if (location.hash && location.hash.length > 1) state.input = decodeState(location.hash);
  syncFormFromState();
  window.addEventListener('hashchange', () => {
    if (location.hash.length > 1 && encodeState(state.input) !== location.hash.slice(1)) {
      state.input = decodeState(location.hash);
      syncFormFromState();
      update();
    }
  });
  update();
}

function autoMount() {
  for (const el of document.querySelectorAll('[data-plan-builder]')) mount(el);
}

if (typeof document !== 'undefined') {
  if (document.readyState === 'loading') document.addEventListener('DOMContentLoaded', autoMount);
  else autoMount();
}
