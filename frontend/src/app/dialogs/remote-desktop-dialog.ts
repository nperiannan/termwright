import { Component, ElementRef, OnInit, computed, effect, inject, signal, viewChild } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { Backend, errText } from '../core/backend';
import { RemoteDesktopRun } from '../core/models';
import { RDP_PORT, RemoteDesktopSetup, VNC_DISPLAY, VNC_PORT, remoteSessionName } from '../core/remote-desktop-setup.service';
import { reportLines } from '../core/remote-desktop-progress';
import { Tabs } from '../core/tabs.service';
import { Ui } from '../core/ui.service';
import { Icon } from '../components/icon';

/** Systems the setup script supports (ID and VERSION_ID of /etc/os-release). */
const SUPPORTED_OS: [string, string][] = [['ubuntu', '22.04'], ['ubuntu', '24.04'], ['debian', '12']];

/**
 * Set Up Remote Desktop: installs XFCE with xrdp (RDP) and/or TigerVNC on the
 * Linux machine of an SSH tab, each for its own new user (name-rdp, name-vnc),
 * by running a script in the background. The dialog shows one line of progress.
 */
@Component({
  selector: 'app-remote-desktop-dialog',
  imports: [FormsModule, Icon],
  template: `
  <div class="modal-backdrop">
    <form class="modal" (ngSubmit)="go(false)" autocomplete="off">
      <div class="modal-head"><span class="mh-ic"><app-icon name="monitor" [size]="17" /></span>
        <span class="mh-t">Set Up Remote Desktop<span class="mh-s">on {{ tab()?.title }} · installs XFCE and the remote desktop services</span></span></div>

      @if (setup.run(); as r) {
        <div class="modal-body">
          <div class="prog" [class.bad]="r.phase === 'failed'" [class.ok]="r.phase === 'done'">
            <span class="pi">
              @switch (r.phase) {
                @case ('running') { <span class="spin"></span> }
                @case ('done') { <app-icon name="check" [size]="18" /> }
                @default { <app-icon name="alert" [size]="18" /> }
              }
            </span>
            <span class="pt"><b>{{ r.step }}</b>@if (r.detail && r.phase === 'running') { <small>{{ r.detail }}</small> }</span>
          </div>
          @if (r.phase === 'running') { <div class="bar"><i [style.width.%]="r.progress"></i></div> }
          @if (r.error) { <div class="warn">{{ r.error }}</div> }
          @for (w of r.warnings; track $index) { <div class="warn soft">{{ w }}</div> }

          @if (r.phase === 'done' && r.check) {
            <pre class="rep">{{ report(r.log) }}</pre>
          }
          @if (r.phase === 'done' && !r.check) {
            <div class="hint">
              @if (r.made.rdp || r.made.vnc) { The sessions {{ madeNames(r.made) }} are in the Session Manager next to this one. }
              @else { Connect to 127.0.0.1 through the SSH session as the jump host: @if (r.ready?.rdp !== false) { port {{ rdpPort }} (RDP) } @if (r.ready?.rdp !== false && r.ready?.vnc !== false) { and } @if (r.ready?.vnc !== false) { port {{ vncPort }} (VNC) }. }
            </div>
          }
          <div class="logbar">
            <button type="button" class="link" (click)="showLog.set(!showLog())">{{ showLog() ? 'Hide details' : 'Show details' }}</button>
            <button type="button" class="link" (click)="copyLog()" title="Copy the error, the warnings and the whole output to the clipboard">{{ copied() ? 'Copied' : 'Copy log' }}</button>
          </div>
          @if (showLog()) { <pre class="log" #log>{{ r.log.join('\\n') }}</pre> }
        </div>
        <div class="modal-foot">
          @if (r.phase === 'running') {
            <button type="button" (click)="setup.cancel()">Cancel setup</button><span class="spacer"></span>
            <button type="button" class="primary" (click)="ui.close()" title="The setup goes on; you are told when it is done">Hide</button>
          } @else {
            @if (r.phase !== 'done' || r.check) { <button type="button" (click)="setup.reset()">{{ r.phase === 'done' ? 'Set Up…' : 'Back' }}</button> }
            <span class="spacer"></span>
            @if (r.made.rdp) { <button type="button" (click)="open(r.made.rdp!)">Open RDP</button> }
            @if (r.made.vnc) { <button type="button" (click)="open(r.made.vnc!)">Open VNC</button> }
            <button type="button" class="primary" (click)="ui.close()">Close</button>
          }
        </div>
      } @else {
      <div class="modal-body">
        <div class="sec">Install</div>
        <div class="cards">
          <button type="button" class="card" [class.on]="rdp()" (click)="rdp.set(!rdp())">
            <span class="cb">{{ rdp() ? '✓' : '' }}</span><span><b>RDP</b><small>xrdp · port {{ rdpPort }}</small></span></button>
          <button type="button" class="card" [class.on]="vnc()" (click)="vnc.set(!vnc())">
            <span class="cb">{{ vnc() ? '✓' : '' }}</span><span><b>VNC</b><small>TigerVNC · :{{ display }} · port {{ vncPort }}</small></span></button>
        </div>

        <div class="sec">Desktop users <span class="faint">· one for each service, created without administrator rights</span></div>
        <div class="two">
          @if (rdp()) {
            <div><label>RDP user</label><input name="ru" [ngModel]="rdpUser()" (ngModelChange)="rdpUser.set($event)" class="mono" spellcheck="false" />
              <div class="us" [class.new]="!exists(rdpUser())">{{ userNote(rdpUser()) }}</div></div>
          }
          @if (vnc()) {
            <div><label>VNC user</label><input name="vu" [ngModel]="vncUser()" (ngModelChange)="vncUser.set($event)" class="mono" spellcheck="false" />
              <div class="us" [class.new]="!exists(vncUser())">{{ userNote(vncUser()) }}</div></div>
          }
        </div>
        @if (problem(); as p) { <div class="warn">{{ p }}</div> }
        @else if (localWarn(); as w) { <div class="warn">{{ w }}</div> }

        <div class="sec">Passwords <span class="faint">· sent over the SSH connection, never shown</span></div>
        <label class="chk" [class.off]="!known()">
          <input type="checkbox" name="sp1" [disabled]="!known()" [ngModel]="samePw()" (ngModelChange)="samePw.set($event)" />
          Use the password of this SSH login for the new users, sudo and VNC</label>
        @if (!known()) { <div class="hint tight">This SSH login did not use a password (a key, or none), so type the ones you need.</div> }
        @if (!samePw()) {
          <div class="rows">
            @if (needAccount()) {
              <label>New users</label>
              <div class="two">
                <input type="password" name="ap" [ngModel]="acct()" (ngModelChange)="acct.set($event)" placeholder="password" autocomplete="new-password" />
                <input type="password" name="ap2" [ngModel]="acct2()" (ngModelChange)="acct2.set($event)" placeholder="again" autocomplete="new-password" />
              </div>
            }
            <label>sudo</label>
            <div class="two">
              <input type="password" name="sp" [disabled]="sudoMode() === 'none'" [ngModel]="sudo()" (ngModelChange)="sudo.set($event)" placeholder="sudo password" autocomplete="off" />
              <label class="chk"><input type="checkbox" name="sn" [ngModel]="sudoMode() === 'none'" (ngModelChange)="sudoMode.set($event ? 'none' : 'own')" /> sudo needs no password</label>
            </div>
            @if (vnc()) {
              <label>VNC</label>
              <div class="two">
                <input type="password" name="vp" [disabled]="vncSame() && needAccount()" [ngModel]="vncSame() && needAccount() ? '' : vncPw()" (ngModelChange)="vncPw.set($event)" placeholder="VNC password (6 to 8 characters)" autocomplete="new-password" />
                @if (needAccount()) { <label class="chk"><input type="checkbox" name="vs" [ngModel]="vncSame()" (ngModelChange)="vncSame.set($event)" /> same as the new users' (first 8 characters)</label> }
              </div>
            }
          </div>
        }

        <details class="more">
          <summary>More options <span class="faint">· {{ access() === 'ssh' ? 'through the SSH session only' : 'from the network' }}{{ createSessions() ? ', creates the sessions' : '' }}{{ force() ? ', reconfigures' : '' }}</span></summary>
          <div class="seg">
            <button type="button" [class.on]="access() === 'ssh'" (click)="access.set('ssh')">Through this SSH session only (recommended)</button>
            <button type="button" [class.on]="access() === 'network'" (click)="access.set('network')">From the network</button>
          </div>
          @if (access() === 'ssh') {
            <div class="hint">Both listen on this computer's own address only; nothing is opened to the network. Termwright reaches them through the SSH connection.</div>
          } @else {
            <div class="hint">Both listen on every network interface of the machine. Anyone who can reach it can try to log in, so allow only the network you need.</div>
            <div class="rows">
              <label>Firewall</label>
              <input name="cidr" [ngModel]="cidr()" (ngModelChange)="cidr.set($event)" class="mono" placeholder="open for, e.g. 10.234.179.0/24 (optional)" spellcheck="false" />
            </div>
            @if (cidrProblem()) { <div class="warn">That is not a network like 10.234.179.0/24.</div> }
          }
          <label class="chk top">
            <input type="checkbox" name="mk" [ngModel]="createSessions()" (ngModelChange)="createSessions.set($event)" />
            <span><b>Create the sessions afterwards</b>, with the user name and password: {{ names() }} next to this session{{ access() === 'ssh' ? ', using it as the jump host' : '' }}. Only when the setup ends without errors.</span>
          </label>
          <label class="chk top">
            <input type="checkbox" name="force" [ngModel]="force()" (ngModelChange)="force.set($event)" />
            <span><b>Reconfigure services that already run</b>: rewrites their settings and restarts them, so anyone connected is disconnected. Use it to repair a desktop.</span>
          </label>
        </details>

        @if (os()) { <div class="os" [class.bad]="!osOk()">{{ os() }}</div> }
        <div class="hint">It runs in the background: nothing is typed in your terminal. Services that already run are left alone, and if one fails, what was created for it is removed again.</div>
        @if (err()) { <div class="warn">{{ err() }}</div> }
      </div>
      <div class="modal-foot">
        <button type="button" (click)="go(true)" [disabled]="busy() || !!problem()" title="Only reports what is running and what would be created; changes nothing">Check First</button>
        <span class="spacer"></span>
        <button type="button" (click)="ui.close()">Cancel</button>
        <button type="submit" class="primary" [disabled]="busy() || !valid()">{{ busy() ? 'Starting…' : 'Set Up' }}</button>
      </div>
      }
    </form>
  </div>`,
  styles: [`
    .sec { font-size: calc(11px * var(--ui-scale, 1)); font-weight: 600; letter-spacing: .05em; text-transform: uppercase; color: var(--muted); margin: 12px 0 6px; }
    .sec:first-child { margin-top: 0; } .sec .faint { text-transform: none; letter-spacing: 0; font-weight: 400; color: var(--faint, var(--muted)); }
    .cards { display: grid; grid-template-columns: 1fr 1fr; gap: 8px; }
    .card, .box { height: auto; display: flex; gap: 10px; align-items: flex-start; padding: 10px 12px; text-align: left; border: 1px solid var(--line, var(--border)); border-radius: var(--r-lg, 8px); background: var(--surface-2, var(--input-bg));
      b { display: block; font-weight: 600; } small { display: block; color: var(--muted); font-size: calc(11.5px * var(--ui-scale, 1)); margin-top: 2px; font-weight: 400; } }
    .card.on { border-color: var(--accent); background: var(--accent-soft, var(--selected)); box-shadow: 0 0 0 1px var(--accent); }
    .cb { width: 16px; height: 16px; flex: none; margin-top: 2px; border-radius: 4px; border: 2px solid var(--border); display: grid; place-items: center; font-size: 11px; line-height: 1; color: #fff; }
    .card.on .cb { background: var(--accent); border-color: var(--accent); }
    .box { margin-top: 10px; cursor: pointer; input { margin-top: 3px; } }
    .rows { display: flex; flex-direction: column; gap: 4px; margin-top: 8px; > label { margin-top: 6px; color: var(--muted); font-size: calc(12px * var(--ui-scale, 1)); } input:not([type=checkbox]) { width: 100%; } input + input { margin-top: 6px; } .seg + input { margin-top: 6px; } }
    .seg > button { height: auto; padding: 6px 10px; }
    .us { font-size: calc(11.5px * var(--ui-scale, 1)); color: var(--muted); margin-top: 3px; } .us.new { color: var(--ok); }
    .chk { display: flex; gap: 8px; align-items: center; margin: 4px 0; font-weight: 400; color: var(--fg, inherit); } .chk.off { opacity: .6; } .chk.top { align-items: flex-start; margin-top: 10px; input { margin-top: 3px; } }
    .two { display: grid; grid-template-columns: 1fr 1fr; gap: 8px; align-items: start; > div > label { display: block; color: var(--muted); font-size: calc(12px * var(--ui-scale, 1)); margin-bottom: 4px; } input:not([type=checkbox]) { width: 100%; } }
    .rows > .two { margin-top: 0; } .two input + input { margin-top: 0; }
    .more { margin-top: 12px; border-top: 1px solid var(--border); padding-top: 8px; summary { cursor: pointer; color: var(--muted); font-size: calc(12px * var(--ui-scale, 1)); font-weight: 600; } summary .faint { font-weight: 400; } .seg { margin-top: 8px; } }
    .hint { color: var(--muted); font-size: calc(12px * var(--ui-scale, 1)); line-height: 1.45; margin-top: 6px; } .hint.tight { margin-top: 0; }
    .warn { user-select: text; color: var(--danger); font-size: calc(12px * var(--ui-scale, 1)); margin-top: 6px; white-space: pre-wrap; } .warn.soft { color: var(--warn, var(--muted)); }
    .os { margin-top: 10px; font-size: calc(12px * var(--ui-scale, 1)); color: var(--ok); &.bad { color: var(--danger); } }
    .prog { display: flex; gap: 12px; align-items: center; padding: 6px 0 10px; .pi { flex: none; display: grid; place-items: center; width: 22px; color: var(--accent); }
      .pt b { display: block; font-size: calc(14px * var(--ui-scale, 1)); font-weight: 600; } .pt small { display: block; color: var(--muted); font-size: calc(11.5px * var(--ui-scale, 1)); margin-top: 2px; font-family: var(--mono); white-space: nowrap; overflow: hidden; text-overflow: ellipsis; max-width: 440px; }
      &.ok .pi { color: var(--ok); } &.bad .pi { color: var(--danger); } }
    .spin { width: 16px; height: 16px; border-radius: 50%; border: 2px solid var(--border); border-top-color: var(--accent); animation: rdspin .8s linear infinite; }
    @keyframes rdspin { to { transform: rotate(360deg); } }
    .bar { height: 4px; border-radius: 2px; background: var(--surface-2, var(--input-bg)); overflow: hidden; margin-bottom: 8px; i { display: block; height: 100%; background: var(--accent); transition: width .4s; } }
    .logbar { display: flex; gap: 14px; } .logbar .link { margin-top: 8px; }
    .link { background: none; border: none; color: var(--accent); padding: 0; height: auto; cursor: pointer; margin-top: 8px; }
    .log, .rep { margin: 8px 0 0; padding: 8px 10px; max-height: 240px; overflow: auto; background: var(--input-bg); border-radius: 6px; font-size: calc(11.5px * var(--ui-scale, 1)); font-family: var(--mono); white-space: pre-wrap; user-select: text; }
  `],
})
export class RemoteDesktopDialog implements OnInit {
  protected ui = inject(Ui);
  protected setup = inject(RemoteDesktopSetup);
  private tabs = inject(Tabs);
  private backend = inject(Backend);

