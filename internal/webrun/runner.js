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

async function context(req, w) {
  const type = { firefox, webkit }[w.browser] || chromium;
  const browser = await type.launch({ headless: !w.show && !(req.record && !req.drive) });
  const opts = { ignoreHTTPSErrors: !!w.insecure, viewport: viewport(w.viewport) };
  if (req.httpUser) opts.httpCredentials = { username: req.httpUser, password: req.httpPassword || '' };
  const statePath = w.state && req.stateDir ? path.join(req.stateDir, w.state.replace(/[^\w.-]+/g, '_') + '.json') : '';
  if (statePath && fs.existsSync(statePath)) opts.storageState = statePath;
  const ctx = await browser.newContext(opts);
  return { browser, ctx, statePath };
}

async function run(req) {
  const w = req.step;
  const timeout = Math.max(1000, (req.timeout || 30) * 1000);
  const { browser, ctx, statePath } = await context(req, w);
  if (w.trace) await ctx.tracing.start({ screenshots: true, snapshots: true });
  let page = await ctx.newPage();
  const pages = [page];
  ctx.on('page', p => { pages.push(p); p.setDefaultTimeout(timeout); });
  page.setDefaultTimeout(timeout);
  const dialogs = [];
  let onDialog = 'accept', dialogText = '';
  ctx.on('page', p => p.on('dialog', d => handle(d)));
  page.on('dialog', d => handle(d));
  async function handle(d) { dialogs.push(d.type() + ': ' + d.message()); try { if (onDialog === 'dismiss') await d.dismiss(); else await d.accept(dialogText || undefined); } catch { /* gone */ } }
  fs.mkdirSync(req.shots, { recursive: true });
  const shot = (name) => { const p = path.join(req.shots, name.endsWith('.png') ? name : name + '.png'); return page.screenshot({ path: p, fullPage: true }).then(() => p); };
  const reads = [];
  let i = -1;
  try {
    if (w.url) { const r = await page.goto(w.url); out({ i, ok: true, note: (r ? r.status() + ' ' : '') + (await page.title()) }); }
    for (i = 0; i < (w.actions || []).length; i++) {
      const a = w.actions[i]; const t = a.target || ''; const v = a.value || '';
      const el = () => loc(page, t).first();
      let note = '';
      switch ((a.do || '').toLowerCase()) {
        case 'goto': { const r = await page.goto(v || t); note = (r ? r.status() + ' ' : '') + (await page.title()); break; }
        case 'click': await el().click(); break;
        case 'dblclick': await el().dblclick(); break;
        case 'rightclick': await el().click({ button: 'right' }); break;
        case 'fill': await el().fill(v); break;
        case 'type': await el().pressSequentially(v, { delay: 30 }); break;
        case 'clear': await el().fill(''); break;
        case 'press': if (t) await el().press(v || 'Enter'); else await page.keyboard.press(v || 'Enter'); break;
        case 'select': {
          const sel = el();
          try { await sel.selectOption({ label: v }); } catch { await sel.selectOption(v); }
          break;
        }
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
        case 'dialog': onDialog = v === 'dismiss' ? 'dismiss' : 'accept'; dialogText = v !== 'dismiss' && v !== 'accept' ? v : ''; note = 'dialogs: ' + onDialog; break;
        case 'tab': {
          await page.waitForTimeout(300);
          let p = null;
          if (v === '' || v === 'last') p = pages[pages.length - 1];
          else if (/^\d+$/.test(v)) p = pages[+v - 1];
          else p = pages.find(x => x.url().includes(v));
          if (!p) throw new Error('no tab ' + (v || 'last') + ' (' + pages.length + ' open)');
          page = p; await page.bringToFront(); note = page.url();
          break;
        }
        case 'close_tab': { await page.close(); pages.splice(pages.indexOf(page), 1); page = pages[pages.length - 1]; note = page ? page.url() : ''; break; }
        case 'reload': await page.reload(); break;
        case 'back': await page.goBack(); break;
        case 'wait':
          if (t) await el().waitFor({ state: 'visible' });
          else if (v && !/^[\d.]+$/.test(v)) await page.waitForURL(u => u.toString().includes(v));
          else await page.waitForTimeout(Math.min(600, parseFloat(v) || 1) * 1000);
          break;
        case 'wait_gone': await el().waitFor({ state: 'hidden' }); break;
        case 'expect': {
          if (t && v) { const txt = await el().innerText(); if (!txt.includes(v)) throw new Error('found "' + txt.slice(0, 200) + '"'); note = 'found'; }
          else { const l = t ? el() : page.getByText(v).first(); await l.waitFor({ state: 'visible' }); note = 'found'; }
          break;
        }
        case 'expect_not': {
          const l = t ? loc(page, t) : page.getByText(v);
          const deadline = Date.now() + Math.min(timeout, 5000);
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
          const deadline = Date.now() + Math.min(timeout, 10000);
          while (!page.url().includes(v) && Date.now() < deadline) await page.waitForTimeout(250);
          if (!page.url().includes(v)) throw new Error('the address is ' + page.url());
          note = page.url();
          break;
        }
        case 'expect_title': { const ti = await page.title(); if (!ti.includes(v)) throw new Error('the title is "' + ti + '"'); note = ti; break; }
        case 'read': {
          const e = el();
          const txt = (await e.evaluate(n => n.tagName === 'INPUT' || n.tagName === 'TEXTAREA' || n.tagName === 'SELECT')) ? await e.inputValue() : (await e.innerText()).trim();
          reads.push({ name: v || t, value: txt }); note = txt.slice(0, 200);
          break;
        }
        case 'screenshot': { const n = v || ('step-' + Date.now()); if (t) { const p = path.join(req.shots, n.endsWith('.png') ? n : n + '.png'); await el().screenshot({ path: p }); note = p; } else note = await shot(n); break; }
        default: throw new Error('unknown action ' + a.do);
      }
      out({ i, ok: true, note });
    }
    if (statePath) { fs.mkdirSync(path.dirname(statePath), { recursive: true }); await ctx.storageState({ path: statePath }); }
    const text = (await page.innerText('body').catch(() => '')).slice(0, 20000);
    out({ done: true, url: page.url(), title: await page.title(), text, reads, dialogs });
    if (w.trace) await ctx.tracing.stop({ path: path.join(req.shots, 'trace-' + Date.now() + '.zip') });
  } catch (e) {
    let p = '', tr = '';
    try { p = await shot('failed-' + Date.now()); } catch { /* no page */ }
    if (w.trace) { tr = path.join(req.shots, 'trace-failed-' + Date.now() + '.zip'); try { await ctx.tracing.stop({ path: tr }); } catch { tr = ''; } }
    out({ i, ok: false, error: String(e && e.message || e).split('\n')[0], shot: p, trace: tr, url: page ? page.url() : '', dialogs });
  } finally {
    await browser.close().catch(() => {});
  }
}

