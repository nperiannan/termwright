import { Component, OnInit, computed, effect, inject, input, output, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { LabService } from '../core/lab.service';
import { Backend, errText } from '../core/backend';
import { EmuDeviceType, EmuHost, EmuImage, EmuPersonaDef, EmuSlot, EmuTemplate, LabDevice, SessionNode } from '../core/models';
import { Ui } from '../core/ui.service';
import { tr } from '../core/i18n';
import { LabHostImages } from './lab-host-images';

/** Shared look of the lab pages (Configure, New Lab, Images, Hosts). */
const PAGE = `
  .ov { position: fixed; inset: 0; background: rgba(15,20,30,.38); z-index: 60; }
  .mdl { position: fixed; left: 50%; top: 50%; transform: translate(-50%, -50%); width: min(1180px, 94vw); height: min(840px, 90vh); background: var(--surface); color: var(--text);
    border: 1px solid var(--line); border-radius: 14px; box-shadow: 0 24px 70px rgba(0,0,0,.3); z-index: 61; display: grid; grid-template-rows: 64px auto minmax(0, 1fr) 60px; font-size: 13px; }
  .mh { display: flex; align-items: center; gap: 12px; padding: 0 20px; border-bottom: 1px solid var(--line); h2 { margin: 0; font-size: 16px; } }
  .mt { display: flex; gap: 4px; padding: 0 16px; border-bottom: 1px solid var(--line); a { padding: 10px 14px; color: var(--muted); font-weight: 500; cursor: pointer; } a.on { color: var(--text); box-shadow: inset 0 -2px var(--accent); } }
  .mb { overflow: auto; min-height: 0; } .pad { padding: 16px 20px; }
  .mf { display: flex; align-items: center; gap: 8px; padding: 0 20px; border-top: 1px solid var(--line); }
  .sp { flex: 1; } .muted { color: var(--muted); } .small { font-size: 12px; } .mono { font-family: var(--mono, monospace); font-size: 12px; } .dlb { position: relative; border: 1px solid var(--line); border-radius: 8px; height: 32px; padding: 0 14px; font-weight: 600; overflow: hidden; cursor: default; opacity: 1 !important;
    &.busy { background: linear-gradient(90deg, var(--accent) 0 var(--p, 0%), var(--accent-soft) var(--p, 0%) 100%); color: var(--text); border-color: var(--accent); span { position: relative; } }
    &.done { background: var(--success, #1fa45a); border-color: var(--success, #1fa45a); color: #fff; } }
  table.files td.acts { white-space: nowrap; text-align: right; } table.files td { overflow-wrap: anywhere; }
  .dq { border: 1px solid var(--line); border-radius: 10px; background: var(--surface); padding: 6px; margin-bottom: 8px; max-height: 260px; overflow: auto;
    .dqh { display: flex; align-items: baseline; gap: 6px; padding: 2px 4px 6px; }
    .dqi { display: flex; align-items: center; gap: 4px; padding: 5px 6px; border-radius: 7px; cursor: pointer; &:hover { background: var(--surface-2); } &.cur { background: var(--accent-soft); } }
    .dqn { flex: 1; min-width: 0; span { display: block; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; font-size: 12.5px; } small { color: var(--muted); font-size: 11px; } .meter { margin: 3px 0 0; } }
    button.xs { height: 22px; min-width: 22px; padding: 0 4px; font-size: 11px; } }
  .catg { display: grid; grid-template-columns: repeat(auto-fill, minmax(240px, 1fr)); gap: 10px; .card { margin: 0; } }
  .lnk { color: var(--accent); cursor: pointer; }
  .unit { flex: none !important; color: var(--muted); font-size: 12px; white-space: nowrap; }
  .g { color: var(--success, #1fa45a); } .w { color: var(--warning, #e39b17); } .r { color: var(--danger); }
  .f { display: grid; grid-template-columns: 150px minmax(0, 1fr); gap: 10px 14px; align-items: center; margin-bottom: 14px; > label { color: var(--muted); } }
  .f3 { display: grid; grid-template-columns: 130px minmax(0, 1fr) 130px minmax(0, 1fr); gap: 8px 12px; align-items: center; > label { color: var(--muted); font-size: 12px; } .wide { grid-column: 2 / span 3; } }
  input, select, textarea { min-width: 0; } .row2 { display: flex; gap: 8px; align-items: center; > * { flex: 1; } > button, > .pill, > .badge { flex: none; } }
  .sec { font-weight: 600; margin: 16px 0 8px; display: flex; justify-content: space-between; align-items: baseline; span { font-weight: 400; } }
  .card { border: 1px solid var(--line); border-radius: 10px; padding: 10px 12px; margin-bottom: 12px; background: var(--surface); }
  .soft { background: var(--surface-2); }
  .seg { display: inline-flex; border: 1px solid var(--line); border-radius: 8px; overflow: hidden; a { padding: 6px 14px; cursor: pointer; } a.on { background: var(--accent-soft); color: var(--accent); font-weight: 600; } }
  table.t { width: 100%; border-collapse: collapse; font-size: 12px; th, td { padding: 6px 8px; text-align: left; border-bottom: 1px solid var(--line); } th { color: var(--muted); font-weight: 600; background: var(--surface-2); } input, select { height: 26px; width: 100%; } }
  .yaml { font: 11.5px/1.55 var(--mono, monospace); background: var(--surface); border: 1px solid var(--line); border-radius: 10px; padding: 12px; white-space: pre; overflow: auto; max-height: 470px; margin: 0; }
  .okb { display: flex; gap: 8px; padding: 10px; border-radius: 10px; font-size: 12px; margin-bottom: 12px; border: 1px solid; }
  .okb.ok { background: color-mix(in srgb, var(--success, #1fa45a) 12%, transparent); border-color: color-mix(in srgb, var(--success, #1fa45a) 40%, transparent); }
  .okb.bad { background: color-mix(in srgb, var(--danger) 10%, transparent); border-color: color-mix(in srgb, var(--danger) 40%, transparent); }
  .okb.wait { background: var(--surface-2); border-color: var(--line); }
  .two { display: grid; grid-template-columns: minmax(0, 1fr) 360px; height: 100%; > .side { border-left: 1px solid var(--line); background: var(--surface-2); padding: 16px; overflow: auto; } }
  .lst { border-right: 1px solid var(--line); background: var(--surface-2); padding: 12px; overflow: auto;
    .grp { font-size: 11px; font-weight: 600; color: var(--muted); text-transform: uppercase; letter-spacing: .04em; margin: 12px 2px 6px; }
    .it { display: flex; gap: 10px; align-items: center; padding: 8px 10px; border-radius: 9px; border: 1px solid transparent; margin-bottom: 3px; cursor: pointer; small { display: block; color: var(--muted); font-size: 11.5px; } }
    .it.on { background: var(--accent-soft); border-color: var(--accent); } .it:hover:not(.on) { background: var(--surface); } }
  .badge { display: inline-block; font-size: 10.5px; padding: 1px 7px; border-radius: 9px; border: 1px solid var(--line); background: var(--surface-2); margin-left: 4px; }
  .badge.g { border-color: var(--success, #1fa45a); } .pill { display: inline-block; padding: 2px 8px; border-radius: 9px; font-size: 11.5px; margin: 0 4px 4px 0; background: var(--surface-2); border: 1px solid var(--line); }
  .drop { border: 1.5px dashed var(--line); border-radius: 10px; padding: 12px; text-align: center; color: var(--muted); font-size: 12px; margin-top: 10px; cursor: pointer; }
  .meter { height: 6px; border-radius: 4px; background: var(--surface-2); overflow: hidden; margin: 6px 0 2px; i { display: block; height: 100%; background: var(--accent); } }
  .big3 { display: grid; grid-template-columns: repeat(3, 1fr); gap: 10px; .card { margin: 0; } b { font-size: 20px; } }
  .chk { display: grid; grid-template-columns: 22px 170px minmax(0, 1fr); gap: 6px 8px; align-items: start; font-size: 12.5px; .y { color: var(--success, #1fa45a); font-weight: 700; } .n { color: var(--warning, #e39b17); font-weight: 700; } .x { color: var(--danger); font-weight: 700; } }
  .log { font: 11px/1.55 var(--mono, monospace); background: #111722; color: #cfd6e4; border-radius: 9px; padding: 10px; height: 170px; overflow: auto; white-space: pre-wrap; margin: 0; }
  i.d { display: inline-block; width: 8px; height: 8px; border-radius: 50%; background: var(--muted); flex: none; } i.d.ok { background: var(--success, #1fa45a); } i.d.wa { background: var(--warning, #e39b17); } i.d.bad { background: var(--danger); }
  ta, textarea { font: 12px/1.5 var(--mono, monospace); }
`;

// ---------------------------------------------------------------- Configure
/** Configure one switch: general, persona and serial numbers, resources. */
@Component({
  selector: 'app-lab-configure',
  imports: [FormsModule],
  template: `
  <div class="ov" (click)="closed.emit()"></div>
  @if (dev(); as d) {
  <div class="mdl">
    <div class="mh"><div><h2>{{ tr('Configure {0}', d.hostname) }}</h2><div class="muted small">{{ emu() ? tr('Emulated') : tr('Simulated') }} · {{ tr(d.stateText || (d.running ? 'running' : 'stopped')) }}</div></div><span class="sp"></span><button type="button" class="sm" (click)="closed.emit()">✕</button></div>
    <div class="mt">
      <a [class.on]="tab() === 'general'" (click)="tab.set('general')">{{ tr('General') }}</a>
      <a [class.on]="tab() === 'persona'" (click)="tab.set('persona')">{{ pd() ? tr('Persona and Serial') : tr('Serial and MAC') }}</a>
      @if (emu()) { <a [class.on]="tab() === 'res'" (click)="tab.set('res')">{{ tr('Resources') }}</a> <a [class.on]="tab() === 'image'" (click)="tab.set('image')">{{ tr('Network, Day 0 and Advanced') }}</a> }
    </div>
    <div class="mb">
      @switch (tab()) {
        @case ('general') {
          <div class="pad"><div class="f">
            <label>{{ tr('Name') }}</label><b>{{ d.hostname }}</b>
            <label>{{ tr('Device') }}</label><span>{{ d.persona?.slots?.[0]?.model || d.model }}@if (d.version) { · {{ d.version }} }</span>
            @if (emu()) { <label>{{ tr('Image') }}</label><span>{{ image()?.name }} <span class="mono muted">{{ image()?.fileName }}</span></span> }
            <label>{{ tr('State') }}</label><span>{{ tr(d.stateText || (d.running ? 'running' : 'stopped')) }}@if (d.error) { · <span class="r">{{ d.error }}</span> }</span>
            <label>{{ tr('Management') }}</label><span class="mono">{{ emu() ? d.ssh : d.mgmtIp }}</span>
            @if (emu()) { <label>{{ tr('Console') }}</label><span class="mono">telnet 127.0.0.1:{{ d.console }} {{ tr('on the host') }}</span> }
            <label>{{ tr('Sessions') }}</label><span class="small">{{ lab.sessionOf(d.id) }}@if (lab.consoleOf(d.id)) { <br />{{ lab.consoleOf(d.id) }} }</span>
            @if (d.template) { <label>{{ tr('Template') }}</label><span>{{ templateName(d.template) }}</span> }
          </div></div>
        }
        @case ('persona') {
          <div class="two">
            <div class="pad">
              @if (pd(); as p) {
                <div class="f">
                  <label>{{ tr('Device') }}</label><select [ngModel]="slots()[0]?.model" (ngModelChange)="setModel(0, $event)" name="cm0">@for (m of p.models ?? []; track m.name) { <option [value]="m.name">{{ m.name }}</option> }</select>
                  @if (p.stack) { <label>{{ tr('Stack') }}</label><span class="seg"><a [class.on]="slots().length === 1" (click)="standalone()">{{ tr('Standalone') }}</a><a [class.on]="slots().length > 1" (click)="slots().length === 1 && addSlot()">{{ slots().length > 1 ? tr('Stack of {0}', slots().length) : tr('Stack') }}</a></span> }
                  @for (v of p.values ?? []; track v.key) { <label>{{ v.label || v.key }}</label><input class="mono" [ngModel]="pvals[v.key] ?? ''" (ngModelChange)="pvals[v.key] = $event; preview()" [name]="'pv' + v.key" [placeholder]="v.default" /> }
                </div>
                <div class="sec">{{ tr('Slots') }}<span class="muted small">{{ tr('the first slot’s serial number is the switch’s') }}</span></div>
                <table class="t"><thead><tr><th>#</th><th>{{ tr('Model') }}</th><th>{{ tr('Serial number') }}</th><th>{{ p.modules || tr('Module') }}</th>@if (slots().length > 1) { <th>{{ tr('Primary') }}</th><th>{{ tr('Ports → VM ports') }}</th><th></th> }</tr></thead><tbody>
                  @for (x of slots(); track $index; let i = $index) {
                    <tr><td>{{ i + 1 }}</td>
                      <td><select [ngModel]="x.model" (ngModelChange)="setModel(i, $event)" [name]="'sm' + i">@for (m of p.models ?? []; track m.name) { <option [value]="m.name">{{ m.name }}</option> }</select></td>
                      <td><input class="mono" [ngModel]="x.serial" (ngModelChange)="patch(i, { serial: $event })" [name]="'ss' + i" /></td>
                      <td><select [ngModel]="x.module" (ngModelChange)="patch(i, { module: $event })" [name]="'sx' + i"><option value="">{{ tr('none') }}</option>@for (m of modulesOf(x.model); track m) { <option [value]="m">{{ m }}</option> }</select></td>
                      @if (slots().length > 1) { <td><input type="radio" [checked]="x.master" (change)="setMaster(i)" [name]="'smr'" /></td>
                        <td><input class="mono" [ngModel]="ports[i] ?? ''" (ngModelChange)="ports[i] = $event; preview()" [name]="'sp' + i" placeholder="49:1, 50:2" /></td>
                        <td><a class="r" (click)="dropSlot(i)">✕</a></td> }
                    </tr>
                  }
                </tbody></table>
                <div class="row2" style="margin: 10px 0 16px; justify-content: flex-start">@if (p.stack) { <button type="button" (click)="addSlot()">+ {{ tr('Add Slot') }}</button> }<button type="button" (click)="newSerials()">↻ {{ tr('New Serials in the Model’s Format') }}</button></div>
              } @else {
                <div class="f"><label>{{ tr('Serial number') }}</label><div class="row2"><input class="mono" [(ngModel)]="serial" name="cser" /><button type="button" (click)="newSerials()">↻ {{ tr('New') }}</button></div></div>
              }
              <div class="f"><label>{{ tr('Base MAC') }}</label><div class="row2"><input class="mono" [(ngModel)]="mac" name="cmac" /><button type="button" (click)="newMac()">↻ {{ tr('New') }}</button></div></div>
              @if (pd(); as p) {
                <div class="card soft"><b>{{ tr('How it is applied') }}</b><div class="muted small" style="margin-top: 4px">{{ tr('Save and Apply powers the switch off, writes {0} on the partition the image mounts there, reads it back, boots it and checks the model with {1}. Each step shows in Events.', p.file, p.check || tr('no check command')) }}</div></div>
              }
            </div>
            <div class="side">
              @if (pd()) {
                <div class="okb" [class.ok]="d.personaOk && !d.personaError" [class.bad]="!!d.personaError" [class.wait]="!d.personaOk && !d.personaError">
                  <span>{{ d.personaError ? '✕' : d.personaOk ? '✓' : '●' }}</span>
                  <div><b>{{ d.personaError ? tr('Not in the switch') : d.personaOk ? tr('In the switch') : tr('Not written yet') }}</b><br />{{ d.personaError || d.personaInfo || tr('Save and Apply writes it') }}</div></div>
                <div class="muted small" style="display: flex; justify-content: space-between; margin-bottom: 6px"><span>{{ tr('Persona file preview') }}</span><span class="mono">{{ pd()!.file }}</span></div>
                <pre class="yaml">{{ yaml() || '…' }}</pre>
              } @else {
                <p class="muted small">{{ emu() ? tr('This image has no persona: the switch keeps the model of its image. Its serial number goes into the VM’s SMBIOS.') : tr('A simulated switch shows these in show version and show switch.') }}</p>
              }
            </div>
          </div>
        }
        @case ('res') {
          <div class="pad"><div class="f">
            <label>vCPU</label><input type="number" min="1" [(ngModel)]="cpus" name="ccpu" style="max-width: 120px" />
            <label>{{ tr('Memory (MB)') }}</label><input type="number" min="128" step="256" [(ngModel)]="ram" name="cram" style="max-width: 160px" />
            <label>{{ tr('Disk (GB)') }}</label><div class="row2" style="max-width: 360px"><input type="number" min="0" [(ngModel)]="disk" name="cdisk" /><span class="muted small">{{ tr('0 = the image’s size; a disk only grows') }}</span></div>
          </div><p class="muted small">{{ tr('They apply when the switch starts next.') }}</p></div>
        }
        @case ('image') {
          @if (image(); as im) {
            <div class="pad"><div class="f">
              <label>{{ tr('Network ports') }}</label><span>{{ im.nics }} × {{ im.nic }} · {{ tr('port names') }} <span class="mono">{{ im.portName || '{n}' }}</span> · {{ tr('port 1 is management') }}</span>
              <label>{{ tr('Ready when') }}</label><span class="mono">{{ im.ready }}</span>
              <label>{{ tr('Login') }}</label><span>{{ im.login }}</span>
              <label>{{ tr('Day 0') }}</label><pre class="mono" style="margin: 0">{{ (im.day0 ?? []).join('\n') || '-' }}</pre>
              <label>{{ tr('Address command') }}</label><span class="mono">{{ im.addrCmd || '-' }}</span>
              <label>{{ tr('More QEMU options') }}</label><span class="mono">{{ im.extra || '-' }}</span>
            </div><p class="muted small">{{ tr('These belong to the image and apply to all its switches.') }} <a (click)="images.emit()" style="color: var(--accent); cursor: pointer">{{ tr('Edit in Images…') }}</a></p></div>
          }
        }
      }
    </div>
    <div class="mf"><span class="muted small">{{ tab() === 'persona' && pd() ? tr('Changes to the persona need a restart of the switch.') : '' }}</span><span class="sp"></span>
      <button type="button" (click)="closed.emit()">{{ tr('Cancel') }}</button>
      <button type="button" (click)="save(false)" [disabled]="lab.busy()">{{ tr('Save') }}</button>
      @if (tab() === 'persona' && pd()) { <button type="button" class="primary" (click)="save(true)" [disabled]="lab.busy()">{{ d.running ? tr('Save and Apply (restarts {0})', d.hostname) : tr('Save and Apply') }}</button> }
    </div>
  </div>
  }`,
  styles: [PAGE],
})
export class LabConfigure implements OnInit {
  readonly devId = input.required<string>();
  readonly emu = input(true);
  readonly closed = output<void>();
  readonly images = output<void>();
  protected lab = inject(LabService);
  private backend = inject(Backend);
  private ui = inject(Ui);
  protected tr = tr;
  protected tab = signal<'general' | 'persona' | 'res' | 'image'>('persona');
  protected dev = computed(() => this.lab.devices().find(d => d.id === this.devId()) ?? null);
  protected image = computed(() => (this.lab.state()?.images ?? []).find(i => i.id === this.dev()?.image) ?? null);
  protected pd = computed<EmuPersonaDef | null>(() => (this.emu() ? this.image()?.persona ?? null : null));
  protected slots = signal<EmuSlot[]>([]);
  protected ports: string[] = [];
  protected pvals: Record<string, string> = {};
  protected serial = ''; protected mac = ''; protected cpus = 1; protected ram = 1024; protected disk = 0;
  protected yaml = signal('');
  private timer: ReturnType<typeof setTimeout> | null = null;

  ngOnInit() {
    const d = this.dev(); if (!d) return;
    this.serial = d.serial; this.mac = d.mac; this.cpus = d.cpus || 1; this.ram = d.ram || 1024; this.disk = d.diskGb || 0;
    const pd = this.pd();
    const sl = d.persona?.slots?.length ? d.persona.slots.map((x, i) => ({ ...x, serial: x.serial || (i === 0 ? d.serial : '') })) : pd ? [{ num: 1, model: pd.models?.[0]?.name ?? '', serial: d.serial, module: '', master: true, ports: null }] : [];
    this.slots.set(sl); this.ports = sl.map(x => (x.ports ?? []).map(p => p.port + ':' + p.vif).join(', '));
    this.pvals = { ...(d.persona?.values ?? {}) };
    if (!pd) this.tab.set('persona');
    this.preview();
  }
  templateName(id: string) { return (this.lab.state()?.templates ?? []).find(t => t.id === id)?.name ?? id; }
  modulesOf(model: string) { return this.pd()?.models?.find(m => m.name === model)?.modules ?? []; }
  patch(i: number, p: Partial<EmuSlot>) { this.slots.set(this.slots().map((x, j) => (j === i ? { ...x, ...p } : x))); this.preview(); }
  setModel(i: number, model: string) { this.patch(i, { model, module: this.modulesOf(model).includes(this.slots()[i]?.module) ? this.slots()[i].module : '' }); }
  setMaster(i: number) { this.slots.set(this.slots().map((x, j) => ({ ...x, master: j === i }))); this.preview(); }
  standalone() { this.slots.set(this.slots().slice(0, 1).map(x => ({ ...x, master: true }))); this.ports = this.ports.slice(0, 1); this.preview(); }
  async addSlot() {
    const last = this.slots()[this.slots().length - 1];
    const s = await this.backend.emuSuggest(last?.model ?? '').catch(() => null);
    this.slots.set([...this.slots(), { num: this.slots().length + 1, model: last?.model ?? '', serial: s?.serial ?? '', module: '', master: false, ports: null }]); this.ports.push(''); this.preview();
  }
  dropSlot(i: number) { this.slots.set(this.slots().filter((_, j) => j !== i)); this.ports.splice(i, 1); this.preview(); }
  async newSerials() {
    if (!this.pd()) { const s = await this.backend.emuSuggest(this.dev()?.model ?? '').catch(() => null); if (s?.serial) this.serial = s.serial; return; }
    const out: EmuSlot[] = [];
    for (const x of this.slots()) { const s = await this.backend.emuSuggest(x.model).catch(() => null); out.push({ ...x, serial: s?.serial || x.serial }); }
    this.slots.set(out); this.preview();
  }
  async newMac() { const s = await this.backend.emuSuggest(this.slots()[0]?.model || this.dev()?.model || '').catch(() => null); if (s?.mac) this.mac = s.mac; }
  private persona() {
    const sl: EmuSlot[] = [];
    for (const [i, x] of this.slots().entries()) {
      const ports: { port: number; vif: number }[] = [];
      for (const f of (this.ports[i] ?? '').split(/[,; ]+/).filter(Boolean)) { const [p, v] = f.split(':').map(Number); if (!p || !v) throw new Error(tr('{0} is not port:vm-port', f)); ports.push({ port: p, vif: v }); }
      sl.push({ ...x, num: i + 1, ports: this.slots().length > 1 ? ports : null });
    }
    return { slots: sl, values: { ...this.pvals } };
  }
  preview() {
    if (!this.pd()) return;
    if (this.timer) clearTimeout(this.timer);
    this.timer = setTimeout(async () => {
      try { this.yaml.set(await this.backend.emuPersonaPreview(this.devId(), this.persona())); } catch (e) { this.yaml.set(errText(e)); }
    }, 250);
  }
  async save(apply: boolean) {
    const d = this.dev(); if (!d) return;
    try {
      if (this.emu() && (+this.cpus !== d.cpus || +this.ram !== d.ram || +this.disk !== (d.diskGb || 0))) { if (!(await this.lab.setResources(d.id, +this.cpus || 1, +this.ram || 1024, +this.disk || 0))) return; }
      if (this.pd()) {
        if (!(await this.lab.setPersona(d.id, this.persona()))) return;
        if (this.mac !== d.mac && !(await this.lab.setIdentity(d.id, this.slots()[0]?.serial ?? d.serial, this.mac))) return;
      } else if ((this.serial !== d.serial || this.mac !== d.mac) && !(await this.lab.setIdentity(d.id, this.serial, this.mac))) return;
      if (apply && !(await this.lab.applyPersona([d.id]))) return;
      this.closed.emit();
    } catch (e) { this.ui.error(errText(e)); }
  }
}

// ---------------------------------------------------------------- New Lab / Add Switches (side panel)
interface NRow { key: string; count: number; names: string; module: string; serial: string; mac: string; }
/** New Lab and Add Switches as a side panel; the canvas shows what it makes. */
@Component({
  selector: 'app-lab-new',
  imports: [FormsModule],
  template: `
  <div class="dockp">
    <div class="mh"><div><h2>{{ exists() ? tr('Add Switches') : tr('New Lab') }}</h2><div class="muted small">{{ tr('Choices here, the lab on the canvas') }}</div></div><span class="sp"></span><button type="button" class="sm" (click)="closed.emit()">✕</button></div>
    <div class="body">
      @if (emu()) {
        <div class="sec">{{ tr('Runs on') }}@if (exists()) { <span class="muted small"> · {{ tr('another host: its cables to this lab become UDP tunnels') }}</span> } <a class="lnk small" (click)="hostsPage.emit()">+ {{ tr('Add a host') }}</a></div>
        @for (h of shownHosts(); track h.session) {
          <div class="hc" [class.on]="host === h.session" [class.off]="!!h.info.problems?.length" (click)="!h.info.problems?.length && pickHost(h.session)">
            <div class="row2"><i class="d" [class]="'d ' + ((h.info.problems ?? []).length ? 'bad' : 'ok')"></i><b>{{ h.name || h.session }}</b><span class="muted small">{{ isVM(h) ? tr('VM ({0})', h.info.virt ?? '') : tr('hardware') }}{{ h.info.kvm === 'yes' ? ' · KVM' : ' · ' + tr('no KVM') }}</span>@if (inLab(h.session)) { <span class="badge">{{ tr('in the lab') }}</span> }@if (host === h.session) { <span class="badge g">{{ tr('chosen') }}</span> }</div>
            <div class="res"><span>vCPU <b>{{ h.info.cpus }}</b> <small>{{ tr('load {0}', h.info.load) }}</small></span><span>RAM <b>{{ gb(h.info.memFree) }}</b> <small>{{ tr('free of {0}', gb(h.info.memTotal)) }}</small></span><span>{{ tr('Disk') }} <b>{{ gb(h.info.diskFree) }}</b> <small>{{ tr('free') }}</small></span></div>
            <div class="meter"><i [style.width.%]="pct(h.info.memTotal - h.info.memFree, h.info.memTotal)"></i></div>
            @if ((h.info.problems ?? []).length) { <span class="r small">{{ h.info.problems![0] }}</span> }
          </div>
        } @empty { <a class="lnk" (click)="hostsPage.emit()">+ {{ tr('Add a KVM host first') }}</a> }
      }
      @if (emu() && (!exists() || !inLab(host))) {
        <div class="f" style="margin-top: 10px">
          <label>{{ tr('Management') }}</label>
          <select [(ngModel)]="mgmt" name="nm">
            <option value="cloud" [disabled]="!info()?.sudo || !info()?.lanAddr">{{ tr('The host’s network, routed (cloud)') }}</option>
            <option value="nat" [disabled]="!info()?.sudo || (!info()?.nat && !info()?.libvirt)">{{ tr('The host’s NAT network') }}{{ best() === 'nat' ? ' ✓' : '' }}</option>
            <option value="lan" [disabled]="!info()?.sudo || !info()?.lanKind">{{ tr('The host’s network (bridged)') }}{{ best() === 'lan' ? ' ✓' : '' }}</option>
            <option value="bridge" [disabled]="!info()?.sudo">{{ tr('Lab bridge on the host') }}{{ best() === 'bridge' ? ' ✓' : '' }}</option>
            <option value="forward">{{ tr('Port forwarding on the host') }}{{ best() === 'forward' ? ' ✓' : '' }}</option>
          </select>
        </div>
        <p class="muted small hint">{{ mgmtText() }}</p>
        @if (mgmt === 'cloud' || mgmt === 'lan') {
          <div class="f"><label>{{ tr('First switch’s address') }}</label><input class="mono" [(ngModel)]="cloudAddr" name="nca" [placeholder]="mgmt === 'lan' ? tr('empty: from a DHCP server') : info()?.lanAddr?.split('/')?.[0] ?? ''" /></div>
          <div class="f"><label>{{ tr('Gateway') }}</label><input class="mono" [(ngModel)]="cloudGw" name="ncg" [placeholder]="info()?.gateway ?? ''" /></div>
        }
      }
      <div class="sec">{{ tr('Switches') }}</div>
      @for (r of rows(); track $index; let i = $index) {
        <div class="card">
          <div class="row2"><select [ngModel]="r.key" (ngModelChange)="patch(i, { key: $event, module: '', names: nameFor($event) }); suggest(i)" [name]="'nk' + i">@for (o of options(); track o.key) { <option [value]="o.key">{{ o.label }}</option> }</select>
            <span class="cnt"><button type="button" class="sm" (click)="patch(i, { count: Math.max(0, r.count - 1) })">−</button><b>{{ r.count }}</b><button type="button" class="sm" (click)="patch(i, { count: r.count + 1 })">+</button></span>
            <a class="x" (click)="drop(i)" [title]="tr('Remove')">✕</a></div>
          <div class="row2" style="margin-top: 6px"><input [ngModel]="r.names" (ngModelChange)="patch(i, { names: $event })" [name]="'nn' + i" [title]="tr('Names: {n} is the number')" />
            @if (modules(r.key).length) { <select [ngModel]="r.module" (ngModelChange)="patch(i, { module: $event })" [name]="'nx' + i"><option value="">{{ tr('no module') }}</option>@for (m of modules(r.key); track m) { <option [value]="m">{{ m }}</option> }</select> }</div>
          @if (emu()) {
            <div class="row2 sm2" style="margin-top: 6px"><span class="lb">{{ tr('Serial from') }}</span><input class="mono" [ngModel]="r.serial" (ngModelChange)="patch(i, { serial: $event })" [name]="'ns' + i" /><button type="button" class="sm" (click)="suggest(i)" [title]="tr('New in the model’s format')">↻</button></div>
            <div class="row2 sm2" style="margin-top: 4px"><span class="lb">{{ tr('MAC from') }}</span><input class="mono" [ngModel]="r.mac" (ngModelChange)="patch(i, { mac: $event })" [name]="'nm' + i" /><button type="button" class="sm" (click)="suggest(i)" [title]="tr('New')">↻</button></div>
          }
        </div>
      }
      <button type="button" class="wide" (click)="more()" [disabled]="!options().length">+ {{ tr('Add a device') }}</button>
      @if (!options().length) { <p class="muted small">{{ emu() ? tr('No images yet: Images → Add Image.') : tr('Load a device pack first.') }}</p> }
      <div class="sec">{{ tr('Cabling') }}</div>
      <span class="seg">@for (c of cablings; track c.v) { <a [class.on]="cable === c.v" (click)="cable = c.v; changed.emit(plan())">{{ tr(c.label) }}</a> }</span>
      @if (emu() && info(); as h) {
        <div class="card soft" style="margin-top: 16px"><div class="row2" style="justify-content: space-between"><span>vCPU {{ need().cpus }} / {{ h.cpus }}</span><span>RAM {{ gb(need().ram) }} / {{ gb(h.memFree) }} {{ tr('free') }}</span></div>
          <div class="meter"><i [style.width.%]="pct(need().ram, h.memFree)" [style.background]="need().ram > h.memFree ? 'var(--danger)' : ''"></i></div>
          @if (need().ram > h.memFree) { <span class="r small">{{ tr('The switches need more memory than the host has free.') }}</span> }</div>
      }
    </div>
    <div class="mf"><label class="small"><input type="checkbox" [(ngModel)]="start" name="nst" /> {{ tr('Start now') }}</label><span class="sp"></span>
      <button type="button" (click)="closed.emit()">{{ tr('Cancel') }}</button>
      <button type="button" class="primary" (click)="create()" [disabled]="!total() || lab.busy() || (emu() && !host)">{{ tr('Create {0} Switches', total()) }}</button></div>
  </div>`,
  styles: [PAGE + `
    :host { position: absolute; right: 0; top: 0; bottom: 0; width: 420px; z-index: 20; }
    .dockp { position: absolute; inset: 0; background: var(--surface); border-left: 1px solid var(--line); box-shadow: -12px 0 30px rgba(0,0,0,.1); z-index: 20; display: grid; grid-template-rows: 64px minmax(0, 1fr) 60px; }
    .body { padding: 14px 18px; overflow: auto; } .f { grid-template-columns: 100px minmax(0, 1fr); } .hint { margin: -6px 0 6px; }
    .cnt { flex: none !important; display: inline-flex; align-items: center; gap: 8px; b { min-width: 18px; text-align: center; } }
    .x { flex: none !important; cursor: pointer; color: var(--muted); padding: 0 4px; } .wide { width: 100%; } .lnk { color: var(--accent); cursor: pointer; font-weight: 400; }
    .hc { border: 1px solid var(--line); border-radius: 10px; padding: 10px 12px; margin-bottom: 8px; cursor: pointer; background: var(--surface);
      &.on { border-color: var(--accent); box-shadow: 0 0 0 3px var(--accent-soft); } &.off:not(.on) { opacity: .55; cursor: default; } .row2 > * { flex: none; } .row2 > b { flex: 1; }
      .res { display: flex; gap: 14px; margin-top: 6px; font-size: 12px; small { color: var(--muted); } } }
    .sm2 .lb { flex: none; width: 76px; font-size: 12px; color: var(--muted); } .sm2 input { height: 26px; font-size: 12px; }
  `],
})
export class LabNew implements OnInit {
  readonly emu = input(true);
  /** A device to start with (dropped on an empty canvas). */
  readonly preset = input('');
  readonly closed = output<void>();
  readonly hostsPage = output<void>();
  /** The names and cabling it would make (for the canvas preview). */
  readonly changed = output<{ names: string[]; cable: string }>();
  readonly made = output<void>();
  protected lab = inject(LabService);
  private backend = inject(Backend);
  private ui = inject(Ui);
  protected tr = tr;
  protected Math = Math;
  protected cablings = [{ v: 'leaf-spine', label: 'Leaf-spine' }, { v: 'chain', label: 'Chain' }, { v: 'ring', label: 'Ring' }, { v: 'none', label: 'None' }];
  protected st = computed(() => (this.emu() ? this.lab.emu() : this.lab.sim()));
  protected exists = computed(() => (this.st()?.devices ?? []).length > 0);
  protected hosts = computed<EmuHost[]>(() => this.lab.emu()?.hosts ?? []);
  protected host = '';
  protected mgmt = 'forward'; protected cloudAddr = ''; protected cloudGw = '';
  protected cable = 'leaf-spine';
  protected start = true;
  protected rows = signal<NRow[]>([]);
  protected info = computed(() => this.hostsInfo());
  private hostsInfo = signal<EmuHost['info'] | null>(null);
  protected options = computed(() => {
    if (!this.emu()) return (this.lab.sim()?.models ?? []).map(m => ({ key: m.sku + '|' + m.os[0], label: m.sku + ' · ' + this.lab.osShort(m.os[0]) }));
    const ims = this.lab.emu()?.images ?? [];
    return (this.lab.emu()?.deviceTypes ?? []).filter(d => d.images?.length).map(d => { const im = ims.find(i => i.id === d.images[0]); return { key: d.key, label: (d.model || im?.name || d.kind) + (d.osName ? ' · ' + d.osName : '') + (im?.version ? ' ' + im.version : '') }; });
  });
  ngOnInit() {
    const ok = this.hosts().filter(h => !(h.info.problems ?? []).length).sort((a, b) => (b.info.memFree || 0) - (a.info.memFree || 0));
    this.host = this.exists() ? this.lab.emu()?.host ?? '' : (ok.find(h => h.session === this.lab.emu()?.host) ?? ok[0])?.session ?? '';
    this.pickMgmt();
    if (this.exists()) this.cable = 'chain';
    const pre = this.options().find(o => o.key === this.preset());
    const first = pre ? [pre] : this.options().slice(0, this.exists() ? 1 : 2);
    this.rows.set(first.map((o, i) => ({ key: o.key, count: pre || this.exists() ? 1 : i === 0 ? 2 : 3, names: this.nameFor(o.key), module: '', serial: '', mac: '' })));
    this.rows().forEach((_, i) => void this.suggest(i));
    this.changed.emit(this.plan());
  }
  protected shownHosts = computed(() => {
    const l = [...this.hosts()];
    return l.sort((a, b) => (b.info.memFree || 0) - (a.info.memFree || 0)); // most free memory first
  });
  isVM(h: EmuHost) { return !!h.info.virt && h.info.virt !== 'none' && h.info.virt !== 'unknown'; }
  /** The host runs switches of the lab already (its management is set). */
  inLab(s: string) { const v = this.lab.emu(); return !!v?.exists && (s === v.host || (v.hostUse ?? []).some(u => u.host === s && u.switches > 0)); }
  pickHost(s: string) { this.host = s; this.pickMgmt(); }
  async suggest(i: number) {
    const r = this.rows()[i]; if (!r || !this.emu()) return;
    const d = this.dt(r.key);
    const s = await this.backend.emuSuggest(d?.model || d?.kind || '').catch(() => null);
    if (s) this.rows.set(this.rows().map((x, j) => (j === i ? { ...x, serial: s.serial || x.serial, mac: s.mac || x.mac } : x)));
  }
  pickMgmt() {
    const h = this.hosts().find(x => x.session === this.host)?.info ?? null;
    this.hostsInfo.set(h);
    this.mgmt = this.best();
  }
  best() {
    const h = this.hostsInfo(); if (!h) return 'forward';
    const vm = !!h.virt && h.virt !== 'none' && h.virt !== 'unknown';
    return h.sudo && h.lanKind && !vm ? 'lan' : h.sudo && h.nat ? 'nat' : h.sudo && h.lanKind ? 'lan' : h.sudo ? 'bridge' : 'forward';
  }
  mgmtText() {
    switch (this.mgmt) {
      case 'nat': return tr('Like GNS3’s NAT node: the switches get addresses from the host’s DHCP ({0}), reach out through the host, and are reached through the host. Works with any image.', this.hostsInfo()?.nat || 'virbr0');
      case 'lan': return tr('The switches join the host’s network and get addresses from its DHCP server.');
      case 'bridge': return tr('The switches get fixed addresses on a bridge of their own on the host, reached through the host.');
      case 'cloud': return tr('Like GNS3’s cloud: the switches get fixed addresses on the host’s network {0} (from the first one on) and are reached from it and reach it with any protocol. The host answers for them, so nothing has to be allowed on a VM host’s virtual switch.', this.hostsInfo()?.lanAddr ?? '');
      default: return tr('Each switch’s SSH is forwarded to a port on the host (no root needed).');
    }
  }
  dt(key: string): EmuDeviceType | undefined { return (this.lab.emu()?.deviceTypes ?? []).find(d => d.key === key); }
  modules(key: string) { return this.emu() ? this.dt(key)?.modules ?? [] : []; }
  nameFor(key: string) {
    const base = this.emu() ? (this.dt(key)?.model || this.dt(key)?.kind || 'sw') : key.split('|')[0];
    return base.toLowerCase().replace(/[^a-z0-9]+/g, '-').replace(/^-|-$/g, '') + '-{n}';
  }
  patch(i: number, p: Partial<NRow>) { this.rows.set(this.rows().map((r, j) => (j === i ? { ...r, ...p } : r))); this.changed.emit(this.plan()); }
  more() { const o = this.options()[0]; if (o) { this.rows.set([...this.rows(), { key: o.key, count: 1, names: this.nameFor(o.key), module: '', serial: '', mac: '' }]); void this.suggest(this.rows().length - 1); this.changed.emit(this.plan()); } }
  drop(i: number) { this.rows.set(this.rows().filter((_, j) => j !== i)); this.changed.emit(this.plan()); }
  protected total = computed(() => this.rows().reduce((n, r) => n + r.count, 0));
  protected need = computed(() => {
    let cpus = 0, ram = 0;
    for (const r of this.rows()) { const d = this.dt(r.key); cpus += (d?.cpus || 1) * r.count; ram += (d?.ram || 1024) * r.count; }
    return { cpus, ram };
  });
  gb(mb?: number) { return !mb ? '-' : mb >= 1024 ? (mb / 1024).toFixed(mb % 1024 ? 1 : 0) + ' GB' : mb + ' MB'; }
  pct(a: number, b: number) { return b ? Math.min(100, (a / b) * 100) : 0; }
  plan() {
    const used = new Set((this.st()?.devices ?? []).map(d => d.hostname.toLowerCase())), names: string[] = [];
    for (const r of this.rows()) {
      let n = 1;
      for (let k = 0; k < Math.min(r.count, 24); k++) {
        let name = '';
        do { name = r.names.includes('{n}') ? r.names.replace('{n}', String(n)) : r.names + '-' + n; n++; } while (used.has(name.toLowerCase()));
        used.add(name.toLowerCase()); names.push(name);
      }
    }
    return { names, cable: this.cable };
  }
  async create() {
    let ok = false;
    if (this.emu()) {
      const groups = [];
      for (const r of this.rows().filter(x => x.count > 0)) {
        const d = this.dt(r.key); if (!d) continue;
        groups.push({ image: d.images[0], model: d.model, module: r.module, count: r.count, names: r.names, serialStart: r.serial.trim(), macStart: r.mac.trim(), cpus: d.cpus, ram: d.ram });
      }
      ok = await this.lab.addEmu({ host: this.host, groups, cabling: this.cable, mgmt: this.exists() && this.inLab(this.host) ? '' : this.mgmt, mgmtNet: this.mgmt === 'cloud' || (this.mgmt === 'lan' && this.cloudAddr.trim()) ? (this.cloudAddr.trim() + ' ' + (this.cloudGw.trim() || this.hostsInfo()?.gateway || '')).trim() : '', start: this.start });
    } else {
      ok = await this.lab.add({ groups: this.rows().filter(x => x.count > 0).map(r => { const [model, os] = r.key.split('|'); return { os, model, count: r.count, names: r.names }; }), mgmtNet: '', cabling: this.cable, password: '', start: this.start });
    }
    if (ok) { this.made.emit(); this.closed.emit(); }
    void this.ui;
  }
}

// ---------------------------------------------------------------- Images and Templates
@Component({
  selector: 'app-lab-images',
  imports: [FormsModule, LabHostImages],
  template: `
  <div class="ov" (click)="closed.emit()"></div>
  <div class="mdl">
    <div class="mh"><div><h2>{{ tr('Images and Templates') }}</h2><div class="muted small">{{ tr('The switch images Termwright can boot, what it knows about each, and the devices made from them') }}</div></div><span class="sp"></span><button type="button" class="sm" (click)="closed.emit()">✕</button></div>
    <div class="mt"><a [class.on]="tab() === 'images'" (click)="tab.set('images')">{{ tr('Images ({0})', images().length) }}</a><a [class.on]="tab() === 'templates'" (click)="tab.set('templates')">{{ tr('Templates ({0})', templates().length) }}</a><a [class.on]="tab() === 'profiles'" (click)="tab.set('profiles')">{{ tr('Profiles from the device pack') }}</a><a [class.on]="tab() === 'onhosts'" (click)="tab.set('onhosts')">{{ tr('On the Hosts') }}</a><a [class.on]="tab() === 'public'" (click)="openPublic()">{{ tr('Public appliances') }}</a></div>
    <div class="mb" style="display: grid; grid-template-columns: 320px minmax(0, 1fr)">
      @switch (tab()) {
        @case ('images') {
          <div class="lst">
            @if (queue().length) {
              <div class="dq">
                <div class="dqh"><b>{{ tr('Downloads') }}</b><span class="muted small">{{ tr('{0} waiting', queue().length - 1) }}</span><span class="sp"></span>@if (queue().length > 1) { <a class="lnk small" (click)="stopAll()">{{ tr('Stop all') }}</a> }</div>
                @for (q of queue(); track q.id; let i = $index) {
                  <div class="dqi" [class.cur]="i === 0" (click)="pick(q.id)">
                    <div class="dqn"><span>{{ q.name }} {{ q.version }}</span><small>{{ i === 0 ? tr('downloading {0}', q.state) : tr('waiting · #{0}', i) }}</small>
                      @if (i === 0) { <div class="meter"><i [style.width]="pctOf(q.state) + '%'"></i></div> }</div>
                    @if (i > 0) { <button type="button" class="xs" [title]="tr('Next')" (click)="move(q.id, 'first'); $event.stopPropagation()">⇡</button>
                      <button type="button" class="xs" [title]="tr('Earlier')" (click)="move(q.id, 'up'); $event.stopPropagation()" [disabled]="i === 1">↑</button>
                      <button type="button" class="xs" [title]="tr('Later')" (click)="move(q.id, 'down'); $event.stopPropagation()" [disabled]="i === queue().length - 1">↓</button> }
                    <button type="button" class="xs" [title]="i === 0 ? tr('Stop this download') : tr('Take it out of the queue')" (click)="stop(q.id); $event.stopPropagation()">✕</button>
                  </div>
                }
              </div>
            }
            @else if (toDownload().length) { <button type="button" class="primary" style="width: 100%; margin-bottom: 6px" (click)="lab.download(toDownload())" [disabled]="lab.busy()">⤓ {{ tr('Download All ({0})', toDownload().length) }}</button> }
            @else if (downloaded()) { <button type="button" class="dlb done" style="width: 100%; margin-bottom: 6px" disabled>✓ {{ tr('All downloaded ({0})', downloaded()) }}</button> }
            @if (images().some(i => i.url)) { <p class="muted small" style="margin: 0 0 6px">{{ tr('Downloads go to {0}', lab.emu()?.downloadDir ?? '') }}</p> }
            @for (g of groups(); track g.name) {
              <div class="grp">{{ g.name }}</div>
              @for (im of g.items; track im.id) {
                <div class="it" [class.on]="edit()?.id === im.id" (click)="pick(im.id)"><div><span>{{ im.version || im.name }}</span>@if (im.persona) { <span class="badge g">{{ tr('persona') }}</span> }
                  <small class="mono">{{ im.fileName || tr('no file chosen') }}</small><small>{{ im.cpus }} vCPU · {{ gb(im.ram) }} RAM · {{ diskName(im.disk) }} · {{ mb(im.size) }}</small><small [class.w]="uploading(im) || !!dl(im)" [class.r]="!im.file && !im.url" [class.g]="!!im.file && !!im.url && !dl(im) && !uploading(im)">{{ dl(im) ? (dl(im) === 'waiting' ? tr('waiting to download') : tr('downloading {0}', dl(im))) : !im.file && im.url ? tr('not downloaded yet') : (im.url ? '✓ ' + tr('downloaded') + ' · ' : '') + where(im) }}</small>@if (dl(im) && dl(im) !== 'waiting') { <div class="meter"><i [style.width]="pctOf(dl(im)) + '%'"></i></div> }</div></div>
              }
            } @empty { <p class="muted small">{{ tr('No images yet.') }}</p> }
            <div class="drop" (click)="addImage()"><b style="color: var(--text)">+ {{ tr('Add Image…') }}</b><br />.qcow2 / .img / .vmdk / .raw · {{ tr('recognised by its name with the device pack’s patterns') }}</div>
            <div class="row2" style="margin-top: 8px"><button type="button" class="sm" (click)="importAppliance()">{{ tr('GNS3 Appliance…') }}</button><button type="button" class="sm" (click)="importProfiles()">{{ tr('Import Profiles…') }}</button></div>
          </div>
          <div class="pad" style="overflow: auto">
            @if (edit(); as im) {
              <div class="row2" style="justify-content: flex-start"><div style="flex: 1"><b style="font-size: 15px">{{ im.name }}@if (im.version) { · {{ im.version }} }</b><div class="muted small mono">{{ im.fileName }} · {{ mb(im.size) }}@if (im.sha256) { · sha256 {{ im.sha256.slice(0, 4) }}…{{ im.sha256.slice(-4) }} }</div></div>
                @if (hasLinks(im)) {
                  @if (!needs(im)) { <button type="button" class="dlb done" disabled>✓ {{ tr('Downloaded') }}</button> }
                  @else if (dl(im)) { <button type="button" class="dlb busy" disabled [style.--p]="pctOf(dl(im)) + '%'"><span>{{ dl(im) === 'waiting' ? tr('Waiting…') : tr('Downloading {0}', dl(im)) }}</span></button> }
                  @else { <button type="button" class="primary" (click)="lab.download([im.id])" [disabled]="lab.busy()">⤓ {{ tr('Download ({0})', mb(dlSize(im))) }}</button> }
                }
                <span class="pill">{{ tr('Used by {0} switches · {1} templates', usedBy(im.id), tplBy(im.id)) }}</span></div>
              <div class="sec">{{ tr('Files') }}<span class="muted small">{{ tr('Downloads go to {0}', lab.emu()?.downloadDir ?? '') }}</span></div>
              <table class="t files"><thead><tr><th>{{ tr('Use') }}</th><th>{{ tr('File') }}</th><th>{{ tr('Size') }}</th><th>{{ tr('On this computer') }}</th><th></th></tr></thead><tbody>
                @for (f of filesOf(im); track f.slot) {
                  <tr><td>{{ slotName(f.slot) }}</td><td class="mono" [title]="f.url || f.page || ''">{{ f.fileName || '-' }}</td><td>{{ mb(f.size) }}</td>
                    <td [class.g]="!!f.file" [class.r]="!f.file && !f.url" class="small">{{ f.file ? '✓ ' + f.file : f.url ? tr('downloads with Download') : tr('get it from the maker, then Choose…') }}</td>
                    <td class="acts">@if (!f.file && !f.url && f.page) { <a class="lnk small" (click)="openPage(f.page)">{{ tr('Download page') }}</a> } <button type="button" class="sm" (click)="chooseSlot(im.id, f.slot)">{{ tr('Choose…') }}</button></td></tr>
                }
              </tbody></table>
              <div class="sec">{{ tr('Boot') }}</div>
              <div class="f3">
                <label>{{ tr('Name') }}</label><input [(ngModel)]="im.name" name="iname" /><label>{{ tr('Version') }}</label><input [(ngModel)]="im.version" name="iver" />
                <label>vCPU</label><input type="number" min="1" [(ngModel)]="im.cpus" name="icpu" /><label>{{ tr('Memory') }}</label><div class="row2"><input type="number" min="256" step="256" [(ngModel)]="im.ram" name="iram" /><span class="unit">MB = {{ gb(im.ram) }}</span></div>
                <label>{{ tr('Disk type') }}</label><select [(ngModel)]="im.disk" name="idisk">@for (b of buses(im.disk); track b.v) { <option [value]="b.v">{{ b.l }}</option> }</select>
                <label>{{ tr('Disk size') }}</label><span class="muted small">{{ tr('the image’s own size; a template or a switch can make it bigger') }}</span>
                <label>{{ tr('Network ports') }}</label><input type="number" min="1" [(ngModel)]="im.nics" name="inics" /><label>{{ tr('Adapter type') }}</label><select [(ngModel)]="im.nic" name="inic">@for (n of nics(im.nic); track n) { <option [value]="n">{{ n }}</option> }</select>
                <label>{{ tr('Port names') }}</label><input class="mono" [(ngModel)]="im.portName" name="ipn" placeholder="{n} or 1/{n}" /><span></span><span></span>
              </div>
              <div class="sec">{{ tr('Console and first login') }}<span class="muted small">{{ tr('how Termwright logs in on the serial console') }}</span></div>
              <div class="f3">
                <label>{{ tr('Ready when') }}</label><input class="mono" [(ngModel)]="im.ready" name="irdy" /><label>{{ tr('Login') }}</label><div class="row2"><input [(ngModel)]="im.login" name="ilog" /><input type="password" [(ngModel)]="password" name="ipw" [placeholder]="tr('password (keychain)')" autocomplete="off" /></div>
                <label>{{ tr('Answers') }}</label><textarea class="wide" rows="2" [(ngModel)]="answers" name="ians" [placeholder]="'[y/N]: => n'"></textarea>
                <label>{{ tr('Day 0') }}</label><textarea class="wide" rows="3" [(ngModel)]="day0" name="id0" placeholder="configure snmp sysName {hostname}"></textarea>
                <label>{{ tr('Set address') }}</label><textarea class="wide mono" rows="2" [(ngModel)]="addrSet" name="iads" [placeholder]="'configure vlan Mgmt ipaddress {mgmtAddr}/{mgmtBits}'" [title]="tr('Typed at each start when the switches have fixed addresses (the host’s network routed, or a lab bridge): {mgmtAddr} {mgmtBits} {mgmtMask} {gateway}')"></textarea>
                <label>{{ tr('Address command') }}</label><input class="mono" [(ngModel)]="im.addrCmd" name="iadr" /><label>{{ tr('Device type') }}</label><input [(ngModel)]="im.deviceType" name="idt" />
                <label>{{ tr('More QEMU options') }}</label><input class="mono wide" [(ngModel)]="im.extra" name="iex" placeholder="-cpu host -machine q35" />
                <span></span><p class="wide muted small" style="margin: 0">{{ tr('Added to the QEMU command line as they are, for images that need them. Examples: -cpu host (the host CPU’s features), -cpu Nehalem, -machine q35, -smp sockets=1,cores=2, -device virtio-rng-pci, -boot order=c, -rtc base=utc. Termwright sets the disk, network ports, console, memory and vCPU itself.') }}</p>
              </div>
              @if (im.persona; as p) {
                <div class="sec">{{ tr('Persona') }} <span class="badge g">{{ p.name }}</span></div>
                <div class="f3">
                  <label>{{ tr('File in the VM') }}</label><span class="mono">{{ p.file }}</span><label>{{ tr('Check after boot') }}</label><input class="mono" [(ngModel)]="p.check" name="ichk" />
                  <label>{{ tr('Models') }}</label><div class="wide">@for (m of p.models ?? []; track m.name) { <span class="pill">{{ m.name }}</span> }</div>
                  <label>{{ p.modules || tr('Modules') }}</label><div class="wide">@for (m of mods(p); track m) { <span class="pill">{{ m }}</span> }@if (p.stack) { <span class="muted small">· {{ tr('stacks') }}</span> }</div>
                </div>
              }
            } @else { <p class="muted">{{ tr('Pick an image on the left, or add one.') }}</p> }
          </div>
        }
        @case ('templates') {
          <div class="lst">
            @for (t of templates(); track t.id) { <div class="it" [class.on]="tpl()?.id === t.id" (click)="pickTpl(t)"><div><span>{{ t.name }}</span><small>{{ imageName(t.image) }}</small><small>{{ t.cpus }} vCPU · {{ gb(t.ram) }} RAM · {{ t.diskGb ? t.diskGb + ' GB ' + tr('disk') : tr('disk of the image') }}@if (t.module) { · {{ t.module }} }</small></div></div> }
            @empty { <p class="muted small">{{ tr('No templates yet.') }}</p> }
            <button type="button" class="primary" style="width: 100%; margin-top: 10px" (click)="newTpl()" [disabled]="!images().length">+ {{ tr('New Template') }}</button>
          </div>
          <div class="pad">
            @if (tpl(); as t) {
              <div class="f">
                <label>{{ tr('Name') }}</label><input [(ngModel)]="t.name" name="tn" placeholder="M-48" />
                <label>{{ tr('Image') }}</label><select [ngModel]="t.image" (ngModelChange)="tplImage($event)" name="ti">@for (im of images(); track im.id) { <option [value]="im.id">{{ im.name }}{{ im.version ? ' · ' + im.version : '' }}</option> }</select>
                @if (tplDef(); as p) {
                  <label>{{ tr('Model') }}</label><select [ngModel]="t.model" (ngModelChange)="t.model = $event; suggestTpl(true, true)" name="tm"><option value="">-</option>@for (m of p.models ?? []; track m.name) { <option [value]="m.name">{{ m.name }}</option> }</select>
                  <label>{{ p.modules || tr('Module') }}</label><select [(ngModel)]="t.module" name="tx"><option value="">{{ tr('none') }}</option>@for (m of tplMods(); track m) { <option [value]="m">{{ m }}</option> }</select>
                }
                <label>vCPU</label><input type="number" min="1" [(ngModel)]="t.cpus" name="tc" style="max-width: 120px" />
                <label>{{ tr('Memory') }}</label><div class="row2" style="max-width: 360px"><input type="number" min="256" step="256" [(ngModel)]="t.ram" name="tr" /><span class="unit">MB = {{ gb(t.ram) }}</span></div>
                <label>{{ tr('Disk') }}</label><div class="row2" style="max-width: 460px"><input type="number" min="0" [(ngModel)]="t.diskGb" name="td" /><span class="unit">GB · {{ tr('0 = the image’s size') }} · {{ diskName(imageOf(t.image)?.disk) }}</span></div>
                <label>{{ tr('First serial number') }}</label><div class="row2"><input class="mono" [(ngModel)]="t.serialStart" name="ts" /><button type="button" (click)="suggestTpl(true, false)">↻ {{ tr('New') }}</button></div>
                <label>{{ tr('First MAC address') }}</label><div class="row2"><input class="mono" [(ngModel)]="t.macStart" name="tmac" /><button type="button" (click)="suggestTpl(false, true)">↻ {{ tr('New') }}</button></div>
              </div>
              <p class="muted small">{{ tr('Used by {0} switches.', tplUse(t.id)) }}</p>
              <div class="row2" style="justify-content: flex-start"><button type="button" class="primary" (click)="saveTpl()" [disabled]="!t.name || !t.image || lab.busy()">{{ tr('Save') }}</button>@if (t.id) { <button type="button" class="danger-ghost" (click)="removeTpl()">{{ tr('Remove') }}</button> }</div>
            } @else { <p class="muted">{{ tr('A template is an image with resources, the model it acts as and where serial numbers and MACs start.') }}</p> }
          </div>
        }
        @case ('onhosts') { <app-lab-host-images style="grid-column: 1 / -1; min-height: 0" (hostsPage)="hostsPage.emit($event)" /> }
        @case ('public') {
          <div class="lst">
            <p class="muted small">{{ tr('Ready-made settings from the public GNS3 registry, with their second disks, CD-ROMs and BIOS files. Add one: its versions join the image list; then download the files (or choose them when the maker wants you to sign in).') }}</p>
            @for (g of catGroups(); track g) { <div class="grp">{{ g }}</div><div class="muted small">{{ catCount(g) }}</div> }
          </div>
          <div class="pad">
            @for (g of catGroups(); track g) {
              <div class="sec">{{ g }}</div>
              <div class="catg">
                @for (c of catIn(g); track c.file) {
                  <div class="card"><div class="row2"><b>{{ c.name }}</b>@if (c.added) { <span class="badge g">{{ tr('added') }}</span> }</div>
                    <p class="muted small" style="margin: 6px 0 8px">{{ c.note }}</p>
                    @if (c.added) { <button type="button" class="sm" (click)="showAdded(c.file)">{{ tr('Show') }}</button> <button type="button" class="sm danger-ghost" (click)="removeCat(c.file, c.name, c.count)" [disabled]="lab.busy()">{{ tr('Remove') }}</button> <span class="muted small">{{ tr('{0} images', c.count) }}</span> }
                    @else { <button type="button" class="sm primary" (click)="addCat(c.file, c.name)" [disabled]="lab.busy()">+ {{ tr('Add') }}</button> }</div>
                }
              </div>
            } @empty { <p class="muted">{{ catErr() || tr('Loading…') }}</p> }
            <p class="muted small">{{ tr('Container appliances are not in the list: they do not run on a KVM host.') }}</p>
          </div>
        }
        @case ('profiles') {
          <div class="lst">
            <p class="muted small">{{ tr('An image file you add is recognised by its name; New Lab offers these devices.') }}</p>
            <p class="muted small">{{ tr('The device pack is loaded or changed in the Simulated Lab tab (the flask at the top right): Load Device Pack.') }}</p>
          </div>
          <div class="pad"><table class="t"><thead><tr><th>{{ tr('Device') }}</th><th>{{ tr('OS') }}</th><th>{{ tr('Modules') }}</th><th>{{ tr('Images') }}</th></tr></thead><tbody>
            @for (d of lab.state()?.deviceTypes ?? []; track d.key) { <tr><td>{{ d.model || d.kind }}</td><td>{{ d.osName || d.kind }}</td><td class="small">{{ (d.modules ?? []).join(', ') || '-' }}</td><td>{{ d.images.length || tr('no image yet') }}</td></tr> }
          </tbody></table></div>
        }
      }
    </div>
    <div class="mf">
      @if (tab() === 'images' && edit(); as im) {
        <button type="button" class="danger-ghost" (click)="removeImage(im)">{{ tr('Remove Image') }}</button><button type="button" (click)="lab.upload(im.id)" [disabled]="!im.file || lab.busy()">{{ tr('Copy to Host Now') }}</button>
        <span class="sp"></span><span class="muted small">{{ tr('Changes apply to switches when they start next') }}</span><button type="button" (click)="pick(im.id)">{{ tr('Revert') }}</button><button type="button" class="primary" (click)="saveImage()" [disabled]="lab.busy()">{{ tr('Save') }}</button>
      } @else { <span class="sp"></span><button type="button" class="primary" (click)="closed.emit()">{{ tr('Close') }}</button> }
    </div>
  </div>`,
  styles: [PAGE],
})
export class LabImages implements OnInit {
  readonly start = input<'images' | 'templates' | 'onhosts'>('images');
  readonly closed = output<void>();
  readonly hostsPage = output<string>();
  protected lab = inject(LabService);
  private backend = inject(Backend);
  private ui = inject(Ui);
  protected tr = tr;
  protected tab = signal<'images' | 'templates' | 'profiles' | 'public' | 'onhosts'>('images');
  protected cat = signal<{ file: string; name: string; group: string; note: string; added: boolean; count: number }[]>([]);
  protected queue = computed(() => (this.lab.emu()?.downloadQueue ?? []).map(id => { const im = this.images().find(i => i.id === id); return { id, name: im?.name ?? id, version: im?.version ?? '', state: im ? this.dl(im) : '' }; }));
  async move(id: string, where: string) { try { this.lab.emu.set(await this.backend.emuDownloadMove(id, where)); } catch (e) { this.ui.error(errText(e)); } }
  async stop(id: string) { try { this.lab.emu.set(await this.backend.emuDownloadStop(id)); } catch (e) { this.ui.error(errText(e)); } }
  async stopAll() { for (const q of [...this.queue()].reverse()) await this.stop(q.id); }
  async removeCat(file: string, name: string, n: number) {
    if (!(await this.ui.confirm(tr('Remove {0}', name), tr('Take the {0} images of {1} out of the list and delete the files Termwright downloaded for them? Files you chose yourself stay.', n, name), tr('Remove'), true))) return;
    try { this.lab.emu.set(await this.backend.emuRemoveFromCatalog(file, true)); this.cat.set(await this.backend.emuCatalog()); this.edit.set(null); } catch (e) { this.ui.error(errText(e)); }
  }
  protected catErr = signal('');
  async openPublicRefresh() { try { this.cat.set(await this.backend.emuCatalog()); } catch { /* keep */ } }
  async openPublic() { this.tab.set('public'); try { this.cat.set(await this.backend.emuCatalog()); } catch (e) { this.catErr.set(errText(e)); } }
  catGroups() { return [...new Set(this.cat().map(c => c.group))]; }
  catIn(g: string) { return this.cat().filter(c => c.group === g); }
  catCount(g: string) { const l = this.catIn(g); return tr('{0} of {1} added', l.filter(c => c.added).length, l.length); }
  async addCat(file: string, name: string) {
    const before = new Set(this.images().map(i => i.id));
    if (await this.lab.addFromCatalog(file, name)) { this.cat.set(await this.backend.emuCatalog().catch(() => this.cat())); const n = this.images().find(i => !before.has(i.id)); if (n) { this.tab.set('images'); this.pick(n.id); } }
  }
  showAdded(file: string) { const im = this.images().find(i => (i as EmuImage).source === 'GNS3 registry ' + file); this.tab.set('images'); if (im) this.pick(im.id); }
  openPage(u: string) { this.backend.openURL(u); }
  protected images = computed(() => this.lab.emu()?.images ?? []);
  protected templates = computed(() => this.lab.emu()?.templates ?? []);
  protected edit = signal<EmuImage | null>(null);
  protected tpl = signal<EmuTemplate | null>(null);
  protected password = ''; protected day0 = ''; protected answers = ''; protected addrSet = '';
  ngOnInit() { this.tab.set(this.start()); const im = this.images()[0]; if (im) this.pick(im.id); }
  protected groups = computed(() => {
    const out: { name: string; items: EmuImage[] }[] = [];
    for (const im of this.images()) {
      const n = im.persona?.osName || im.deviceType || tr('Other');
      let g = out.find(x => x.name === n); if (!g) out.push(g = { name: n, items: [] });
      g.items.push(im);
    }
    return out;
  });
  mb(b: number) { return !b ? '-' : b >= 1 << 30 ? (b / (1 << 30)).toFixed(1) + ' GB' : Math.round(b / (1 << 20)) + ' MB'; }
  gb(mb?: number) { return !mb ? '-' : mb >= 1024 ? (mb / 1024).toFixed(mb % 1024 ? 1 : 0) + ' GB' : mb + ' MB'; }
  where(im: EmuImage) { const x = im as EmuImage & { onHost?: boolean; upload?: string; missing?: boolean }; return x.missing ? tr('the file is missing') : x.upload ? tr('copying to the host {0}', x.upload) : x.onHost ? tr('on the host') : tr('copied when a switch first starts'); }
  uploading(im: EmuImage) { return !!(im as EmuImage & { upload?: string }).upload; }
  usedBy(id: string) { return this.lab.devices().filter(d => d.image === id).length; }
  tplBy(id: string) { return this.templates().filter(t => t.image === id).length; }
  tplUse(id: string) { return this.lab.devices().filter(d => d.template === id).length; }
  imageName(id: string) { const im = this.images().find(i => i.id === id); return im ? im.name + (im.version ? ' · ' + im.version : '') : '-'; }
  mods(p: EmuPersonaDef) { return [...new Set((p.models ?? []).flatMap(m => m.modules ?? []))]; }
  pick(id: string) {
    const im = this.images().find(i => i.id === id); if (!im) return;
    this.edit.set({ ...im, persona: im.persona ? { ...im.persona } : im.persona }); this.password = '';
    this.day0 = (im.day0 ?? []).join('\n'); this.addrSet = (im.addrSet ?? []).join('\n'); this.answers = (im.answers ?? []).map(a => a.match + ' => ' + a.send).join('\n');
  }
  async saveImage() {
    const im = this.edit(); if (!im) return;
    const out: EmuImage = { ...im, password: this.password, day0: this.day0.split(/\r?\n/).filter(l => l.trim()), addrSet: this.addrSet.split(/\r?\n/).filter(l => l.trim()),
      answers: this.answers.split(/\r?\n/).filter(l => l.includes('=>')).map(l => { const i = l.indexOf('=>'); return { match: l.slice(0, i).trim(), send: l.slice(i + 2).trim() }; }),
      cpus: +im.cpus || 1, ram: +im.ram || 1024, nics: +im.nics || 1 };
    if (await this.lab.saveImage(out)) this.pick(im.id);
  }
  private filters = () => [{ displayName: tr('VM images'), pattern: '*.qcow2;*.img;*.vmdk;*.raw' }];
  async addImage() { const p = await this.backend.chooseOpenFile(tr('Add VM Image'), this.filters()).catch(() => ''); if (p && await this.lab.addImage(p)) { const l = this.images(); if (l.length) this.pick(l[l.length - 1].id); } }
  async chooseFile(id: string) { const p = await this.backend.chooseOpenFile(tr('VM image file'), this.filters()).catch(() => ''); if (p && await this.lab.setImageFile(id, p)) this.pick(id); }
  async importAppliance() { const p = await this.backend.chooseOpenFile(tr('Import GNS3 Appliance'), [{ displayName: tr('GNS3 appliances'), pattern: '*.gns3a' }]).catch(() => ''); if (p && await this.lab.importAppliance(p)) { const l = this.images(); if (l.length) this.pick(l[l.length - 1].id); } }
  async importProfiles() { const p = await this.backend.chooseOpenFile(tr('Import Image Profiles'), [{ displayName: tr('Image profiles'), pattern: '*.json' }]).catch(() => ''); if (p) await this.lab.importProfiles(p); }
  async removeImage(im: EmuImage) { if (await this.ui.confirm(tr('Remove image'), tr('Forget {0}? Its copy on the hosts stays.', im.name), tr('Remove'), true) && await this.lab.removeImage(im.id)) this.edit.set(null); }
  protected toDownload = computed(() => this.images().filter(i => this.needs(i) && !this.dl(i)).map(i => i.id));
  filesOf(im: EmuImage) { return [{ slot: '', fileName: im.fileName, file: im.file, size: im.size, url: im.url ?? '', page: im.page ?? '' }, ...(im.disks ?? []).map(d => ({ slot: d.slot, fileName: d.fileName, file: d.file, size: d.size, url: d.url ?? '', page: d.page ?? '' }))]; }
  needs(im: EmuImage) { return this.filesOf(im).some(f => !f.file && !!f.url); }
  hasLinks(im: EmuImage) { return this.filesOf(im).some(f => !!f.url); }
  dlSize(im: EmuImage) { return this.filesOf(im).filter(f => !f.file && f.url).reduce((n, f) => n + (f.size || 0), 0); }
  slotName(s: string) { return ({ '': tr('Disk'), hdb: tr('Second disk'), hdc: tr('Third disk'), hdd: tr('Fourth disk'), cdrom: 'CD-ROM', bios: 'BIOS' } as Record<string, string>)[s] ?? s; }
  async chooseSlot(id: string, slot: string) {
    if (!slot) { await this.chooseFile(id); return; }
    const p = await this.backend.chooseOpenFile(tr('Choose the file'), [{ displayName: tr('Disk, CD-ROM or BIOS files'), pattern: '*.qcow2;*.img;*.iso;*.fd;*.bin;*.vmdk;*.raw' }]).catch(() => '');
    if (p) { try { this.lab.emu.set(await this.backend.emuSetDiskFile(id, slot, p)); this.pick(id); } catch (e) { this.ui.error(errText(e)); } }
  }
  pctOf(p: string) { const m = /(\d+)\s*%/.exec(p); return Math.max(0, Math.min(100, m ? +m[1] : 0)); }
  protected downloading = computed(() => {
    const l = this.images().filter(i => this.dl(i));
    if (!l.length) return { n: 0, at: 0, pct: 0 };
    const all = this.images().filter(i => i.url && (this.dl(i) || i.file));
    const act = l.find(i => this.dl(i) !== 'waiting');
    const doneN = all.filter(i => i.file).length;
    return { n: all.length, at: Math.min(all.length, doneN + 1), pct: act ? this.pctOf(this.dl(act)) : 0 };
  });
  protected downloaded = computed(() => this.images().filter(i => this.hasLinks(i) && !this.needs(i)).length);
  dl(im: EmuImage) { return ((im as EmuImage & { download?: string }).download ?? '').replace('unpacking', this.tr('unpacking')); }
  imageOf(id: string) { return this.images().find(i => i.id === id); }
  diskName(b?: string) { return ({ ide: 'IDE', sata: 'SATA', scsi: 'SCSI', virtio: 'virtio', sd: 'SD' } as Record<string, string>)[b || 'ide'] ?? (b || 'IDE'); }
  buses(cur?: string) { const l = ['ide', 'sata', 'scsi', 'virtio']; if (cur && !l.includes(cur)) l.push(cur); return l.map(v => ({ v, l: this.diskName(v) + (v === 'ide' ? ' (' + tr('most network images') + ')' : v === 'virtio' ? ' (' + tr('fastest, needs drivers') + ')' : '') })); }
  nics(cur?: string) { const l = ['e1000', 'virtio-net-pci', 'vmxnet3', 'rtl8139', 'e1000e', 'i82559er']; if (cur && !l.includes(cur)) l.push(cur); return l; }
  async suggestTpl(serial: boolean, mac: boolean) {
    const t = this.tpl(); if (!t) return;
    const s = await this.backend.emuSuggest(t.model || this.imageOf(t.image)?.name || '').catch(() => null);
    if (!s) return;
    this.tpl.set({ ...t, serialStart: serial || !t.serialStart ? s.serial || t.serialStart : t.serialStart, macStart: mac || !t.macStart ? s.mac || t.macStart : t.macStart });
  }
  protected tplDef = computed(() => this.images().find(i => i.id === this.tpl()?.image)?.persona ?? null);
  tplMods() { return (this.tplDef()?.models ?? []).find(x => x.name === this.tpl()?.model)?.modules ?? []; }
  pickTpl(t: EmuTemplate) { this.tpl.set({ ...t }); this.pickTplFill(); }
  newTpl() { const im = this.images()[0]; this.tpl.set({ id: '', name: '', image: im?.id ?? '', cpus: im?.cpus || 1, ram: im?.ram || 2048, diskGb: 0, model: im?.persona?.models?.[0]?.name ?? '', module: '', values: {}, serialStart: '', macStart: '' }); void this.suggestTpl(true, true); }
  pickTplFill() { const t = this.tpl(); if (t && (!t.serialStart || !t.macStart)) void this.suggestTpl(false, false); }
  tplImage(id: string) { const t = this.tpl(); const im = this.images().find(i => i.id === id); if (t && im) this.tpl.set({ ...t, image: id, cpus: im.cpus || t.cpus, ram: im.ram || t.ram, model: '', module: '' }); }
  async saveTpl() {
    const t = this.tpl(); if (!t) return;
    const isNew = !t.id;
    if (await this.lab.saveTemplate({ ...t, cpus: +t.cpus || 1, ram: +t.ram || 1024, diskGb: +t.diskGb || 0 })) { const l = this.templates(); const n = isNew ? l[l.length - 1] : l.find(x => x.id === t.id); if (n) this.pickTpl(n); }
  }
  async removeTpl() { const t = this.tpl(); if (t?.id && await this.lab.removeTemplate(t.id)) this.tpl.set(null); }
}

// ---------------------------------------------------------------- KVM hosts
@Component({
  selector: 'app-lab-hosts',
  imports: [FormsModule],
  template: `
  <div class="ov" (click)="closed.emit()"></div>
  <div class="mdl" style="grid-template-rows: 64px 0 minmax(0, 1fr) 60px">
    <div class="mh"><div><h2>{{ tr('KVM Hosts') }}</h2><div class="muted small">{{ tr('Linux machines the switches run on - reached with their SSH sessions') }}</div></div><span class="sp"></span><button type="button" class="sm" (click)="closed.emit()">✕</button></div>
    <div></div>
    <div class="mb" style="display: grid; grid-template-columns: 300px minmax(0, 1fr)">
      <div class="lst">
        @for (h of hosts(); track h.session) {
          <div class="it" [class.on]="cur()?.session === h.session" (click)="sel.set(h.session)"><i class="d" [class]="'d ' + dot(h)"></i><div><span>{{ h.name || h.session }}</span><small>{{ h.session }}</small><small>{{ h.info.os || '?' }} · {{ isVM(h) ? tr('VM ({0})', h.info.virt ?? '') : tr('hardware') }} · {{ state(h) }}</small></div></div>
        } @empty { <p class="muted small">{{ tr('No KVM host yet.') }}</p> }
        <div class="card" style="margin-top: 10px"><b>+ {{ tr('Add a Host') }}</b>
          <select [(ngModel)]="pick" name="hpick" style="width: 100%; margin: 8px 0">@for (s of sshSessions(); track s) { <option [value]="s">{{ s }}</option> }</select>
          <button type="button" class="primary" style="width: 100%" (click)="add()" [disabled]="!pick || lab.busy()">{{ lab.busy() ? tr('Logging in and checking…') : tr('Check and Add') }}</button>
          <p class="muted small" style="margin: 6px 0 0">{{ tr('An SSH session of a Linux machine; make one in the Session Manager first.') }}</p></div>
      </div>
      <div class="pad" style="overflow: auto">
        @if (cur(); as h) {
          <div class="row2" style="justify-content: flex-start"><div style="flex: 1"><b style="font-size: 15px">{{ h.name || h.session }}</b> <span class="badge">{{ state(h) }}</span>
            <div class="muted small">{{ h.info.user }} · {{ h.info.os }} · {{ tr('kernel') }} {{ h.info.kernel }} · {{ h.info.arch }} · {{ isVM(h) ? tr('VM ({0})', h.info.virt ?? '') : tr('hardware') }}@if (h.checked) { · {{ tr('checked {0}', h.checked) }} }</div></div>
            <button type="button" (click)="lab.checkHost(h.session)" [disabled]="lab.busy()">{{ tr('Check Again') }}</button></div>
          <div class="big3" style="margin-top: 14px">
            <div class="card"><span class="muted small">vCPU</span><div><b>{{ h.info.cpus }}</b> <span class="muted">{{ tr('cores · load {0}', h.info.load) }}</span></div><div class="meter"><i [style.width.%]="pct(h.info.load, h.info.cpus)"></i></div></div>
            <div class="card"><span class="muted small">{{ tr('Memory') }}</span><div><b>{{ gb(h.info.memFree) }}</b> <span class="muted">{{ tr('free of {0}', gb(h.info.memTotal)) }}</span></div><div class="meter"><i [style.width.%]="pct(h.info.memTotal - h.info.memFree, h.info.memTotal)"></i></div></div>
            <div class="card"><span class="muted small">{{ tr('Disk in ~/{0}', h.info.dir) }}</span><div><b>{{ gb(h.info.diskFree) }}</b> <span class="muted">{{ tr('free') }}</span></div></div>
          </div>
          <div style="display: grid; grid-template-columns: 1fr 1fr; gap: 14px; margin-top: 14px">
            <div class="card"><b>{{ tr('What the host has') }}</b><div class="chk" style="margin-top: 8px">
              <span [class]="h.info.qemu ? 'y' : 'x'">{{ h.info.qemu ? '✓' : '✕' }}</span><span>QEMU</span><span class="muted">{{ h.info.qemu || tr('not installed') }}{{ h.info.qemuImg ? ' · qemu-img ✓' : '' }}</span>
              <span [class]="h.info.kvm === 'yes' ? 'y' : h.info.kvm === 'none' && !h.info.vt ? 'n' : 'x'">{{ h.info.kvm === 'yes' ? '✓' : '!' }}</span><span>KVM</span><span class="muted">{{ h.info.kvm === 'yes' ? tr('/dev/kvm usable') : h.info.kvm === 'noaccess' ? tr('no access to /dev/kvm') : tr('no /dev/kvm: software emulation, slow') }}</span>
              <span [class]="h.info.sudo ? 'y' : 'n'">{{ h.info.sudo ? '✓' : '!' }}</span><span>{{ tr('sudo without password') }}</span><span class="muted">{{ h.info.sudo ? tr('taps, bridges, NAT') : tr('only port forwarding') }}</span>
              <span [class]="h.info.guestfs ? 'y' : 'x'">{{ h.info.guestfs ? '✓' : '✕' }}</span><span>libguestfs</span><span class="muted">{{ tr('writes persona files') }}</span>
              <span [class]="h.info.nat ? 'y' : 'x'">{{ h.info.nat ? '✓' : '✕' }}</span><span>{{ tr('NAT network') }}</span><span class="muted">{{ h.info.nat ? 'virbr0 ' + h.info.nat : tr('libvirt’s virbr0 with DHCP') }}</span>
            </div>
            @for (p of h.info.problems ?? []; track p) { <p class="r small" style="margin: 8px 0 0">{{ p }}</p> }</div>
            <div class="card"><b>{{ tr('Management of the switches') }}</b><div class="chk" style="margin-top: 8px">
              <span [class]="h.info.sudo && h.info.nat ? 'y' : 'x'">{{ h.info.sudo && h.info.nat ? '✓' : '✕' }}</span><span>{{ tr('The host’s NAT network') }}</span><span class="muted">{{ h.info.nat ? tr('DHCP on virbr0') : tr('after Set Up') }}@if (best(h) === 'nat') { <span class="badge g">{{ tr('best here') }}</span> }</span>
              <span [class]="h.info.sudo && h.info.lanKind ? (isVM(h) ? 'n' : 'y') : 'x'">{{ h.info.sudo && h.info.lanKind ? (isVM(h) ? '!' : '✓') : '✕' }}</span><span>{{ tr('The host’s network (bridged)') }}</span><span class="muted">{{ h.info.lanIf ? h.info.lanIf + ' (' + h.info.lanKind + ')' : tr('no port to join') }}{{ isVM(h) ? ' - ' + tr('its virtual switch must allow MAC changes and forged transmits') : '' }}@if (best(h) === 'lan') { <span class="badge g">{{ tr('best here') }}</span> }</span>
              <span [class]="h.info.sudo && h.info.lanAddr ? 'y' : 'x'">{{ h.info.sudo && h.info.lanAddr ? '✓' : '✕' }}</span><span>{{ tr('The host’s network, routed (cloud)') }}</span><span class="muted">{{ h.info.lanAddr ? (h.info.uplink + ' ' + h.info.lanAddr + ' · ' + tr('fixed addresses, any protocol, no virtual switch settings')) : tr('no network port with an address') }}</span>
              <span [class]="h.info.sudo ? 'y' : 'x'">{{ h.info.sudo ? '✓' : '✕' }}</span><span>{{ tr('Lab bridge on the host') }}</span><span class="muted">{{ tr('from the host only') }}</span>
              <span class="y">✓</span><span>{{ tr('Port forwarding on the host') }}</span><span class="muted">{{ tr('always works') }}</span>
            </div>@for (n of h.info.notes ?? []; track n) { <p class="muted small" style="margin: 8px 0 0">{{ n }}</p> }</div>
          </div>
          @if (h.info.canSetup || setupHere()) {
            <div class="card" style="margin-top: 14px"><div class="row2" style="justify-content: flex-start"><b style="flex: none">{{ tr('Set Up QEMU/KVM') }}</b><span class="muted small">{{ tr('installs what is missing, as root on the host') }}</span><span class="sp"></span></div>
              @if (setupHere(); as s) {
                <div style="display: grid; grid-template-columns: 260px 1fr; gap: 16px; margin-top: 8px">
                  <div>@for (st of steps; track st) { <div class="small" style="padding: 4px 0"><span [class]="stepCls(s, st)">{{ stepMark(s, st) }}</span> {{ tr(stepLabel(st)) }}</div> }
                    <p class="small" [class.r]="!!s.error" [class.g]="s.done && !s.error">{{ s.done ? (s.error ? tr('Set-up failed') + ': ' + s.error : tr('Set up') + ' ✓') : tr('Setting up…') }}</p></div>
                  <pre class="log">{{ s.lines.join('\n') }}</pre>
                </div>
                @if (s.done) { <button type="button" class="sm" (click)="lab.setup.set(null)">{{ tr('Close') }}</button> }
              } @else {
                <ul class="small">@for (p of h.info.plan ?? []; track p) { <li>{{ p }}</li> }</ul>
                <div class="row2" style="justify-content: flex-start">@if (!h.info.sudo) { <input type="password" [(ngModel)]="pw" name="spw" [placeholder]="tr('sudo password of {0}', h.info.user)" autocomplete="off" style="max-width: 260px" /> }
                  <button type="button" class="primary" (click)="setup(h)" [disabled]="!h.info.sudo && !pw">{{ tr('Set Up') }}</button></div>
              }
            </div>
          }
        } @else { <p class="muted">{{ tr('Add a host on the left.') }}</p> }
      </div>
    </div>
    <div class="mf">@if (cur(); as h) { <button type="button" class="danger-ghost" (click)="remove(h)">{{ tr('Remove Host') }}</button> }<span class="sp"></span><button type="button" class="primary" (click)="closed.emit()">{{ tr('Close') }}</button></div>
  </div>`,
  styles: [PAGE],
})
export class LabHosts {
  readonly closed = output<void>();
  protected lab = inject(LabService);
  private ui = inject(Ui);
  protected tr = tr;
  protected hosts = computed<EmuHost[]>(() => this.lab.emu()?.hosts ?? []);
  readonly pick0 = input('', { alias: 'select' });
  protected sel = signal('');
  ngOnInit() { if (this.pick0()) this.sel.set(this.pick0()); }
  protected cur = computed(() => this.hosts().find(h => h.session === this.sel()) ?? this.hosts()[0] ?? null);
  protected pick = '';
  protected pw = '';
  protected steps = ['packages', 'nat', 'kvm', 'group'];
  constructor() { effect(() => { const s = this.sshSessions(); if (!this.pick && s.length) this.pick = s[0]; }); }
  protected sshSessions = computed(() => {
    const out: string[] = [], skip = [this.lab.emu()?.folder, this.lab.sim()?.folder].filter(Boolean) as string[];
    const have = new Set(this.hosts().map(h => h.session));
    const walk = (n: SessionNode | null | undefined) => n?.children?.forEach(c => { if (c.isFolder) { if (!skip.some(f => c.path === f || c.path.startsWith(f + '/'))) walk(c); } else if (c.protocol === 'SSH2' && !have.has(c.path)) out.push(c.path); });
    walk(this.ui.tree());
    return out;
  });
  protected setupHere = computed(() => { const s = this.lab.setup(); return s && s.host === this.cur()?.session ? s : null; });
  isVM(h: EmuHost) { return !!h.info.virt && h.info.virt !== 'none' && h.info.virt !== 'unknown'; }
  best(h: EmuHost) { const i = h.info; return i.sudo && i.lanKind && !this.isVM(h) ? 'lan' : i.sudo && i.nat ? 'nat' : i.sudo && i.lanKind ? 'lan' : i.sudo ? 'bridge' : 'forward'; }
  dot(h: EmuHost) { return (h.info.problems ?? []).length ? 'bad' : h.info.canSetup ? 'wa' : 'ok'; }
  state(h: EmuHost) { return (h.info.problems ?? []).length ? tr('{0} to fix', (h.info.problems ?? []).length) : h.info.canSetup ? tr('can be set up') : tr('ready'); }
  gb(mb?: number) { return !mb ? '-' : mb >= 1024 ? (mb / 1024).toFixed(mb >= 10240 ? 0 : 1) + ' GB' : mb + ' MB'; }
  pct(a: number, b: number) { return b ? Math.min(100, (a / b) * 100) : 0; }
  async add() { const h = await this.lab.checkHost(this.pick); if (h) { this.sel.set(this.pick); this.pick = ''; } }
  async setup(h: EmuHost) { const pw = this.pw; this.pw = ''; await this.lab.setupHost(h.session, pw); }
  async remove(h: EmuHost) { if (await this.ui.confirm(tr('Remove Host'), tr('Forget {0} as a KVM host? Nothing on it is changed.', h.name || h.session), tr('Remove'), true)) await this.lab.removeHost(h.session); }
  stepLabel(s: string) { return ({ packages: 'Packages', nat: 'NAT network (libvirt, virbr0)', kvm: 'KVM module', group: 'KVM group' } as Record<string, string>)[s] ?? s; }
  private stepIdx(s: { step: string; done: boolean }, st: string) { const i = this.steps.indexOf(s.step), j = this.steps.indexOf(st); return { i, j }; }
  stepMark(s: { step: string; done: boolean; error: string }, st: string) { const { i, j } = this.stepIdx(s, st); return s.done && !s.error ? '✓' : j < i ? '✓' : j === i ? (s.error ? '✕' : '…') : '○'; }
  stepCls(s: { step: string; done: boolean; error: string }, st: string) { const m = this.stepMark(s, st); return m === '✓' ? 'g' : m === '✕' ? 'r' : m === '…' ? 'w' : 'muted'; }
}
