// Termwright web step runner: reads a job (JSON) on stdin, drives the browser with Playwright and prints
// one JSON line per action, then a last line with the page's text. With "record": true it opens the
// page in a visible browser and prints the actions the person does until the window is closed.
const { chromium, firefox, webkit } = require('playwright');
const fs = require('fs');
const path = require('path');

function out(o) { process.stdout.write(JSON.stringify(o) + '\n'); }

// A target: CSS, text=…, role=button[name="Save"], label=…, placeholder=…, testid=…, //xpath;
// "frame=#main >> text=Save" looks inside a frame first.
function loc(page, t) {
  let root = page;
  const parts = t.split(/\s*>>\s*/);
  while (parts.length > 1 && parts[0].startsWith('frame=')) root = root.frameLocator(parts.shift().slice(6));
  t = parts.join(' >> ');
  if (t.startsWith('label=')) return root.getByLabel(t.slice(6), { exact: false });
  if (t.startsWith('placeholder=')) return root.getByPlaceholder(t.slice(12));
  if (t.startsWith('testid=')) return root.getByTestId(t.slice(7));
  if (t.startsWith('alt=')) return root.getByAltText(t.slice(4));
  if (t.startsWith('title=')) return root.getByTitle(t.slice(6));
  if (t.startsWith('//')) return root.locator('xpath=' + t);
  return root.locator(t);
}

function viewport(v) {
  const m = /^(\d+)\s*[x×]\s*(\d+)$/.exec(v || '');
  return m ? { width: +m[1], height: +m[2] } : undefined;
}

// every browser this runner started: closed before it exits, so none is left running (macOS keeps a
// browser without windows in the Dock)
const BROWSERS = [];
async function quit(code) {
  await Promise.all(BROWSERS.map(b => b.close().catch(() => {})));
  process.exit(code);
}
for (const sig of ['SIGTERM', 'SIGINT', 'SIGHUP']) process.on(sig, () => quit(0));

async function context(req, w, show) {
  const type = { firefox, webkit }[w.browser] || chromium;
  let browser;
  try { browser = await type.launch({ headless: !show }); }
  catch (e) { // Playwright's own browser does not start (e.g. a broken download): the installed Google Chrome or Edge
    if (type !== chromium) throw e;
    browser = await chromium.launch({ headless: !show, channel: 'chrome' }).catch(() => chromium.launch({ headless: !show, channel: 'msedge' })).catch(() => { throw e; });
  }
  BROWSERS.push(browser);
  const opts = { ignoreHTTPSErrors: !!w.insecure, viewport: viewport(w.viewport) };
  if (req.httpUser) opts.httpCredentials = { username: req.httpUser, password: req.httpPassword || '' };
  const statePath = w.state && req.stateDir ? path.join(req.stateDir, w.state.replace(/[^\w.-]+/g, '_') + '.json') : '';
  if (statePath && fs.existsSync(statePath)) opts.storageState = statePath;
  let videoDir = '';
  if (req.video) { videoDir = fs.mkdtempSync(path.join(require('os').tmpdir(), 'tw-video-')); opts.recordVideo = { dir: videoDir, size: viewport(w.viewport) || { width: 1280, height: 720 } }; }
  const ctx = await browser.newContext(opts);
  return { browser, ctx, statePath, videoDir };
}

// open starts a browser for a step: S holds the page, the tabs, the dialogs and the values read.
async function open(req, w, show) {
  const S = await context(req, w, show);
  S.timeout = Math.max(1000, (req.timeout || 30) * 1000);
  S.pages = []; S.dialogs = []; S.reads = []; S.onDialog = 'accept'; S.dialogText = '';
  const handle = async d => { S.dialogs.push(d.type() + ': ' + d.message()); try { if (S.onDialog === 'dismiss') await d.dismiss(); else await d.accept(S.dialogText || undefined); } catch { /* gone */ } };
  S.ctx.on('page', p => { S.pages.push(p); p.setDefaultTimeout(S.timeout); p.on('dialog', handle); });
  S.page = await S.ctx.newPage();
  if (req.shots) fs.mkdirSync(req.shots, { recursive: true });
  return S;
}

async function saveState(S) {
  if (!S.statePath) return;
  fs.mkdirSync(path.dirname(S.statePath), { recursive: true });
  await S.ctx.storageState({ path: S.statePath }).catch(() => {});
}

const NO_HEAL = ['expect_not', 'expect_count', 'wait_gone'];