  readonly rdpPort = RDP_PORT;
  readonly vncPort = VNC_PORT;
  readonly display = VNC_DISPLAY;
  readonly tab = computed(() => this.tabs.get(this.ui.remoteDesktopTab()));
  readonly rdp = signal(true);
  readonly vnc = signal(true);
  readonly rdpUser = signal('');
  readonly vncUser = signal('');
  readonly access = signal<'ssh' | 'network'>('ssh');
  readonly cidr = signal('');
  readonly createSessions = signal(true);
  readonly force = signal(false);
  /** The password of the SSH login is known (typed at the login or saved), so it can be reused. */
  readonly known = signal(false);
  readonly samePw = signal(false);
  readonly acct = signal('');
  readonly acct2 = signal('');
  readonly sudoMode = signal<'own' | 'none'>('own');
  readonly sudo = signal('');
  readonly vncSame = signal(true);
  readonly vncPw = signal('');
  readonly busy = signal(false);
  readonly err = signal('');
  readonly os = signal('');
  readonly osOk = signal(true);
  readonly showLog = signal(false);
  readonly copied = signal(false);
  /** What the machine says: users that exist and who is logged in at its own screen. */
  readonly users = signal<string[]>([]);
  readonly localUsers = signal<string[]>([]);
  private logEl = viewChild<ElementRef<HTMLElement>>('log');

