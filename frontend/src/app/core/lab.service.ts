import { Injectable, computed, inject, signal } from '@angular/core';
import { Backend, errText } from './backend';
import { LabDevice, LabLink, LabOutput, LabRequest, LabView } from './models';
import { Ui } from './ui.service';

/** The simulated lab (switches from a device pack, simulated on this computer): its
 *  state for the Lab window and the Session Manager, refreshed while it runs. */
@Injectable({ providedIn: 'root' })
export class LabService {
  private backend = inject(Backend);
  private ui = inject(Ui);
  readonly state = signal<LabView | null>(null);
  readonly busy = signal(false);
  /** The Lab window is open (refresh more often). */
  readonly watching = signal(false);
  private timer: ReturnType<typeof setTimeout> | null = null;

  readonly devices = computed(() => this.state()?.devices ?? []);
  readonly links = computed(() => this.state()?.links ?? []);
  /** "127.0.0.1:port" → switch, for the Session Manager's dots. */
  readonly byPort = computed(() => new Map(this.devices().map(d => [d.port, d] as [number, LabDevice])));
  readonly running = computed(() => this.devices().filter(d => d.running).length);

  constructor() { void this.refresh(); }

  async refresh() {
    try { this.state.set(await this.backend.labState()); } catch { /* not ready */ }
    this.schedule();
  }

  private schedule() {
    if (this.timer) clearTimeout(this.timer);
    const live = this.watching() || (this.state()?.running ?? false);
    if (live) this.timer = setTimeout(() => void this.refresh(), this.watching() ? 2000 : 6000);
  }

  /** The switch behind a session (a session to 127.0.0.1 on a lab port). */
  deviceOf(host: string | undefined, port: number | undefined): LabDevice | undefined {
    return host === '127.0.0.1' && port ? this.byPort().get(port) : undefined;
  }

  private async run(f: () => Promise<LabView>, ok?: string): Promise<boolean> {
    this.busy.set(true);
    try {
      this.state.set(await f());
      if (ok) this.ui.toast(ok, 'success');
      return true;
    } catch (e) {
      this.ui.error(errText(e));
      await this.refresh();
      return false;
    } finally { this.busy.set(false); this.schedule(); }
  }

  async add(r: LabRequest) {
    const ok = await this.run(() => this.backend.labAdd(r), r.groups.reduce((n, g) => n + g.count, 0) + ' simulated switches added' + (r.start ? ' and started' : ''));
    if (ok) await this.ui.refreshTree();
    return ok;
  }
  start(ids: string[] = []) { return this.run(() => this.backend.labStart(ids)); }
  stop(ids: string[] = []) { return this.run(() => this.backend.labStop(ids)); }
  async remove(ids: string[], sessions: boolean) {
    const ok = await this.run(() => this.backend.labRemove(ids, sessions));
    if (ok) await this.ui.refreshTree();
    return ok;
  }
  connect(l: LabLink) { return this.run(() => this.backend.labConnect(l)); }
  disconnect(id: string, port: string) { return this.run(() => this.backend.labDisconnect(id, port)); }
  place(id: string, x: number, y: number) { void this.backend.labPlace(id, Math.round(x), Math.round(y)).catch(() => {}); }
  freePorts(id: string) { return this.backend.labFreePorts(id).then(p => p ?? []).catch(() => [] as string[]); }
  show(id: string, cmd: string) { return this.backend.labShow(id, cmd).catch(e => errText(e)); }
  setIdentity(id: string, serial: string, mac: string) { return this.run(() => this.backend.labSetIdentity(id, serial, mac), 'Saved'); }
  setSerials(ids: string[], serials: string[]) { return this.run(() => this.backend.labSetSerials(ids, serials), 'Serial numbers set'); }
  setOutputs(o: LabOutput[]) { return this.run(() => this.backend.labSetOutputs(o), 'Outputs saved'); }
  /** Reads a device pack for the consent step (nothing is kept). */
  packInfo(path: string) { return this.backend.labPackInfo(path); }
  acceptPack(path: string, hash: string) { return this.run(() => this.backend.labPackAccept(path, hash), 'Device pack loaded'); }
  removePack() { return this.run(() => this.backend.labPackRemove(), 'Device pack removed'); }
  osShort(os: string) { return this.state()?.os?.[os]?.short || os.toUpperCase(); }
  sessionOf(id: string) { return this.state()?.sessions?.[id] ?? ''; }
}