// act does one action; it returns a note for the log, and the fallback target used when the target was
// not found (healed).
async function act(S, req, a) {
  let page = S.page; const v = a.value || ''; let t = a.target || '', healed = '';
  const kind = (a.do || '').toLowerCase();
  if (t && a.alt && a.alt.length && !NO_HEAL.includes(kind)) { // self-healing: the first of the target and its fallbacks that is there
    const all = [t, ...a.alt.filter(x => x && x !== t)];
    try {
      let c = loc(page, all[0]); for (const x of all.slice(1)) c = c.or(loc(page, x));
      await c.first().waitFor({ state: 'attached' });
    } catch { /* each is tried below */ }
    for (const x of all) { if (await loc(page, x).count().catch(() => 0)) { if (x !== t) healed = x; t = x; break; } }
  }
  const el = () => loc(page, t).first();
  const shot = name => { const p = path.join(req.shots, name.endsWith('.png') ? name : name + '.png'); return page.screenshot({ path: p, fullPage: true }).then(() => p); };
  let note = '';
  switch (kind) {
    case 'goto': { const r = await page.goto(v || t); note = (r ? r.status() + ' ' : '') + (await page.title()); break; }
    case 'click': await el().click(); break;
    case 'dblclick': await el().dblclick(); break;
    case 'rightclick': await el().click({ button: 'right' }); break;
    case 'fill': await el().fill(v); break;
    case 'type': await el().pressSequentially(v, { delay: 30 }); break;
    case 'clear': await el().fill(''); break;
    case 'press': if (t) await el().press(v || 'Enter'); else await page.keyboard.press(v || 'Enter'); break;
    case 'select': { const sel = el(); try { await sel.selectOption({ label: v }); } catch { await sel.selectOption(v); } break; }
    case 'check': await el().check(); break;
    case 'uncheck': await el().uncheck(); break;
    case 'hover': await el().hover(); break;
    case 'focus': await el().focus(); break;
    case 'scroll': if (t) await el().scrollIntoViewIfNeeded(); else await page.mouse.wheel(0, parseInt(v || '600', 10)); break;
    case 'upload': {
      const files = v.split(',').map(f => path.isAbsolute(f.trim()) ? f.trim() : path.join(req.root || '.', f.trim()));
      await el().setInputFiles(files); note = files.map(f => path.basename(f)).join(', ');
      break;
    }
    case 'download': {
      const [dl] = await Promise.all([page.waitForEvent('download'), el().click()]);
      const p = path.join(req.shots, v || dl.suggestedFilename());
      await dl.saveAs(p); note = p;
      break;
    }
    case 'dialog': S.onDialog = v === 'dismiss' ? 'dismiss' : 'accept'; S.dialogText = v !== 'dismiss' && v !== 'accept' ? v : ''; note = 'dialogs: ' + S.onDialog; break;
    case 'tab': {
      await page.waitForTimeout(300);
      let p = null;
      if (v === '' || v === 'last') p = S.pages[S.pages.length - 1];
      else if (/^\d+$/.test(v)) p = S.pages[+v - 1];
      else p = S.pages.find(x => x.url().includes(v));
      if (!p) throw new Error('no tab ' + (v || 'last') + ' (' + S.pages.length + ' open)');
      S.page = page = p; await page.bringToFront(); note = page.url();
      break;
    }
    case 'close_tab': { await page.close(); S.pages.splice(S.pages.indexOf(page), 1); S.page = page = S.pages[S.pages.length - 1]; note = page ? page.url() : ''; break; }
    case 'reload': await page.reload(); break;
    case 'back': await page.goBack(); break;
    case 'wait':
      if (t) await el().waitFor({ state: 'visible' });
      else if (v && !/^[\d.]+$/.test(v)) await page.waitForURL(u => u.toString().includes(v));
      else await page.waitForTimeout(Math.min(600, parseFloat(v) || 1) * 1000);
      break;
    case 'wait_gone': await el().waitFor({ state: 'hidden' }); break;
    case 'expect': {
      if (t && v) { await el().waitFor({ state: 'visible' }); const deadline = Date.now() + Math.min(S.timeout, 5000); let txt = '';
        while (!(txt = await el().innerText()).includes(v) && Date.now() < deadline) await page.waitForTimeout(250);
        if (!txt.includes(v)) throw new Error('found "' + txt.slice(0, 200) + '"'); note = 'found'; }
      else { const l = t ? el() : page.getByText(v).first(); await l.waitFor({ state: 'visible' }); note = 'found'; }
      break;
    }
    case 'expect_not': {
      const l = t ? loc(page, t) : page.getByText(v);
      const deadline = Date.now() + Math.min(S.timeout, 5000);
      while (await l.count() > 0 && Date.now() < deadline) await page.waitForTimeout(250);
      if (await l.count() > 0) throw new Error('it is still there');
      note = 'not there';
      break;
    }
    case 'expect_value': { const got = await el().inputValue(); if (got !== v) throw new Error('found "' + got.slice(0, 200) + '"'); note = 'found'; break; }
    case 'expect_count': { await page.waitForTimeout(200); const n = await loc(page, t).count(); if (String(n) !== String(v)) throw new Error('found ' + n); note = n + ''; break; }
    case 'expect_checked': { const c = await el().isChecked(); if (c !== (v !== 'false')) throw new Error(c ? 'it is ticked' : 'it is not ticked'); note = c ? 'ticked' : 'not ticked'; break; }
    case 'expect_enabled': { const e = await el().isEnabled(); if (e !== (v !== 'false')) throw new Error(e ? 'it is enabled' : 'it is disabled'); note = e ? 'enabled' : 'disabled'; break; }
    case 'expect_url': {
      const deadline = Date.now() + Math.min(S.timeout, 10000);
      while (!page.url().includes(v) && Date.now() < deadline) await page.waitForTimeout(250);
      if (!page.url().includes(v)) throw new Error('the address is ' + page.url());
      note = page.url();
      break;
    }
    case 'expect_title': { const ti = await page.title(); if (!ti.includes(v)) throw new Error('the title is "' + ti + '"'); note = ti; break; }
    case 'read': {
      const e = el();
      const txt = (await e.evaluate(n => n.tagName === 'INPUT' || n.tagName === 'TEXTAREA' || n.tagName === 'SELECT')) ? await e.inputValue() : (await e.innerText()).trim();
      S.reads.push({ name: v || t, value: txt }); note = txt.slice(0, 200);
      break;
    }
    case 'screenshot': { const n = v || ('step-' + Date.now()); if (t) { const p = path.join(req.shots, n.endsWith('.png') ? n : n + '.png'); await el().screenshot({ path: p }); note = p; } else note = await shot(n); break; }
    default: throw new Error('unknown action ' + a.do);
  }
  return { note, healed };
}

// login: the app's recorded login, when its first field shows (the app asks to log in); then back to url.
async function login(S, req, url) {
  const L = req.login || [];
  const first = L.find(a => a.target);
  if (!first) return false;
  // the login form can come late (a single sign-on page that redirects and draws itself): wait for its first
  // field until the page has settled (no network for a moment), then 4 s more - at most 20 s
  const field = loc(S.page, first.target).first();
  // a web app opened for a person: wait for its login page up to 30 s (a person then sees what happened)
  const shows = req.person ? await field.waitFor({ state: 'visible', timeout: 30000 }).then(() => true, () => false) : await Promise.race([
    field.waitFor({ state: 'visible', timeout: 20000 }).then(() => true, () => false),
    S.page.waitForLoadState('networkidle', { timeout: 20000 }).catch(() => {})
      .then(() => field.waitFor({ state: 'visible', timeout: 4000 }).then(() => true, () => false)),
  ]);
  if (!shows) { out({ i: -1, ok: true, note: 'no login asked: ' + first.target + ' did not show on ' + S.page.url() + (req.person ? ' in 30 s' : '') }); return false; }
  // after the password is typed and sent (a click or Enter), the login is done: a click recorded after it
  // (on the app's first page, e.g. "Dashboard") is tried but does not fail the login - that page can differ
  let typed = false, sent = false;
  for (const a of L) {
    try { await act(S, req, a); } catch (e) {
      const msg = String(e.message || e).split('\n')[0];
      if (sent) { out({ i: -1, ok: true, note: 'after the login, skipped: ' + (a.do + ' ' + (a.target || '')).trim() }); continue; }
      throw new Error('the app\'s login (' + (a.do + ' ' + (a.target || '')).trim() + '): ' + msg);
    }
    if (a.secret) typed = true;
    else if (typed && /^(click|press|dblclick|check|submit)$/i.test(a.do || '')) sent = true;
  }
  await S.page.waitForLoadState('networkidle', { timeout: 10000 }).catch(() => {});
  await saveState(S);
  if (url && !S.page.url().startsWith(url)) await S.page.goto(url);
  return true;
}

