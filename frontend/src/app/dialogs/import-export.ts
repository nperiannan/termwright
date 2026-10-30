import { ChangeDetectorRef, Component, inject, input, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { Backend, errText } from '../core/backend';
import { ExportReport, ImportResult, ImportSource, SettingsSummary } from '../core/models';
import { Icon } from '../components/icon';
import { FolderPicker } from '../components/folder-picker';
import { Ui } from '../core/ui.service';

type Source = 'twzip' | 'twdir' | 'folder' | 'zip' | 'xml' | 'ini' | 'ssh' | 'putty' | 'moba' | 'mremoteng' | 'csv' | 'ansible' | 'netbox' | 'aws';

/** Import sessions from SecureCRT (config folder, .zip, .xml, .ini), PuTTY/KiTTY, MobaXterm or OpenSSH config. */
@Component({
  selector: 'app-import-dialog',
  imports: [FormsModule, Icon, FolderPicker],
  template: `
  <div class="modal-backdrop">
    <form class="modal" (ngSubmit)="run()">
      <div class="modal-head"><span class="mh-ic"><app-icon name="import" [size]="17" /></span>
        <span class="mh-t">Import Sessions<span class="mh-s">Bring sessions in from another program or an inventory</span></span></div>
      <div class="modal-body">
        @if (!result()) {
          @if (modern()) {
            @if (found().length) {
              <div class="found">
                <div class="fh"><span class="g"></span><b>Found on this computer</b></div>
                @for (f of found(); track f.path) {
                  <div class="fr"><span class="app">{{ f.app }}</span><span class="fp" [title]="f.path">{{ f.what }} · {{ f.path }}</span>
                    <button type="button" [class.primary]="path === f.path" (click)="useFound(f)">{{ path === f.path ? 'Chosen' : 'Use' }}</button></div>
                }
              </div>
            }
            @for (g of groups; track g.title) {
              <div class="sec">{{ $first && found().length ? 'Or choose a source - ' : '' }}{{ g.title }}</div>
              <div class="cards">
                @for (o of g.items; track o.v) {
                  <button type="button" class="sc" [class.on]="source === o.v" (click)="source = o.v; pick(o.v)"><b>{{ cardOf(o.v).t }}</b><span>{{ cardOf(o.v).s }}</span></button>
                }
              </div>
            }
          }
          <div class="grid">
            <label class="cl-only" style="align-self: start; padding-top: 1px">Source</label>
            <div class="src-groups cl-only">
              @for (g of groups; track g.title) {
                <div class="src-group">
                  <div class="src-title">{{ g.title }}</div>
                  @for (o of g.items; track o.v) {
                    <label><input type="radio" name="src" [value]="o.v" [(ngModel)]="source" (ngModelChange)="pick(o.v)" /> {{ o.label }}</label>
                  }
                </div>
              }
            </div>
            @if (source === 'netbox') {
              <label>NetBox URL</label><input name="nbu" [(ngModel)]="nbUrl" placeholder="https://netbox.example.com" />
              <label>API token</label><input name="nbt" type="password" [(ngModel)]="nbToken" placeholder="(saved token, if any)" autocomplete="off" />
              <label>Filter</label><input name="nbf" [(ngModel)]="nbFilter" placeholder="optional, e.g. site=dc1&role=leaf&status=active" />
            } @else if (source === 'aws') {
              <label>Profile</label><input name="awsp" [(ngModel)]="awsProfile" placeholder="(default)" />
              <label>Region</label><input name="awsr" [(ngModel)]="awsRegion" placeholder="e.g. us-east-1 (default from your AWS config)" />
              <label></label>
              <div class="checks"><label><input type="checkbox" name="awspub" [(ngModel)]="awsPublic" /> Use public IP addresses (default: private, e.g. via a jump host)</label></div>
            } @else {
            <label>{{ modern() ? pathLabel() : 'Path' }}</label>
            <div class="row"><input name="path" class="pathin" [(ngModel)]="path" (change)="checkSettings()" required /><button type="button" (click)="browse()">Browse...</button></div>
            }
            @if (source === 'mremoteng') {
              <label>Master password</label>
              <input name="mrp" type="password" [(ngModel)]="configPass" autocomplete="off"
                placeholder="only if the file has its own master password (the default needs none)" />
            }
            @if (isSecureCRT()) {
              <label>Config passphrase</label>
              <input name="ccp" type="password" [(ngModel)]="configPass" autocomplete="off"
                placeholder="only if you set a configuration passphrase in SecureCRT" />
            }
            <div class="pair">
              <div class="f"><label>Into folder</label>
                @if (modern()) { <app-folder-picker [(value)]="target" /> } @else { <input name="tf" [(ngModel)]="target" placeholder="(top level)" /> }</div>
              <div class="f"><label>If a name exists</label>
                @if (modern()) {
                  <div class="seg"><button type="button" [class.on]="conflict === 'rename'" (click)="conflict = 'rename'">Keep both</button>
                    <button type="button" [class.on]="conflict === 'overwrite'" (click)="conflict = 'overwrite'">Replace</button>
                    <button type="button" [class.on]="conflict === 'skip'" (click)="conflict = 'skip'">Skip</button></div>
                } @else {
                <select name="conf" [(ngModel)]="conflict">
                  <option value="rename">Keep both (rename)</option>
                  <option value="overwrite">Overwrite</option>
                  <option value="skip">Skip</option>
                </select>
                }</div>
            </div>
            @if (sum(); as m) {
              <label class="cl-only" style="align-self: start">Settings</label>
              <div class="setbox">
                <div class="st"><b>Settings in this export</b>{{ m.app ? ' (Termwright ' + m.app + ')' : '' }}</div>
                <label><input type="checkbox" name="sl" [(ngModel)]="partLogins" /><span>Device types, accounts and Folder Defaults
                  <span class="n">{{ m.deviceTypes }} device types · {{ m.accounts }} account{{ m.accounts === 1 ? '' : 's' }} · {{ m.folderDefaults }} folder default{{ m.folderDefaults === 1 ? '' : 's' }}</span></span></label>
                <label><input type="checkbox" name="sb" [(ngModel)]="partLibrary" /><span>Command library, checklists, Run and Collect, backup jobs, alerts, buttons, shortcuts, workspaces
                  <span class="n">{{ m.library }} item{{ m.library === 1 ? '' : 's' }}</span></span></label>
                <label><input type="checkbox" name="so" [(ngModel)]="partOptions" /><span>Global Options - appearance, terminal and behaviour
                  <span class="n">replaces yours</span></span></label>
                <div class="hint">Items with the same name are replaced, the others are kept. Folders on this computer, the master password and
                  open tabs stay as they are. Passwords come with Passwords &amp; Accounts → Import.</div>
              </div>
            }
            <label></label>
            @if (source === 'csv') {
              <div class="hint">First row = column names (comma, semicolon or tab separated). Only a host is needed (<code>host</code>, or ip/address); the others are
                <code>name, folder, protocol</code> (ssh, telnet, rdp, vnc, serial, raw, wol)<code>, port, username, device_type, environment, jump_host, identity_file,
                description, mac, account, files_panel, com_port, baud, network</code>. <code>jump_host</code> is the path of another session
                (<code>Folder/name</code>), so RDP and VNC through an SSH session work too.
                <button type="button" class="link" (click)="saveTemplate()">Save a template CSV…</button> ·
                <button type="button" class="link" (click)="backend.openURL(formatGuide)">Format guide</button></div>
            } @else if (source === 'ansible') {
              <div class="hint">Groups become folders; <code>ansible_host, ansible_user, ansible_port, ansible_connection,
                ansible_ssh_private_key_file</code> and <code>ansible_network_os</code> (→ device type) are used.</div>
            } @else if (source === 'netbox') {
              <div class="hint">Folders are Site/Role, the device type comes from the platform. The token needs read access to DCIM
                devices and is saved in the OS keychain.</div>
            } @else if (source === 'aws') {
              <div class="hint">Runs <code>aws ec2 describe-instances</code>. Running Linux instances are imported into AWS/&lt;region&gt;/&lt;VPC&gt;
                with user ec2-user and the key ~/.ssh/&lt;KeyName&gt;.pem. Set a jump session on the folder (Folder Defaults) to reach private IPs.</div>
            } @else if (source === 'putty') {
              <div class="hint">On Windows export the sessions with: <code>reg export HKCU\\Software\\SimonTatham\\PuTTY\\Sessions putty.reg</code>
                (KiTTY: HKCU\\Software\\9bis.com\\KiTTY\\Sessions). PuTTY Session Manager folders (PsmPath) are kept. PuTTY .ppk keys are
                used as they are.<br />SuperPuTTY: choose its <code>Sessions.xml</code> (File → Export Sessions, or the file in SuperPuTTY's data folder).
                Its folders are kept; a password given with <code>-pw</code> in the extra arguments is saved in the OS keychain, not in the session file.</div>
            } @else if (source === 'moba') {
              <div class="hint">In MobaXterm: right-click "User sessions" → Export all sessions to file (.mxtsessions). SSH, Telnet and Serial
                sessions are imported with their folders; RDP, VNC, FTP etc. are skipped.</div>
            } @else if (source === 'mremoteng') {
              <div class="hint">In mRemoteNG: File → Save Connection File As… and choose that <code>confCons.xml</code> (the default one is in
                %APPDATA%\\mRemoteNG). Containers become folders; SSH, Telnet, RAW, RDP and VNC connections are imported with the user name,
                domain, port and protocol they inherit from their container. Saved passwords are decrypted and moved into the OS keychain;
                if you set a master password in mRemoteNG, type it above. HTTP, ICA, Rlogin and SSH1 connections are skipped.
                If mRemoteNG encrypts the complete file, turn that off first (Tools → Options → Security).</div>
            } @else if (source === 'ssh') {
              <div class="hint">Each <code>Host</code> entry becomes an SSH2 session (HostName, User, Port, IdentityFile, ProxyJump).</div>
            } @else {
            <div class="hint">
              @switch (source) {
                @case ('twzip') { A .zip written by Export Sessions in Termwright. Its sessions keep everything; the settings in it can be imported too. }
                @case ('twdir') { A folder written by Export Sessions in Termwright (it holds a Sessions folder). }
                @case ('zip') { A .zip of a SecureCRT Config or Config/Sessions folder (e.g. a backup). }
                @case ('xml') { In SecureCRT: Tools → Export Settings… writes an .xml file with all sessions. }
                @case ('ini') { One SecureCRT session file from Config/Sessions. }
              }
              @if (isSecureCRT()) { Typical locations: Windows %APPDATA%\\VanDyke\\Config, macOS ~/Library/Application Support/VanDyke/SecureCRT/Config,
              Linux ~/.vandyke/SecureCRT/Config. Saved passwords (encrypted or plain text) are moved into the OS keychain and removed from the copied session files; everything else is copied unchanged. }</div>
            }
          </div>
        } @else {
          @let r = result()!;
          <p><b>{{ r.imported?.length ?? 0 }}</b> imported, <b>{{ r.renamed?.length ?? 0 }}</b> renamed,
             <b>{{ r.skipped?.length ?? 0 }}</b> skipped, <b>{{ r.errors?.length ?? 0 }}</b> errors.</p>
          @if (r.settings?.length) { <p><b>Settings</b></p><ul class="lst">@for (n of r.settings!; track n) { <li>{{ n }}</li> }</ul> }
          @if (r.passwordsImported) { <p><b>{{ r.passwordsImported }}</b> saved password(s) moved to the OS keychain.</p> }
          @if (r.errors?.length) { <pre class="log">{{ r.errors!.join('\\n') }}</pre> }
          @if (r.notes?.length) {
            <div class="hint">Left out or replaced in the file ({{ r.notes!.length }}):</div>
            <pre class="log">{{ r.notes!.join('\\n') }}</pre>
            <div class="hint"><button type="button" class="link" (click)="backend.openURL(formatGuide)">Format guide</button> - the columns and values the import accepts.</div>
          }
          @if (r.passwordErrors?.length) {
            <div class="hint">Passwords not imported (enter the SecureCRT configuration passphrase or the mRemoteNG master password and import again with "Overwrite",
              or type them when connecting):</div>
            <pre class="log">{{ r.passwordErrors!.join('\\n') }}</pre>
          }
          @if (r.renamed?.length) { <div class="hint">Renamed: {{ r.renamed!.join(', ') }}</div> }
        }
        @if (error()) { <div class="err">{{ error() }}</div> }
      </div>
      <div class="modal-foot">
        <span class="spacer"></span>
        @if (!result()) {
          <button type="button" (click)="ui.close()">Cancel</button>
          <button type="submit" class="primary" [disabled]="busy()">{{ busy() ? 'Importing...' : 'Import' }}</button>
        } @else {
          <button type="button" class="primary" (click)="ui.close()">Close</button>
        }
      </div>
    </form>
  </div>`,
  styles: [`
    .link { background: none; border: 0; padding: 0; color: var(--accent); cursor: pointer; font: inherit; }
    .src-groups { display: flex; flex-direction: column; gap: 10px; }
    .src-group { display: flex; flex-direction: column; gap: 5px; label { display: flex; gap: 8px; align-items: center; cursor: pointer; } }
    .src-title { font-size: calc(11px * var(--ui-scale, 1)); font-weight: 600; text-transform: uppercase; letter-spacing: .04em; color: var(--muted); }
    .setbox { display: flex; flex-direction: column; gap: 6px; border: 1px solid var(--line); border-radius: var(--r-lg, 6px); padding: 8px 12px; background: var(--surface, transparent);
      label { display: flex; gap: 8px; align-items: flex-start; cursor: pointer; input { margin-top: 3px; flex: none; } }
      .n { margin-left: 6px; color: var(--muted); font-size: calc(11.5px * var(--ui-scale, 1)); }
      .hint { margin: 2px 0 0; } }
    :host-context(.modern) .setbox { grid-column: 1 / -1; margin-top: 4px; }
    .lst { margin: 4px 0 10px; padding-left: 18px; font-size: calc(12.5px * var(--ui-scale, 1)); }
    .log { max-height: 160px; overflow: auto; font-size: calc(11px * var(--ui-scale, 1)); background: var(--input-bg); padding: 6px; }
    :host-context(.modern) {
      .modal { width: min(640px, 95vw); }
      .found { border: 1px solid var(--line); border-radius: var(--r-lg); padding: 8px 12px; margin-bottom: 12px; background: var(--surface);
        .fh { display: flex; align-items: center; gap: 8px; margin-bottom: 4px; } .g { width: 8px; height: 8px; border-radius: 50%; background: var(--ok); }
        .fr { display: flex; align-items: center; gap: 10px; padding: 4px 0; border-top: 1px solid var(--line); }
        .fh + .fr { border-top: 0; }
        .app { font-weight: 600; min-width: 84px; } .fp { flex: 1; min-width: 0; color: var(--muted); font-size: calc(12px * var(--ui-scale, 1)); white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
        button { height: 26px; } }
      .sec { font-size: calc(11px * var(--ui-scale, 1)); font-weight: 600; letter-spacing: .06em; text-transform: uppercase; color: var(--faint); margin: 10px 0 6px; }
      .cards { display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); gap: 8px; }
      .sc { height: auto; display: flex; flex-direction: column; align-items: flex-start; gap: 2px; padding: 9px 10px; text-align: left; border: 1px solid var(--line);
        border-radius: var(--r-lg); background: var(--surface-2); min-width: 0;
        b { font-weight: 600; } span { color: var(--muted); font-size: calc(11.5px * var(--ui-scale, 1)); white-space: nowrap; overflow: hidden; text-overflow: ellipsis; max-width: 100%; }
        &.on { border-color: var(--accent); background: var(--accent-soft); box-shadow: 0 0 0 1px var(--accent); } }
      .grid { margin-top: 14px; }
      .pathin { font-family: var(--mono); font-size: calc(12px * var(--ui-scale, 1)); }
    }`],
})
export class ImportDialog {
  readonly formatGuide = 'https://github.com/nperiannan/termwright/blob/main/docs/IMPORT_FORMAT.md';
  protected ui = inject(Ui);
  protected backend = inject(Backend);
  private cdr = inject(ChangeDetectorRef);
  readonly modern = () => this.ui.settings()?.layout !== 'classic';
  /** Sessions of other programs found in their usual places on this computer. */
  readonly found = signal<ImportSource[]>([]);
  constructor() { this.backend.findImportSources().then(l => this.found.set(l ?? [])).catch(() => {}); }
  useFound(f: ImportSource) {
    const m: Record<string, Source> = { SecureCRT: 'folder', PuTTY: 'putty', SuperPuTTY: 'putty', MobaXterm: 'moba', OpenSSH: 'ssh' };
    this.source = m[f.app] ?? this.source;
    this.path = f.path;
    void this.checkSettings();
  }
  cardOf(v: Source): { t: string; s: string } {
    const c: Record<Source, { t: string; s: string }> = {
      twzip: { t: 'Termwright .zip', s: 'sessions and settings' }, twdir: { t: 'Termwright folder', s: 'export folder' },
      folder: { t: 'SecureCRT', s: 'Config folder' }, zip: { t: 'SecureCRT .zip', s: 'Sessions folder archive' }, xml: { t: 'SecureCRT .xml', s: 'Export Settings file' },
      ini: { t: 'SecureCRT .ini', s: 'one session file' }, putty: { t: 'PuTTY / KiTTY', s: '.reg, folder, SuperPuTTY' }, moba: { t: 'MobaXterm', s: '.mxtsessions, .ini' }, mremoteng: { t: 'mRemoteNG', s: 'confCons.xml' },
      ssh: { t: 'OpenSSH', s: '~/.ssh/config' }, csv: { t: 'CSV', s: 'spreadsheet export' }, ansible: { t: 'Ansible', s: 'INI or YAML inventory' },
      netbox: { t: 'NetBox', s: 'devices with a primary IP' }, aws: { t: 'AWS EC2', s: 'via the AWS CLI' },
    };
    return c[v];
  }
  pathLabel() { return this.source === 'folder' ? 'Config folder' : this.source === 'twdir' ? 'Export folder' : this.source === 'putty' ? 'File or folder' : 'File'; }
  readonly result = signal<ImportResult | null>(null);
  readonly busy = signal(false);
  readonly error = signal('');
  source: Source = 'twzip';
  /** Settings in a Termwright export, when the chosen file has them. */
  readonly sum = signal<SettingsSummary | null>(null);
  partLogins = true; partLibrary = true; partOptions = false;
  async checkSettings() {
    const p = this.path;
    const s = p && !['netbox', 'aws'].includes(this.source) ? await this.backend.exportSettingsIn(p).catch(() => null) : null;
    if (p === this.path) this.sum.set(s);
  }
  path = '';
  target = '';
  conflict: 'rename' | 'overwrite' | 'skip' = 'rename';
  configPass = '';
  isSecureCRT() { return ['folder', 'zip', 'xml', 'ini'].includes(this.source); }
  nbUrl = ''; nbToken = ''; nbFilter = ''; awsProfile = ''; awsRegion = ''; awsPublic = false;

  readonly groups: { title: string; items: { v: Source; label: string }[] }[] = [
    { title: 'Termwright', items: [
      { v: 'twzip', label: 'Termwright export (.zip) - sessions and settings' },
      { v: 'twdir', label: 'Termwright export folder' },
    ] },
    { title: 'SecureCRT', items: [
      { v: 'folder', label: 'SecureCRT configuration folder (Config or Config/Sessions)' },
      { v: 'zip', label: 'SecureCRT Sessions folder as a .zip archive' },
      { v: 'xml', label: 'SecureCRT XML settings export (.xml)' },
      { v: 'ini', label: 'SecureCRT single session file (.ini)' },
    ] },
    { title: 'Other terminal apps', items: [
      { v: 'putty', label: 'PuTTY / KiTTY / SuperPuTTY sessions (.reg export, SuperPuTTY Sessions.xml, or the ~/.putty/sessions folder)' },
      { v: 'moba', label: 'MobaXterm sessions (.mxtsessions export or MobaXterm.ini)' },
      { v: 'mremoteng', label: 'mRemoteNG connections (confCons.xml)' },
      { v: 'ssh', label: 'OpenSSH client config (~/.ssh/config)' },
    ] },
    { title: 'Inventories', items: [
      { v: 'csv', label: 'CSV / spreadsheet export (.csv)' },
      { v: 'ansible', label: 'Ansible inventory (INI or YAML)' },
      { v: 'netbox', label: 'NetBox (devices with a primary IP)' },
      { v: 'aws', label: 'AWS EC2 instances (uses the AWS CLI)' },
    ] },
  ];

  pick(v: Source) {
    this.path = v === 'netbox' || v === 'aws' ? v : '';
    this.sum.set(null);
    if (v === 'ssh') void this.sshDefault();
  }

  async sshDefault() {
    this.path = await this.backend.defaultSSHConfigPath().catch(() => '');
    this.cdr.markForCheck();
  }

  async browse() {
    const filters: Record<Source, { displayName: string; pattern: string }[]> = {
      twzip: [{ displayName: 'Termwright export (*.zip)', pattern: '*.zip' }], twdir: [],
      folder: [], zip: [{ displayName: 'Zip archive (*.zip)', pattern: '*.zip' }], xml: [{ displayName: 'SecureCRT XML export (*.xml)', pattern: '*.xml' }],
      ini: [{ displayName: 'Session file (*.ini)', pattern: '*.ini' }], ssh: [],
      putty: [{ displayName: 'PuTTY / KiTTY registry export or SuperPuTTY Sessions.xml', pattern: '*.reg;*.xml' }],
      csv: [{ displayName: 'CSV', pattern: '*.csv;*.tsv;*.txt' }], ansible: [], netbox: [], aws: [],
      moba: [{ displayName: 'MobaXterm sessions', pattern: '*.mxtsessions;*.ini' }],
      mremoteng: [{ displayName: 'mRemoteNG connections (confCons.xml)', pattern: '*.xml' }],
    };
    if (this.source === 'putty' && !(await this.ui.confirm('PuTTY Sessions', 'Choose a file - a .reg export or SuperPuTTY Sessions.xml? (Cancel = choose a ~/.putty/sessions folder)', 'File'))) {
      const d = await this.backend.chooseDirectory('Select ~/.putty/sessions Folder').catch(() => '');
      if (d) this.path = d;
      this.cdr.markForCheck();
      return;
    }
    const p = this.source === 'folder' || this.source === 'twdir'
      ? await this.backend.chooseDirectory(this.source === 'twdir' ? 'Select Export Folder' : 'Select SecureCRT Config Folder').catch(() => '')
      : await this.backend.chooseOpenFile('Select File', filters[this.source]).catch(() => '');
    if (p) { this.path = p; await this.checkSettings(); }
    this.cdr.markForCheck();
  }

  /** Saves the example CSV of the import format. */
  async saveTemplate() {
    try {
      const p = await this.backend.saveTextAs('Save Template CSV', 'termwright-sessions-template.csv', await this.backend.importTemplate());
      if (p) this.ui.toast('Saved ' + p, 'success');
    } catch (e) { this.ui.error(e); }
  }

  async run() {
    if (!this.path) return;
    this.busy.set(true);
    this.error.set('');
    try {
      const settings = this.sum() ? [this.partLogins && 'logins', this.partLibrary && 'library', this.partOptions && 'options'].filter((x): x is string => !!x) : [];
      const opt = { targetFolder: this.target, conflict: this.conflict, includeDefault: false, configPassphrase: this.isSecureCRT() || this.source === 'mremoteng' ? this.configPass : '', settings };
      const r = this.source === 'netbox' ? await this.backend.importNetBox(this.nbUrl, this.nbToken, this.nbFilter, opt)
        : this.source === 'aws' ? await this.backend.importAWS(this.awsProfile, this.awsRegion, this.awsPublic, opt)
        : await this.backend.importSessions(this.path, opt);
      this.result.set(r);
      await this.ui.refreshTree();
      if (settings.length) await this.ui.loadSettings();
    } catch (e) { this.error.set(errText(e)); }
    this.busy.set(false);
  }
}

type ExportFormat = 'dir' | 'zip' | 'xml' | 'putty-reg' | 'kitty-reg' | 'putty-dir' | 'moba' | 'tabby' | 'ssh-config' | 'csv';

const OTHER_FORMATS: { v: ExportFormat; label: string; ext: string; hint: string }[] = [
  { v: 'putty-reg', label: 'PuTTY (.reg, Windows)', ext: 'reg', hint: 'Double-click the .reg file on Windows (or File → Import in Registry Editor); the sessions appear in PuTTY. The jump host becomes an SSH proxy (PuTTY 0.77 or newer).' },
  { v: 'kitty-reg', label: 'KiTTY (.reg, Windows)', ext: 'reg', hint: 'Double-click the .reg file on Windows; the sessions appear in KiTTY with their folders.' },
  { v: 'putty-dir', label: 'PuTTY sessions folder (Linux / macOS)', ext: '', hint: 'Choose ~/.putty/sessions (or copy the files there).' },
  { v: 'moba', label: 'MobaXterm (.mxtsessions)', ext: 'mxtsessions', hint: 'In MobaXterm: right-click "User sessions" → Import sessions from file. The jump host becomes the SSH gateway.' },
  { v: 'tabby', label: 'Tabby profiles (.yaml)', ext: 'yaml', hint: "Add the profiles to Tabby's config.yaml (Settings → Config file → Show config file) and restart Tabby." },
  { v: 'ssh-config', label: 'OpenSSH config (ssh, VS Code, any SSH tool)', ext: 'conf', hint: 'Add it to ~/.ssh/config, or save it as ~/.ssh/termwright.conf and put "Include termwright.conf" at the top of ~/.ssh/config.' },
  { v: 'csv', label: 'CSV (spreadsheet, inventory)', ext: 'csv', hint: 'Opens in Excel / Numbers / LibreOffice; Termwright and many tools import it again.' },
];

/** Export sessions: SecureCRT format (folder, zip, XML) or for other programs. */
@Component({
  selector: 'app-export-dialog',
  imports: [FormsModule, Icon],
  template: `
  <div class="modal-backdrop">
    <form class="modal exp" (ngSubmit)="run()">
      <div class="modal-head"><span class="mh-ic"><app-icon name="export" [size]="17" /></span>
        <span class="mh-t">Export Sessions<span class="mh-s">Save sessions for a backup, a colleague or another program</span></span></div>
      <div class="modal-body">
        @if (report(); as r) {
          <p><b>Exported {{ r.count }} session{{ r.count === 1 ? '' : 's' }}</b> to <span class="mono">{{ dest }}</span>.</p>
          <p class="hint">{{ hint() }}</p>
          @if (r.skipped.length) {
            <p><b>Not exported</b> ({{ r.skipped.length }}): the format has no such sessions.</p>
            <ul class="lst">@for (s of r.skipped.slice(0, 12); track s) { <li>{{ s }}</li> }@if (r.skipped.length > 12) { <li>…</li> }</ul>
          }
          @if (r.notes.length) {
            <p><b>Notes</b> - what stays in Termwright and how some settings were written:</p>
            <ul class="lst">@for (n of r.notes; track n) { <li>{{ n }}</li> }</ul>
          }
        } @else {
        @if (modern()) {
          <div class="xs">
            <div class="lbl">Sessions</div>
            <div class="seg scope"><button type="button" [class.on]="scope === 'all'" (click)="scope = 'all'">All sessions</button>
              <button type="button" [class.on]="scope === 'sel'" [disabled]="!selected" (click)="scope = 'sel'">{{ selected ? 'Selected: ' + selected : 'Selected (nothing selected)' }}</button></div>
            <div class="sec">Termwright - everything is kept</div>
            <div class="cards">
              @for (f of own; track f.v) { <button type="button" class="sc" [class.on]="format === f.v" (click)="format = f.v; dest = ''"><b>{{ f.t }}</b><span>{{ f.s }}</span></button> }
            </div>
            @if (own.some(o => o.v === format)) {
              <label class="withset" [class.disabled]="format === 'xml'"><input type="checkbox" name="ws" [(ngModel)]="withSettings" [disabled]="format === 'xml'" />
                <span><b>Include settings</b> - device types, accounts (without passwords), Folder Defaults, command library, checklists, jobs and Global Options.
                  @if (format === 'xml') { XML holds sessions only; choose Zip or Folder to take the settings along. }</span></label>
            }
            <div class="sec">Other programs - name, folder, address, login, key, jump host</div>
            <div class="chipset">
              @for (f of others; track f.v) { <button type="button" [class.on]="format === f.v" (click)="format = f.v; dest = ''" [title]="f.label">{{ chipOf(f.v) }}</button> }
            </div>
            <div class="lbl" style="margin-top: 14px">Save to</div>
            <div class="row"><input name="dest2" class="pathin" [(ngModel)]="dest" required /><button type="button" (click)="browse()">Browse...</button></div>
            <div class="hint">@if (hint()) { {{ hint() }} } Passwords are not written into the export - Passwords &amp; Accounts → Export / Import moves them.</div>
          </div>
        } @else {
        <div class="grid">
          <label>Sessions</label>
          <div class="checks">
            <label><input type="radio" name="scope" value="all" [(ngModel)]="scope" /> All sessions</label>
            <label [class.disabled]="!selected"><input type="radio" name="scope" value="sel" [(ngModel)]="scope" [disabled]="!selected" />
              Selected: {{ selected || '(nothing selected)' }}</label>
          </div>
          <label>Format</label>
          <div class="checks">
            <div class="grp">Termwright (everything is kept)</div>
            <label><input type="radio" name="fmt" value="dir" [(ngModel)]="format" (ngModelChange)="dest = ''" /> Folder (Sessions/*.ini; also works as a SecureCRT Config/Sessions folder)</label>
            <label><input type="radio" name="fmt" value="zip" [(ngModel)]="format" (ngModelChange)="dest = ''" /> Zip archive</label>
            <label><input type="radio" name="fmt" value="xml" [(ngModel)]="format" (ngModelChange)="dest = ''" /> XML (also for SecureCRT's Import Settings)</label>
            @if (own.some(o => o.v === format)) {
              <label [class.disabled]="format === 'xml'"><input type="checkbox" name="ws1" [(ngModel)]="withSettings" [disabled]="format === 'xml'" />
                Include settings (device types, accounts, Folder Defaults, command library, Global Options){{ format === 'xml' ? ' - Zip or Folder only' : '' }}</label>
            }
            <div class="grp">Other programs (name, folder, address, login, key, jump host, forwards, serial settings)</div>
            @for (f of others; track f.v) {
              <label><input type="radio" name="fmt" [value]="f.v" [(ngModel)]="format" (ngModelChange)="dest = ''" /> {{ f.label }}</label>
            }
          </div>
          <label>Destination</label>
          <div class="row"><input name="dest" [(ngModel)]="dest" required /><button type="button" (click)="browse()">Browse...</button></div>
          @if (hint()) { <label></label><div class="hint">{{ hint() }} Passwords are never exported.</div> }
        </div>
        }
        }
        @if (error()) { <div class="err">{{ error() }}</div> }
      </div>
      <div class="modal-foot">
        <span class="spacer"></span>
        @if (report()) {
          <button type="button" (click)="reveal()">Show File</button>
          <button type="button" class="primary" (click)="ui.close()">Done</button>
        } @else {
          <button type="button" (click)="ui.close()">Cancel</button>
          <button type="submit" class="primary">Export</button>
        }
      </div>
    </form>
  </div>`,
  styles: [`
    .exp { width: min(720px, 95vw); }
    .grp { font-size: calc(11.5px * var(--ui-scale, 1)); color: var(--muted); text-transform: uppercase; letter-spacing: .03em; margin: 6px 0 2px; }
    .grp:first-child { margin-top: 0; }
    .lst { margin: 4px 0 10px; padding-left: 18px; font-size: calc(12.5px * var(--ui-scale, 1)); max-height: 180px; overflow: auto; }
    .mono { font-family: var(--mono); font-size: calc(12px * var(--ui-scale, 1)); }
    .disabled { opacity: .6; }
    :host-context(.modern) {
      .exp { width: min(640px, 95vw); }
      .withset { display: flex; gap: 8px; align-items: flex-start; margin-top: 10px; cursor: pointer; font-size: calc(12.5px * var(--ui-scale, 1)); color: var(--muted);
        input { margin-top: 2px; } b { color: var(--text, inherit); font-weight: 600; } }
      .lbl { font-size: calc(12px * var(--ui-scale, 1)); font-weight: 500; color: var(--muted); margin-bottom: 5px; }
      .scope { max-width: 100%; button { white-space: nowrap; overflow: hidden; text-overflow: ellipsis; } }
      .sec { font-size: calc(11px * var(--ui-scale, 1)); font-weight: 600; letter-spacing: .06em; text-transform: uppercase; color: var(--faint); margin: 14px 0 6px; }
      .cards { display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); gap: 8px; }
      .sc { height: auto; display: flex; flex-direction: column; align-items: flex-start; gap: 2px; padding: 9px 10px; text-align: left; border: 1px solid var(--line);
        border-radius: var(--r-lg); background: var(--surface-2);
        b { font-weight: 600; } span { color: var(--muted); font-size: calc(11.5px * var(--ui-scale, 1)); }
        &.on { border-color: var(--accent); background: var(--accent-soft); box-shadow: 0 0 0 1px var(--accent); } }
      .row { display: flex; gap: 8px; input { flex: 1; } }
      .pathin { font-family: var(--mono); font-size: calc(12px * var(--ui-scale, 1)); }
      .hint { margin-top: 8px; }
    }
  `],
})
export class ExportDialog {
  protected ui = inject(Ui);
  private backend = inject(Backend);
  private cdr = inject(ChangeDetectorRef);
  readonly error = signal('');
  readonly report = signal<ExportReport | null>(null);
  readonly others = OTHER_FORMATS;
  readonly modern = () => this.ui.settings()?.layout !== 'classic';
  readonly own: { v: ExportFormat; t: string; s: string }[] = [
    { v: 'zip', t: 'Zip archive', s: 'one file, easy to send' }, { v: 'dir', t: 'Folder', s: 'Sessions/*.ini' }, { v: 'xml', t: 'XML', s: 'also for SecureCRT' }];
  chipOf(v: ExportFormat) {
    return ({ 'putty-reg': 'PuTTY .reg', 'kitty-reg': 'KiTTY .reg', 'putty-dir': 'PuTTY folder', moba: 'MobaXterm', tabby: 'Tabby', 'ssh-config': 'OpenSSH config', csv: 'CSV' } as Record<string, string>)[v] ?? v;
  }
  readonly selected = this.ui.selectedPath();
  scope: 'all' | 'sel' = this.selected ? 'sel' : 'all';
  format: ExportFormat = 'zip';
  dest = '';
  /** Zip and folder exports also carry the settings (on by default). */
  withSettings = true;

  hint() { return OTHER_FORMATS.find(f => f.v === this.format)?.hint ?? ''; }

  async browse() {
    const other = OTHER_FORMATS.find(f => f.v === this.format);
    const ext = other ? other.ext : this.format;
    const name = this.format === 'ssh-config' ? 'termwright.conf' : this.format === 'tabby' ? 'tabby-profiles.yaml' : 'termwright-sessions.' + ext;
    const p = this.format === 'dir' || this.format === 'putty-dir'
      ? await this.backend.chooseDirectory('Export To Folder').catch(() => '')
      : await this.backend.chooseSaveFile('Export As', name, [{ displayName: ext.toUpperCase(), pattern: '*.' + ext }]).catch(() => '');
    if (p) this.dest = p;
    this.cdr.markForCheck();
  }

  async run() {
    if (!this.dest) return;
    this.error.set('');
    const paths = this.scope === 'sel' ? [this.selected] : [];
    try {
      if (OTHER_FORMATS.some(f => f.v === this.format)) {
        this.report.set(await this.backend.exportSessionsFor(paths, this.dest, this.format));
      } else {
        const set = this.withSettings && this.format !== 'xml';
        const n = await this.backend.exportSessions(paths, this.dest, this.format, set);
        this.ui.toast(`Exported ${n} session(s)${set ? ' and the settings' : ''} to ${this.dest}`, 'success');
        this.ui.close();
      }
    } catch (e) { this.error.set(errText(e)); }
  }

  async reveal() { await this.backend.showInFolder(this.dest).catch(e => this.ui.error(e)); }
}
