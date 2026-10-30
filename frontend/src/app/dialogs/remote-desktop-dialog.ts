import { Component, OnInit, computed, inject, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { Backend, errText } from '../core/backend';
import { RDP_PORT, RemoteDesktopSetup, VNC_DISPLAY, VNC_PORT, remoteSessionName } from '../core/remote-desktop-setup.service';
import { Tabs } from '../core/tabs.service';
import { Ui } from '../core/ui.service';
import { Icon } from '../components/icon';

/**
 * Set Up Remote Desktop: installs XFCE with xrdp (RDP) and/or TigerVNC on the
 * Linux machine of an SSH tab, by running a script in that tab.
 */
@Component({
  selector: 'app-remote-desktop-dialog',
  imports: [FormsModule, Icon],
  template: `
  <div class="modal-backdrop">
    <form class="modal" (ngSubmit)="go(false)" autocomplete="off">
      <div class="modal-head"><span class="mh-ic"><app-icon name="monitor" [size]="17" /></span>
        <span class="mh-t">Set Up Remote Desktop<span class="mh-s">on {{ tab()?.title }} · installs XFCE and the remote desktop services</span></span></div>
      <div class="modal-body">
        <div class="sec">Install</div>
        <div class="cards">
          <button type="button" class="card" [class.on]="rdp()" (click)="rdp.set(!rdp())">
            <span class="cb">{{ rdp() ? '✓' : '' }}</span><span><b>RDP</b><small>xrdp, port {{ rdpPort }} - for Windows Remote Desktop and Termwright RDP</small></span></button>
          <button type="button" class="card" [class.on]="vnc()" (click)="vnc.set(!vnc())">
            <span class="cb">{{ vnc() ? '✓' : '' }}</span><span><b>VNC</b><small>TigerVNC, display :{{ display }}, port {{ vncPort }}</small></span></button>
        </div>

        <div class="grid">
          <label>Desktop user</label>
          <input name="user" [ngModel]="user()" (ngModelChange)="user.set($event)" class="mono" spellcheck="false" />
        </div>
        @if (userProblem(); as p) { <div class="warn">{{ p }}</div> }

        <div class="sec">Who can connect</div>
        <div class="seg">
          <button type="button" [class.on]="access() === 'ssh'" (click)="access.set('ssh')">Through this SSH session only (recommended)</button>
          <button type="button" [class.on]="access() === 'network'" (click)="access.set('network')">From the network</button>
        </div>
        @if (access() === 'ssh') {
          <div class="hint">Both listen on this computer's own address only; nothing is opened to the network. Termwright reaches them through the SSH connection.</div>
        } @else {
          <div class="hint">Both listen on every network interface of the machine. Anyone who can reach it can try to log in, so allow only the network you need.</div>
          <div class="grid">
            <label>Open the firewall for</label>
            <input name="cidr" [ngModel]="cidr()" (ngModelChange)="cidr.set($event)" class="mono" placeholder="optional, e.g. 10.234.179.0/24" spellcheck="false" />
          </div>
          @if (cidrProblem()) { <div class="warn">That is not a network like 10.234.179.0/24.</div> }
        }

        <label class="box">
          <input type="checkbox" name="mk" [(ngModel)]="createSessions" />
          <span><b>Create the sessions afterwards</b>
            <small>{{ names() }} next to this session{{ access() === 'ssh' ? ', using it as the jump host' : '' }}. Only when the script ends without errors.</small></span>
        </label>

        <label class="box">
          <input type="checkbox" name="force" [(ngModel)]="force" />
          <span><b>Reconfigure services that already run</b>
            <small>Rewrites their settings and restarts them, so anyone connected is disconnected. Use it to repair a desktop (for example a black screen).</small></span>
        </label>

        @if (os()) { <div class="os" [class.bad]="!osOk()">{{ os() }}</div> }
        <div class="hint">The script runs in this SSH tab so you see everything. You type your <b>sudo password</b> there, and the <b>VNC password</b> when it asks. Services that already run are left alone.</div>
        @if (err()) { <div class="warn">{{ err() }}</div> }
      </div>
      <div class="modal-foot">
        <button type="button" (click)="go(true)" [disabled]="busy() || !valid()" title="Runs the script with --check: it only reports what is running and changes nothing">Check First</button>
        <span class="spacer"></span>
        <button type="button" (click)="ui.close()">Cancel</button>
        <button type="submit" class="primary" [disabled]="busy() || !valid()">{{ busy() ? 'Starting…' : 'Set Up' }}</button>
      </div>
    </form>
  </div>`,
  styles: [`
    .sec { font-size: calc(11px * var(--ui-scale, 1)); font-weight: 600; letter-spacing: .05em; text-transform: uppercase; color: var(--muted); margin: 10px 0 6px; }
    .sec:first-child { margin-top: 0; }
    .cards { display: grid; grid-template-columns: 1fr 1fr; gap: 8px; }
    .card, .box { height: auto; display: flex; gap: 10px; align-items: flex-start; padding: 10px 12px; text-align: left; border: 1px solid var(--line, var(--border)); border-radius: var(--r-lg, 8px); background: var(--surface-2, var(--input-bg));
      b { display: block; font-weight: 600; } small { display: block; color: var(--muted); font-size: calc(11.5px * var(--ui-scale, 1)); margin-top: 2px; font-weight: 400; } }
    .card.on { border-color: var(--accent); background: var(--accent-soft, var(--selected)); box-shadow: 0 0 0 1px var(--accent); }
    .cb { width: 16px; height: 16px; flex: none; margin-top: 2px; border-radius: 4px; border: 2px solid var(--border); display: grid; place-items: center; font-size: 11px; line-height: 1; color: #fff; }
    .card.on .cb { background: var(--accent); border-color: var(--accent); }
    .box { margin-top: 12px; cursor: pointer; input { margin-top: 3px; } }
    .grid { margin-top: 10px; }
    .seg > button { height: auto; padding: 6px 10px; }
    .hint { color: var(--muted); font-size: calc(12px * var(--ui-scale, 1)); line-height: 1.45; margin-top: 6px; }
    .warn { color: var(--danger); font-size: calc(12px * var(--ui-scale, 1)); margin-top: 6px; white-space: pre-wrap; }
    .os { margin-top: 10px; font-size: calc(12px * var(--ui-scale, 1)); color: var(--ok); &.bad { color: var(--danger); } }
  `],
})
export class RemoteDesktopDialog implements OnInit {
  protected ui = inject(Ui);
  private tabs = inject(Tabs);
  private backend = inject(Backend);
  private setup = inject(RemoteDesktopSetup);

  readonly rdpPort = RDP_PORT;
  readonly vncPort = VNC_PORT;
  readonly display = VNC_DISPLAY;
  readonly tab = computed(() => this.tabs.get(this.ui.remoteDesktopTab()));
  readonly rdp = signal(true);
  readonly vnc = signal(true);
  readonly access = signal<'ssh' | 'network'>('ssh');
  readonly busy = signal(false);
  readonly err = signal('');
  readonly os = signal('');
  readonly osOk = signal(true);
  readonly user = signal('');
  readonly cidr = signal('');
  createSessions = true;
  force = false;

  readonly names = computed(() => {
    const n = this.tab()?.session.name ?? '';
    return [this.rdp() ? `“${remoteSessionName(n, 'RDP')}”` : '', this.vnc() ? `“${remoteSessionName(n, 'VNC')}”` : ''].filter(Boolean).join(' and ');
  });
  readonly userProblem = computed(() => {
    const u = this.user().trim();
    if (!u) return 'Say which user gets the desktop.';
    if (u === 'root') return 'Use a normal user: a graphical session as root is not allowed.';
    if (!/^[a-z_][a-z0-9_-]{0,31}$/.test(u)) return 'That is not a Linux user name (lower case letters, digits, - and _).';
    return '';
  });
  readonly cidrProblem = () => this.access() === 'network' && this.cidr().trim() !== '' && !/^[0-9a-fA-F:.]+(\/\d{1,3})?$/.test(this.cidr().trim());
  readonly valid = () => (this.rdp() || this.vnc()) && !this.userProblem() && !this.cidrProblem();

  ngOnInit() {
    const t = this.tab();
    if (!t) { this.ui.close(); return; }
    const u = (t.shellUser || t.session.username || '').trim();
    this.user.set(u === 'root' ? '' : u);
    void this.detectOs(t.session.path);
  }

  /** Reads the Linux version in a background login; the script only supports Ubuntu 24.04 so far. */
  private async detectOs(path: string) {
    if (!path) return;
    try {
      const out = await Promise.race([
        this.backend.runFactCommand(path, '', `. /etc/os-release 2>/dev/null; echo "TWOS|\${ID}|\${VERSION_ID}|\${PRETTY_NAME}"`),
        new Promise<string>((_, no) => setTimeout(() => no(new Error('timeout')), 10000)),
      ]);
      const m = /^TWOS\|([^|]*)\|([^|]*)\|(.*)$/m.exec(out.replace(/\r/g, ''));
      if (!m) return;
      const ok = m[1] === 'ubuntu' && m[2] === '24.04';
      this.osOk.set(ok);
      this.os.set(ok ? `${m[3]} - supported.` : `${m[3] || 'This system'} is not supported yet: the script supports Ubuntu 24.04 only and stops before changing anything.`);
    } catch { /* unknown: the script checks again */ }
  }

  async go(check: boolean) {
    if (!this.valid() || this.busy()) return;
    this.busy.set(true);
    this.err.set('');
    try {
      await this.setup.run({
        tabId: this.ui.remoteDesktopTab(), user: this.user().trim(), rdp: this.rdp(), vnc: this.vnc(), access: this.access(),
        cidr: this.cidr().trim(), createSessions: this.createSessions, check, force: this.force,
      });
      this.ui.close();
    } catch (e) {
      this.err.set(errText(e));
    } finally {
      this.busy.set(false);
    }
  }
}
