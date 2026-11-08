import { Injectable, inject } from '@angular/core';
import { Ui } from './ui.service';
import { i18n } from './i18n';

/** The buttons Escape presses in a dialog, by their English text: Close first; Cancel only when the
 *  dialog has no Close button (a disabled Close - something is running - means Escape does nothing). */
const CLOSE = ['Close', 'Done'];
const CANCEL = ['Cancel', 'Not Now', 'No'];

/**
 * Keyboard and screen reader support for every dialog, without markup in each one:
 *  - Escape closes the menu that is open, else the top dialog (through its own Close or Cancel button,
 *    so a dialog's own checks - a setup that is running, unsaved changes - still apply);
 *  - Tab and Shift+Tab stay inside the top dialog, and a new dialog gets the focus;
 *  - dialogs are announced as dialogs with their title, and icon-only buttons by their tooltip.
 */
@Injectable({ providedIn: 'root' })
export class A11y {
  private ui = inject(Ui);
  private seen = new WeakSet<Element>();

  constructor() {
    if (typeof document === 'undefined') return;
    // an open menu takes Escape before anything else - also before a terminal, which would send it to the device
    document.addEventListener('keydown', e => {
      if (e.key !== 'Escape') return;
      if (this.ui.menu()) { this.ui.menu.set(null); e.preventDefault(); e.stopPropagation(); return; }
      // a dialog is open but the focus is outside it (in a terminal behind it): the dialog gets Escape
      const m = this.top(), t = e.target as Node | null;
      if (m && t && !m.contains(t)) { e.stopPropagation(); this.key(e); }
    }, true);
    document.addEventListener('keydown', e => this.key(e));
    // terminal output changes the page all the time: mark at most every 150 ms, and not for the terminals
    let timer = 0;
    const obs = new MutationObserver(list => {
      if (timer || list.every(r => (r.target as Element).closest?.('.xterm') ?? (r.target.parentElement?.closest('.xterm')))) return;
      timer = window.setTimeout(() => { timer = 0; this.mark(); }, 150);
    });
    obs.observe(document.body, { subtree: true, childList: true, attributes: true, attributeFilter: ['title'] });
    this.mark();
  }

  /** The dialog on top: the last visible .modal in the page (overlays come after the dialogs). */
  private top(): HTMLElement | null {
    const all = [...document.querySelectorAll<HTMLElement>('.modal')].filter(m => m.offsetParent !== null || m.getClientRects().length);
    return all.pop() ?? null;
  }

  private label(b: HTMLElement): string { return (b.textContent ?? '').replace(/\s+/g, ' ').trim(); }
  private is(b: HTMLElement, names: string[]): boolean {
    const t = this.label(b), title = b.getAttribute('title') ?? '';
    return names.some(n => t === n || t === (i18n.lookup(n) ?? n) || (!t && (title === n || title === (i18n.lookup(n) ?? n))));
  }

  private key(e: KeyboardEvent) {
    if (e.defaultPrevented || e.isComposing) return;
    if (e.key === 'Escape') {
      if (this.ui.menu()) { this.ui.menu.set(null); e.preventDefault(); return; }
      const m = this.top();
      if (!m) {
        // the session side panel (it has its own Escape when the focus is inside it)
        const panel = [...document.querySelectorAll<HTMLElement>('app-session-panel')].find(x => x.offsetParent !== null || x.getClientRects().length);
        const x = panel?.querySelector('app-icon[name="x"]')?.closest<HTMLButtonElement>('button');
        if (x) { e.preventDefault(); x.click(); }
        return;
      }
      const buttons = [...m.querySelectorAll<HTMLButtonElement>('.modal-foot button, .modal-head button')].filter(b => b.offsetParent !== null);
      const close = buttons.filter(b => this.is(b, CLOSE));
      const pick = close.length ? close.find(b => !b.disabled) : buttons.find(b => this.is(b, CANCEL) && !b.disabled);
      if (pick) { e.preventDefault(); pick.click(); }
      return;
    }
    if (e.key === 'Tab') {
      const m = this.top();
      if (!m) return;
      const f = [...m.querySelectorAll<HTMLElement>('button, [href], input, select, textarea, [tabindex]:not([tabindex="-1"])')]
        .filter(x => !(x as HTMLButtonElement).disabled && x.offsetParent !== null && x.tabIndex >= 0);
      if (!f.length) return;
      const a = document.activeElement as HTMLElement | null, i = a ? f.indexOf(a) : -1;
      if (i < 0 || (e.shiftKey && i === 0) || (!e.shiftKey && i === f.length - 1)) {
        e.preventDefault();
        (e.shiftKey ? f[f.length - 1] : f[0]).focus();
      }
    }
  }

  /** Roles and labels; focus into a dialog that just opened. */
  private mark() {
    document.querySelectorAll<HTMLElement>('.modal').forEach(m => {
      if (m.getAttribute('role') !== 'dialog') { m.setAttribute('role', 'dialog'); m.setAttribute('aria-modal', 'true'); }
      const t = m.querySelector('.mh-t');
      const name = t ? (t.childNodes[0]?.textContent ?? t.textContent ?? '').trim() : '';
      if (name && m.getAttribute('aria-label') !== name) m.setAttribute('aria-label', name);
      if (!this.seen.has(m)) {
        this.seen.add(m);
        // after the dialog's own autofocus had its turn
        setTimeout(() => {
          if (m === this.top() && !m.contains(document.activeElement)) {
            const first = m.querySelector<HTMLElement>('input:not([type=hidden]):not([disabled]), select, textarea, .modal-body button, .modal-foot button.primary');
            first?.focus({ preventScroll: true });
          }
        }, 60);
      }
    });
    document.querySelectorAll<HTMLElement>('button[title]:not([aria-label])').forEach(b => {
      if (!this.label(b)) b.setAttribute('aria-label', b.getAttribute('title') ?? '');
    });
    document.querySelectorAll<HTMLElement>('button[title][aria-label]').forEach(b => {
      if (!this.label(b) && b.getAttribute('aria-label') !== b.getAttribute('title')) b.setAttribute('aria-label', b.getAttribute('title') ?? '');
    });
  }
}
