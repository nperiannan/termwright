import { ChangeDetectorRef, Component, inject, input, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { Backend, errText } from '../core/backend';
import { ImportResult } from '../core/models';
import { Ui } from '../core/ui.service';

type Source = 'folder' | 'zip' | 'xml' | 'ini' | 'ssh' | 'putty' | 'moba' | 'csv' | 'ansible' | 'netbox' | 'aws';

/** Import sessions from SecureCRT (config folder, .zip, .xml, .ini), PuTTY/KiTTY, MobaXterm or OpenSSH config. */
@Component({
  selector: 'app-import-dialog',
  imports: [FormsModule],
  template: `
  <div class="modal-backdrop">
    <form class="modal" (ngSubmit)="run()">
      <div class="modal-head">Import Sessions</div>
      <div class="modal-body">
        @if (!result()) {
          <div class="grid">
            <label style="align-self: start; padding-top: 1px">Source</label>
            <div class="src-groups">
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
            <label>Path</label>
            <div class="row"><input name="path" [(ngModel)]="path" required /><button type="button" (click)="browse()">Browse...</button></div>
            }
            @if (isSecureCRT()) {
              <label>Config passphrase</label>
              <input name="ccp" type="password" [(ngModel)]="configPass" autocomplete="off"
                placeholder="only if you set a configuration passphrase in SecureCRT" />
            }
            <label>Into folder</label><input name="tf" [(ngModel)]="target" placeholder="(top level)" />
            <label>If name exists</label>
            <select name="conf" [(ngModel)]="conflict">
              <option value="rename">Keep both (rename)</option>
              <option value="overwrite">Overwrite</option>
              <option value="skip">Skip</option>
            </select>
            <label></label>
            @if (source === 'csv') {
              <div class="hint">First row = column names. Recognised: <code>name, host</code> (or ip/address), <code>protocol, port, username,
                folder, device_type, description, identity_file, com_port, baud</code>. Comma, semicolon or tab separated.</div>
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
                used as they are.</div>
            } @else if (source === 'moba') {
              <div class="hint">In MobaXterm: right-click "User sessions" → Export all sessions to file (.mxtsessions). SSH, Telnet and Serial
                sessions are imported with their folders; RDP, VNC, FTP etc. are skipped.</div>
            } @else if (source === 'ssh') {
              <div class="hint">Each <code>Host</code> entry becomes an SSH2 session (HostName, User, Port, IdentityFile, ProxyJump).</div>
            } @else {
            <div class="hint">
              @switch (source) {
                @case ('zip') { A .zip of a SecureCRT Config or Config/Sessions folder (e.g. a backup). }
                @case ('xml') { In SecureCRT: Tools → Export Settings… writes an .xml file with all sessions. }
                @case ('ini') { One SecureCRT session file from Config/Sessions. }
              }
              Typical locations: Windows %APPDATA%\\VanDyke\\Config, macOS ~/Library/Application Support/VanDyke/SecureCRT/Config,
              Linux ~/.vandyke/SecureCRT/Config. Saved passwords (encrypted or plain text) are moved into the OS keychain and removed from the copied session files; everything else is copied unchanged.</div>
            }
          </div>
        } @else {
          @let r = result()!;
          <p><b>{{ r.imported?.length ?? 0 }}</b> imported, <b>{{ r.renamed?.length ?? 0 }}</b> renamed,
             <b>{{ r.skipped?.length ?? 0 }}</b> skipped, <b>{{ r.errors?.length ?? 0 }}</b> errors.</p>
          @if (r.passwordsImported) { <p><b>{{ r.passwordsImported }}</b> saved password(s) moved to the OS keychain.</p> }
          @if (r.errors?.length) { <pre class="log">{{ r.errors!.join('\\n') }}</pre> }
          @if (r.passwordErrors?.length) {
            <div class="hint">Passwords not imported (enter the SecureCRT configuration passphrase and import again with "Overwrite",
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
    .src-groups { display: flex; flex-direction: column; gap: 10px; }
    .src-group { display: flex; flex-direction: column; gap: 5px; label { display: flex; gap: 8px; align-items: center; cursor: pointer; } }
    .src-title { font-size: 11px; font-weight: 600; text-transform: uppercase; letter-spacing: .04em; color: var(--muted); }
    .log { max-height: 160px; overflow: auto; font-size: 11px; background: var(--input-bg); padding: 6px; }`],
})
export class ImportDialog {
  protected ui = inject(Ui);
  private backend = inject(Backend);
  private cdr = inject(ChangeDetectorRef);
  readonly result = signal<ImportResult | null>(null);
  readonly busy = signal(false);
  readonly error = signal('');
  source: Source = 'folder';
  path = '';
  target = '';
  conflict: 'rename' | 'overwrite' | 'skip' = 'rename';
  configPass = '';
  isSecureCRT() { return ['folder', 'zip', 'xml', 'ini'].includes(this.source); }
  nbUrl = ''; nbToken = ''; nbFilter = ''; awsProfile = ''; awsRegion = ''; awsPublic = false;

