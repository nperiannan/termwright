/**
 * Interface language.
 *
 * English is the source language and the fallback: every text in the app is its
 * own English wording, and a language is a dictionary from that wording to its
 * translation (see ../i18n/). Anything without an entry stays English, so a
 * language can be added screen by screen.
 *
 *  - Texts in templates and menus are translated where they appear on screen
 *    (text nodes and the title, placeholder and aria-label attributes), so a
 *    template needs no markup. Terminal output, session and file names are left
 *    alone (see SKIP).
 *  - Texts built in code use tr('Delete {0} sessions?', n).
 *  - A dictionary key with {0}, {1}… matches texts with a number or name in them.
 *    The translation may hold plural forms, {0|сессия|сессии|сессий} (one, few,
 *    many), chosen with the language's own plural rules.
 */

import { signal } from '@angular/core';

export type Dict = Record<string, string>;

/** preview = not finished: offered and applied only in nightly and development builds, not in a release. */
export const LANGUAGES: { code: string; name: string; preview?: boolean }[] = [
  { code: '', name: 'English' },
  { code: 'ru', name: 'Русский (частично)', preview: true },
];

/** True in nightly ("0.24.0~git1234567") and development builds, where unfinished languages are offered. */
export const previewLanguages = signal(false);
export const setPreviewLanguages = (version: string) => previewLanguages.set(/~|-dev|nightly/i.test(version));

const loaders: Record<string, () => Promise<{ default: Dict }>> = {
  ru: () => import('../i18n/ru'),
};

/** Parts of the page that show data, not interface text. */
const SKIP = 'script,style,textarea,pre,code,app-terminal-view,.xterm,[translate="no"],.name,.nm';
const ATTRS = ['title', 'placeholder', 'aria-label'];

interface Rec { src: string; out: string }

const norm = (s: string) => s.replace(/\s+/g, ' ').trim();

class I18n {
  lang = '';
  private dict: Dict = {};
  private patterns: { re: RegExp; to: string }[] = [];
  private seen = new Map<Node, Rec>();
  private obs?: MutationObserver;
  private plural?: Intl.PluralRules;
  private adds = 0;

  /** Switches the interface language ('' = English). */
  async set(code: string) {
    if (!(code in loaders) || (LANGUAGES.find(l => l.code === code)?.preview && !previewLanguages())) code = '';
    if (code === this.lang && (code === '' || this.obs)) return;
    this.stop();
    this.lang = code;
    document.documentElement.lang = code || 'en';
    if (!code) return;
    try {
      this.load((await loaders[code]()).default);
    } catch { this.lang = ''; document.documentElement.lang = 'en'; return; }
    this.plural = new Intl.PluralRules(code);
    this.walk(document.body);
    this.obs = new MutationObserver(recs => this.mutated(recs));
    this.obs.observe(document.body, { subtree: true, childList: true, characterData: true, attributes: true, attributeFilter: ATTRS });
  }

  private load(d: Dict) {
    this.dict = {};
    this.patterns = [];
    for (const [k, v] of Object.entries(d)) {
      const key = norm(k);
      if (/\{\d\}/.test(key)) {
        const esc = key.replace(/[.*+?^$()|[\]\\]/g, '\\$&').replace(/\{(\d)\}/g, '(?<g$1>.+?)');
        this.patterns.push({ re: new RegExp('^' + esc + '$'), to: v });
      } else this.dict[key] = v;
    }
  }

  /** Back to English: every translated text on the page is put back. */
  private stop() {
    this.obs?.disconnect();
    this.obs = undefined;
    for (const [n, r] of this.seen) {
      if (n.nodeType === Node.TEXT_NODE) (n as Text).data = r.src;
      else (n as Attr).value = r.src;
    }
    this.seen.clear();
    this.dict = {};
    this.patterns = [];
  }