async function start(S, req, w) {
  if (!w.url) return;
  const r = await S.page.goto(w.url);
  out({ i: -1, ok: true, note: (r ? r.status() + ' ' : '') + (await S.page.title()) });
  if (await login(S, req, w.url)) out({ i: -1, ok: true, note: 'logged in' });
}

async function picture(S, req, i) {
  if (!req.pictures) return '';
  const p = path.join(req.shots, (req.prefix || 'web') + '-' + String(i + 1).padStart(2, '0') + '.jpg');
  return S.page.screenshot({ path: p, type: 'jpeg', quality: 55 }).then(() => p, () => '');
}

async function run(req) {
  const w = req.step;
  const S = await open(req, w, !!w.show);
  if (w.trace) await S.ctx.tracing.start({ screenshots: true, snapshots: true });
  let i = -1;
  try {
    await start(S, req, w);
    let last = '';
    for (i = 0; i < (w.actions || []).length; i++) {
      const { note, healed } = await act(S, req, w.actions[i]);
      last = await picture(S, req, i);
      out({ i, ok: true, note, healed, pic: last });
    }
    await saveState(S);
    const text = (await S.page.innerText('body').catch(() => '')).slice(0, 20000);
    // the page at the end: the step's checks run on it - a failed check shows this picture
    let end = '';
    if (req.failShot && req.shots) {
      end = last;
      if (!end) { const p = path.join(req.shots, (req.prefix || 'web') + '-end-' + Date.now() + '.png'); end = await S.page.screenshot({ path: p, fullPage: true }).then(() => p, () => ''); }
    }
    out({ done: true, url: S.page.url(), title: await S.page.title(), text, reads: S.reads, dialogs: S.dialogs, end });
    if (w.trace) await S.ctx.tracing.stop({ path: path.join(req.shots, 'trace-' + Date.now() + '.zip') });
    await S.ctx.close().catch(() => {});
  } catch (e) {
    let p = '', tr = '', vid = '';
    if (req.failShot !== false) try { const pp = path.join(req.shots, 'failed-' + Date.now() + '.png'); await S.page.screenshot({ path: pp, fullPage: true }); p = pp; } catch { /* no page */ }
    if (w.trace) { tr = path.join(req.shots, 'trace-failed-' + Date.now() + '.zip'); try { await S.ctx.tracing.stop({ path: tr }); } catch { tr = ''; } }
    if (S.videoDir) {
      const v = S.page && S.page.video(); await S.ctx.close().catch(() => {});
      try { if (v) { vid = path.join(req.shots, 'failed-' + Date.now() + '.webm'); await v.saveAs(vid); } } catch { vid = ''; }
    }
    out({ i, ok: false, error: String(e && e.message || e).split('\n')[0], shot: p, trace: tr, video: vid, url: S.page ? S.page.url() : '', dialogs: S.dialogs });
  } finally {
    await S.browser.close().catch(() => {});
    if (S.videoDir) fs.rmSync(S.videoDir, { recursive: true, force: true });
  }
}