  constructor() {
    effect(() => { // keep the details scrolled to the end
      this.setup.run();
      const el = this.logEl()?.nativeElement;
      if (el) setTimeout(() => (el.scrollTop = el.scrollHeight), 0);
    });
  }

  readonly exists = (u: string) => this.users().includes(u.trim());
  readonly userNote = (u: string) => !u.trim() ? '' : this.exists(u) ? 'exists on the machine: used as it is, its password is not changed' : 'new user, without administrator rights';
  /** Passwords for new users are needed when a chosen user does not exist yet. */
  readonly needAccount = computed(() => (this.rdp() && !this.exists(this.rdpUser())) || (this.vnc() && !this.exists(this.vncUser())));
  readonly names = computed(() => {
    const n = this.tab()?.session.name ?? '';
    return [this.rdp() ? `“${remoteSessionName(n, 'RDP')}”` : '', this.vnc() ? `“${remoteSessionName(n, 'VNC')}”` : ''].filter(Boolean).join(' and ');
  });
  readonly problem = computed(() => {
    if (!this.rdp() && !this.vnc()) return 'Choose RDP, VNC or both.';
    const chosen = [this.rdp() ? this.rdpUser().trim() : null, this.vnc() ? this.vncUser().trim() : null].filter((u): u is string => u !== null);
    for (const u of chosen) {
      if (!u) return 'Say which user gets each desktop.';
      if (u === 'root') return 'Use a normal user: a graphical session as root is not allowed.';
      if (!/^[a-z_][a-z0-9_-]{0,31}$/.test(u)) return `"${u}" is not a Linux user name (lower case letters, digits, - and _).`;
    }
    if (this.rdp() && this.vnc() && this.rdpUser().trim() === this.vncUser().trim()) return 'RDP and VNC need different users: one user cannot have both desktops at once.';
    return '';
  });
  readonly localWarn = computed(() => {
    const bad = [this.rdp() ? this.rdpUser().trim() : '', this.vnc() ? this.vncUser().trim() : ''].filter(u => u && this.localUsers().includes(u));
    return bad.length ? `${bad.join(' and ')} ${bad.length > 1 ? 'are' : 'is'} logged in at this machine's own screen. A remote desktop for the same user often stays black: choose another user.` : '';
  });
  readonly cidrProblem = () => this.access() === 'network' && this.cidr().trim() !== '' && !/^[0-9a-fA-F:.]+(\/\d{1,3})?$/.test(this.cidr().trim());
  /** What is still missing before Set Up can start. */
  private readonly missing = computed(() => {
    if (this.samePw()) return '';
    if (this.needAccount()) {
      if (this.acct().length < 6) return 'The password of the new users needs at least 6 characters.';
      if (this.acct() !== this.acct2()) return 'The two passwords of the new users differ.';
    }
    if (this.sudoMode() === 'own' && !this.sudo()) return 'Type the sudo password.';
    if (this.vnc() && !(this.vncSame() && this.needAccount()) && (this.vncPw().length < 6 || this.vncPw().length > 8)) return 'The VNC password needs 6 to 8 characters.';
    return '';
  });
  readonly valid = () => !this.problem() && !this.cidrProblem() && !this.missing();

