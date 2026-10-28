import { Component, computed, inject, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { Backend, errText } from '../core/backend';
import { ENVIRONMENTS } from '../core/env';
import { PROTOCOLS, Session, SessionNode, defaultPort } from '../core/models';
import { SCHEME_NAMES } from '../core/themes';
import { Ui } from '../core/ui.service';
import { Icon } from '../components/icon';

/** Fields the dialog can change; a field is applied only when its box is ticked. */
interface Edit {
  folder: string; deviceType: string; ownLogin: boolean; username: string; password: string; noPassword: boolean;
  protocol: string; port: number; jumpSession: string; environment: string; colorScheme: string; keywordSet: string; autoReconnect: boolean;
}
type Key = 'folder' | 'deviceType' | 'username' | 'password' | 'protocol' | 'port' | 'jumpSession' | 'environment' | 'colorScheme' | 'keywordSet' | 'autoReconnect';

/** Edit Sessions: changes the same settings in many sessions at once. */
@Component({
  selector: 'app-bulk-edit',
  imports: [FormsModule, Icon],
  template: `
  <div class="modal-backdrop">
    <div class="modal bulk">
      <div class="modal-head"><span class="mh-ic"><app-icon name="bulk" [size]="17" /></span>
        <span class="mh-t">Edit {{ paths().length }} Sessions<span class="mh-s">Change only what you tick - everything else stays as it is in each session</span></span></div>
      <div class="modal-body">
        <p class="hint"><span class="cl-only">Tick what to change - everything else stays as it is in each session.</span>
          <a (click)="showList = !showList">{{ showList ? 'Hide' : 'Show' }} the sessions</a></p>
        @if (showList) { <div class="list mono">@for (p of paths(); track p) { <div>{{ p }}</div> }</div> }
        <div class="grid3">
          <input type="checkbox" [(ngModel)]="on.folder" /><label>Move to folder</label>
          <input [(ngModel)]="e.folder" list="be-folders" placeholder="(top level)" (focus)="on.folder = true" />
          <datalist id="be-folders">@for (f of folders(); track f) { <option [value]="f"></option> }</datalist>

          <input type="checkbox" [(ngModel)]="on.deviceType" /><label>Device type</label>
          <div class="row">
            <select [(ngModel)]="e.deviceType" (focus)="on.deviceType = true">
              <option value="">(none)</option>
              @for (d of deviceTypes(); track d) { <option [value]="d">{{ d }}</option> }
            </select>
            @if (e.deviceType) { <label class="inline"><input type="checkbox" [ngModel]="!e.ownLogin" (ngModelChange)="e.ownLogin = !$event" /> Use the device type's username and password</label> }
          </div>

          <input type="checkbox" [(ngModel)]="on.username" /><label>Username</label>
          <input [(ngModel)]="e.username" (focus)="on.username = true" autocomplete="off" />

          <input type="checkbox" [(ngModel)]="on.password" /><label>Password</label>
          <div class="row">
            <input type="password" [(ngModel)]="e.password" [disabled]="e.noPassword" (focus)="on.password = true" autocomplete="new-password"
              placeholder="saved in the keychain for each session" />
            <label class="inline"><input type="checkbox" [(ngModel)]="e.noPassword" (ngModelChange)="on.password = true" /> No password</label>
          </div>

          <input type="checkbox" [(ngModel)]="on.protocol" /><label>Protocol</label>
          <select [(ngModel)]="e.protocol" (ngModelChange)="on.protocol = true; e.port = defPort(e.protocol)">
            @for (p of protocols; track p) { <option [value]="p">{{ p }}</option> }
          </select>

          <input type="checkbox" [(ngModel)]="on.port" /><label>Port</label>
          <input type="number" class="port" [(ngModel)]="e.port" (focus)="on.port = true" min="1" max="65535" />

          <input type="checkbox" [(ngModel)]="on.jumpSession" /><label>Jump host</label>
          <select [(ngModel)]="e.jumpSession" (focus)="on.jumpSession = true">
            <option value="">(none - connect directly)</option>
            @for (j of jumps(); track j) { <option [value]="j">{{ j }}</option> }
          </select>

          <input type="checkbox" [(ngModel)]="on.environment" /><label>Environment</label>
          <select [(ngModel)]="e.environment" (focus)="on.environment = true">
            <option value="">(none / folder default)</option>
            @for (v of envs; track v.id) { <option [value]="v.id">{{ v.label }}</option> }
          </select>

          <input type="checkbox" [(ngModel)]="on.colorScheme" /><label>Color scheme</label>
          <select [(ngModel)]="e.colorScheme" (focus)="on.colorScheme = true">
            <option value="">(global default)</option>
            @for (c of schemes(); track c) { <option [value]="c">{{ c }}</option> }
          </select>

          <input type="checkbox" [(ngModel)]="on.keywordSet" /><label>Keyword set</label>
          <select [(ngModel)]="e.keywordSet" (focus)="on.keywordSet = true">
            <option value="">(global default)</option>
            @for (k of keywordSets(); track k) { <option [value]="k">{{ k }}</option> }
          </select>

          <input type="checkbox" [(ngModel)]="on.autoReconnect" /><label>Auto reconnect</label>
          <label class="inline"><input type="checkbox" [(ngModel)]="e.autoReconnect" (ngModelChange)="on.autoReconnect = true" /> Reconnect when the connection drops</label>
        </div>
        @if (err()) { <p class="err">{{ err() }}</p> }
      </div>
      <div class="modal-foot">
        <span class="hint">{{ count() ? count() + ' setting' + (count() === 1 ? '' : 's') + ' to change' : '' }}</span>
        <span class="spacer"></span>
        <button type="button" (click)="ui.close()">Cancel</button>
        <button type="button" class="primary" [disabled]="!count() || busy()" (click)="apply()">{{ busy() ? 'Saving…' : 'Apply to ' + paths().length + ' Sessions' }}</button>
      </div>
    </div>
  </div>`,
  styles: [`
    .bulk { width: min(680px, 95vw); }
    .grid3 { display: grid; grid-template-columns: 18px 130px 1fr; gap: 8px 8px; align-items: center; }
    .row { display: flex; gap: 10px; align-items: center; flex-wrap: wrap; }
    .inline { display: inline-flex; gap: 6px; align-items: center; white-space: nowrap; }
    .port { width: 110px; }
    .list { max-height: 120px; overflow: auto; font-size: calc(12px * var(--ui-scale, 1)); border: 1px solid var(--border); border-radius: 6px; padding: 6px 8px; margin-bottom: 10px; }
    .hint a { color: var(--accent); cursor: pointer; }
    .err { color: var(--danger); white-space: pre-wrap; }
    .spacer { flex: 1; }
    :host-context(.modern) {
      .grid3 { grid-template-columns: 34px 120px 1fr; gap: 0 10px; border: 1px solid var(--line); border-radius: var(--r-lg); padding: 4px 14px; background: var(--surface);
        > * { min-height: 40px; display: flex; align-items: center; } > datalist { display: none; } > input:not([type=checkbox]), > .row > input, > .row > select, > select { min-height: 30px; }
        > input[type=checkbox] { appearance: none; -webkit-appearance: none; position: relative; width: 30px; height: 18px; min-height: 0; margin: 11px 0; border-radius: 9px;
          background: var(--line); cursor: pointer; transition: background .15s;
          &::before { content: ''; position: absolute; left: 2px; top: 2px; width: 14px; height: 14px; border-radius: 50%; background: #fff; box-shadow: 0 1px 2px rgba(0, 0, 0, .3); transition: left .15s; }
          &:checked { background: var(--accent); &::before { left: 14px; } } }
        > input[type=checkbox]:not(:checked) + label { color: var(--muted); }
        > input[type=checkbox]:not(:checked) + label + * { opacity: .5; } }
      .list { border-color: var(--line); border-radius: var(--r-lg); }
    }
  `],
})
export class BulkEdit {
  protected ui = inject(Ui);
  private backend = inject(Backend);
  readonly paths = this.ui.bulkPaths;
  readonly protocols = PROTOCOLS;
  readonly envs = ENVIRONMENTS;
  showList = false;
  readonly busy = signal(false);
  readonly err = signal('');
  e: Edit = { folder: '', deviceType: '', ownLogin: false, username: '', password: '', noPassword: false, protocol: 'SSH2', port: 22,
    jumpSession: '', environment: '', colorScheme: '', keywordSet: '', autoReconnect: false };
  on: Record<Key, boolean> = { folder: false, deviceType: false, username: false, password: false, protocol: false, port: false,
    jumpSession: false, environment: false, colorScheme: false, keywordSet: false, autoReconnect: false };

  readonly deviceTypes = computed(() => (this.ui.settings()?.deviceTypes ?? []).map(d => d.name));
  readonly keywordSets = computed(() => (this.ui.settings()?.keywordSets ?? []).map(k => k.name));
  readonly schemes = computed(() => [...SCHEME_NAMES, ...(this.ui.settings()?.customSchemes ?? []).map(c => c.name)]);
  readonly folders = computed(() => {
    const out: string[] = [];
    const walk = (n: SessionNode | null | undefined) => n?.children?.forEach(c => { if (c.isFolder) { out.push(c.path); walk(c); } });
    walk(this.ui.tree());
    return out;
  });
  readonly jumps = computed(() => {
    const out: string[] = [];
    const sel = new Set(this.paths());
    const walk = (n: SessionNode | null | undefined) => n?.children?.forEach(c => { if (c.isFolder) walk(c); else if (c.protocol === 'SSH2' && !sel.has(c.path)) out.push(c.path); });
    walk(this.ui.tree());
    return out;
  });

  constructor() {
    // start from the first session's values so the fields show something sensible
    const first = this.paths()[0];
    if (first) this.backend.getSession(first).then(s => {
      this.e = { ...this.e, folder: s.folder, deviceType: s.deviceType, ownLogin: s.ownLogin, username: s.username, protocol: s.protocol,
        port: s.port || defaultPort(s.protocol), jumpSession: s.jumpSession, environment: s.environment ?? '', colorScheme: s.colorScheme,
        keywordSet: s.keywordSet, autoReconnect: s.autoReconnect };
    }).catch(() => {});
  }

  defPort(p: string) { return defaultPort(p) || this.e.port; }

  count() { return Object.values(this.on).filter(Boolean).length; }

  private change(s: Session) {
    const e = this.e, on = this.on;
    if (on.folder) s.folder = e.folder.trim().replace(/^\/+|\/+$/g, '');
    if (on.deviceType) { s.deviceType = e.deviceType; s.ownLogin = !!e.deviceType && e.ownLogin; }
    if (on.username) s.username = e.username.trim();
    if (on.protocol) s.protocol = e.protocol;
    if (on.port) s.port = Number(e.port) || defaultPort(s.protocol);
    else if (on.protocol) s.port = defaultPort(s.protocol) || s.port;
    if (on.jumpSession) s.jumpSession = e.jumpSession;
    if (on.environment) s.environment = e.environment;
    if (on.colorScheme) s.colorScheme = e.colorScheme;
    if (on.keywordSet) s.keywordSet = e.keywordSet;
    if (on.autoReconnect) s.autoReconnect = e.autoReconnect;
    if (on.password) s.savePassword = true;
  }

  async apply() {
    this.busy.set(true);
    this.err.set('');
    const failed: string[] = [];
    let done = 0;
    const folder = this.e.folder.trim().replace(/^\/+|\/+$/g, '');
    if (this.on.folder && folder && !this.folders().includes(folder)) await this.backend.createFolder(folder).catch(() => {});
    for (const p of this.paths()) {
      try {
        const s = await this.backend.getSession(p);
        this.change(s);
        const saved = await this.backend.saveSession(s, p);
        if (this.on.password) await this.backend.setSessionPassword(saved.path, this.e.noPassword ? '' : this.e.password, this.e.noPassword);
        done++;
      } catch (e) { failed.push(`${p}: ${errText(e)}`); }
    }
    this.busy.set(false);
    this.ui.selectedPaths.set(new Set());
    await this.ui.refreshTree();
    if (failed.length) { this.err.set('Not changed:\n' + failed.join('\n')); return; }
    this.ui.toast(`Changed ${done} session${done === 1 ? '' : 's'}`, 'success');
    this.ui.close();
  }
}
