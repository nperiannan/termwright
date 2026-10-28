import { Component, computed, inject, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { Backend, errText } from '../core/backend';
import { CONSOLE_METHODS, SessionNode } from '../core/models';
import { Ui } from '../core/ui.service';
import { Icon } from '../components/icon';
import { FolderPicker } from '../components/folder-picker';

interface Port { port: number; name: string; exists: boolean; }

/** Console Server Ports: creates one Console session per port (line) of a console server. */
@Component({
  selector: 'app-console-ports',
  imports: [FormsModule, Icon, FolderPicker],
  template: `
  <div class="modal-backdrop">
    <div class="modal cp">
      <div class="modal-head"><span class="mh-ic"><app-icon name="serial" [size]="17" /></span>
        <span class="mh-t">Console Server Ports<span class="mh-s">One session per port of {{ server() }} - each logs in through it</span></span></div>
      <div class="modal-body">
        <p class="hint cl-only">Creates a session for each port of <b>{{ server() }}</b>{{ host() ? ' (' + host() + ')' : '' }}. Each one connects through
          the console server's session (address, jump host, SSH login) and then logs in to the device cabled to that port.</p>
        <div class="grid">
          <label>Access</label>
          <select [ngModel]="method()" (ngModelChange)="method.set($event)">
            @for (m of methods; track m.id) { <option [value]="m.id">{{ m.label }}</option> }
          </select>
          @if (method() === 'custom') {
            <label>Custom</label>
            <div class="row">
              <select [ngModel]="cproto()" (ngModelChange)="cproto.set($event)"><option value="SSH2">SSH2</option><option value="Telnet">Telnet</option></select>
              <span>TCP port</span><input type="number" class="short" [ngModel]="cbase()" (ngModelChange)="cbase.set(+$event || 0)" /><span>+ port</span>
              @if (cproto() !== 'Telnet') { <span>user</span><input [ngModel]="cuser()" (ngModelChange)="cuser.set($event)" placeholder="{user}:{port}" /> }
            </div>
          }
          <label>Ports</label>
          <div>
            <textarea rows="6" spellcheck="false" [ngModel]="spec()" (ngModelChange)="spec.set($event)"
              placeholder="1-16&#10;or one port per line with the device's name:&#10;5 core-sw1&#10;6 core-sw2"></textarea>
            <div class="hint">Ranges and lists (1-16, 20, 24) or "port name" per line.</div>
          </div>
          <div class="pair">
            <div class="f"><label>Names</label>
              <input [ngModel]="pattern()" (ngModelChange)="pattern.set($event)" title="{n} = port number, {nn} = two digits, {server} = the console server's name" /></div>
            <div class="f"><label>Device type</label>
              <select [ngModel]="deviceType()" (ngModelChange)="deviceType.set($event)">
                <option value="">(none)</option>
                @for (d of deviceTypes(); track d) { <option [value]="d">{{ d }}</option> }
              </select></div>
          </div>
          <label>Folder</label>
          @if (modern()) { <app-folder-picker [value]="folder()" (valueChange)="folder.set($event)" /> }
          @else {
          <input [ngModel]="folder()" (ngModelChange)="folder.set($event)" list="cp-folders" placeholder="(top level)" />
          <datalist id="cp-folders">@for (f of folders(); track f) { <option [value]="f"></option> }</datalist>
          }
        </div>
        <div class="prev">
          <table class="list">
            <thead><tr><th>Port</th><th>Session</th><th>Connects with</th></tr></thead>
            <tbody>
              @for (p of ports(); track p.port) {
                <tr [class.dim]="p.exists"><td>{{ p.port }}</td><td>{{ p.name }}{{ p.exists ? ' (exists, skipped)' : '' }}</td><td class="mono">{{ how(p.port) }}</td></tr>
              } @empty { <tr><td colspan="3" class="hint">Enter the ports above.</td></tr> }
            </tbody>
          </table>
        </div>
        @if (err()) { <p class="err">{{ err() }}</p> }
      </div>
      <div class="modal-foot">
        <span class="hint">{{ todo().length }} session{{ todo().length === 1 ? '' : 's' }} to create</span>
        <span class="spacer"></span>
        <button type="button" (click)="ui.close()">Cancel</button>
        <button type="button" class="primary" [disabled]="!todo().length || busy()" (click)="create()">{{ busy() ? 'Creating…' : 'Create Sessions' }}</button>
      </div>
    </div>
  </div>`,
  styles: [`
    :host-context(.modern) { .prev { border: 0 !important; margin-top: 14px; } textarea { font-family: var(--mono); } }
    .cp { width: min(720px, 95vw); }
    .grid { display: grid; grid-template-columns: 100px 1fr; gap: 8px 10px; align-items: center; }
    .row { display: flex; gap: 8px; align-items: center; flex-wrap: wrap; }
    .short { width: 80px; }
    textarea { width: 100%; font-family: var(--mono); font-size: calc(12.5px * var(--ui-scale, 1)); }
    .prev { margin-top: 12px; max-height: 30vh; overflow: auto; border: 1px solid var(--border); border-radius: 6px; }
    table { width: 100%; border-collapse: collapse; font-size: calc(12.5px * var(--ui-scale, 1)); }
    th, td { text-align: left; padding: 4px 8px; border-bottom: 1px solid var(--border); }
    th { position: sticky; top: 0; background: var(--panel); color: var(--muted); font-weight: 600; }
    tr.dim td { color: var(--muted); }
    .mono { font-family: var(--mono); }
    .err { color: var(--danger); white-space: pre-wrap; }
    .spacer { flex: 1; }
  `],
})
export class ConsolePorts {
  protected ui = inject(Ui);
  private backend = inject(Backend);
  readonly methods = CONSOLE_METHODS;
  readonly server = this.ui.consoleServerFor;
  readonly modern = () => this.ui.settings()?.layout !== 'classic';
  readonly node = computed<SessionNode | null>(() => this.ui.findNode(this.server()));
  readonly host = computed(() => this.node()?.hostname ?? '');
  readonly method = signal('opengear-ssh');
  readonly cproto = signal('SSH2');
  readonly cbase = signal(0);
  readonly cuser = signal('{user}:{port}');
  readonly spec = signal('1-16');
  readonly pattern = signal((this.server().split('/').pop() || 'console') + ' port {nn}');
  readonly folder = signal(this.server().includes('/') ? this.server().slice(0, this.server().lastIndexOf('/')) : '');
  readonly deviceType = signal('');
  readonly busy = signal(false);
  readonly err = signal('');

  readonly deviceTypes = computed(() => (this.ui.settings()?.deviceTypes ?? []).map(d => d.name));
  readonly folders = computed(() => {
    const out: string[] = [];
    const walk = (n: SessionNode | null | undefined) => n?.children?.forEach(c => { if (c.isFolder) { out.push(c.path); walk(c); } });
    walk(this.ui.tree());
    return out;
  });

  readonly ports = computed<Port[]>(() => {
    const out = new Map<number, string>();
    const name = (n: number) => this.pattern()
      .replaceAll('{nn}', String(n).padStart(2, '0')).replaceAll('{n}', String(n))
      .replaceAll('{server}', this.server().split('/').pop() ?? '').replace(/[\\/]/g, '-').trim();
    for (const line of this.spec().split('\n')) {
      const named = line.trim().match(/^(\d{1,4})\s+(\S.*)$/);
      if (named) { out.set(+named[1], named[2].trim().replace(/[\\/]/g, '-')); continue; }
      for (const part of line.split(/[,\s]+/)) {
        const r = part.match(/^(\d{1,4})(?:-(\d{1,4}))?$/);
        if (!r) continue;
        const a = +r[1], b = r[2] ? +r[2] : a;
        for (let n = Math.min(a, b); n <= Math.max(a, b) && n - Math.min(a, b) < 512; n++) if (n > 0 && !out.has(n)) out.set(n, name(n));
      }
    }
    const folder = this.cleanFolder();
    return [...out.entries()].sort((x, y) => x[0] - y[0])
      .map(([port, nm]) => ({ port, name: nm, exists: !!this.ui.findNode(folder ? folder + '/' + nm : nm) }));
  });
  readonly todo = computed(() => this.ports().filter(p => !p.exists && p.name));

  private cleanFolder() { return this.folder().trim().replace(/^\/+|\/+$/g, ''); }

  how(n: number) {
    const host = this.host() || '?', user = this.node()?.username || 'user', nn = String(n).padStart(2, '0');
    switch (this.method()) {
      case 'opengear-ssh': return `ssh ${user}:port${nn}@${host}`;
      case 'avocent-ssh': return `ssh ${user}:ttyS${n}@${host}`;
      case 'ssh-3000': return `ssh -p ${3000 + n} ${user}@${host}`;
      case 'telnet-2000': return `telnet ${host} ${2000 + n}`;
      case 'telnet-7000': return `telnet ${host} ${7000 + n}`;
      default: {
        const port = (this.cbase() || this.node()?.port || (this.cproto() === 'Telnet' ? 23 : 22)) + (this.cbase() ? n : 0);
        if (this.cproto() === 'Telnet') return `telnet ${host} ${port}`;
        const u = (this.cuser() || '{user}').replaceAll('{user}', user).replaceAll('{port}', String(n)).replaceAll('{nn}', nn);
        return `ssh -p ${port} ${u}@${host}`;
      }
    }
  }

  async create() {
    this.busy.set(true);
    this.err.set('');
    const folder = this.cleanFolder();
    const failed: string[] = [];
    let done = 0;
    try {
      if (folder && !this.folders().includes(folder)) await this.backend.createFolder(folder).catch(() => {});
      const base = await this.backend.newSessionDefaults();
      for (const p of this.todo()) {
        try {
          const s = { ...base, path: '', name: p.name, folder, protocol: 'Console', hostname: '', port: 0, initialCr: true,
            consoleServer: this.server(), consolePort: p.port, consoleMethod: this.method(), deviceType: this.deviceType(),
            ...(this.method() === 'custom' ? { consoleProtocol: this.cproto(), consoleBase: this.cbase(), consoleUser: this.cuser() } : {}) };
          await this.backend.saveSession(s, '');
          done++;
        } catch (e) { failed.push(`${p.name}: ${errText(e)}`); }
      }
    } catch (e) { failed.push(errText(e)); }
    this.busy.set(false);
    await this.ui.refreshTree();
    if (failed.length) { this.err.set('Not created:\n' + failed.join('\n')); return; }
    this.ui.toast(`Created ${done} console session${done === 1 ? '' : 's'}`, 'success');
    this.ui.close();
  }
}
