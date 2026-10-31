import { ChangeDetectorRef, Component, OnInit, inject, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { Autofocus } from '../core/autofocus';
import { Backend, errText } from '../core/backend';
import { AccountInfo, Credential } from '../core/models';

type Acct = Credential & { password: string; hasPw: boolean; saved: string };
import { askRef, isRef } from '../core/secret-ref';
import { Ui } from '../core/ui.service';
import { Icon } from '../components/icon';
import { PwMeter } from '../components/pw-meter';
import { MIN_MASTER, pwStrength } from '../core/pw-strength';

type Page = 'credentials' | 'keys' | 'managers' | 'master' | 'vault';

/** Options → Passwords & Accounts: accounts (named credentials), master password, encrypted vault export/import. */
@Component({
  selector: 'app-passwords-dialog',
  imports: [FormsModule, Autofocus, Icon, PwMeter],
  template: `
  <div class="modal-backdrop">
    <div class="modal wide">
      <div class="modal-head"><span class="mh-ic"><app-icon name="lock" [size]="17" /></span>
        <span class="mh-t">Passwords &amp; Accounts<span class="mh-s">Accounts that sessions log in with, saved passwords and password managers - passwords never go into session files</span></span></div>
      <div class="modal-split">
        <nav class="cat">
          <a [class.on]="page() === 'credentials'" (click)="page.set('credentials')"><app-icon name="key" [size]="15" />Accounts</a>
          <a [class.on]="page() === 'keys'" (click)="page.set('keys'); loadKeys()"><app-icon name="lock" [size]="15" />Key Passphrases</a>
          <a [class.on]="page() === 'managers'" (click)="page.set('managers'); loadManagers()"><app-icon name="shield" [size]="15" />Password Managers</a>
          <a [class.on]="page() === 'master'" (click)="page.set('master')"><app-icon name="lock" [size]="15" />Master Password</a>
          <a [class.on]="page() === 'vault'" (click)="page.set('vault')"><app-icon name="export" [size]="15" />Export / Import</a>
        </nav>
        <div class="modal-body">
          <h2 class="page-title">{{ pageNames[page()] }}</h2>
          @if (ui.locked()) {
            <div class="locked">Saved passwords are locked.
              <input type="password" [(ngModel)]="unlockPw" placeholder="Master password" (keydown.enter)="unlock()" appAutofocus />
              <button type="button" class="primary" (click)="unlock()">Unlock</button></div>
          }
          @switch (page()) {
            @case ('credentials') {
              <p class="hint">An account is a username with a password or an SSH private key that sessions, folders (Folder Defaults) and device types log in with -
                change its password here once and every session that uses it follows. In Logon Actions, <b>\\s</b> sends its username and <b>\\w</b> its password.</p>
              <div class="row acc-bar"><input class="acc-q" [(ngModel)]="accQ" placeholder="Search accounts" />
                <button type="button" (click)="add()"><app-icon name="plus" [size]="14" class="md-only" /> New Account</button>
                <button type="button" (click)="loadCandidates()" title="Sessions that share the same username and saved password">Convert Sessions…</button></div>
              <table class="list creds accts">
                <thead><tr><th>Account</th><th>Username</th><th>Login</th><th>Used by</th><th>Changed</th></tr></thead>
                <tbody>
                  @for (c of shownCreds(); track c) {
                    <tr [class.sel]="c === cur" (click)="cur = c">
                      <td><b>{{ c.name || '(new account)' }}</b></td>
                      <td class="mono">{{ c.username }}</td>
                      <td>@if (c.auth === 'key') { <span class="pill kc" [title]="c.keyFile || ''">⚿ Private key</span> }
                        @else if (c.password) { <span class="pill pm2">{{ isRef(c.password) ? '◆ Password manager' : '● Changed' }}</span> }
                        @else if (pwKind(c) === 'manager') { <span class="pill pm2">◆ Password manager</span> }
                        @else if (pwKind(c) === 'keychain') { <span class="pill kc">● Keychain</span> }
                        @else { <span class="pill ask">○ Asked when connecting</span> }</td>
                      <td [class.unused]="!usedText(c)" class="used">{{ usedText(c) || 'not used' }}</td>
                      <td class="muted">{{ changedText(c) }}</td>
                    </tr>
                  } @empty { <tr><td colspan="5" class="muted">No accounts yet - New Account, or Convert Sessions… to make one from sessions that share a login.</td></tr> }
                </tbody>
              </table>
              @if (cur; as c) {
                <div class="acc-detail">
                  <label>Name</label><input [(ngModel)]="c.name" placeholder="e.g. lab-admin" appAutofocus />
                  <label>Username</label><input class="mono" [(ngModel)]="c.username" placeholder="login name" />
                  <label>Log in with</label>
                  <div class="seg w2"><button type="button" [class.on]="c.auth !== 'key'" (click)="c.auth = ''">Password</button><button type="button" [class.on]="c.auth === 'key'" (click)="c.auth = 'key'">Private key</button></div>
                  @if (c.auth === 'key') {
                    <label>Private key</label>
                    <div class="row nowrap"><input class="mono" [(ngModel)]="c.keyFile" placeholder="~/.ssh/id_ed25519" spellcheck="false" />
                      <button type="button" (click)="pickAcctKey(c)">Browse…</button></div>
                    <div class="hint wide">SSH sessions that use this account log in with this key (a session's own key file still wins). The passphrase is asked once and can be saved with Key Passphrases. Telnet and serial sessions use the password below.</div>
                  }
                  <label>{{ c.auth === 'key' ? 'Password (optional)' : 'Password' }}</label>
                  <div class="row nowrap"><input [type]="isRef(c.password) ? 'text' : 'password'" [(ngModel)]="c.password" [placeholder]="c.hasPw ? '(saved - type to change)' : (c.auth === 'key' ? '(none - for Telnet, or when the key is refused)' : '(none - asked when connecting)')" />
                    <button type="button" class="pm" title="From a password manager (1Password, Bitwarden, Vault, command)" (click)="credRef(c)"><span class="cl-only">🔑</span><app-icon name="shield" [size]="15" class="md-only" /></button></div>
                  <label>Note</label><input [(ngModel)]="c.note" placeholder="optional" />
                  <span></span>
                  <div class="hint">@if (info(c); as i) { @if (i.sessions) { Changing the password updates the {{ i.sessions }} session{{ i.sessions === 1 ? '' : 's' }} that use{{ i.sessions === 1 ? 's' : '' }} this account. <a (click)="showSessions(c)">Show the sessions</a> } @else { Not used yet: choose it in a session's Account list, Folder Defaults or a device type. } }</div>
                </div>
              }
              @if (candidates(); as cs) {
                <div class="cands">
                  <b>Sessions with the same username and saved password</b>
                  @for (g of cs; track g.username + g.paths[0]) {
                    <div class="cand"><span class="mono">{{ g.username }}</span><span class="muted">{{ g.paths.length }} sessions - {{ g.paths.slice(0, 3).join(', ') }}{{ g.paths.length > 3 ? ', …' : '' }}</span>
                      <button type="button" (click)="convert(g)">Convert to Account…</button></div>
                  } @empty { <div class="muted">No sessions share a username and saved password.</div> }
                </div>
              }
              <div class="row"><button type="button" class="danger-text" [disabled]="!cur" (click)="removeCur()">Remove Account…</button><span class="spacer"></span>
                <button type="button" class="primary" (click)="saveCreds()">Save Accounts</button></div>
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
                <label>New</label><div><input type="password" [(ngModel)]="newPw" autocomplete="new-password" placeholder="At least 4 characters" style="width: 100%" />
                  @if (newPw || !hasMaster()) { <app-pw-meter [pw]="newPw" /> }</div>
                <label>Confirm</label><input type="password" [(ngModel)]="newPw2" autocomplete="new-password" />
              </div>
              <p class="hint">Any 4 or more characters - capitals, numbers and symbols make it stronger but are not needed.</p>
              <div class="row">
                @if (hasMaster()) { <button type="button" (click)="lockNow()" title="Lock the saved passwords now and show the unlock window"><app-icon name="lock" [size]="14" /> Lock Now</button> }
                <span class="spacer"></span>
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
      <div class="modal-foot"><span class="spacer"></span><button type="button" [class.primary]="modern()" (click)="ui.close()">{{ modern() ? 'Done' : 'Close' }}</button></div>
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
    .muted { color: var(--muted); }
    .acc-bar { margin-bottom: 10px; gap: 8px; .acc-q { flex: 1; } button { white-space: nowrap; } }
    .accts { tr { cursor: default; } tr.sel td { background: var(--selected); } .used { color: var(--accent); } .used.unused { color: var(--faint); } }
    .pill { display: inline-flex; align-items: center; height: 20px; padding: 0 8px; border-radius: 10px; font-size: calc(11px * var(--ui-scale, 1)); font-weight: 600; white-space: nowrap;
      &.kc { color: var(--ok, #22c55e); background: color-mix(in srgb, var(--ok, #22c55e) 14%, transparent); }
      &.pm2 { color: #a78bfa; background: color-mix(in srgb, #a78bfa 16%, transparent); }
      &.ask { color: #e5a93b; background: color-mix(in srgb, #e5a93b 15%, transparent); } }
    .acc-detail .wide { grid-column: 2 / -1; margin-top: -2px; }
    .acc-detail .seg { min-width: 0; }
    .acc-detail { display: grid; grid-template-columns: 90px 1fr 90px 1fr; gap: 8px 12px; align-items: center; padding: 12px 14px; margin-bottom: 12px; border: 1px solid var(--border); border-radius: 10px;
      > label { color: var(--muted); text-align: right; } > .hint { grid-column: 2 / span 3; margin: 0; a { color: var(--accent); cursor: pointer; } }
      > input, .row input { width: 100%; min-width: 0; } .row { gap: 4px; min-width: 0; input { flex: 1; } } }
    .cands { display: flex; flex-direction: column; gap: 6px; padding: 10px 12px; margin-bottom: 12px; border: 1px dashed var(--border); border-radius: 10px;
      .cand { display: flex; gap: 10px; align-items: center; > .muted { flex: 1; min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; } button { white-space: nowrap; } } }
    td.mono { font-size: calc(11px * var(--ui-scale, 1)); word-break: break-all; }
    :host-context(.modern) {
      table.list { width: 100%; border: 1px solid var(--line); border-radius: var(--r-lg); border-collapse: separate; border-spacing: 0; overflow: hidden; margin-bottom: 12px;
        th { height: 30px; padding: 0 10px; text-align: left; font-size: calc(11px * var(--ui-scale, 1)); font-weight: 600; letter-spacing: .05em; text-transform: uppercase;
          color: var(--faint); background: var(--surface-2); border-bottom: 1px solid var(--line); }
        td { padding: 5px 10px; border-top: 1px solid var(--line); vertical-align: middle; }
        tbody tr:first-child td { border-top: 0; }
        td input { width: 100%; border-color: transparent; background: transparent; &:hover { border-color: var(--line); } &:focus { border-color: var(--accent); background: var(--input-bg); } } }
      .creds td:nth-child(2) input { font-weight: 600; }
      .kt { width: 36px; padding-right: 0 !important; }
      .k { width: 28px; height: 28px; border-radius: 7px; background: var(--accent-soft); color: var(--accent); display: grid; place-items: center; }
      .pm, .icon-btn { width: 28px; height: 28px; padding: 0; border: 0 !important; background: transparent !important; color: var(--muted); display: inline-grid; place-items: center;
        &:hover { color: var(--text); } }
      .locked { border: 1px solid var(--line); border-radius: var(--r-lg); padding: 10px 12px; background: var(--surface-2); }
    }
  `],
})
export class PasswordsDialog implements OnInit {
  protected ui = inject(Ui);
  private backend = inject(Backend);
  private cdr = inject(ChangeDetectorRef);
  readonly page = signal<Page>('credentials');
  readonly modern = () => this.ui.settings()?.layout !== 'classic';
  readonly pageNames: Record<Page, string> = { credentials: 'Accounts', keys: 'Key Passphrases', managers: 'Password Managers', master: 'Master Password', vault: 'Export / Import' };
  readonly hasMaster = signal(false);
  readonly msg = signal('');
  readonly err = signal('');
  creds: Acct[] = [];
  cur: Acct | null = null;
  accQ = '';
  readonly accInfo = signal<AccountInfo[]>([]);
  readonly candidates = signal<{ username: string; paths: string[] }[] | null>(null);
  private removed: string[] = [];
  unlockPw = ''; curPw = ''; newPw = ''; newPw2 = ''; vaultPw = ''; vaultPw2 = '';

