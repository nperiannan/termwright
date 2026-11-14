import { Component, OnDestroy, computed, effect, inject, input, signal, untracked } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { LabService } from '../core/lab.service';
import { Backend, errText } from '../core/backend';
import { EmuServer, EmuServerView, LabDevice } from '../core/models';
import { Ui } from '../core/ui.service';
import { tr } from '../core/i18n';

/** A lab server in the side pane: its services (HTTP, FTP, TFTP, DHCP), how to reach it, its shared folder
 *  (files in from this computer, out, deleted) and its DHCP leases. */
@Component({
  selector: 'app-lab-server',
  standalone: true,
  imports: [FormsModule],
  template: `
  @let v = view(); @let s = edit();
  @if (s) {
    <div class="srv">
      <div class="sh"><b>{{ tr('Services') }}</b><small class="muted">{{ tr('port {0}', s.dhcpPort || 1) }} · {{ v?.addr || '-' }}</small><span class="sp"></span>
        @if (dirty()) { <button type="button" class="sm" (click)="revert()">{{ tr('Revert') }}</button><button type="button" class="sm primary" (click)="apply()" [disabled]="busy()">{{ busy() ? tr('Applying…') : tr('Apply') }}</button> }</div>
      <div class="sv"><label class="sw" [title]="tr('On or off')"><input type="checkbox" [ngModel]="s.http" (ngModelChange)="set({ http: $event })" /><i></i></label><b>HTTP</b>@if (url('http'); as u) { <a class="l" (click)="open(u)">{{ tr('Open') }} ↗</a> } @else { <span></span> }
        <span class="d">{{ tr('port') }} <input class="pn" type="number" [ngModel]="s.httpPort || 80" (ngModelChange)="set({ httpPort: +$event })" /> · {{ tr('the shared folder, read only') }}</span></div>
      <div class="sv"><label class="sw"><input type="checkbox" [ngModel]="s.ftp" (ngModelChange)="set({ ftp: $event })" /><i></i></label><b>FTP</b>@if (url('ftp'); as u) { <a class="l" (click)="copy(u)">{{ tr('Copy URL') }}</a> } @else { <span></span> }
        <span class="d">{{ tr('port') }} <input class="pn" type="number" [ngModel]="s.ftpPort || 21" (ngModelChange)="set({ ftpPort: +$event })" /> · {{ tr('read and write') }}<br />{{ tr('user') }} <code>{{ s.user || 'lab' }}</code> / <code>{{ pw() || '••••' }}</code> <a (click)="showPw()">{{ pw() ? tr('Copy') : tr('Show') }}</a> · {{ tr('SSH/SFTP too') }}</span></div>
      <div class="sv"><label class="sw"><input type="checkbox" [ngModel]="s.tftp" (ngModelChange)="set({ tftp: $event })" /><i></i></label><b>TFTP</b><span></span>
        <span class="d">{{ tr('port') }} 69 · <label class="ck"><input type="checkbox" [ngModel]="s.tftpWrite" (ngModelChange)="set({ tftpWrite: $event })" /> {{ tr('uploads allowed') }}</label></span></div>
      <div class="sv"><label class="sw"><input type="checkbox" [ngModel]="s.dhcp" (ngModelChange)="set({ dhcp: $event })" /><i></i></label><b>DHCP</b><span></span>
        <span class="d">{{ tr('on port {0}, never on the management network', s.dhcpPort || 1) }}</span></div>
      @if (s.dhcp) {
        <div class="dh">
          <span>{{ tr('From') }}</span><input [ngModel]="s.from" (ngModelChange)="set({ from: $event })" />
          <span>{{ tr('To') }}</span><input [ngModel]="s.to" (ngModelChange)="set({ to: $event })" />
          <span>{{ tr('Gateway') }}</span><input [ngModel]="s.gateway" (ngModelChange)="set({ gateway: $event })" [placeholder]="tr('none')" />
          <span>DNS</span><input [ngModel]="s.dns" (ngModelChange)="set({ dns: $event })" [placeholder]="tr('none')" />
          <span>{{ tr('Lease') }}</span><span><input class="pn" type="number" [ngModel]="(s.lease || 3600) / 60" (ngModelChange)="set({ lease: $event * 60 })" /> {{ tr('min') }}</span>
          <span>{{ tr('Boot file') }}</span><select [ngModel]="s.bootFile || ''" (ngModelChange)="set({ bootFile: $event })"><option value="">{{ tr('none') }}</option>@for (f of plainFiles(); track f) { <option [value]="f">{{ f }}</option> }</select>
          <span>{{ tr('TFTP server') }}</span><input [ngModel]="s.bootFrom" (ngModelChange)="set({ bootFrom: $event })" [placeholder]="tr('this server')" />
        </div>
      }
      @if (err()) { <p class="err small">{{ err() }}</p> }

      <div class="sh"><b>{{ tr('Access') }}</b><small class="muted">{{ v?.mgmt ? tr('management network') : tr('through its host') }}</small></div>
      <div class="acc">
        @if (v?.mgmt) {
          <span>SSH / SFTP</span><span class="mono">{{ s.user || 'lab' }}&#64;{{ v!.mgmt }} <a (click)="copy('sftp://' + (s.user || 'lab') + '@' + v!.mgmt)">{{ tr('Copy') }}</a></span>
          <span>EXOS</span><span class="mono ell" [title]="exos()">{{ exos() }} <a (click)="copy(exos())">{{ tr('Copy') }}</a></span>
        } @else { <span></span><span class="muted">{{ tr('Its management address is known once it runs on the host’s network; Upload… works always.') }}</span> }
      </div>

      <div class="sh"><b>{{ tr('Shared folder') }}</b><small class="muted">/srv · {{ tr('{0} files', fileN()) }}@if (v?.status?.freeKb) { · {{ tr('{0} free', gb(v!.status.freeKb)) }} }</small></div>
      <div class="ftab">
        <div class="fr fh"><input type="checkbox" [checked]="allOn()" (change)="all($any($event.target).checked)" [disabled]="!(v?.files?.length)" /><span>{{ tr('Name') }}</span><span class="n">{{ tr('Size') }}</span></div>
        @for (f of v?.files ?? []; track f.path) {
          <label class="fr" [title]="f.path + ' · ' + when(f.mod)"><input type="checkbox" [checked]="chk().has(f.path)" (change)="tick(f.path)" /><span class="ell">{{ f.dir ? '▸ ' : '' }}{{ f.path }}</span><span class="n">{{ f.dir ? '' : size(f.size) }}</span></label>
        } @empty { <p class="muted small" style="padding: 6px 10px">{{ dev().running ? (v?.error || tr('Empty: Upload… puts files from this computer here.')) : tr('Its files show when it runs.') }}</p> }
      </div>
      <div class="fb"><button type="button" class="sm primary" (click)="upload()" [disabled]="!dev().running || busy()">{{ tr('Upload…') }}</button><button type="button" class="sm" (click)="mkdir()" [disabled]="!dev().running || busy()">{{ tr('Folder…') }}</button>
        <button type="button" class="sm" (click)="download()" [disabled]="chk().size !== 1 || busy()">{{ tr('Download') }}</button><span class="sp"></span>
        <button type="button" class="sm danger" (click)="del()" [disabled]="!chk().size || busy()">{{ tr('Delete') }}@if (chk().size) { ({{ chk().size }}) }</button></div>

      <div class="sh"><b>{{ tr('DHCP leases') }}</b><small class="muted">{{ v?.status?.leases?.length ?? 0 }}</small></div>
      <div class="ftab">
        @for (l of v?.status?.leases ?? []; track l.mac) { <div class="fr ls" [title]="l.mac + ' · ' + tr('expires in {0}', l.expires)"><span class="mono">{{ l.ip }}</span><span class="ell">{{ l.name || l.mac }}</span><span class="muted n">{{ l.expires }}</span></div> }
        @empty { <p class="muted small" style="padding: 6px 10px">{{ s.dhcp ? tr('No lease yet: a device on port {0}’s network that asks DHCP gets one.', s.dhcp ? (s.dhcpPort || 1) : 1) : tr('DHCP is off.') }}</p> }
      </div>
      @if (v?.image === 'making') { <p class="muted xs">{{ tr('Making the Server image from it: new servers will start with their packages, offline.') }}</p> }
      @else if (!s.installed) { <p class="muted xs">{{ tr('The first start installs its packages (it needs internet once); then it becomes the Server image.') }}</p> }
    </div>
  }
  `,
})
export class LabServer implements OnDestroy {
  readonly dev = input.required<LabDevice>();
  private backend = inject(Backend);
  private ui = inject(Ui);
  protected lab = inject(LabService);
  protected readonly tr = tr;
  protected view = signal<EmuServerView | null>(null);
  protected edit = signal<EmuServer | null>(null);
  protected dirty = signal(false);
  protected busy = signal(false);
  protected err = signal('');
  protected pw = signal('');
  protected chk = signal(new Set<string>());
  private timer: ReturnType<typeof setInterval> | undefined;
  private devId = computed(() => this.dev().id);
  private devRunning = computed(() => !!this.dev().running);
  constructor() {
    effect(() => { // another server: everything again (the device object itself changes with every refresh of the lab)
      this.devId();
      untracked(() => {
        const d = this.dev();
        this.view.set(null); this.dirty.set(false); this.err.set(''); this.pw.set(''); this.chk.set(new Set());
        this.edit.set(d.server ? { ...d.server } : null);
        void this.load();
      });
    });
    effect(() => { // it started: its files and leases at once
      if (this.devRunning()) untracked(() => void this.load());
    });
    this.timer = setInterval(() => { if (this.dev().running && !this.busy()) void this.load(); }, 10000);
  }
  ngOnDestroy() { clearInterval(this.timer); }
  async load() {
    const id = this.dev().id;
    try {
      const v = await this.backend.emuServerState(id);
      if (this.dev().id !== id) return;
      this.view.set(v);
      if (!this.dirty()) this.edit.set({ ...v.settings });
      const have = new Set((v.files ?? []).map(f => f.path));
      this.chk.set(new Set([...this.chk()].filter(p => have.has(p))));
    } catch { /* not a server any more */ }
  }
  set(p: Partial<EmuServer>) { const s = this.edit(); if (s) { this.edit.set({ ...s, ...p }); this.dirty.set(true); this.err.set(''); } }
  revert() { this.dirty.set(false); this.err.set(''); this.edit.set(this.view()?.settings ? { ...this.view()!.settings } : this.dev().server ? { ...this.dev().server! } : null); }
  async apply() {
    const s = this.edit(); if (!s) return;
    this.busy.set(true); this.err.set('');
    try { this.lab.emu.set(await this.backend.emuSetServer(this.dev().id, s)); this.dirty.set(false); this.ui.toast(this.dev().running ? tr('Server settings applied') : tr('Kept: they apply when it starts'), 'success'); await this.load(); }
    catch (e) { this.err.set(errText(e)); }
    finally { this.busy.set(false); }
  }
  protected plainFiles = computed(() => (this.view()?.files ?? []).filter(f => !f.dir).map(f => f.path));
  protected fileN = computed(() => (this.view()?.files ?? []).filter(f => !f.dir).length);
  protected allOn = computed(() => { const f = this.view()?.files ?? []; return f.length > 0 && f.every(x => this.chk().has(x.path)); });
  tick(p: string) { const s = new Set(this.chk()); if (s.has(p)) s.delete(p); else s.add(p); this.chk.set(s); }
  all(on: boolean) { this.chk.set(on ? new Set((this.view()?.files ?? []).map(f => f.path)) : new Set()); }
  url(kind: 'http' | 'ftp') {
    const v = this.view(), s = this.edit(); if (!v?.mgmt || !s) return '';
    if (kind === 'http') return s.http ? `http://${v.mgmt}${(s.httpPort || 80) === 80 ? '' : ':' + s.httpPort}/` : '';
    return s.ftp ? `ftp://${s.user || 'lab'}@${v.mgmt}${(s.ftpPort || 21) === 21 ? '' : ':' + s.ftpPort}/` : '';
  }
  exos() {
    const v = this.view(), s = this.edit(); const f = [...this.chk()].find(p => this.plainFiles().includes(p)) ?? this.plainFiles()[0] ?? 'file';
    return `download url http://${v?.mgmt ?? '<server>'}${(s?.httpPort || 80) === 80 ? '' : ':' + s?.httpPort}/${f} vr VR-Mgmt`;
  }
  open(u: string) { this.backend.openURL(u); }
  async copy(t: string) { await this.backend.clipboardSet(t); this.ui.toast(tr('Copied'), 'success', 1500); }
  async showPw() {
    if (this.pw()) { await this.copy(this.pw()); return; }
    try { this.pw.set(await this.backend.emuServerPassword(this.dev().id)); } catch (e) { this.ui.error(e); }
  }
  private dirOf() { const c = [...this.chk()]; const d = (this.view()?.files ?? []).find(f => f.dir && c.length === 1 && f.path === c[0]); return d?.path ?? ''; }
  async upload() {
    const files = await this.backend.chooseOpenFiles(tr('Files for the Server')).catch(() => null);
    if (!files?.length) return;
    const dir = this.dirOf();
    await this.run(async () => this.view.set(await this.backend.emuServerUpload(this.dev().id, files, dir)), tr('{0} file(s) copied to the server', files.length));
  }
  async mkdir() {
    const n = await this.ui.input(tr('New Folder'), tr('Its name in the shared folder'), ''); if (!n?.trim()) return;
    const at = this.dirOf();
    await this.run(async () => this.view.set(await this.backend.emuServerMkdir(this.dev().id, (at ? at + '/' : '') + n.trim())));
  }
  async download() {
    const p = [...this.chk()][0]; if (!p || this.view()?.files?.find(f => f.path === p)?.dir) { this.ui.toast(tr('Choose one file'), 'info'); return; }
    const to = await this.backend.chooseSaveFile(tr('Save the File'), p.split('/').pop() ?? p).catch(() => ''); if (!to) return;
    await this.run(() => this.backend.emuServerDownload(this.dev().id, p, to), tr('Saved'));
  }
  async del() {
    const list = [...this.chk()]; if (!list.length) return;
    if (!(await this.ui.confirm(tr('Delete from the Server'), tr('Delete {0} from the shared folder of {1}? It cannot be undone.', list.length === 1 ? list[0] : tr('{0} items', list.length), this.dev().hostname), tr('Delete'), true))) return;
    await this.run(async () => { this.view.set(await this.backend.emuServerDelete(this.dev().id, list)); this.chk.set(new Set()); });
  }
  private async run(fn: () => Promise<unknown>, ok = '') {
    this.busy.set(true);
    try { await fn(); if (ok) this.ui.toast(ok, 'success'); } catch (e) { this.ui.error(e); } finally { this.busy.set(false); }
  }
  size(n: number) { return n >= 2 ** 30 ? (n / 2 ** 30).toFixed(1) + ' GB' : n >= 2 ** 20 ? (n / 2 ** 20).toFixed(1) + ' MB' : n >= 1024 ? Math.round(n / 1024) + ' KB' : n + ' B'; }
  gb(kb: number) { return kb >= 1048576 ? (kb / 1048576).toFixed(1) + ' GB' : Math.round(kb / 1024) + ' MB'; }
  when(t: string) { const d = new Date(t); return isNaN(+d) ? '' : d.toLocaleString([], { day: '2-digit', month: 'short', hour: '2-digit', minute: '2-digit', hour12: false }); }
}