  /** The translation of an English text, or undefined. */
  lookup(text: string): string | undefined {
    const key = norm(text);
    const d = this.dict[key];
    if (d !== undefined) return d;
    for (const p of this.patterns) {
      const m = p.re.exec(key);
      if (m?.groups) return this.fill(p.to, m.groups);
    }
    return undefined;
  }

  private fill(to: string, g: Record<string, string>): string {
    to = to.replace(/\{(\d)\|([^}]*)\}/g, (_, i: string, forms: string) => this.pick(Number(g['g' + i]), forms.split('|')));
    return to.replace(/\{(\d)\}/g, (_, i: string) => g['g' + i] ?? '');
  }

  private pick(n: number, forms: string[]): string {
    const c = this.plural?.select(n) ?? 'other';
    const i = c === 'one' ? 0 : c === 'few' ? 1 : 2;
    return forms[i] ?? forms[forms.length - 1];
  }

  private skipped(el: Element | null): boolean { return !el || !!el.closest(SKIP); }

  private text(n: Text) {
    const cur = n.data;
    const rec = this.seen.get(n);
    if (rec && cur === rec.out) return;
    const m = /^(\s*)([\s\S]*?)(\s*)$/.exec(cur);
    if (!m || !m[2] || this.skipped(n.parentElement)) { if (rec) this.seen.delete(n); return; }
    const t = this.lookup(m[2]);
    if (t === undefined) { if (rec) this.seen.delete(n); return; }
    const out = m[1] + t + m[3];
    this.seen.set(n, { src: cur, out });
    n.data = out;
    this.prune();
  }

  private attr(a: Attr) {
    const cur = a.value;
    const rec = this.seen.get(a);
    if (rec && cur === rec.out) return;
    const t = a.ownerElement && !this.skipped(a.ownerElement) ? this.lookup(cur) : undefined;
    if (t === undefined) { if (rec) this.seen.delete(a); return; }
    this.seen.set(a, { src: cur, out: t });
    a.value = t;
    this.prune();
  }

  private walk(root: Node) {
    if (root.nodeType === Node.TEXT_NODE) { this.text(root as Text); return; }
    if (root.nodeType !== Node.ELEMENT_NODE) return;
    const el = root as Element;
    if (this.skipped(el)) return;
    const w = document.createTreeWalker(el, NodeFilter.SHOW_ELEMENT | NodeFilter.SHOW_TEXT);
    const todo: Node[] = [el];
    for (let n = w.nextNode(); n; n = w.nextNode()) todo.push(n);
    for (const n of todo) {
      if (n.nodeType === Node.TEXT_NODE) this.text(n as Text);
      else for (const name of ATTRS) { const a = (n as Element).getAttributeNode(name); if (a) this.attr(a); }
    }
  }

  private mutated(recs: MutationRecord[]) {
    for (const r of recs) {
      if (r.type === 'childList') r.addedNodes.forEach(n => this.walk(n));
      else if (r.type === 'characterData') this.text(r.target as Text);
      else if (r.type === 'attributes' && r.attributeName) {
        const a = (r.target as Element).getAttributeNode(r.attributeName);
        if (a) this.attr(a);
      }
    }
  }

  /** Forget translated nodes that left the page. */
  private prune() {
    if (++this.adds < 4000) return;
    this.adds = 0;
    for (const [n] of this.seen) {
      const gone = n.nodeType === Node.TEXT_NODE ? !n.isConnected : !(n as Attr).ownerElement?.isConnected;
      if (gone) this.seen.delete(n);
    }
  }
}

export const i18n = new I18n();

/** Translates a text built in code. {0}, {1}… are filled with args. */
export function tr(text: string, ...args: (string | number)[]): string {
  const fill = (s: string) => s.replace(/\{(\d)\}/g, (_, i: string) => String(args[Number(i)] ?? ''));
  if (!i18n.lang) return fill(text);
  const key = args.length ? text.replace(/\{(\d)\}/g, (_, i: string) => String(args[Number(i)] ?? '')) : text;
  return i18n.lookup(key) ?? fill(text);
}
