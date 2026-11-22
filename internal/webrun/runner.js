// Termwright web step runner: reads a step (JSON) on stdin, drives the browser with Playwright and
// prints one JSON line per action, then a last line with the page's text.
const { chromium, firefox, webkit } = require('playwright');
const fs = require('fs');
const path = require('path');

function out(o) { process.stdout.write(JSON.stringify(o) + '\n'); }

function loc(page, t) {
  if (t.startsWith('label=')) return page.getByLabel(t.slice(6));
  if (t.startsWith('placeholder=')) return page.getByPlaceholder(t.slice(12));
  if (t.startsWith('//')) return page.locator('xpath=' + t);
  return page.locator(t);
}

(async () => {
  const req = JSON.parse(fs.readFileSync(0, 'utf8'));
  const w = req.step;
  const type = { firefox, webkit }[w.browser] || chromium;
  const timeout = Math.max(1000, (req.timeout || 30) * 1000);
  const browser = await type.launch({ headless: !w.show });
  const ctx = await browser.newContext({ ignoreHTTPSErrors: !!w.insecure });
  const page = await ctx.newPage();
  page.setDefaultTimeout(timeout);
  const shot = (name) => { const p = path.join(req.shots, name.endsWith('.png') ? name : name + '.png'); fs.mkdirSync(req.shots, { recursive: true }); return page.screenshot({ path: p, fullPage: true }).then(() => p); };
  let i = -1;
  try {
    if (w.url) { const r = await page.goto(w.url); out({ i, ok: true, note: (r ? r.status() + ' ' : '') + (await page.title()) }); }
    for (i = 0; i < (w.actions || []).length; i++) {
      const a = w.actions[i]; const t = a.target || ''; const v = a.value || '';
      let note = '';
      switch ((a.do || '').toLowerCase()) {
        case 'goto': { const r = await page.goto(v || t); note = (r ? r.status() + ' ' : '') + (await page.title()); break; }
        case 'click': await loc(page, t).first().click(); break;
        case 'fill': await loc(page, t).first().fill(v); break;
        case 'press': if (t) await loc(page, t).first().press(v || 'Enter'); else await page.keyboard.press(v || 'Enter'); break;
        case 'select': await loc(page, t).first().selectOption(v); break;
        case 'check': await loc(page, t).first().check(); break;
        case 'uncheck': await loc(page, t).first().uncheck(); break;
        case 'hover': await loc(page, t).first().hover(); break;
        case 'wait':
          if (t) await loc(page, t).first().waitFor({ state: 'visible' });
          else await page.waitForTimeout(Math.min(600, parseFloat(v) || 1) * 1000);
          break;
        case 'expect': {
          if (t && v) { const txt = await loc(page, t).first().innerText(); if (!txt.includes(v)) throw new Error('found "' + txt.slice(0, 200) + '"'); note = 'found'; }
          else { const l = t ? loc(page, t) : page.getByText(v); await l.first().waitFor({ state: 'visible' }); note = 'found'; }
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
        case 'screenshot': note = await shot(v || ('step-' + Date.now())); break;
        default: throw new Error('unknown action ' + a.do);
      }
      out({ i, ok: true, note });
    }
    const text = (await page.innerText('body').catch(() => '')).slice(0, 20000);
    out({ done: true, url: page.url(), title: await page.title(), text });
  } catch (e) {
    let p = '';
    try { p = await shot('failed-' + Date.now()); } catch { /* no page */ }
    out({ i, ok: false, error: String(e && e.message || e).split('\n')[0], shot: p, url: page.url() });
  } finally {
    await browser.close().catch(() => {});
  }
})().catch(e => { out({ i: -2, ok: false, error: String(e && e.message || e).split('\n')[0] }); process.exit(0); });