  ngOnInit() {
    const t = this.tab();
    if (!t) { this.ui.close(); return; }
    const base = (t.shellUser || t.session.username || '').trim().replace(/-(rdp|vnc)$/, '');
    if (base && base !== 'root') { this.rdpUser.set(base + '-rdp'); this.vncUser.set(base + '-vnc'); }
    void this.backend.remoteDesktopInfo(t.id).then(i => {
      this.known.set(i.knownPassword);
      this.samePw.set(i.knownPassword);
    }).catch(() => {});
    void this.detectOs(t.session.path);
  }

  /** Reads the Linux version, the users and who is logged in at its screen, in a background login. */
  private async detectOs(path: string) {
    if (!path) return;
    try {
      const out = await Promise.race([
        this.backend.runFactCommand(path, '', `. /etc/os-release 2>/dev/null; echo "TWOS|\${ID}|\${VERSION_ID}|\${PRETTY_NAME}"; echo "TWSEAT|$(loginctl list-sessions --no-legend 2>/dev/null | awk '$4=="seat0"{print $3}' | sort -u | tr '\\n' ' ')"; echo "TWUSERS|$(getent passwd | awk -F: '$3>=1000 && $3<60000{print $1}' | tr '\\n' ' ')"`),
        new Promise<string>((_, no) => setTimeout(() => no(new Error('timeout')), 10000)),
      ]);
      const clean = out.replace(/\r/g, '');
      const list = (tag: string) => (new RegExp(`^${tag}\\|(.*)$`, 'm').exec(clean)?.[1] ?? '').trim().split(/\s+/).filter(Boolean);
      this.localUsers.set(list('TWSEAT'));
      this.users.set(list('TWUSERS'));
      const m = /^TWOS\|([^|]*)\|([^|]*)\|(.*)$/m.exec(clean);
      if (!m) return;
      const ok = SUPPORTED_OS.some(([id, ver]) => m[1] === id && m[2] === ver);
      this.osOk.set(ok);
      this.os.set(ok ? `${m[3]} - supported.` : `${m[3] || 'This system'} is not supported yet: the script supports Ubuntu 22.04 and 24.04 and Debian 12 and stops before changing anything on other systems.`);
    } catch { /* unknown: the script checks again */ }
  }