  readonly groups: { title: string; items: { v: Source; label: string }[] }[] = [
    { title: 'SecureCRT', items: [
      { v: 'folder', label: 'SecureCRT configuration folder (Config or Config/Sessions)' },
      { v: 'zip', label: 'SecureCRT Sessions folder as a .zip archive' },
      { v: 'xml', label: 'SecureCRT XML settings export (.xml)' },
      { v: 'ini', label: 'SecureCRT single session file (.ini)' },
    ] },
    { title: 'Other terminal apps', items: [
      { v: 'putty', label: 'PuTTY / KiTTY sessions (.reg export, or the ~/.putty/sessions folder)' },
      { v: 'moba', label: 'MobaXterm sessions (.mxtsessions export or MobaXterm.ini)' },
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
    if (v === 'ssh') void this.sshDefault();
  }

  async sshDefault() {
    this.path = await this.backend.defaultSSHConfigPath().catch(() => '');
    this.cdr.markForCheck();
  }

  async browse() {
    const filters: Record<Source, { displayName: string; pattern: string }[]> = {
      folder: [], zip: [{ displayName: 'Zip archive (*.zip)', pattern: '*.zip' }], xml: [{ displayName: 'SecureCRT XML export (*.xml)', pattern: '*.xml' }],
      ini: [{ displayName: 'Session file (*.ini)', pattern: '*.ini' }], ssh: [],
      putty: [{ displayName: 'Registry export', pattern: '*.reg' }],
      csv: [{ displayName: 'CSV', pattern: '*.csv;*.tsv;*.txt' }], ansible: [], netbox: [], aws: [],
      moba: [{ displayName: 'MobaXterm sessions', pattern: '*.mxtsessions;*.ini' }],
    };
    if (this.source === 'putty' && !(await this.ui.confirm('PuTTY Sessions', 'Choose a .reg export file? (Cancel = choose a ~/.putty/sessions folder)', 'File'))) {
      const d = await this.backend.chooseDirectory('Select ~/.putty/sessions Folder').catch(() => '');
      if (d) this.path = d;
      this.cdr.markForCheck();
      return;
    }
    const p = this.source === 'folder'
      ? await this.backend.chooseDirectory('Select SecureCRT Config Folder').catch(() => '')
      : await this.backend.chooseOpenFile('Select File', filters[this.source]).catch(() => '');
    if (p) this.path = p;
    this.cdr.markForCheck();
  }

  async run() {
    if (!this.path) return;
    this.busy.set(true);
    this.error.set('');
    try {
      const opt = { targetFolder: this.target, conflict: this.conflict, includeDefault: false, configPassphrase: this.isSecureCRT() ? this.configPass : '' };
      const r = this.source === 'netbox' ? await this.backend.importNetBox(this.nbUrl, this.nbToken, this.nbFilter, opt)
        : this.source === 'aws' ? await this.backend.importAWS(this.awsProfile, this.awsRegion, this.awsPublic, opt)
        : await this.backend.importSessions(this.path, opt);
      this.result.set(r);
      await this.ui.refreshTree();
    } catch (e) { this.error.set(errText(e)); }
    this.busy.set(false);
  }
}

/** Export sessions as a SecureCRT-compatible folder, zip or XML. */
@Component({
  selector: 'app-export-dialog',
  imports: [FormsModule],
  template: `
  <div class="modal-backdrop">
    <form class="modal" (ngSubmit)="run()">
      <div class="modal-head">Export Sessions</div>
      <div class="modal-body">
        <div class="grid">
          <label>Sessions</label>
          <div class="checks">
            <label><input type="radio" name="scope" value="all" [(ngModel)]="scope" /> All sessions</label>
            <label [class.disabled]="!selected"><input type="radio" name="scope" value="sel" [(ngModel)]="scope" [disabled]="!selected" />
              Selected: {{ selected || '(nothing selected)' }}</label>
          </div>
          <label>Format</label>
          <div class="checks">
            <label><input type="radio" name="fmt" value="dir" [(ngModel)]="format" (ngModelChange)="dest = ''" /> Folder (Sessions/*.ini, copy into SecureCRT's Config folder)</label>
            <label><input type="radio" name="fmt" value="zip" [(ngModel)]="format" (ngModelChange)="dest = ''" /> Zip archive</label>
            <label><input type="radio" name="fmt" value="xml" [(ngModel)]="format" (ngModelChange)="dest = ''" /> XML (for Import Settings)</label>
          </div>
          <label>Destination</label>
          <div class="row"><input name="dest" [(ngModel)]="dest" required /><button type="button" (click)="browse()">Browse...</button></div>
        </div>
        @if (error()) { <div class="err">{{ error() }}</div> }
      </div>
      <div class="modal-foot">
        <span class="spacer"></span>
        <button type="button" (click)="ui.close()">Cancel</button>
        <button type="submit" class="primary">Export</button>
      </div>
    </form>
  </div>`,
})
export class ExportDialog {
  protected ui = inject(Ui);
  private backend = inject(Backend);
  private cdr = inject(ChangeDetectorRef);
  readonly error = signal('');
  readonly selected = this.ui.selectedPath();
  scope: 'all' | 'sel' = this.selected ? 'sel' : 'all';
  format: 'dir' | 'zip' | 'xml' = 'zip';
  dest = '';

  async browse() {
    const p = this.format === 'dir'
      ? await this.backend.chooseDirectory('Export To Folder').catch(() => '')
      : await this.backend.chooseSaveFile('Export As', 'termwright-sessions.' + this.format,
          [{ displayName: this.format.toUpperCase(), pattern: '*.' + this.format }]).catch(() => '');
    if (p) this.dest = p;
    this.cdr.markForCheck();
  }

  async run() {
    if (!this.dest) return;
    try {
      const n = await this.backend.exportSessions(this.scope === 'sel' ? [this.selected] : [], this.dest, this.format);
      this.ui.toast(`Exported ${n} session(s) to ${this.dest}`, 'success');
      this.ui.close();
    } catch (e) { this.error.set(errText(e)); }
  }
}
