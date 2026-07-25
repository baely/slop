/* compose — generates the baely/infra service dir for a slop app.
   Everything is computed client-side; nothing leaves the page. */
(function () {
'use strict';

/* ==== generator core (start) ==== */
/* Pure functions. No DOM, no globals. Extracted verbatim by the node tests. */

var APP_RE = /^[a-z0-9]([a-z0-9-]*[a-z0-9])?$/;
var HOST_LABEL_RE = /^[a-z0-9]([a-z0-9-]*[a-z0-9])?$/;
var ENV_KEY_RE = /^[A-Za-z_][A-Za-z0-9_]*$/;
var SHA_RE = /^[0-9a-f]{7,40}$/;

function serviceDir(app) {
  return 'docker/github.com_baely_slop_' + app;
}

/* --- validation --- */

function checkApp(v) {
  if (!v) return 'Required. Compose project name: lowercase letters, digits, dashes.';
  if (/[A-Z]/.test(v)) return 'No uppercase. Compose project names are lowercase.';
  var bad = v.match(/[^a-z0-9-]/);
  if (bad) return 'Invalid character "' + bad[0] + '". Only lowercase letters, digits and dashes.';
  if (v.charAt(0) === '-' || v.charAt(v.length - 1) === '-') return 'Cannot start or end with a dash.';
  if (!APP_RE.test(v)) return 'Not a valid compose project name.';
  return '';
}

function checkDomain(v) {
  if (!v) return 'Required. The hostname Traefik routes on.';
  if (/^[a-z]+:\/\//i.test(v)) return 'Hostname only — drop the scheme.';
  if (v.indexOf('/') !== -1) return 'Hostname only — no path.';
  if (v.indexOf(':') !== -1) return 'Hostname only — no port. That goes in Container Port.';
  if (/[A-Z]/.test(v)) return 'Lowercase only.';
  if (v.length > 253) return 'Too long. Max 253 characters.';
  var parts = v.split('.');
  if (parts.length < 2) return 'Needs at least one dot, e.g. app.baileys.dev.';
  for (var i = 0; i < parts.length; i++) {
    var p = parts[i];
    if (!p) return 'Empty label — check for a stray dot.';
    if (p.length > 63) return 'Label "' + p + '" is over 63 characters.';
    if (!HOST_LABEL_RE.test(p)) return 'Label "' + p + '" is not a valid hostname label.';
  }
  if (!/^[a-z]{2,}$/.test(parts[parts.length - 1])) return 'Last label must be alphabetic, e.g. dev.';
  return '';
}

function checkPort(v) {
  if (v === '' || v == null) return 'Required. The port the container listens on.';
  if (!/^\d+$/.test(v)) return 'Digits only.';
  var n = Number(v);
  if (n < 1 || n > 65535) return 'Out of range. 1–65535.';
  return '';
}

function checkSha(v) {
  if (!v) return 'Required for the # Ref: header. Use the slop commit SHA.';
  if (!SHA_RE.test(v)) return 'Not a git SHA. 7–40 lowercase hex characters.';
  return '';
}

/* Optional. A named volume is the only way state survives a redeploy. */
function checkVolume(v) {
  if (!v) return '';
  if (v.charAt(0) !== '/') return 'Absolute container path, e.g. /data.';
  if (/\s/.test(v)) return 'No spaces.';
  if (v.indexOf(':') !== -1) return 'Mount path only — the volume name is derived.';
  if (v === '/') return 'Pick a real directory, not the container root.';
  return '';
}

function checkRows(rows) {
  var seen = {};
  return rows.map(function (r, i) {
    var k = r.key.trim();
    if (!k) return r.value.trim() ? 'Key required for this row.' : '';
    if (!ENV_KEY_RE.test(k)) return 'Invalid key. Letters, digits, underscore; cannot start with a digit.';
    if (Object.prototype.hasOwnProperty.call(seen, k)) return 'Duplicate of row ' + (seen[k] + 1) + '.';
    seen[k] = i;
    return '';
  });
}

/* Returns per-field messages plus a flat, human list of what is blocking output. */
function validate(s) {
  var fields = {
    app: checkApp(s.app),
    domain: checkDomain(s.domain),
    port: checkPort(s.port),
    sha: checkSha(s.sha),
    volume: checkVolume(s.volume)
  };
  var rows = checkRows(s.rows);
  var names = {
    app: 'App Name', domain: 'Public Domain', port: 'Container Port',
    sha: 'Slop Commit SHA', volume: 'Data Volume'
  };
  var blocking = [];
  ['app', 'domain', 'port', 'sha', 'volume'].forEach(function (k) {
    if (fields[k]) blocking.push(names[k] + ' — ' + fields[k]);
  });
  rows.forEach(function (msg, i) {
    if (msg) blocking.push('Env row ' + (i + 1) + ' — ' + msg);
  });
  return { fields: fields, rows: rows, blocking: blocking };
}

/* --- YAML emitters --- */

/* True when a plain (unquoted) YAML scalar would not round-trip as this string. */
function needsYamlQuote(s) {
  if (s === '') return true;
  if (/^\s|\s$/.test(s)) return true;
  if (/^[-?:,[\]{}#&*!|>'"%@`]/.test(s)) return true;
  if (/:(\s|$)/.test(s)) return true;      // "key: value" would split the scalar
  if (/\s#/.test(s)) return true;          // starts a trailing comment
  if (/["']/.test(s)) return true;         // legal plain, but quote it so the intent is unambiguous
  if (/[\n\r\t\x00-\x1f\x7f]/.test(s)) return true;
  if (/^(true|false|null|yes|no|on|off|~)$/i.test(s)) return true;
  if (/^[+-]?(\d[\d_]*(\.\d*)?([eE][+-]?\d+)?|\.\d+|0x[0-9a-fA-F]+|0o[0-7]+)$/.test(s)) return true;
  return false;
}

function yamlDouble(s) {
  return '"' + String(s)
    .replace(/\\/g, '\\\\')
    .replace(/"/g, '\\"')
    .replace(/\n/g, '\\n')
    .replace(/\r/g, '\\r')
    .replace(/\t/g, '\\t') + '"';
}

function yamlLabel(s) {
  return needsYamlQuote(s) ? yamlDouble(s) : s;
}

/* Rows that end up inline under environment: vs in the .env file. */
function splitRows(s) {
  var inline = [], secret = [];
  s.rows.forEach(function (r) {
    var k = r.key.trim();
    if (!k) return;
    if (s.secrets && r.secret) secret.push({ key: k, value: r.value });
    else inline.push({ key: k, value: r.value.trim() });
  });
  return { inline: inline, secret: secret };
}

function buildDeployYaml(s) {
  var split = splitRows(s);
  var publish = !!(s.title.trim() || s.desc.trim());
  var mount = s.volume.trim();
  var vol = s.app + '-data';
  var L = [];

  L.push('# Repo: https://github.com/baely/slop (' + s.app + '/)');
  L.push('# Ref: ' + s.sha);
  L.push('name: ' + s.app);
  L.push('');
  L.push('services:');
  L.push('  ' + s.app + ':');
  L.push('    image: registry.baileys.dev/' + s.app + ':latest');
  L.push('    restart: unless-stopped');
  if (s.secrets) L.push('    env_file: ".env"');
  if (split.inline.length) {
    L.push('    environment:');
    split.inline.forEach(function (r) {
      L.push('      ' + r.key + ': ' + yamlDouble(r.value));
    });
  }
  L.push('    labels:');
  L.push('      - ' + yamlLabel('traefik.enable=true'));
  L.push('      - ' + yamlLabel('traefik.http.routers.' + s.app + '.rule=Host(`' + s.domain + '`)'));
  L.push('      - ' + yamlLabel('traefik.http.services.' + s.app + '.loadbalancer.server.port=' + s.port));
  if (publish) {
    L.push('      - ' + yamlLabel('baileys.public.url=https://' + s.domain));
    if (s.title.trim()) L.push('      - ' + yamlLabel('baileys.public.title=' + s.title.trim()));
    if (s.desc.trim()) L.push('      - ' + yamlLabel('baileys.public.description=' + s.desc.trim()));
  }
  if (mount) {
    L.push('    volumes:');
    L.push('      - ' + vol + ':' + mount);
  }
  L.push('    networks:');
  L.push('      - web');
  L.push('');
  if (mount) {
    L.push('volumes:');
    L.push('  ' + vol + ':');
    L.push('');
  }
  L.push('networks:');
  L.push('  web:');
  L.push('    external: true');

  return L.join('\n') + '\n';
}

/* Committed template only — the placeholder is fixed, real values never enter this app. */
function buildSampleEnv(s) {
  var secret = splitRows(s).secret;
  if (!secret.length) return '';
  return secret.map(function (r) { return r.key + '=changeme'; }).join('\n') + '\n';
}

function buildSteps(s) {
  var dir = serviceDir(s.app);
  var branch = 'add-' + s.app;
  var L = [];
  var n = 0;

  L.push('# ' + (++n) + ' — Build and push the image (from slop/' + s.app + '/)');
  L.push('docker build --platform linux/amd64 -t registry.baileys.dev/' + s.app + ':latest --push .');
  L.push('');
  L.push('# ' + (++n) + ' — Add the service dir in baely/infra');
  L.push('git checkout -b ' + branch);
  L.push('mkdir -p ' + dir);
  L.push('#   write deploy.yaml' + (s.secrets ? ' and sample.env' : '') + ' into that dir, then:');
  L.push('git add ' + dir + '/deploy.yaml');
  if (s.secrets) L.push('git add -f ' + dir + '/sample.env   # env files are gitignored');
  L.push('git commit -m "Add ' + s.app + ' service"');
  L.push('git push -u origin ' + branch);
  L.push('');
  if (s.secrets) {
    L.push('# ' + (++n) + ' — Write the real .env on the server BEFORE merging');
    L.push('ssh user@192.168.0.82');
    L.push('mkdir -p ~/github/infra/' + dir);
    L.push('vi ~/github/infra/' + dir + '/.env   # same keys as sample.env, real values');
    L.push('chmod 600 ~/github/infra/' + dir + '/.env');
    L.push('');
  }
  L.push('# ' + (++n) + ' — Merge is the deploy (PRs touching docker/** are auto-approved)');
  L.push('gh pr create --fill');
  L.push('gh pr merge --auto --squash <pr>');
  L.push('');
  L.push('# Redeploying later, after pushing a new image: bump the `# Ref:` line in');
  L.push('# deploy.yaml and merge — any change to the service dir triggers a redeploy.');

  return L.join('\n') + '\n';
}

function blockedText(blocking) {
  var L = ['# ' + blocking.length + ' problem' + (blocking.length === 1 ? '' : 's') + ' blocking output.', '#'];
  blocking.forEach(function (b) { L.push('# ' + b); });
  return L.join('\n');
}

/* ==== generator core (end) ==== */

/* ---------- DOM ---------- */

var STORE_KEY = 'compose.form.v1';

var $ = function (id) { return document.getElementById(id); };

var el = {
  app: $('f-app'), domain: $('f-domain'), port: $('f-port'), sha: $('f-sha'),
  volume: $('f-volume'), title: $('f-title'), desc: $('f-desc'),
  secrets: $('f-secrets'), secretsHint: $('secrets-hint'),
  rows: $('rows'), rowsHead: $('rows-head'), rowsEmpty: $('rows-empty'),
  addRow: $('add-row'), reset: $('reset'),
  pathText: $('path-text'), status: $('status'),
  copyStatus: $('copy-status')
};

var panes = {
  yaml: { out: $('o-yaml'), meta: $('m-yaml'), btn: $('c-yaml'), text: '', copyable: false },
  env: { out: $('o-env'), meta: $('m-env'), btn: $('c-env'), text: '', copyable: false },
  sh: { out: $('o-sh'), meta: $('m-sh'), btn: $('c-sh'), text: '', copyable: false }
};

/* The .rl spans are the field names for narrow screens, where the header row is
   hidden; they collapse away once the header row appears at 640px. */
var ROW_TPL =
  '<span class="c-key"><span class="rl">Key</span>' +
  '<input type="text" class="k" autocomplete="off" autocapitalize="none" spellcheck="false" placeholder="ADDR"></span>' +
  '<span class="c-value"><span class="rl">Value</span>' +
  '<input type="text" class="v" autocomplete="off" autocapitalize="none" spellcheck="false" placeholder=":8080"></span>' +
  '<span class="c-secret"><label class="sec"><input type="checkbox" class="s"><span class="rl">Secret</span></label></span>' +
  '<span class="c-del"><button type="button" class="btn link d">Delete</button></span>' +
  '<span class="c-err"></span>';

function addRow(data) {
  var row = document.createElement('div');
  row.className = 'row';
  row.innerHTML = ROW_TPL;
  var k = row.querySelector('.k'), v = row.querySelector('.v'), s = row.querySelector('.s');
  if (data) { k.value = data.key || ''; v.value = data.value || ''; s.checked = !!data.secret; }
  row.querySelector('.d').addEventListener('click', function () {
    row.remove();
    render();
    save();
  });
  el.rows.appendChild(row);
  return row;
}

function readState() {
  var rows = [];
  var nodes = el.rows.querySelectorAll('.row');
  for (var i = 0; i < nodes.length; i++) {
    rows.push({
      key: nodes[i].querySelector('.k').value,
      value: nodes[i].querySelector('.v').value,
      secret: nodes[i].querySelector('.s').checked
    });
  }
  return {
    app: el.app.value.trim(),
    domain: el.domain.value.trim(),
    port: el.port.value.trim(),
    sha: el.sha.value.trim(),
    volume: el.volume.value.trim(),
    title: el.title.value,
    desc: el.desc.value,
    secrets: el.secrets.getAttribute('aria-pressed') === 'true',
    rows: rows
  };
}

function setField(id, msg, input) {
  $(id).textContent = msg;
  if (input.value.trim() && msg) input.setAttribute('aria-invalid', 'true');
  else input.removeAttribute('aria-invalid');
}

function setPane(pane, text, copyable, meta, cls) {
  pane.text = text;
  pane.copyable = copyable;
  pane.out.textContent = text;
  pane.out.className = 'out ' + pane.out.getAttribute('data-size') + (cls ? ' ' + cls : '');
  pane.meta.textContent = meta;
}

function countLines(t) {
  return t ? t.replace(/\n$/, '').split('\n').length : 0;
}

function render() {
  var s = readState();
  var res = validate(s);

  setField('e-app', res.fields.app, el.app);
  setField('e-domain', res.fields.domain, el.domain);
  setField('e-port', res.fields.port, el.port);
  setField('e-sha', res.fields.sha, el.sha);
  setField('e-volume', res.fields.volume, el.volume);

  var nodes = el.rows.querySelectorAll('.row');
  for (var i = 0; i < nodes.length; i++) {
    var n = i + 1;
    nodes[i].querySelector('.c-err').textContent = res.rows[i] || '';
    nodes[i].querySelector('.k').setAttribute('aria-label', 'Key, row ' + n);
    nodes[i].querySelector('.v').setAttribute('aria-label', 'Value, row ' + n);
    var box = nodes[i].querySelector('.s');
    box.setAttribute('aria-label', 'Secret, row ' + n);
    box.disabled = !s.secrets;
    nodes[i].querySelector('.d').setAttribute('aria-label', 'Delete row ' + n);

    // A secret's value is never ours to hold: lock the field and drop anything
    // already typed, so a real credential can't reach the output or localStorage.
    var val = nodes[i].querySelector('.v');
    var isSecret = s.secrets && box.checked;
    val.disabled = isSecret;
    val.placeholder = isSecret ? 'changeme' : ':8080';
    if (isSecret && val.value) { val.value = ''; s.rows[i].value = ''; }
  }
  el.rowsHead.hidden = nodes.length === 0;
  el.rowsEmpty.hidden = nodes.length > 0;

  el.secretsHint.textContent = s.secrets
    ? 'env_file: ".env" written'
    : 'env_file omitted';

  // Target path headline
  el.pathText.textContent = '';
  el.pathText.appendChild(document.createTextNode('docker/github.com_baely_slop_'));
  var appSpan = document.createElement('span');
  appSpan.className = 'app';
  appSpan.textContent = res.fields.app ? '…' : s.app;
  el.pathText.appendChild(appSpan);
  el.pathText.appendChild(document.createTextNode('/'));

  if (res.blocking.length) {
    var msg = blockedText(res.blocking);
    setPane(panes.yaml, msg, false, 'blocked', 'blocked');
    setPane(panes.env, msg, false, 'blocked', 'blocked');
    setPane(panes.sh, msg, false, 'blocked', 'blocked');
    el.status.textContent = res.blocking.length + ' problem' +
      (res.blocking.length === 1 ? '' : 's') + ' blocking output.';
    el.status.className = 'status bad';
    return;
  }

  var split = splitRows(s);
  var yaml = buildDeployYaml(s);
  var sh = buildSteps(s);

  setPane(panes.yaml, yaml, true, countLines(yaml) + ' lines', '');
  setPane(panes.sh, sh, true, (sh.match(/^# \d+ —/gm) || []).length + ' steps', '');

  if (!s.secrets) {
    setPane(panes.env, 'Secrets off. No sample.env in this service dir.', false, '0 keys', '');
  } else if (!split.secret.length) {
    setPane(panes.env,
      '0 keys marked Secret.\nTick Secret on a row, or turn Needs Secrets off —\nenv_file points at a .env that would not exist.',
      false, '0 keys', 'warned');
  } else {
    var env = buildSampleEnv(s);
    setPane(panes.env, env, true, split.secret.length + ' key' + (split.secret.length === 1 ? '' : 's'), '');
  }

  el.status.textContent = countLines(yaml) + ' lines · ' + split.inline.length +
    ' inline env · ' + split.secret.length + ' secret key' + (split.secret.length === 1 ? '' : 's');
  el.status.className = 'status';
}

/* ---------- copy ---------- */

var copyTimer = null;

function say(msg) {
  el.copyStatus.textContent = msg;
}

function legacyCopy(text) {
  var ta = document.createElement('textarea');
  ta.value = text;
  ta.setAttribute('readonly', '');
  ta.style.position = 'fixed';
  ta.style.top = '-1000px';
  document.body.appendChild(ta);
  ta.select();
  var ok = false;
  try { ok = document.execCommand('copy'); } catch (e) { ok = false; }
  ta.remove();
  return ok;
}

function flash(btn) {
  if (copyTimer) { clearTimeout(copyTimer.t); copyTimer.b.textContent = 'Copy'; }
  btn.textContent = 'Copied';
  copyTimer = { b: btn, t: setTimeout(function () { btn.textContent = 'Copy'; copyTimer = null; }, 1400) };
}

function doCopy(pane) {
  if (!pane.copyable) {
    say('Nothing to copy — ' + pane.meta.textContent + '.');
    return;
  }
  var text = pane.text;
  var done = function () { flash(pane.btn); say('Copied ' + text.length + ' characters.'); };
  if (navigator.clipboard && window.isSecureContext) {
    navigator.clipboard.writeText(text).then(done, function () {
      if (legacyCopy(text)) done();
      else say('Copy blocked by the browser. Select the text and copy manually.');
    });
  } else if (legacyCopy(text)) {
    done();
  } else {
    say('Copy blocked by the browser. Select the text and copy manually.');
  }
}

/* ---------- persistence ---------- */

function save() {
  try {
    localStorage.setItem(STORE_KEY, JSON.stringify(readState()));
  } catch (e) { /* private mode or quota — the form still works, it just won't survive a reload */ }
}

function load() {
  var raw = null;
  try { raw = localStorage.getItem(STORE_KEY); } catch (e) { raw = null; }
  if (!raw) return false;
  var s;
  try { s = JSON.parse(raw); } catch (e) { return false; }
  if (!s || typeof s !== 'object') return false;
  el.app.value = s.app || '';
  el.domain.value = s.domain || '';
  el.port.value = s.port || '';
  el.sha.value = s.sha || '';
  el.volume.value = s.volume || '';
  el.title.value = s.title || '';
  el.desc.value = s.desc || '';
  el.secrets.setAttribute('aria-pressed', s.secrets ? 'true' : 'false');
  if (Array.isArray(s.rows)) {
    s.rows.forEach(function (r) {
      if (r && typeof r === 'object') addRow({ key: String(r.key || ''), value: String(r.value || ''), secret: !!r.secret });
    });
  }
  return true;
}

/* ---------- wiring ---------- */

['yaml', 'env', 'sh'].forEach(function (k) {
  panes[k].out.setAttribute('data-size', k === 'yaml' ? 'tall' : (k === 'sh' ? 'mid' : 'short'));
  panes[k].btn.addEventListener('click', function () { doCopy(panes[k]); });
});

[el.app, el.domain, el.port, el.sha, el.volume, el.title, el.desc].forEach(function (input) {
  input.addEventListener('input', function () { render(); save(); });
});

el.rows.addEventListener('input', function () { render(); save(); });
el.rows.addEventListener('change', function () { render(); save(); });

el.secrets.addEventListener('click', function () {
  var on = el.secrets.getAttribute('aria-pressed') === 'true';
  el.secrets.setAttribute('aria-pressed', on ? 'false' : 'true');
  render();
  save();
});

el.addRow.addEventListener('click', function () {
  var row = addRow(null);
  render();
  save();
  row.querySelector('.k').focus();
});

el.reset.addEventListener('click', function () {
  [el.app, el.domain, el.port, el.sha, el.volume, el.title, el.desc].forEach(function (i) { i.value = ''; });
  el.secrets.setAttribute('aria-pressed', 'false');
  el.rows.textContent = '';
  say('Form cleared.');
  render();
  save();
});

if (!load()) {
  addRow(null);
}
render();

})();
