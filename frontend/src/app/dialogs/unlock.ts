import { Component, inject, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { Autofocus } from '../core/autofocus';
import { Backend } from '../core/backend';
import { Ui } from '../core/ui.service';
import { Icon } from '../components/icon';

/** Shown at start-up when a master password is set: saved passwords stay
 *  locked until it is typed. "Not now" keeps them locked (passwords are then
 *  asked when connecting); Passwords & Accounts can unlock later. */
@Component({
  selector: 'app-unlock',
  imports: [FormsModule, Autofocus, Icon],
  template: `
  <div class="lock-screen">
    <form class="box" (ngSubmit)="unlock()" autocomplete="off">
      <img [src]="ui.lightUi() ? 'icon.svg' : 'icon-dark.svg'" alt="" width="54" height="54" />
      <div class="ic"><app-icon name="lock" [size]="16" /></div>
      <h1>Termwright is locked</h1>
      <p>Type the master password to use your saved passwords.</p>
      <div class="field" [class.bad]="wrong()">
        <input [type]="show() ? 'text' : 'password'" name="mp" [(ngModel)]="pw" placeholder="Master password" appAutofocus (input)="wrong.set(false)" />
        <button type="button" class="eye" (click)="show.set(!show())" [title]="show() ? 'Hide' : 'Show'">{{ show() ? 'Hide' : 'Show' }}</button>
      </div>
      @if (wrong()) { <div class="err">That is not the master password.</div> }
      <button type="submit" class="primary go" [disabled]="!pw || busy()">{{ busy() ? 'Unlocking…' : 'Unlock' }}</button>
      <button type="button" class="later" (click)="later()">Not now - ask for passwords when connecting</button>
    </form>
  </div>`,
  styles: [`
    .lock-screen { position: fixed; inset: 0; z-index: 2000; display: grid; place-items: center; background: color-mix(in srgb, var(--bg) 92%, transparent); backdrop-filter: blur(8px); }
    .box { width: 360px; max-width: calc(100vw - 32px); display: flex; flex-direction: column; align-items: center; text-align: center; gap: 10px; padding: 32px 28px 22px;
      background: var(--surface, var(--panel)); border: 1px solid var(--line, var(--border)); border-radius: 14px; box-shadow: var(--shadow); position: relative; }
    .ic { position: absolute; top: 72px; left: calc(50% + 14px); width: 26px; height: 26px; border-radius: 50%; display: grid; place-items: center; background: var(--accent); color: #fff;
      border: 3px solid var(--surface, var(--panel)); }
    h1 { margin: 8px 0 0; font-size: calc(19px * var(--ui-scale, 1)); font-weight: 650; }
    p { margin: 0 0 6px; color: var(--muted); }
    .field { width: 100%; display: flex; align-items: center; border: 1px solid var(--border); border-radius: var(--r, 6px); background: var(--input-bg);
      &:focus-within { border-color: var(--accent); box-shadow: 0 0 0 3px var(--accent-soft); }
      &.bad { border-color: var(--danger); }
      input { flex: 1; min-width: 0; height: 38px; border: 0 !important; background: transparent; padding: 0 12px; font-size: calc(14px * var(--ui-scale, 1)); box-shadow: none !important; }
      .eye { border: 0 !important; background: transparent !important; color: var(--muted); height: 30px; padding: 0 10px; font-size: calc(12px * var(--ui-scale, 1)); } }
    .err { color: var(--danger); font-size: calc(12.5px * var(--ui-scale, 1)); align-self: flex-start; }
    .go { width: 100%; height: 38px; margin-top: 4px; }
    .later { border: 0 !important; background: transparent !important; color: var(--muted); font-size: calc(12px * var(--ui-scale, 1)); height: auto; padding: 4px; &:hover { color: var(--text); } }
  `],
})
export class Unlock {
  protected ui = inject(Ui);
  private backend = inject(Backend);
  pw = '';
  readonly show = signal(false);
  readonly wrong = signal(false);
  readonly busy = signal(false);

  async unlock() {
    if (!this.pw || this.busy()) return;
    this.busy.set(true);
    const ok = await this.backend.unlockPasswords(this.pw).catch(() => false);
    this.busy.set(false);
    this.pw = '';
    if (ok) { this.ui.locked.set(false); this.ui.lockScreen.set(false); this.ui.toast('Saved passwords unlocked', 'success'); }
    else this.wrong.set(true);
  }

  later() { this.ui.lockScreen.set(false); }
}