// ---- snapshot: the page's visible elements a person (or an AI) can act on, each with a target ----
const SNAPSHOT = `(() => {
  const q = s => JSON.stringify(s);
  const all = (root, out) => { for (const el of root.querySelectorAll('*')) { out.push(el); if (el.shadowRoot) all(el.shadowRoot, out); } return out; };
  const els = all(document, []);
  const unique = (root, s) => { try { return root.querySelectorAll(s).length === 1; } catch { return false; } };
  const visible = el => { const r = el.getBoundingClientRect(); if (r.width < 2 || r.height < 2) return false; const st = getComputedStyle(el); return st.visibility !== 'hidden' && st.display !== 'none' && st.opacity !== '0'; };
  const role = el => { const t = el.tagName.toLowerCase(), ty = (el.getAttribute('type') || 'text').toLowerCase();
    return el.getAttribute('role') || (t === 'button' || (t === 'input' && ['button', 'submit', 'reset'].includes(ty)) ? 'button' : t === 'a' && el.hasAttribute('href') ? 'link'
      : t === 'select' ? 'combobox' : t === 'input' && ty === 'checkbox' ? 'checkbox' : t === 'input' && ty === 'radio' ? 'radio' : t === 'textarea' || (t === 'input' && !['hidden', 'file', 'image'].includes(ty)) ? 'textbox'
      : t === 'input' && ty === 'file' ? 'file' : el.isContentEditable ? 'textbox' : t === 'summary' ? 'button' : ''); };
  const label = el => { const l = el.labels && el.labels[0]; if (l) return l.innerText.trim().split('\\n')[0];
    const by = el.getAttribute('aria-labelledby'); if (by) { const x = by.split(/\\s+/).map(i => document.getElementById(i)).filter(Boolean).map(e => e.innerText.trim()).join(' '); if (x) return x; } return ''; };
  const own = el => { const t = (el.innerText || '').trim(); if (t) return t; const r = el.getRootNode(); return r && r.host ? (r.host.getAttribute('aria-label') || r.host.innerText || '').trim() : ''; }; // a button in a web component: its host's text
  const name = el => (el.getAttribute('aria-label') || label(el) || el.getAttribute('title') || (['INPUT', 'TEXTAREA', 'SELECT'].includes(el.tagName) ? '' : own(el).split('\\n')[0]) || el.getAttribute('placeholder') || el.value || '').replace(/\\s+/g, ' ').trim().slice(0, 80);
  const randomId = id => /^[A-Za-z0-9_-]{12,}$/.test(id) && /[A-Z]/.test(id) && /[a-z]/.test(id) && /\\d/.test(id);
  const out = [], seen = new Set();
  for (const el of els) {
    if (out.length >= 300) break;
    const r = role(el); if (!r || !visible(el) || seen.has(el)) continue;
    const t = el.tagName.toLowerCase(), field = ['input', 'textarea', 'select'].includes(t) || el.isContentEditable, nm = name(el), root = el.getRootNode();
    if (!field && r !== 'checkbox' && r !== 'radio' && !nm) continue;
    const tg = [], add = x => { if (x && !tg.includes(x)) tg.push(x); };
    const tid = el.getAttribute('data-testid') || el.getAttribute('data-test-id') || el.getAttribute('data-test'); if (tid) add('testid=' + tid);
    if (field) { const lb = label(el); if (lb && lb.length < 50) add('label=' + lb); if (el.placeholder) add('placeholder=' + el.placeholder); }
    if (nm && r !== 'textbox') add('role=' + r + '[name=' + q(nm) + ']');
    if (el.id && !/^\\d|[:\\s]|\\d{3,}/.test(el.id) && !randomId(el.id) && unique(root, '#' + CSS.escape(el.id))) add('#' + CSS.escape(el.id));
    if (field && el.name && unique(root, t + '[name=' + q(el.name) + ']')) add(t + '[name=' + q(el.name) + ']');
    if (root && root.host) { const hc = [...root.host.classList].find(c => !/^(ng-|en-u-|is-|has-)/.test(c) && c.length > 3); if (hc) add(root.host.tagName.toLowerCase() + '.' + CSS.escape(hc) + ' ' + t); }
    if (!tg.length && nm && nm.length < 40) add('text=' + nm);
    if (!tg.length) continue;
    seen.add(el);
    const e = { role: r, name: nm, target: tg[0] };
    if (tg.length > 1) e.alt = tg.slice(1, 3);
    if (field && r !== 'checkbox' && r !== 'radio') { const v = t === 'select' ? (el.selectedOptions[0] || {}).text : el.isContentEditable ? el.innerText : el.value; if (v && el.type !== 'password') e.value = String(v).slice(0, 80); if (el.type === 'password') e.secret = true; }
    if (t === 'select') e.options = [...el.options].slice(0, 30).map(o => o.text.trim());
    if (r === 'checkbox' || r === 'radio') e.checked = !!el.checked || el.getAttribute('aria-checked') === 'true';
    if (el.disabled || el.getAttribute('aria-disabled') === 'true') e.disabled = true;
    out.push(e);
  }
  const heads = els.filter(e => /^H[1-3]$/.test(e.tagName) && visible(e)).slice(0, 20).map(e => e.innerText.trim().slice(0, 80)).filter(Boolean);
  const dialog = els.find(e => (e.getAttribute('role') === 'dialog' || e.tagName === 'DIALOG') && visible(e));
  return { elements: out, headings: heads, dialog: dialog ? (dialog.getAttribute('aria-label') || dialog.innerText.trim().split('\\n')[0] || 'a dialog').slice(0, 80) : '' };
})()`;