  /** Copies the error, the warnings and the whole output (the passwords are already masked). */
  async copyLog() {
    const r = this.setup.run();
    if (!r) return;
    const text = [...(r.error ? [`Error: ${r.error}`] : []), ...r.warnings.map(w => `Warning: ${w}`), '', ...r.log].join('\n');
    try { await this.backend.clipboardSet(text); } catch { await navigator.clipboard?.writeText(text).catch(() => {}); }
    this.copied.set(true);
    setTimeout(() => this.copied.set(false), 2000);
  }
  report(log: string[]) { return reportLines(log).join('\n'); }
  madeNames(m: { rdp?: string; vnc?: string }) { return [m.rdp, m.vnc].filter(Boolean).map(p => `“${p!.split('/').pop()}”`).join(' and '); }
  open(path: string) { this.setup.open(path); this.ui.close(); }

  async go(check: boolean) {
    if (this.busy() || this.problem() || this.cidrProblem() || (!check && this.missing())) return;
    this.busy.set(true);
    this.err.set('');
    const run: RemoteDesktopRun = {
      tabId: this.ui.remoteDesktopTab(), rdp: this.rdp(), vnc: this.vnc(),
      rdpUser: this.rdpUser().trim(), vncUser: this.vncUser().trim(), createUsers: this.needAccount(),
      network: this.access() === 'network', cidr: this.cidr().trim(), check, force: this.force(),
      sudoSame: this.samePw(), sudoNone: !this.samePw() && this.sudoMode() === 'none', sudoPassword: this.sudo(),
      accountSame: this.samePw(), accountPassword: this.acct(), vncSame: this.samePw() || (this.vncSame() && this.needAccount()), vncPassword: this.vncPw(),
    };
    try {
      await this.setup.start(run, this.createSessions());
      this.showLog.set(false); // a check's report is already shown above the log
      this.sudo.set(''); this.acct.set(''); this.acct2.set(''); this.vncPw.set(''); // forget the passwords
    } catch (e) {
      this.err.set(errText(e));
    } finally {
      this.busy.set(false);
    }
  }
}