// ---- recording: what the person does in the browser becomes actions ----
const RECORDER = `(() => {
  if (window.__twRecOn) return; window.__twRecOn = true;
  const q = s => JSON.stringify(s);
  const unique = (s) => { try { return document.querySelectorAll(s).length === 1; } catch { return false; } };
  const text = el => (el.innerText || el.value || '').trim().split('\\n')[0].replace(/\\s+/g, ' ').slice(0, 60);
  function role(el) {
    const t = el.tagName.toLowerCase(), ty = (el.getAttribute('type') || '').toLowerCase();
    return el.getAttribute('role') || (t === 'button' || (t === 'input' && ['button', 'submit', 'reset'].includes(ty)) ? 'button'
      : t === 'a' && el.hasAttribute('href') ? 'link' : t === 'select' ? 'combobox' : t === 'input' && ty === 'checkbox' ? 'checkbox'
      : t === 'input' && ty === 'radio' ? 'radio' : t === 'textarea' || (t === 'input' && !ty) || ty === 'text' ? 'textbox' : '');
  }
  function target(el) {
    const tid = el.getAttribute('data-testid') || el.getAttribute('data-test-id') || el.getAttribute('data-test');
    if (tid) return 'testid=' + tid;
    const t = el.tagName.toLowerCase();
    if (['input', 'textarea', 'select'].includes(t)) { // a field: by what the person reads next to it
      const lab = el.labels && el.labels[0] && el.labels[0].innerText.trim().split('\\n')[0];
      if (lab && lab.length < 50) return 'label=' + lab;
      if (el.placeholder) return 'placeholder=' + el.placeholder;
    }
    if (el.id && !/^\\d|[:\\s]|\\d{3,}/.test(el.id) && unique('#' + CSS.escape(el.id))) return '#' + CSS.escape(el.id);
    if (['input', 'textarea', 'select'].includes(t) && el.name && unique(t + '[name=' + q(el.name) + ']')) return t + '[name=' + q(el.name) + ']';
    const r = role(el), name = (el.getAttribute('aria-label') || el.getAttribute('title') || text(el)).trim();
    if (r && name) return 'role=' + r + '[name=' + q(name) + ']';
    if (name && name.length < 40 && !['input', 'textarea', 'select'].includes(t)) return 'text=' + name;
    const path = []; let n = el;
    while (n && n.nodeType === 1 && n !== document.body && path.length < 6) {
      let s = n.tagName.toLowerCase();
      if (n.id && unique('#' + CSS.escape(n.id))) { path.unshift('#' + CSS.escape(n.id)); break; }
      const sib = [...(n.parentElement ? n.parentElement.children : [])].filter(x => x.tagName === n.tagName);
      if (sib.length > 1) s += ':nth-of-type(' + (sib.indexOf(n) + 1) + ')';
      path.unshift(s); n = n.parentElement;
    }
    return path.join(' > ');
  }
  const clickable = el => el.closest('a,button,[role=button],[role=link],[role=tab],[role=menuitem],input[type=button],input[type=submit],input[type=checkbox],input[type=radio],label,summary,option,li,td,span,div') || el;
  const send = a => window.__twRec && window.__twRec(a);
  document.addEventListener('click', e => {
    if (!e.altKey && e.target.closest && e.target.closest('input,textarea,select,option')) return; // fields: their change
    const el = clickable(e.target);
    if (e.altKey) { e.preventDefault(); e.stopPropagation(); const tg = target(el); send(tg.startsWith('text=') ? { do: 'expect', value: text(el) } : { do: 'expect', target: tg, value: text(el) }); flash(el, '#3fb950'); return; }
    const t = el.tagName.toLowerCase(), ty = (el.getAttribute('type') || '').toLowerCase();
    if (t === 'label' && el.control) return; // its field records the change
    if ((t === 'input' && !['button', 'submit', 'reset', 'checkbox', 'radio'].includes(ty)) || t === 'textarea' || t === 'select' || t === 'option') return;
    if (t === 'input' && (ty === 'checkbox' || ty === 'radio')) return;
    send({ do: 'click', target: target(el) }); flash(el, '#58a6ff');
  }, true);
  document.addEventListener('change', e => {
    const el = e.target, t = el.tagName.toLowerCase(), ty = (el.getAttribute('type') || '').toLowerCase();
    if (t === 'select') send({ do: 'select', target: target(el), value: el.options[el.selectedIndex] ? el.options[el.selectedIndex].text.trim() : el.value });
    else if (ty === 'checkbox' || ty === 'radio') send({ do: el.checked ? 'check' : 'uncheck', target: target(el) });
    else if (ty === 'file') send({ do: 'upload', target: target(el), value: [...el.files].map(f => f.name).join(',') });
    else if (t === 'input' || t === 'textarea') send({ do: 'fill', target: target(el), value: ty === 'password' ? '{{password}}' : el.value, secret: ty === 'password' });
  }, true);
  document.addEventListener('keydown', e => {
    if (e.key === 'Enter' && e.target && ['INPUT'].includes(e.target.tagName)) {
      const el = e.target; const ty = (el.getAttribute('type') || '').toLowerCase();
      send({ do: 'fill', target: target(el), value: ty === 'password' ? '{{password}}' : el.value, secret: ty === 'password' });
      send({ do: 'press', target: target(el), value: 'Enter' });
    }
  }, true);
  function flash(el, c) { const o = el.style.outline; el.style.outline = '2px solid ' + c; setTimeout(() => { el.style.outline = o; }, 500); }
  const bar = () => { if (document.getElementById('__tw_rec') || !document.body) return; const d = document.createElement('div'); d.id = '__tw_rec';
    d.innerHTML = '<b style="color:#f85149">●</b> Termwright is recording - click, type and choose as usual; Alt+click a text to check it is shown; close this window to finish';
    d.style.cssText = 'position:fixed;z-index:2147483647;left:50%;bottom:12px;transform:translateX(-50%);background:#1f2328;color:#fff;font:12px system-ui;padding:6px 12px;border-radius:14px;box-shadow:0 4px 16px #0006;pointer-events:none;opacity:.92';
    document.body.appendChild(d); };
  if (document.readyState === 'loading') document.addEventListener('DOMContentLoaded', bar); else bar();
})();`;