// ---- recording: what the person does in the browser becomes actions ----
const RECORDER = `(() => {
  if (window.__twRecOn) return; window.__twRecOn = true;
  if (window.__twPicking === undefined) window.__twPicking = false;
  const q = s => JSON.stringify(s);
  const unique = (s) => { try { return document.querySelectorAll(s).length === 1; } catch { return false; } };
  const text = el => (el.innerText || el.value || '').trim().split('\\n')[0].replace(/\\s+/g, ' ').slice(0, 60);
  function role(el) {
    const t = el.tagName.toLowerCase(), ty = (el.getAttribute('type') || '').toLowerCase();
    return el.getAttribute('role') || (t === 'button' || (t === 'input' && ['button', 'submit', 'reset'].includes(ty)) ? 'button'
      : t === 'a' && el.hasAttribute('href') ? 'link' : t === 'select' ? 'combobox' : t === 'input' && ty === 'checkbox' ? 'checkbox'
      : t === 'input' && ty === 'radio' ? 'radio' : t === 'textarea' || (t === 'input' && !ty) || ty === 'text' ? 'textbox' : '');
  }
  // targets: the ways to find el, the most readable first (the rest are fallbacks when the GUI changes)
  function targets(el) {
    const out = [], add = x => { if (x && !out.includes(x)) out.push(x); };
    const tid = el.getAttribute('data-testid') || el.getAttribute('data-test-id') || el.getAttribute('data-test');
    if (tid) add('testid=' + tid);
    const t = el.tagName.toLowerCase(), field = ['input', 'textarea', 'select'].includes(t);
    if (field) { // a field: by what the person reads next to it
      const lab = el.labels && el.labels[0] && el.labels[0].innerText.trim().split('\\n')[0];
      if (lab && lab.length < 50) add('label=' + lab);
      if (el.placeholder) add('placeholder=' + el.placeholder);
    }
    // a field in a web component (shadow DOM): its host's own class names it (Playwright's CSS looks inside)
    const root = el.getRootNode && el.getRootNode();
    if (root && root.host) {
      const hc = [...root.host.classList].find(c => !/^(ng-|en-u-|is-|has-)/.test(c) && c.length > 3);
      if (hc) add(root.host.tagName.toLowerCase() + '.' + CSS.escape(hc) + ' ' + t);
    }
    const randomId = id => /^[A-Za-z0-9_-]{12,}$/.test(id) && /[A-Z]/.test(id) && /[a-z]/.test(id) && /\\d/.test(id); // made anew on every load
    if (el.id && !/^\\d|[:\\s]|\\d{3,}/.test(el.id) && !randomId(el.id) && unique('#' + CSS.escape(el.id))) add('#' + CSS.escape(el.id));
    if (field && el.name && unique(t + '[name=' + q(el.name) + ']')) add(t + '[name=' + q(el.name) + ']');
    const r = role(el), name = (el.getAttribute('aria-label') || el.getAttribute('title') || text(el)).trim();
    if (r && name) add('role=' + r + '[name=' + q(name) + ']');
    if (name && name.length < 40 && !field) add('text=' + name);
    const path = []; let n = el;
    while (n && n.nodeType === 1 && n !== document.body && path.length < 6) {
      let s = n.tagName.toLowerCase();
      if (n.id && unique('#' + CSS.escape(n.id))) { path.unshift('#' + CSS.escape(n.id)); break; }
      const sib = [...(n.parentElement ? n.parentElement.children : [])].filter(x => x.tagName === n.tagName);
      if (sib.length > 1) s += ':nth-of-type(' + (sib.indexOf(n) + 1) + ')';
      path.unshift(s); n = n.parentElement;
    }
    add(path.join(' > '));
    return out;
  }
  const T = el => { const l = targets(el); return { target: l[0], alt: l.slice(1, 3) }; };
  window.__twTargets = targets;
  const clickable = el => el.closest('a,button,[role=button],[role=link],[role=tab],[role=menuitem],input[type=button],input[type=submit],input[type=checkbox],input[type=radio],label,summary,option,li,td,span,div') || el;
  const mine = e => e.composedPath && e.composedPath().some(n => n && n.id === '__tw_rec'); // the toolbar's own clicks
  const on = () => window.__twRecOn2 && !window.__twPaused;
  const send = (a, el) => { if (!on() || !window.__twRec) return; if (window.__twSay) a.words = window.__twSay(a, el); window.__twRec(a).then(st => st && window.__twShow && window.__twShow(st)); };
  // the element itself, also inside a web component (shadow DOM): e.target is only its host there
  const real = e => (e.composedPath && e.composedPath()[0]) || e.target;
  // typing in a field of a web component: its change event does not leave the component, so the value is
  // taken when the field loses the focus (or Enter, or a click elsewhere)
  let pend = null;
  const flush = () => {
    if (!pend) return; const el = pend; pend = null;
    const ty = (el.getAttribute('type') || '').toLowerCase();
    send({ do: 'fill', ...T(el), value: ty === 'password' ? '{{password}}' : el.value, secret: ty === 'password' });
  };
  document.addEventListener('input', e => {
    if (!on() || mine(e)) return;
    const el = real(e), t = el.tagName && el.tagName.toLowerCase(), ty = (el.getAttribute && el.getAttribute('type') || '').toLowerCase();
    if (el === e.target) return; // not in a web component: its change records it
    if ((t === 'input' && !['checkbox', 'radio', 'file', 'button', 'submit'].includes(ty)) || t === 'textarea') { if (pend && pend !== el) flush(); pend = el; }
  }, true);
  document.addEventListener('focusout', e => { if (on() && pend && real(e) === pend) flush(); }, true);
  window.addEventListener('beforeunload', () => { if (on()) flush(); });
  // a check instead of a click (the toolbar's Check modes, or Alt+click): what must be true there
  const check = (mode, tg) => {
    const field = tg.closest && tg.closest('input,textarea,select'), el = field || clickable(tg), g = T(el), ty = field ? (field.getAttribute('type') || '').toLowerCase() : '';
    if (mode === 'visible') send({ do: 'expect', ...g }, el);
    else if (mode === 'value' && field && (ty === 'checkbox' || ty === 'radio')) send({ do: 'expect_checked', ...g, value: field.checked ? 'false' : 'true' }); // the click flipped it already (it is cancelled): the state before
    else if (mode === 'value' && field) send({ do: 'expect_value', ...g, value: field.tagName === 'SELECT' ? field.value : field.value });
    else send(g.target.startsWith('text=') ? { do: 'expect', value: text(el) } : { do: 'expect', ...g, value: text(el) }, el);
    flash(el, '#3fb950');
  };
  document.addEventListener('click', e => {
    if (!on() || mine(e)) return;
    flush();
    const tg = real(e), mode = window.__twMode || 'rec';
    if (mode !== 'rec' || e.altKey) { e.preventDefault(); e.stopPropagation(); check(mode === 'rec' ? 'text' : mode, tg); if (!e.shiftKey) window.__twSetMode('rec'); return; }
    if (tg.closest && tg.closest('input,textarea,select,option')) return; // fields: their change
    const el = clickable(tg);
    const t = el.tagName.toLowerCase(), ty = (el.getAttribute('type') || '').toLowerCase();
    if (t === 'label' && el.control) return; // its field records the change
    if ((t === 'input' && !['button', 'submit', 'reset', 'checkbox', 'radio'].includes(ty)) || t === 'textarea' || t === 'select' || t === 'option') return;
    if (t === 'input' && (ty === 'checkbox' || ty === 'radio')) return;
    send({ do: 'click', ...T(el) }, el); flash(el, '#58a6ff');
  }, true);
  document.addEventListener('change', e => {
    if (!on() || mine(e)) return;
    const el = e.target, t = el.tagName.toLowerCase(), ty = (el.getAttribute('type') || '').toLowerCase();
    if (t === 'select') send({ do: 'select', ...T(el), value: el.options[el.selectedIndex] ? el.options[el.selectedIndex].text.trim() : el.value });
    else if (ty === 'checkbox' || ty === 'radio') send({ do: el.checked ? 'check' : 'uncheck', ...T(el) });
    else if (ty === 'file') send({ do: 'upload', ...T(el), value: [...el.files].map(f => f.name).join(',') });
    else if (t === 'input' || t === 'textarea') send({ do: 'fill', ...T(el), value: ty === 'password' ? '{{password}}' : el.value, secret: ty === 'password' });
  }, true);
  document.addEventListener('keydown', e => {
    if (!window.__twRecOn2 || mine(e)) return;
    if (e.key === 'Escape' && (window.__twMode || 'rec') !== 'rec') { window.__twSetMode('rec'); return; }
    if (!on()) return;
    const el = real(e);
    if (e.key === 'Enter' && el && ['INPUT'].includes(el.tagName)) {
      if (pend === el) pend = null;
      const ty = (el.getAttribute('type') || '').toLowerCase();
      send({ do: 'fill', ...T(el), value: ty === 'password' ? '{{password}}' : el.value, secret: ty === 'password' });
      send({ do: 'press', ...T(el), value: 'Enter' });
    }
  }, true);
  for (const ev of ['mousedown', 'mouseup', 'pointerdown', 'pointerup']) document.addEventListener(ev, e => {
    if (on() && !mine(e) && (window.__twMode || 'rec') !== 'rec') { e.preventDefault(); e.stopPropagation(); }
  }, true);
  // hover: the element under the mouse and the target the action would use
  let hoverEl = null, raf = 0;
  document.addEventListener('mousemove', e => {
    if (!on() || mine(e)) { window.__twHover && window.__twHover(null); return; }
    hoverEl = real(e); if (raf) return;
    raf = requestAnimationFrame(() => { raf = 0; const el = hoverEl && (window.__twMode === 'value' ? (hoverEl.closest('input,textarea,select') || hoverEl) : clickable(hoverEl)); window.__twHover && window.__twHover(el, el ? T(el).target : ''); });
  }, true);
  function flash(el, c) { const o = el.style.outline; el.style.outline = '2px solid ' + c; setTimeout(() => { el.style.outline = o; }, 500); }
  // the toolbar: in its own shadow root (the page's styles do not reach it), its clicks are not recorded
  const say = (a, el) => { const nm = t => { if (!t) return 'the page'; if (el && /^[#.\\[a-z]/i.test(t) && !/^(label|text|placeholder|testid|title|alt|role)=/.test(t)) { const x = (el.getAttribute('aria-label') || text(el) || '').trim(); if (x) return x.length > 32 ? x.slice(0, 30) + '…' : x; } let m = /name=("(?:[^"\\\\]|\\\\.)*")/.exec(t); if (m) { try { return JSON.parse(m[1]); } catch { } }
      m = /^(?:label|text|placeholder|testid|title|alt)=(.*)$/.exec(t); return m ? m[1] : t.length > 32 ? t.slice(0, 30) + '…' : t; };
    const q = x => '“' + x + '”', n = q(nm(a.target)), v = a.value || '';
    switch (a.do) { case 'click': return 'Click ' + n; case 'fill': return 'Type ' + q(v) + ' into ' + n; case 'select': return 'Choose ' + q(v) + ' in ' + n;
      case 'check': return 'Tick ' + n; case 'uncheck': return 'Untick ' + n; case 'press': return 'Press ' + v; case 'upload': return 'Upload ' + v;
      case 'expect': { const nn = nm(a.target); return a.target ? (v ? (nn.indexOf(v) >= 0 || v.indexOf(nn) >= 0 ? '✓ ' + q(v) + ' is shown' : '✓ ' + n + ' shows ' + q(v)) : '✓ ' + n + ' is shown') : '✓ the page shows ' + q(v); }
      case 'expect_value': return '✓ ' + n + ' is ' + q(v); case 'expect_checked': return '✓ ' + n + (v === 'false' ? ' is not ticked' : ' is ticked'); case 'tab': return 'Go to the new tab'; }
    return a.do + ' ' + n; };
  window.__twSay = say;
  let ui = null;
  const build = () => {
    if (ui || !document.body) return ui;
    const host = document.createElement('div'); host.id = '__tw_rec';
    host.style.cssText = 'all:initial;position:fixed;z-index:2147483647;bottom:14px;left:50%;transform:translateX(-50%);display:flex;flex-direction:column-reverse;align-items:center'; // bottom centre: app menus are on top
    const r = host.attachShadow({ mode: 'open' });
    r.innerHTML = \`<style>
      :host{all:initial} *{box-sizing:border-box;font:12.5px -apple-system,system-ui,Segoe UI,sans-serif}
      .tb{display:flex;align-items:center;gap:2px;background:#1b1f24;color:#e6e8eb;border:1px solid #30363d;border-radius:10px;box-shadow:0 10px 30px rgba(0,0,0,.35);padding:5px;user-select:none}
      .st{display:flex;align-items:center;gap:7px;padding:0 9px;font-weight:600;cursor:move;white-space:nowrap} .dot{width:8px;height:8px;border-radius:50%;background:#f85149;box-shadow:0 0 0 3px rgba(248,81,73,.25)}
      .tb.paused .dot{background:#d29922;box-shadow:none}
      button{all:unset;cursor:pointer;padding:5px 9px;border-radius:6px;color:#c9d1d9;white-space:nowrap} button:hover{background:#2d333b} button.on{background:rgba(47,129,247,.22);color:#79c0ff}
      button.done{color:#3fb950;font-weight:600} button:disabled{opacity:.35;cursor:default;background:none}
      .sep{width:1px;height:18px;background:#30363d;margin:0 3px}
      .feed{margin-bottom:6px;background:#1b1f24;border:1px solid #30363d;border-radius:10px;padding:5px 0;box-shadow:0 10px 30px rgba(0,0,0,.35);max-width:420px}
      .feed div{padding:3px 12px;color:#c9d1d9;display:flex;gap:8px;white-space:nowrap;overflow:hidden;text-overflow:ellipsis} .feed div span{color:#8b949e;min-width:16px;text-align:right}
      .feed div:last-child{background:rgba(47,129,247,.1)} .feed:empty{display:none}
      .hint{margin-bottom:6px;background:#2f81f7;color:#fff;border-radius:8px;padding:5px 10px;display:none} .hint.show{display:block}
    </style>
    <div class="wrap" style="display:flex;flex-direction:column-reverse;align-items:center"><div class="tb" part="bar"><span class="st" title="Drag to move"><span class="dot"></span><span class="lab">Recording</span></span><span class="sep"></span>
      <button data-a="pause" title="Stop recording for a while (actions are not kept)">⏸ Pause</button>
      <button data-m="text" title="Click a text: the test checks it is shown there">✓ Check text</button>
      <button data-m="visible" title="Click an element: the test checks it is visible">👁 Check visible</button>
      <button data-m="value" title="Click a field: the test checks its value (or that a checkbox is ticked)">= Check value</button>
      <span class="sep"></span><button data-a="undo" title="Drop the last recorded action">⤺ Undo</button><button data-a="done" class="done" title="Finish recording">Done</button></div>
    <div class="hint"></div><div class="feed"></div></div>\`;
    document.documentElement.appendChild(host);
    const box = document.createElement('div'), tag = document.createElement('div');
    box.style.cssText = 'position:fixed;z-index:2147483646;pointer-events:none;border:2px solid #2f81f7;border-radius:3px;display:none;transition:all .05s';
    tag.style.cssText = 'position:fixed;z-index:2147483646;pointer-events:none;background:#2f81f7;color:#fff;font:11px ui-monospace,Menlo,monospace;padding:2px 6px;border-radius:4px;display:none;max-width:60vw;overflow:hidden;text-overflow:ellipsis;white-space:nowrap';
    host.__box = box; host.__tag = tag; document.documentElement.appendChild(box); document.documentElement.appendChild(tag);
    const $ = s => r.querySelector(s), tb = $('.tb');
    r.addEventListener('click', e => {
      const b = e.target.closest('button'); if (!b || b.disabled) return;
      if (b.dataset.m) window.__twSetMode(window.__twMode === b.dataset.m ? 'rec' : b.dataset.m);
      else if (b.dataset.a === 'pause') { window.__twPaused = !window.__twPaused; window.__twSetMode('rec'); paint(); }
      else if (b.dataset.a === 'undo') window.__twRec({ undo: true }).then(st => st && window.__twShow(st));
      else if (b.dataset.a === 'done') { flush(); window.__twRec({ done: true }); }
    });
    // drag the toolbar out of the way
    let drag = null;
    $('.st').addEventListener('mousedown', e => { const b = host.getBoundingClientRect(); drag = { x: e.clientX - b.left, y: e.clientY - b.top }; e.preventDefault(); });
    addEventListener('mousemove', e => { if (!drag) return; host.style.transform = 'none'; host.style.bottom = 'auto'; host.style.left = Math.max(0, e.clientX - drag.x) + 'px'; host.style.top = Math.max(0, e.clientY - drag.y) + 'px'; }, true);
    addEventListener('mouseup', () => { drag = null; }, true);
    ui = { host, r, tb, box, tag };
    return ui;
  };
  let last = { count: 0, last: [] };
  const paint = () => {
    const u = build(); if (!u) return;
    const m = window.__twMode || 'rec', $ = s => u.r.querySelector(s);
    u.tb.classList.toggle('paused', !!window.__twPaused);
    $('.lab').textContent = (window.__twPaused ? 'Paused' : 'Recording') + (last.count ? ' · ' + last.count : '');
    $('[data-a=pause]').textContent = window.__twPaused ? '▶ Resume' : '⏸ Pause';
    for (const b of u.r.querySelectorAll('[data-m]')) { b.classList.toggle('on', b.dataset.m === m); b.disabled = !!window.__twPaused; }
    $('[data-a=undo]').disabled = !last.count;
    const h = $('.hint'); h.classList.toggle('show', m !== 'rec');
    h.textContent = m === 'text' ? 'Click the text to check (Esc: back to recording, Shift+click: several)' : m === 'visible' ? 'Click what must be visible (Esc: back)' : m === 'value' ? 'Click the field to check its value (Esc: back)' : '';
    $('.feed').innerHTML = last.last.map((w, i) => '<div><span>' + (last.count - last.last.length + i + 1) + '</span>' + w.replace(/[&<>]/g, c => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;' }[c])) + '</div>').join('');
  };
  window.__twShow = st => { last = { count: st.count || 0, last: (st.last || []).slice(-3) }; paint(); };
  window.__twSetMode = m => { window.__twMode = m; if (m === 'rec') window.__twHover && window.__twHover(null); paint(); };
  window.__twHover = (el, target) => { const u = ui; if (!u) return;
    if (!el || !el.getBoundingClientRect) { u.box.style.display = u.tag.style.display = 'none'; return; }
    const b = el.getBoundingClientRect(), c = (window.__twMode || 'rec') === 'rec' ? '#2f81f7' : '#3fb950';
    Object.assign(u.box.style, { display: 'block', left: b.left - 2 + 'px', top: b.top - 2 + 'px', width: b.width + 4 + 'px', height: b.height + 4 + 'px', borderColor: c });
    Object.assign(u.tag.style, { display: 'block', left: Math.max(0, b.left - 2) + 'px', top: Math.max(0, b.top - 22) + 'px', background: c }); u.tag.textContent = target; };
  window.__twSetRec = on => { window.__twRecOn2 = on; if (on) { build(); paint(); } else if (ui) { ui.host.remove(); ui.box.remove(); ui.tag.remove(); ui = null; } };
  const init = () => (window.__twState ? window.__twState() : Promise.resolve({ rec: true })).then(st => { window.__twSetRec(!!st.rec); if (st.rec) window.__twShow(st); });
  if (document.readyState === 'loading') document.addEventListener('DOMContentLoaded', init); else init();
})();`;

