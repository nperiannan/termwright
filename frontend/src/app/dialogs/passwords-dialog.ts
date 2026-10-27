import { ChangeDetectorRef, Component, OnInit, inject, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { Autofocus } from '../core/autofocus';
import { Backend, errText } from '../core/backend';
import { Credential } from '../core/models';
import { askRef, isRef } from '../core/secret-ref';
import { Ui } from '../core/ui.service';

type Page = 'credentials' | 'keys' | 'managers' | 'master' | 'vault';

/** Options → Passwords & Credentials: named credentials, master password, encrypted vault export/import. */
@Component({
  selector: 'app-passwords-dialog',
  imports: [FormsModule, Autofocus],
  template: `
  <div class="modal-backdrop">
    <div class="modal wide">
      <div class="modal-head">Passwords &amp; Credentials</div>
      <div class="modal-split">
        <nav class="cat">
          <a [class.on]="page() === 'credentials'" (click)="page.set('credentials')">Credentials</a>
          <a [class.on]="page() === 'keys'" (click)="page.set('keys'); loadKeys()">Key Passphrases</a>
          <a [class.on]="page() === 'managers'" (click)="page.set('managers'); loadManagers()">Password Managers</a>
          <a [class.on]="page() === 'master'" (click)="page.set('master')">Master Password</a>
          <a [class.on]="page() === 'vault'" (click)="page.set('vault')">Export / Import</a>
        </nav>
        <div class="modal-body">
          @if (ui.locked()) {
            <div class="locked">Saved passwords are locked.
              <input type="password" [(ngModel)]="unlockPw" placeholder="Master password" (keydown.enter)="unlock()" appAutofocus />
              <button type="button" class="primary" (click)="unlock()">Unlock</button></div>
          }
          @switch (page()) {
            @case ('credentials') {
              <p class="hint">Named username/password pairs for Logon Actions (Session Options → Logon Actions → Credentials).
                In a Send string, <b>\\s</b> sends the username and <b>\\w</b> the password. Passwords are stored in the OS keychain.</p>
              <table class="list">
                <thead><tr><th>Name</th><th>Username</th><th>Password</th><th></th></tr></thead>
                <tbody>
                  @for (c of creds; track $index) {
                    <tr>
                      <td><input [(ngModel)]="c.name" placeholder="e.g. lab-admin" [disabled]="!!c.saved" /></td>
                      <td><input [(ngModel)]="c.username" /></td>
                      <td><input [type]="isRef(c.password) ? 'text' : 'password'" [(ngModel)]="c.password" [placeholder]="c.hasPw ? '(saved - type to change)' : '(none)'" /></td>
                      <td class="nowrap"><button type="button" title="From a password manager (1Password, Bitwarden, Vault, command)" (click)="credRef(c)">🔑</button>
                        <button type="button" class="icon-btn" (click)="remove($index)">✕</button></td>
                    </tr>
                  }
                </tbody>
              </table>
              <div class="row"><button type="button" (click)="add()">Add</button><span class="spacer"></span>
                <button type="button" class="primary" (click)="saveCreds()">Save Credentials</button></div>
            }
            @case ('keys') {
              <p class="hint">Passphrases of encrypted SSH private keys (OpenSSH, PEM or PuTTY .ppk), saved in the OS keychain so the
                key unlocks without asking. They are saved from the passphrase prompt (<i>Remember</i>), Key Login Setup, or here.</p>
              <table class="list">
                <thead><tr><th>Key file</th><th>Used by</th><th></th></tr></thead>
                <tbody>
                  @for (k of keys(); track k.path) {
                    <tr>
                      <td class="mono" [class.missing]="!k.exists" [title]="k.exists ? k.path : 'File not found: ' + k.path">{{ k.path }}</td>
                      <td>{{ k.sessions?.length ? k.sessions!.join(', ') : '-' }}</td>
                      <td class="nowrap"><button type="button" (click)="changeKey(k.path)">Change...</button>
                        <button type="button" (click)="forgetKey(k.path)">Forget</button></td>
                    </tr>
                  } @empty { <tr><td colspan="3" class="hint">No saved key passphrases.</td></tr> }
                </tbody>
              </table>
              <div class="row"><button type="button" (click)="addKey()">Add...</button></div>
            }
            @case ('managers') {
              <p class="hint">Instead of saving a password, any password field (session, device type, credential) can hold a
                <b>reference</b> - click <i>Password manager...</i> next to it. Termwright reads the secret when it connects, keeps it in
                memory for 10 minutes and never writes it to disk.</p>
              <table class="list refs">
                <thead><tr><th>Manager</th><th>Reference</th><th>Needs</th></tr></thead>
                <tbody>
                  <tr><td>1Password</td><td class="mono">op://Network/core-sw1/password</td><td>1Password CLI <code>op</code>, signed in (desktop-app integration or service account)</td></tr>
                  <tr><td>Bitwarden</td><td class="mono">bw://core-sw1 · bw://core-sw1#username</td><td>Bitwarden CLI <code>bw</code>, logged in; asks for the master password once to unlock</td></tr>
                  <tr><td>HashiCorp Vault</td><td class="mono">vault://secret/network/core-sw1#password</td><td>Vault address and token below (or VAULT_ADDR / VAULT_TOKEN / <code>vault login</code>); KV v2 or v1</td></tr>
                  <tr><td>Any tool</td><td class="mono">cmd://security find-generic-password -s lab -w</td><td>a command that prints the password</td></tr>
                </tbody>
              </table>
              <div class="grid">
                <label>Vault address</label><input [(ngModel)]="vaultAddr" placeholder="https://vault.example.com:8200 (blank = VAULT_ADDR)" />
                <label>Vault token</label>
                <div class="row"><input type="password" [(ngModel)]="vaultToken" autocomplete="off" [placeholder]="hasVaultToken() ? '(saved in the OS keychain - type to change)' : '(blank = VAULT_TOKEN or ~/.vault-token)'" />
                  <button type="button" class="primary" (click)="saveManagers()">Save</button></div>
                <label>Test a reference</label>
                <div class="row"><input class="mono" [(ngModel)]="testRef" placeholder="op://… bw://… vault://… cmd://…" (keydown.enter)="runTest()" />
                  <button type="button" (click)="runTest()">Test</button></div>
                <label></label>
                <div class="row"><button type="button" (click)="forgetCache()">Forget fetched secrets</button>
                  <span class="hint">clears the 10-minute cache and locks Bitwarden again</span></div>
              </div>
            }
            @case ('master') {
              <p class="hint">With a master password, Termwright asks for it at start-up before it uses any saved session or credential
                password (the passwords themselves stay in the OS keychain). Leave "New" empty to remove the master password.</p>
              <div class="grid">
                @if (hasMaster()) {
                  <label>Current</label><input type="password" [(ngModel)]="curPw" autocomplete="off" />
                }
                <label>New</label><input type="password" [(ngModel)]="newPw" autocomplete="new-password" />
                <label>Confirm</label><input type="password" [(ngModel)]="newPw2" autocomplete="new-password" />
              </div>
              <div class="row"><span class="spacer"></span>
                <button type="button" class="primary" (click)="setMaster()">{{ hasMaster() ? (newPw ? 'Change' : 'Remove') : 'Set' }} Master Password</button></div>
            }
            @case ('vault') {
              <p class="hint">Exports every saved session password, device type password, SSH key passphrase and all credentials into one file encrypted with
                AES-256-GCM (key derived with scrypt from the passphrase). Use it to move passwords to another computer;
                import it there with the same passphrase. Session files themselves are exported with File → Export.</p>
              <div class="grid">
                <label>Passphrase</label><input type="password" [(ngModel)]="vaultPw" autocomplete="new-password" />
                <label>Confirm (export)</label><input type="password" [(ngModel)]="vaultPw2" autocomplete="new-password" />
              </div>
              <div class="row"><span class="spacer"></span>
                <button type="button" (click)="importVault()">Import...</button>
                <button type="button" class="primary" (click)="exportVault()">Export...</button></div>
            }
          }
          @if (msg()) { <div class="hint ok">{{ msg() }}</div> }
          @if (err()) { <div class="err">{{ err() }}</div> }
        </div>
      </div>
      <div class="modal-foot"><span class="spacer"></span><button type="button" (click)="ui.close()">Close</button></div>
    </div>
  </div>`,
  styles: [`
    .locked { display: flex; gap: 8px; align-items: center; padding: 8px; margin-bottom: 10px; border-radius: 4px; background: var(--input-bg); }
    .locked input { flex: 1; }
    .ok { color: var(--accent); margin-top: 8px; }
    .err { color: var(--danger); margin-top: 8px; }
    .missing { color: var(--muted); text-decoration: line-through; }
    .refs { margin-bottom: 12px; td { font-size: calc(12px * var(--ui-scale, 1)); vertical-align: top; } .mono { white-space: nowrap; } }
    .nowrap { white-space: nowrap; }
    td.mono { font-size: calc(11px * var(--ui-scale, 1)); word-break: break-all; }
  `],
})
export class PasswordsDialog implements OnInit {
  protected ui = inject(Ui);
  private backend = inject(Backend);
  private cdr = inject(ChangeDetectorRef);
  readonly page = signal<Page>('credentials');
  readonly hasMaster = signal(false);
  readonly msg = signal('');
  readonly err = signal('');
  creds: (Credential & { password: string; hasPw: boolean; saved: string })[] = [];
  private removed: string[] = [];
  unlockPw = ''; curPw = ''; newPw = ''; newPw2 = ''; vaultPw = ''; vaultPw2 = '';

  async ngOnInit() {
    this.hasMaster.set(!!this.ui.settings()?.masterHash);
    this.creds = await Promise.all((this.ui.settings()?.credentials ?? []).map(async c => ({
      ...c, password: '', saved: c.name, hasPw: await this.backend.hasCredentialPassword(c.name).catch(() => false),
    })));
    this.cdr.markForCheck();
  }

  private status(m: string, e = '') { this.msg.set(m); this.err.set(e); }

  readonly isRef = isRef;
  async credRef(c: { password: string }) {
    const r = await askRef(this.ui, this.backend, isRef(c.password) ? c.password : '');
    if (r) { c.password = r; this.cdr.markForCheck(); }
  }
  vaultAddr = ''; vaultToken = ''; testRef = '';
  readonly hasVaultToken = signal(false);
  async loadManagers() {
    this.vaultAddr = this.ui.settings()?.vaultAddr ?? '';
    this.hasVaultToken.set(await this.backend.hasVaultToken().catch(() => false));
  }
  async saveManagers() {
    try {
      const s = this.ui.settings();
      if (s) await this.ui.saveSettings({ ...s, vaultAddr: this.vaultAddr.trim() });
      if (this.vaultToken) { await this.backend.setVaultToken(this.vaultToken); this.vaultToken = ''; this.hasVaultToken.set(true); }
      this.status('Saved.');
    } catch (e) { this.status('', errText(e)); }
  }
  async runTest() {
    if (!this.testRef.trim()) return;
    try { this.status(await this.backend.testSecretRef(this.testRef.trim())); } catch (e) { this.status('', errText(e)); }
  }
  async forgetCache() { await this.backend.forgetCachedSecrets(); this.status('Fetched secrets forgotten.'); }

  readonly keys = signal<{ path: string; exists: boolean; sessions: string[] | null }[]>([]);
  async loadKeys() {
    try { this.keys.set(await this.backend.keyPassphrases()); } catch (e) { this.status('', errText(e)); }
  }
  async changeKey(path: string) {
    const pw = await this.ui.input('Key Passphrase', 'Passphrase for ' + path, '', true);
    if (pw === null) return;
    try { await this.backend.setKeyPassphrase(path, pw); this.status('Passphrase saved.'); await this.loadKeys(); } catch (e) { this.status('', errText(e)); }
  }
  async addKey() {
    const path = await this.backend.chooseOpenFile('Choose Private Key').catch(() => '');
    if (path) await this.changeKey(path);
  }
  async forgetKey(path: string) {
    try { await this.backend.forgetKeyPassphrase(path); this.status('Passphrase removed.'); await this.loadKeys(); } catch (e) { this.status('', errText(e)); }
  }

  async unlock() {
    if (await this.backend.unlockPasswords(this.unlockPw).catch(() => false)) {
      this.ui.locked.set(false);
      this.status('Saved passwords unlocked.');
    } else this.status('', 'Wrong master password.');
    this.unlockPw = '';
  }

  add() { this.creds.push({ name: '', username: '', password: '', hasPw: false, saved: '' }); }

  remove(i: number) {
    const [c] = this.creds.splice(i, 1);
    if (c.saved) this.removed.push(c.saved);
  }

  async saveCreds() {
    const list = this.creds.filter(c => c.name.trim());
    const names = list.map(c => c.name.trim());
    if (new Set(names).size !== names.length) { this.status('', 'Credential names must be unique.'); return; }
    try {
      for (const n of this.removed) await this.backend.setCredentialPassword(n, '');
      this.removed = [];
      for (const c of list) {
        c.name = c.name.trim();
        if (c.password) { await this.backend.setCredentialPassword(c.name, c.password); c.hasPw = true; c.password = ''; }
        c.saved = c.name;
      }
      const s = this.ui.settings();
      if (s) await this.ui.saveSettings({ ...s, credentials: list.map(c => ({ name: c.name, username: c.username })) });
      this.creds = list;
      this.status(`Saved ${list.length} credential(s).`);
    } catch (e) { this.status('', errText(e)); }
    this.cdr.markForCheck();
  }

  async setMaster() {
    if (this.newPw !== this.newPw2) { this.status('', 'The new passwords do not match.'); return; }
    try {
      await this.backend.setMasterPassword(this.curPw, this.newPw);
      await this.ui.loadSettings();
      this.hasMaster.set(!!this.newPw);
      this.status(this.newPw ? 'Master password set.' : 'Master password removed.');
      this.curPw = this.newPw = this.newPw2 = '';
    } catch (e) { this.status('', errText(e)); }
    this.cdr.markForCheck();
  }

  async exportVault() {
    if (this.vaultPw.length < 8) { this.status('', 'Use a passphrase of at least 8 characters.'); return; }
    if (this.vaultPw !== this.vaultPw2) { this.status('', 'The passphrases do not match.'); return; }
    const dest = await this.backend.chooseSaveFile('Export Password Vault', 'termwright-passwords.twvault',
      [{ displayName: 'Termwright vault', pattern: '*.twvault;*.ocvault' }]).catch(() => '');
    if (!dest) return;
    try {
      const n = await this.backend.exportVault(dest, this.vaultPw);
      this.status(`Exported ${n} password(s).`);
      this.vaultPw = this.vaultPw2 = '';
    } catch (e) { this.status('', errText(e)); }
    this.cdr.markForCheck();
  }

  async importVault() {
    if (!this.vaultPw) { this.status('', 'Enter the passphrase the vault was exported with.'); return; }
    const src = await this.backend.chooseOpenFile('Import Password Vault',
      [{ displayName: 'Termwright vault', pattern: '*.twvault;*.ocvault' }]).catch(() => '');
    if (!src) return;
    try {
      const n = await this.backend.importVault(src, this.vaultPw);
      await this.ui.loadSettings();
      await this.ngOnInit();
      this.status(`Imported ${n} password(s).`);
      this.vaultPw = this.vaultPw2 = '';
    } catch (e) { this.status('', errText(e)); }
    this.cdr.markForCheck();
  }
}
