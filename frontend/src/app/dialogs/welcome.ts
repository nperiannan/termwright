import { Component, computed, inject, signal } from '@angular/core';
import { Backend, errText } from '../core/backend';
import { FileFilter, ImportSource } from '../core/models';
import { Ui } from '../core/ui.service';
import { UI_SIZES, uiFontSize } from '../core/ui-fonts';
import { Icon } from '../components/icon';

/** The welcome flow shows once per version that changes it (new installs and updates). */
export const WELCOME_VERSION = '0.21';

interface FileKind { label: string; title: string; filters: FileFilter[]; folder?: boolean; }

/** First run: theme and text size, import sessions from another terminal
 *  program, done. Shown once on a new install; Help → Welcome… shows it again. */
@Component({
  selector: 'app-welcome',
  imports: [Icon],
  template: `
  <div class="modal-backdrop">
    <div class="wel" role="dialog" aria-label="Welcome to Termwright">
      <div class="top">
        <img [src]="ui.lightUi() ? 'icon.svg' : 'icon-dark.svg'" alt="" width="30" height="30" />
        <div class="dots">@for (s of titles; track $index) { <span [class.on]="$index === step()" [class.done]="$index < step()"></span> }</div>
        <button type="button" class="skip" (click)="finish()">Skip</button>
      </div>

      @switch (step()) {
        @case (0) {
          <h2>Welcome to Termwright</h2>
          <p class="sub">Make it comfortable to look at - you can change all of this later in Global Options.</p>
          <div class="lbl">Theme</div>
          <div class="themes">
            @for (t of themes; track t.v) {
              <button type="button" class="th" [class.on]="theme() === t.v" (click)="setTheme(t.v)">
                <span class="pv" [class]="'pv ' + t.v"><i></i><i></i><i></i><b></b></span>
                <span class="tn"><app-icon [name]="t.icon" [size]="14" /> {{ t.label }}</span>
              </button>
            }
          </div>
          <div class="lbl">Text size</div>
          <div class="seg">
            @for (z of sizes; track z.v) { <button type="button" [class.on]="size() === z.v" (click)="setSize(z.v)"><span [style.font-size.px]="z.v + 2">Aa</span><small>{{ z.v }} px</small></button> }
          </div>
          <div class="lbl">Layout</div>
          <div class="seg wide">
            <button type="button" [class.on]="!classic()" (click)="ui.patchSettings({ layout: '' })"><b>Modern</b><small>tabs and search in the title bar</small></button>
            <button type="button" [class.on]="classic()" (click)="ui.patchSettings({ layout: 'classic' })"><b>Classic</b><small>toolbar, tab strip and menu bar</small></button>
          </div>
        }
        @case (1) {
          <h2>Bring your sessions</h2>
          <p class="sub">Import the sessions, folders and saved passwords of the terminal program you use today.
            @if (have()) { You already have {{ have() }} sessions - import more, or continue. }</p>
          @if (sources() === null) {
            <p class="muted">Looking for other terminal programs on this computer…</p>
          } @else if (sources()!.length) {
            <div class="lbl">Found on this computer</div>
            <div class="found">
              @for (s of sources(); track s.path) {
                <div class="src">
                  <span class="app">{{ s.app }}</span>
                  <span class="path" [title]="s.path">{{ s.what }} · {{ s.path }}</span>
                  @if (done()[s.path]; as d) {
                    <span class="ok"><app-icon name="check" [size]="14" /> {{ d }}</span>
                  } @else {
                    <button type="button" class="primary" [disabled]="busy()" (click)="importPath(s.path, s.app)">Import</button>
                  }
                </div>
              }
            </div>
          } @else {
            <p class="muted">No sessions of SecureCRT, PuTTY, MobaXterm or OpenSSH found in their usual places - import an export file instead.</p>
          }
          <div class="lbl">From an export file</div>
          <div class="chips">
            @for (k of kinds; track k.label) { <button type="button" [disabled]="busy()" (click)="importFile(k)">{{ k.label }}</button> }
            <button type="button" class="link" (click)="moreImport()">More (Ansible, NetBox, AWS, passphrase)…</button>
          </div>
          @if (busy()) { <p class="muted">Importing…</p> }
          @if (error()) { <p class="err">{{ error() }}</p> }
        }
        @case (2) {
          <div class="ready"><app-icon name="check" [size]="26" /></div>
          <h2>You're ready</h2>
          <p class="sub">{{ imported() ? imported() + ' session' + (imported() === 1 ? '' : 's') + ' imported.' : 'Add a session, or just type an address to connect.' }}</p>
          <ul class="tips">
            <li><kbd>{{ keys }}</kbd> or the search box: find a session, a command or an action - or type <span class="mono">ssh admin&#64;10.1.1.1</span> and press Enter</li>
            <li>Right-click a tab or a session for everything you can do with it</li>
            <li>Discover Devices scans a network and adds what answers as sessions</li>
          </ul>
        }
      }

      <div class="foot">
        @if (step() > 0) { <button type="button" (click)="step.set(step() - 1)">Back</button> }
        <span class="grow"></span>
        @if (step() === 2) {
          <button type="button" (click)="finish('discover')">Discover Devices</button>
          <button type="button" class="primary" (click)="finish()">Start Using Termwright</button>
        } @else {
          <button type="button" class="primary" (click)="next()">{{ step() === 1 && !imported() ? 'Not Now' : 'Continue' }}</button>
        }
      </div>
    </div>
  </div>`,
  styles: [`
    .wel { width: 600px; max-width: calc(100vw - 32px); max-height: calc(100vh - 40px); overflow: auto; padding: 22px 28px 20px; border-radius: var(--r-lg); background: var(--surface);
      border: 1px solid var(--line); box-shadow: var(--shadow); display: flex; flex-direction: column; }
    .top { display: flex; align-items: center; gap: 12px; margin-bottom: 14px;
      .dots { flex: 1; display: flex; justify-content: center; gap: 6px; span { width: 24px; height: 4px; border-radius: 2px; background: var(--line); }
        .on { background: var(--accent); } .done { background: color-mix(in srgb, var(--accent) 45%, var(--line)); } }
      .skip { border: 0; background: transparent; color: var(--muted); height: auto; padding: 2px 4px; &:hover { color: var(--text); background: transparent; } } }
    h2 { margin: 4px 0 4px; font-size: calc(21px * var(--ui-scale, 1)); font-weight: 600; }
    .sub { margin: 0 0 14px; color: var(--muted); line-height: 1.5; }
    .lbl { margin: 14px 0 6px; font-size: calc(11px * var(--ui-scale, 1)); font-weight: 600; letter-spacing: .06em; text-transform: uppercase; color: var(--faint); }
    .themes { display: grid; grid-template-columns: repeat(3, 1fr); gap: 10px; }
    .th { height: auto; padding: 8px; display: flex; flex-direction: column; gap: 8px; border: 1px solid var(--line); border-radius: var(--r-lg); background: var(--surface-2);
      &.on { border-color: var(--accent); box-shadow: 0 0 0 3px var(--accent-soft); }
      .tn { display: flex; align-items: center; justify-content: center; gap: 6px; font-weight: 500; } }
    .pv { position: relative; display: block; height: 62px; border-radius: 7px; overflow: hidden; border: 1px solid rgba(127, 127, 127, .25);
      i { position: absolute; left: 0; right: 0; top: 0; height: 12px; } i:nth-child(2) { top: 12px; bottom: 0; width: 30%; height: auto; } i:nth-child(3) { top: 12px; bottom: 0; left: 30%; height: auto; }
      b { position: absolute; left: 36%; top: 22px; width: 40%; height: 4px; border-radius: 2px; box-shadow: 0 9px 0, 0 18px 0; } }
    .pv.light { i:nth-child(1) { background: #fff; } i:nth-child(2) { background: #f3f4f7; } i:nth-child(3) { background: #fbfbfc; } b { background: #2f9e5b; color: #c6cbd3; } }
    .pv.dark { i:nth-child(1) { background: #222327; } i:nth-child(2) { background: #26272b; } i:nth-child(3) { background: #1b1c1f; } b { background: #43c46b; color: #4a4c54; } }
    .pv.system { background: linear-gradient(135deg, #fbfbfc 50%, #1b1c1f 50%); i { display: none; } b { background: #4f9cf7; color: #8b8e96; } }
    .seg { display: flex; gap: 6px;
      button { flex: 1; height: 54px; display: flex; flex-direction: column; align-items: center; justify-content: center; gap: 2px; line-height: 1.1;
        border: 1px solid var(--line); border-radius: var(--r); background: var(--surface-2);
        small { font-size: 11px; color: var(--muted); font-weight: 400; }
        &.on { border-color: var(--accent); box-shadow: 0 0 0 3px var(--accent-soft); } } }
    .seg.wide button { height: 52px; font-size: calc(13px * var(--ui-scale, 1)); }
    .found { display: flex; flex-direction: column; gap: 6px; }
    .src { display: grid; grid-template-columns: 110px 1fr auto; align-items: center; gap: 10px; padding: 10px 12px; border: 1px solid var(--line); border-radius: var(--r); background: var(--surface-2);
      .app { font-weight: 600; } .path { color: var(--muted); font-size: calc(12px * var(--ui-scale, 1)); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
      .ok { display: inline-flex; align-items: center; gap: 4px; color: var(--ok); font-weight: 500; white-space: nowrap; } }
    .chips { display: flex; flex-wrap: wrap; gap: 6px; align-items: center; button { border-radius: 999px; padding: 0 12px; } }
    .link { border: 0 !important; background: transparent !important; color: var(--accent); &:hover { text-decoration: underline; } }
    .muted { color: var(--muted); } .err { color: var(--danger); }
    .ready { width: 52px; height: 52px; border-radius: 50%; display: flex; align-items: center; justify-content: center; background: rgba(67, 196, 107, .15); color: var(--ok); margin-bottom: 8px; }
    .tips { margin: 4px 0 0; padding-left: 18px; line-height: 1.7; color: var(--text);
      kbd { font: 11px var(--ui-font); border: 1px solid var(--line); border-radius: 4px; padding: 0 5px; color: var(--muted); } .mono { font-family: var(--mono); } }
    .foot { display: flex; gap: 8px; margin-top: 22px; .grow { flex: 1; } }
  `],
})
export class Welcome {
  protected ui = inject(Ui);
  private backend = inject(Backend);
  readonly titles = ['Look', 'Sessions', 'Done'];
  readonly step = signal(0);
  readonly busy = signal(false);
  readonly error = signal('');
  readonly sources = signal<ImportSource[] | null>(null);
  /** Source path → "148 sessions imported". */
  readonly done = signal<Record<string, string>>({});
  readonly imported = signal(0);
  /** Sessions already in the Session Manager. */
  readonly have = computed(() => {
    let n = 0;
    const walk = (x: { isFolder: boolean; children?: unknown[] | null }) => (x.children as typeof x[] ?? []).forEach(c => (c.isFolder ? walk(c) : n++));
    const r = this.ui.tree();
    if (r) walk(r);
    return n;
  });
  readonly keys = navigator.platform.toLowerCase().includes('mac') || navigator.userAgent.includes('Mac OS') ? '⌘G' : 'Ctrl+Shift+G';