// picking: the person clicks the element to use; its targets come back.
const PICKER = `new Promise(resolve => {
  const box = document.createElement('div'), tip = document.createElement('div');
  box.style.cssText = 'position:fixed;z-index:2147483646;pointer-events:none;border:2px solid #58a6ff;background:#58a6ff22;border-radius:3px;transition:all .05s';
  tip.style.cssText = 'position:fixed;z-index:2147483647;left:50%;top:10px;transform:translateX(-50%);background:#1f2328;color:#fff;font:12px system-ui;padding:6px 12px;border-radius:14px;box-shadow:0 4px 16px #0006;pointer-events:none';
  tip.textContent = '⌖ Click the element for the action (Esc: cancel)';
  document.body.append(box, tip);
  let cur = null;
  const move = e => { cur = e.target; const r = cur.getBoundingClientRect(); Object.assign(box.style, { left: r.left + 'px', top: r.top + 'px', width: r.width + 'px', height: r.height + 'px' }); tip.textContent = '⌖ ' + (window.__twTargets(cur)[0] || '') + '   (Esc: cancel)'; };
  const done = v => { removeEventListener('mousemove', move, true); removeEventListener('click', click, true); removeEventListener('keydown', key, true); box.remove(); tip.remove(); resolve(v); };
  const click = e => { e.preventDefault(); e.stopPropagation(); const l = window.__twTargets(e.target); done({ target: l[0], alt: l.slice(1, 3), text: (e.target.innerText || e.target.value || '').trim().slice(0, 80), tag: e.target.tagName.toLowerCase() }); };
  const key = e => { if (e.key === 'Escape') { e.preventDefault(); done(null); } };
  addEventListener('mousemove', move, true); addEventListener('click', click, true); addEventListener('keydown', key, true);
})`;