  async ngOnInit() {
    this.hasMaster.set(!!this.ui.settings()?.masterHash);
    await this.loadAccounts();
  }

  private async loadAccounts() {
    this.creds = await Promise.all((this.ui.settings()?.credentials ?? []).map(async c => ({
      ...c, note: c.note ?? '', password: '', saved: c.name, hasPw: await this.backend.hasCredentialPassword(c.name).catch(() => false),
    })));
    this.cur = this.creds[0] ?? null;
    this.accInfo.set((await this.backend.accountsInfo().catch(() => null)) ?? []);
    this.cdr.markForCheck();
  }

  shownCreds() { const q = this.accQ.trim().toLowerCase(); return q ? this.creds.filter(c => (c.name + ' ' + c.username + ' ' + (c.note ?? '')).toLowerCase().includes(q)) : this.creds; }
  info(c: Acct) { return this.accInfo().find(i => i.name === c.saved) ?? null; }
  pwKind(c: Acct) { return this.info(c)?.password ?? (c.hasPw ? 'keychain' : ''); }
  usedText(c: Acct) {
    const i = this.info(c);
    if (!i) return '';
    const parts = [i.sessions ? i.sessions + ' session' + (i.sessions === 1 ? '' : 's') : '', ...i.folders.map(f => 'folder "' + f + '"'), ...i.deviceTypes.map(d => 'device type ' + d)].filter(Boolean);
    return parts.join(' · ');
  }
  changedText(c: Acct) {
    if (!c.changed) return '-';
    const d = new Date(c.changed), now = new Date();
    if (d.toDateString() === now.toDateString()) return 'Today';
    return d.toLocaleDateString('en-GB', { day: 'numeric', month: 'short', ...(d.getFullYear() !== now.getFullYear() ? { year: 'numeric' } : {}) });
  }
  async showSessions(c: Acct) {
    const l = (await this.backend.accountSessions(c.saved).catch(() => null)) ?? [];
    await this.ui.confirm('Sessions Using ' + c.saved, l.length ? l.slice(0, 40).join('\n') + (l.length > 40 ? '\n… and ' + (l.length - 40) + ' more' : '') : 'No session uses this account.', 'OK');
  }
  async loadCandidates() {
    this.candidates.set((await this.backend.accountCandidates().catch(() => null)) ?? []);
    this.cdr.markForCheck();
  }
  async convert(g: { username: string; paths: string[] }) {
    const name = await this.ui.input('Convert to Account', `Account name for the ${g.paths.length} sessions that log in as ${g.username} (their username and saved password move into the account)`, g.username);
    if (!name?.trim()) return;
    try {
      await this.backend.convertToAccount(name.trim(), g.paths);
      await this.ui.loadSettings();
      await this.ui.refreshTree();
      await this.loadAccounts();
      this.cur = this.creds.find(c => c.name === name.trim()) ?? this.cur;
      await this.loadCandidates();
      this.status(`${g.paths.length} sessions now log in with the account ${name.trim()}.`);
    } catch (e) { this.status('', errText(e)); }
  }
  async removeCur() {
    const c = this.cur;
    if (!c) return;
    const used = this.usedText(c);
    if (used && !(await this.ui.confirm('Remove Account', `${c.saved} is used by ${used}. They will log in with their own username and password (or be asked) after it is removed.`, 'Remove', true))) return;
    this.remove(this.creds.indexOf(c));
    this.cur = this.creds[0] ?? null;
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

  add() { const c: Acct = { name: '', username: '', note: '', password: '', hasPw: false, saved: '' }; this.creds.push(c); this.cur = c; }

  async pickAcctKey(c: Acct) {
    const p = await this.backend.chooseOpenFile('Choose Private Key').catch(() => '');
    if (p) { c.keyFile = p; this.cdr.markForCheck(); }
  }

  remove(i: number) {
    const [c] = this.creds.splice(i, 1);
    if (c.saved) this.removed.push(c.saved);
  }

  async saveCreds() {
    const list = this.creds.filter(c => c.name.trim());
    const names = list.map(c => c.name.trim());
    if (new Set(names).size !== names.length) { this.status('', 'Account names must be unique.'); return; }
    try {
      for (const n of this.removed) await this.backend.setCredentialPassword(n, '');
      this.removed = [];
      // renamed accounts: sessions, folders, device types and the saved password follow
      let renamed = false;
      for (const c of list) {
        if (c.saved && c.name.trim() !== c.saved) { await this.backend.renameAccount(c.saved, c.name.trim()); c.saved = c.name.trim(); renamed = true; }
      }
      if (renamed) await this.ui.loadSettings();
      for (const c of list) {
        c.name = c.name.trim();
        if (c.password) { await this.backend.setCredentialPassword(c.name, c.password); c.hasPw = true; c.password = ''; c.changed = new Date().toISOString(); }
        c.saved = c.name;
      }
      const s = this.ui.settings();
      const prev = new Map((s?.credentials ?? []).map(c => [c.name, c.changed]));
      if (s) await this.ui.saveSettings({ ...s, credentials: list.map(c => ({ name: c.name, username: c.username, auth: c.auth === 'key' ? 'key' as const : undefined,
        keyFile: c.auth === 'key' ? c.keyFile?.trim() || undefined : undefined, note: c.note?.trim() || undefined, changed: c.changed || prev.get(c.name) })) });
      await this.ui.loadSettings();
      this.creds = list;
      this.accInfo.set((await this.backend.accountsInfo().catch(() => null)) ?? []);
      this.status(`Saved ${list.length} account${list.length === 1 ? '' : 's'}.`);
    } catch (e) { this.status('', errText(e)); }
    this.cdr.markForCheck();
  }

  async lockNow() {
    if (!(await this.backend.lockPasswords().catch(() => false))) return;
    this.ui.locked.set(true);
    this.ui.close();
    this.ui.lockScreen.set(true);
  }

  async setMaster() {
    if (this.newPw && !pwStrength(this.newPw).ok) { this.status('', `Use at least ${MIN_MASTER} characters.`); return; }
    if (this.newPw !== this.newPw2) { this.status('', 'The new passwords do not match.'); return; }
    try {
      await this.backend.setMasterPassword(this.curPw, this.newPw);
      await this.ui.loadSettings();
      this.hasMaster.set(!!this.newPw);
      this.status(this.newPw ? 'Master password set. Termwright asks for it when it starts - Lock Now shows the unlock window.' : 'Master password removed.');
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