  readonly themes = [
    { v: 'light', label: 'Light', icon: 'sun' },
    { v: 'dark', label: 'Dark', icon: 'moon' },
    { v: 'system', label: 'System', icon: 'theme-system' },
  ];
  readonly sizes = UI_SIZES;
  readonly theme = computed(() => { const t = this.ui.settings()?.theme; return t === 'light' || t === 'dark' ? t : 'system'; });
  readonly size = computed(() => uiFontSize(this.ui.settings()?.uiFontSize));
  readonly classic = computed(() => this.ui.settings()?.layout === 'classic');

  readonly kinds: FileKind[] = [
    { label: 'SecureCRT (.xml, .zip)', title: 'SecureCRT Export', filters: [{ displayName: 'SecureCRT export (*.xml, *.zip)', pattern: '*.xml;*.zip' }] },
    { label: 'SecureCRT Config folder', title: 'Select the SecureCRT Config Folder', filters: [], folder: true },
    { label: 'PuTTY / KiTTY (.reg)', title: 'PuTTY or KiTTY Registry Export', filters: [{ displayName: 'Registry export (*.reg)', pattern: '*.reg' }] },
    { label: 'SuperPuTTY (Sessions.xml)', title: 'SuperPuTTY Sessions.xml', filters: [{ displayName: 'Sessions.xml', pattern: '*.xml' }] },
    { label: 'MobaXterm (.mxtsessions)', title: 'MobaXterm Sessions', filters: [{ displayName: 'MobaXterm sessions', pattern: '*.mxtsessions;*.ini' }] },
    { label: 'CSV', title: 'CSV File', filters: [{ displayName: 'CSV', pattern: '*.csv;*.tsv;*.txt' }] },
  ];