// recorder sets up recording in a browser: recorded actions are printed while S.rec is on.
async function recorder(S, req) {
  const filled = {};
  S.rec = false; S.recLog = [];
  const st = () => ({ rec: S.rec, count: S.recLog.length, last: S.recLog.slice(-3).map(x => x.words) });
  // values the test already has as variables are recorded as {{name}} (longest first)
  const vars = Object.entries((req && req.recVars) || {}).filter(([k, v]) => k && v && String(v).length > 1).sort((a, b) => String(b[1]).length - String(a[1]).length);
  const named = v => { if (typeof v !== 'string' || !v) return v; for (const [k, x] of vars) if (v === x) return '{{' + k + '}}'; for (const [k, x] of vars) if (String(x).length > 3 && v.includes(x)) v = v.split(x).join('{{' + k + '}}'); return v; };
  await S.ctx.exposeBinding('__twState', () => st());
  await S.ctx.exposeBinding('__twRec', async (src, a) => {
    if (!a) return st();
    if (a.done) { if (S.onDone) await S.onDone(); return st(); }
    if (a.undo) { if (S.recLog.length) { S.recLog.pop(); out({ rec: { do: '__undo' } }); } return st(); }
    if (!a.do || !S.rec) return st();
    if (a.do === 'fill') { // the same value again (Enter, then the change event): once
      if (filled[a.target] === a.value) return st();
      filled[a.target] = a.value;
    }
    if (!a.secret && a.value !== undefined) a.value = named(a.value);
    const words = a.words || ''; delete a.words;
    S.recLog.push({ words: words || a.do });
    out({ rec: a });
    return st();
  });
  await S.ctx.addInitScript(RECORDER);
  S.ctx.on('page', p => { if (S.rec && S.pages.length > 1) { S.recLog.push({ words: 'Go to the new tab' }); out({ rec: { do: 'tab', value: 'last' } }); } });
  S.setRec = async on => { S.rec = on; for (const p of S.pages) await p.evaluate(v => window.__twSetRec && window.__twSetRec(v), on).catch(() => {}); };
}