async function record(req) {
  const w = req.step;
  const { browser, ctx } = await context(req, w);
  const filled = {};
  await ctx.exposeBinding('__twRec', (src, a) => {
    if (!a || !a.do) return;
    if (a.do === 'fill') { // the same value again (Enter, then the change event): once
      if (filled[a.target] === a.value) return;
      filled[a.target] = a.value;
    }
    out({ rec: a });
  });
  await ctx.addInitScript(RECORDER);
  const page = await ctx.newPage();
  let opened = 0;
  ctx.on('page', p => { opened++; if (opened > 0 && p !== page) out({ rec: { do: 'tab', value: 'last' } }); });
  if (w.url) await page.goto(w.url);
  if (req.drive) { // tests: do these as a person would, then stop
    for (const d of req.drive) {
      const el = loc(page, d.target).first();
      if (d.do === 'fill') { await el.click(); await el.fill(d.value); await el.dispatchEvent('change'); }
      else if (d.do === 'select') await el.selectOption({ label: d.value });
      else if (d.do === 'altclick') await el.click({ modifiers: ['Alt'] });
      else await el.click();
      await page.waitForTimeout(150);
    }
    await page.waitForTimeout(300);
    out({ done: true, url: page.url() });
    await browser.close();
    return;
  }
  await new Promise(resolve => { browser.on('disconnected', resolve); ctx.on('close', resolve); page.on('close', () => { if (ctx.pages().length === 0) resolve(); }); });
  out({ done: true });
  await browser.close().catch(() => {});
}

(async () => {
  const req = JSON.parse(fs.readFileSync(0, 'utf8'));
  if (req.record) await record(req); else await run(req);
})().catch(e => { out({ i: -2, ok: false, error: String(e && e.message || e).split('\n')[0] }); process.exit(0); });
