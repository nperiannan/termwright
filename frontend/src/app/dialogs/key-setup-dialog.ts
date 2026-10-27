import { ChangeDetectorRef, Component, OnInit, computed, inject, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { Backend, errText } from '../core/backend';
import { Tabs } from '../core/tabs.service';
import { Ui } from '../core/ui.service';

interface Step { text: string; state: 'run' | 'ok' | 'err' | 'info'; }

/**
 * Key Login Setup (like ssh-keygen + ssh-copy-id): create a key on this computer
 * or reuse an existing one, add its public key to the server of the current SSH2
 * tab, test a key-only login and switch the session to the key.
 */
@Component({
  selector: 'app-key-setup-dialog',
  imports: [FormsModule],
  template: `
  <div class="modal-backdrop">
    <form class="modal" (ngSubmit)="run()" autocomplete="off">
      <div class="modal-head">Key Login Setup{{ tab() ? ' - ' + tab()!.title : '' }}</div>
      <div class="modal-body">
        @if (tab()) {
          <p class="hint">Creates or reuses a key on <b>this computer</b>, adds its public key to
            <b>{{ tab()!.session.hostname }}</b> (Linux, macOS, BSD or Windows OpenSSH) and makes the session log in with it.</p>
        } @else {
          <p class="hint">Creates a key pair on this computer. To install it on a server, open an SSH2 tab and use
            right-click → Set Up Key Login…</p>
        }
        <div class="checks">
          <label><input type="radio" name="mode" value="new" [(ngModel)]="mode" /> Generate a new key</label>
          <label><input type="radio" name="mode" value="existing" [(ngModel)]="mode" [disabled]="!tab()" /> Use an existing key on this computer</label>
        </div>
        <div class="grid">
          @if (mode === 'new') {
            <label>Key type</label>
            <select name="kind" [(ngModel)]="kind" (ngModelChange)="kindChanged()">
              <option value="ed25519">Ed25519 (recommended)</option>
              <option value="rsa">RSA 4096 (older servers/devices)</option>
              <option value="ecdsa">ECDSA P-256</option>
            </select>
            <label>Save as</label><input name="path" [(ngModel)]="path" class="mono" />
            <label>Comment</label><input name="comment" [(ngModel)]="comment" />
          } @else {
            <label>Private key</label>
            <div class="row">
              <select name="existing" [(ngModel)]="path">
                @for (k of existing(); track k) { <option [value]="k">{{ k }}</option> }
                @if (path && !existing().includes(path)) { <option [value]="path">{{ path }}</option> }
              </select>
              <button type="button" (click)="browse()">Browse…</button>
            </div>
          }
          <label>Passphrase</label>
          <input type="password" name="pass" [(ngModel)]="pass" [placeholder]="mode === 'new' ? 'optional (recommended)' : 'if the key has one'" />
          @if (mode === 'new') {
            <label>Confirm</label><input type="password" name="pass2" [(ngModel)]="pass2" />
          }
          @if (tab()) {
            <label>Then</label>
            <div class="checks">
              <label><input type="checkbox" name="inst" [(ngModel)]="install" /> Add the public key to the server</label>
              <label><input type="checkbox" name="test" [(ngModel)]="test" /> Test a key-only login</label>
              <label><input type="checkbox" name="use" [(ngModel)]="use" [disabled]="!tab()!.session.path" />
                Use this key for the session from the next connection</label>
              @if (pass) {
                <label><input type="checkbox" name="savep" [(ngModel)]="savePass" /> Save the passphrase in the OS keychain</label>
              }
            </div>
          }
        </div>
        @if (steps().length) {
          <ul class="steps">
            @for (s of steps(); track $index) { <li [class]="s.state">{{ s.state === 'ok' ? '✓' : s.state === 'err' ? '✕' : s.state === 'run' ? '…' : '•' }} {{ s.text }}</li> }
          </ul>
        }
        @if (pub()) {
          <label class="publabel">Public key <button type="button" class="link" (click)="copy()">Copy</button></label>
          <textarea class="pub mono" readonly rows="3">{{ pub() }}</textarea>
        }
      </div>
      <div class="modal-foot">
        <span class="spacer"></span>
        <button type="button" (click)="ui.close()">{{ finished() ? 'Close' : 'Cancel' }}</button>
        @if (!finished()) { <button type="submit" class="primary" [disabled]="busy()">{{ busy() ? 'Working…' : 'Start' }}</button> }
      </div>
    </form>
  </div>`,
  styles: [`
    .steps { list-style: none; padding: 8px; margin: 10px 0 0; background: var(--input-bg); border-radius: 4px; font-size: calc(12px * var(--ui-scale, 1)); user-select: text;
      li { padding: 2px 0; white-space: pre-wrap; } .ok { color: var(--ok); } .err { color: var(--danger); } .run { color: var(--muted); } }
    .pub { width: 100%; font-size: calc(11px * var(--ui-scale, 1)); resize: vertical; }
    .publabel { display: flex; gap: 8px; align-items: center; margin-top: 10px; }
    .link { background: none; border: none; color: var(--accent); padding: 0; height: auto; cursor: pointer; }
  `],
})
export class KeySetupDialog implements OnInit {
  protected ui = inject(Ui);
  private backend = inject(Backend);
  private tabs = inject(Tabs);
  private cdr = inject(ChangeDetectorRef);
  readonly tab = computed(() => this.tabs.get(this.ui.keySetupTab()));
  readonly existing = signal<string[]>([]);
  readonly steps = signal<Step[]>([]);
  readonly pub = signal('');
  readonly busy = signal(false);
  readonly finished = signal(false);
  mode: 'new' | 'existing' = 'new';
  kind = 'ed25519';
  path = '';
  comment = '';
  pass = '';
  pass2 = '';
  install = true;
  test = true;
  use = true;
  savePass = true;
  private defaultBase = '';

  async ngOnInit() {
    const t = this.tab();
    const d = await this.backend.keySetupDefaults(t?.session.hostname ?? '');
    this.path = d.defaultPath;
    this.defaultBase = d.defaultPath.replace(/_ed25519$/, '');
    this.comment = d.comment;
    this.existing.set(d.existing);
    this.use = !!t?.session.path;
    this.cdr.markForCheck();
  }

  kindChanged() {
    if (this.defaultBase && this.path.startsWith(this.defaultBase)) this.path = this.defaultBase + '_' + this.kind;
  }

  async browse() {
    const p = await this.backend.chooseOpenFile('Choose Private Key').catch(() => '');
    if (p) { this.path = p.replace(/\.pub$/, ''); this.cdr.markForCheck(); }
  }

  private add(text: string, state: Step['state']) { this.steps.update(l => [...l, { text, state }]); return this.steps().length - 1; }
  private set(i: number, text: string, state: Step['state']) { this.steps.update(l => l.map((s, j) => (j === i ? { text, state } : s))); }

  async run() {
    if (this.mode === 'new' && this.pass !== this.pass2) { this.steps.set([{ text: 'The passphrases do not match.', state: 'err' }]); return; }
    if (!this.path.trim()) return;
    const t = this.tab();
    this.busy.set(true);
    this.steps.set([]);
    let i = -1;
    try {
      if (this.mode === 'new') {
        i = this.add(`Generating ${this.kind} key…`, 'run');
        this.pub.set(await this.backend.generateKeyPair(this.kind, this.path, this.comment, this.pass));
        this.set(i, `Key saved: ${this.path} (public key: ${this.path}.pub)`, 'ok');
      } else {
        i = this.add('Reading public key…', 'run');
        this.pub.set(await this.backend.publicKeyOf(this.path, this.pass));
        this.set(i, `Using ${this.path}`, 'ok');
      }
      if (t && this.install) {
        i = this.add(`Adding the public key on ${t.session.hostname}…`, 'run');
        this.set(i, await this.backend.installPublicKey(t.id, this.pub()), 'ok');
      }
      if (t && this.test) {
        i = this.add('Testing a login with only this key…', 'run');
        await this.backend.testKeyLogin(t.id, this.path, this.pass);
        this.set(i, 'Key login works', 'ok');
      }
      if (t && this.use && t.session.path) {
        i = this.add('Updating the session…', 'run');
        await this.backend.useKeyForSession(t.session.path, this.path, this.pass, this.savePass);
        this.set(i, 'Session updated: identity file set, public key tried first (password stays as fallback)', 'ok');
        await this.ui.refreshTree();
      }
      if (!t) this.add('Copy the public key below to the server\'s authorized keys.', 'info');
      this.finished.set(true);
    } catch (e) {
      if (i >= 0) this.set(i, errText(e), 'err'); else this.add(errText(e), 'err');
    }
    this.busy.set(false);
    this.cdr.markForCheck();
  }

  copy() { void this.backend.clipboardSet(this.pub()).then(() => this.ui.toast('Public key copied')); }
}