// closed: the person closed the browser - or its last window (on macOS the browser keeps running then)
const closed = S => new Promise(resolve => {
  S.browser.on('disconnected', resolve); S.ctx.on('close', resolve);
  const watch = p => p.on('close', () => { if (S.ctx.pages().length === 0) resolve(); });
  S.ctx.pages().forEach(watch); S.ctx.on('page', watch);
});

async function drive(S, list) { // tests: do these as a person would
  for (const d of list) {
    const el = loc(S.page, d.target).first();
    if (d.do === 'fill') { await el.click(); await el.fill(d.value); await el.press('Tab'); }
    else if (d.do === 'select') await el.selectOption({ label: d.value });
    else if (d.do === 'altclick') await el.click({ modifiers: ['Alt'] });
    else await el.click();
    await S.page.waitForTimeout(150);
  }
  await S.page.waitForTimeout(300);
}

async function record(req) {
  const w = req.step;
  const S = await open(req, w, !req.drive);
  await recorder(S, req);
  S.onDone = async () => { await saveState(S); await S.ctx.close().catch(() => {}); }; // Done: the same as closing the window
  try { await start(S, req, w); } catch (e) { out({ i: -1, ok: false, error: String(e.message || e).split('\n')[0] }); }
  await saveState(S);
  await S.setRec(true);
  if (req.drive) { await drive(S, req.drive); out({ done: true, url: S.page.url() }); await S.browser.close(); return; }
  await closed(S);
  await saveState(S); // logged in now: the next browser starts logged in
  out({ done: true });
  await S.browser.close().catch(() => {});
}

// serve keeps a browser open for step-through runs: commands come one per line on stdin, each answered
// by its lines and then { ack: id }. The browser closing ends it ({ closed: true }).
async function serve(req, lines) {
  const w = req.step;
  const S = await open(req, w, !req.drive && !req.headless);
  await recorder(S, req);
  S.onDone = async () => { await S.setRec(false); out({ rec: { do: '__done' } }); }; // Done in a step-by-step browser: stop recording, keep it open
  if (req.trace) await S.ctx.tracing.start({ screenshots: true, snapshots: true });
  let busy = Promise.resolve();
  const bye = async () => { await saveState(S); out({ closed: true }); await quit(0); };
  closed(S).then(bye);
  const err = e => String(e && e.message || e).split('\n')[0];
  const handle = async c => {
    try {
      switch (c.cmd) {
        case 'start': await start(S, req, Object.assign({}, w, c.url !== undefined ? { url: c.url } : {})); break;
        case 'run':
          for (let k = 0; k < (c.actions || []).length; k++) {
            const i = (c.from || 0) + k;
            try { const { note, healed } = await act(S, req, c.actions[k]); out({ i, ok: true, note, healed }); }
            catch (e) { out({ i, ok: false, error: err(e), url: S.page.url() }); break; }
          }
          await saveState(S);
          break;
        case 'pick': { await S.page.bringToFront(); const v = await S.page.evaluate(PICKER).catch(() => null); out({ pick: v }); break; }
        case 'record': await S.setRec(!!c.on); break;
        case 'drive': await drive(S, c.drive || []); break;
        case 'snapshot': out({ done: true, url: S.page.url(), title: await S.page.title(), snap: await S.page.evaluate(SNAPSHOT) }); break;
        case 'text': out({ done: true, url: S.page.url(), title: await S.page.title(), text: (await S.page.innerText('body').catch(() => '')).slice(0, 20000), reads: S.reads, dialogs: S.dialogs }); break;
        case 'close': await bye(); break;
      }
    } catch (e) { out({ i: -1, ok: false, error: err(e) }); }
    out({ ack: c.id });
  };
  const take = l => { let c; try { c = JSON.parse(l); } catch { return; } busy = busy.then(() => handle(c)); };
  for (const l of lines.queue.splice(0)) take(l);
  lines.take = take;
  if (lines.ended) busy.then(bye); else lines.onEnd = () => busy.then(bye);
}

(async () => {
  const rl = require('readline').createInterface({ input: process.stdin });
  const lines = { queue: [], take: null, ended: false, onEnd: null };
  let gotFirst;
  const firstLine = new Promise(res => { gotFirst = res; });
  rl.on('line', l => { if (gotFirst) { const g = gotFirst; gotFirst = null; g(l); } else if (lines.take) lines.take(l); else lines.queue.push(l); });
  rl.on('close', () => { lines.ended = true; if (lines.onEnd) lines.onEnd(); });
  const req = JSON.parse(await firstLine);
  if (req.serve) { await serve(req, lines); return; } // runs until the browser closes (bye)
  if (req.record) await record(req); else await run(req);
  await quit(0);
})().catch(e => { out({ i: -2, ok: false, error: String(e && e.message || e).split('\n')[0] }); return quit(0); });