  setTheme(v: string) { this.ui.patchSettings({ theme: v }); }
  setSize(v: number) { this.ui.patchSettings({ uiFontSize: v }); }

  next() {
    const n = this.step() + 1;
    this.step.set(n);
    if (n === 1 && this.sources() === null) {
      this.backend.findImportSources().then(l => this.sources.set(l ?? [])).catch(() => this.sources.set([]));
    }
  }

  async importPath(path: string, key = path) {
    this.busy.set(true);
    this.error.set('');
    try {
      const r = await this.backend.importSessions(path, { targetFolder: '', conflict: 'rename', includeDefault: false, configPassphrase: '' });
      const n = (r.imported?.length ?? 0) + (r.renamed?.length ?? 0);
      this.imported.update(x => x + n);
      this.done.update(d => ({ ...d, [key === path ? path : path]: `${n} session${n === 1 ? '' : 's'} imported` + (r.passwordsImported ? `, ${r.passwordsImported} password${r.passwordsImported === 1 ? '' : 's'}` : '') }));
      if (r.errors?.length) this.error.set(r.errors.slice(0, 3).join(' · '));
      await this.ui.refreshTree();
    } catch (e) { this.error.set(errText(e)); }
    this.busy.set(false);
  }

  async importFile(k: FileKind) {
    const p = k.folder
      ? await this.backend.chooseDirectory(k.title).catch(() => '')
      : await this.backend.chooseOpenFile(k.title, k.filters).catch(() => '');
    if (!p) return;
    await this.importPath(p);
    const d = this.done()[p];
    if (d && !this.error()) this.ui.toast(d, 'success');
  }

  moreImport() { this.finish('import'); }

  finish(then?: 'discover' | 'import') {
    this.ui.patchSettings({ welcomeSeen: WELCOME_VERSION });
    this.ui.close();
    if (then) this.ui.open(then);
  }
}
