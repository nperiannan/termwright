import { Component, computed, effect, signal, untracked } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { EmuImage } from '../core/models';
import { tr } from '../core/i18n';
import { TvBar, TvSorts } from '../core/table-view';
import { LabHostImages } from './lab-host-images';
import { VmScreen } from './vm-screen';
import { LabImages } from './lab-pages';

type Quick = 'all' | 'here' | 'todo' | 'host' | 'used';
type Badge = { text: string; cls: 'ok' | 'h' | 'w' | 'm' | 'r' };

/**
 * The Appliances page, next to Traffic: the images the switches and computers boot, what Termwright knows about each,
 * the templates made from them, the device pack's profiles, the copies on the hosts and the public catalogue. A page of
 * the lab (not a dialog): counts at the top, the images by family on the left with a state badge each, the chosen one's
 * settings in labelled cards on the right.
 */
@Component({
  selector: 'app-lab-appliances',
  imports: [FormsModule, LabHostImages, TvBar, TvSorts, VmScreen],
  template: `
  <section class="apg" [attr.aria-label]="tr('Appliances')">
    <header class="aph">
      <div class="apt"><h2>{{ tr('Appliances') }}</h2><span class="muted small">{{ tr('The images the switches and computers boot, what Termwright knows about each, and the templates made from them') }}</span></div>
      <span class="sp"></span>
      <button type="button" class="primary" (click)="tab.set('images'); addImage()">＋ {{ tr('Add Image…') }}</button>
      <button type="button" (click)="tab.set('images'); openIso()">{{ tr('Install from ISO…') }}</button>
      <span class="imw"><button type="button" (click)="imp.set(!imp())" aria-haspopup="menu" [attr.aria-expanded]="imp()">{{ tr('Import') }} ▾</button>
        @if (imp()) { <div class="imm" role="menu"><button type="button" role="menuitem" (click)="imp.set(false); importAppliance()">{{ tr('GNS3 Appliance…') }}</button><button type="button" role="menuitem" (click)="imp.set(false); importProfiles()">{{ tr('Import Profiles…') }}</button></div> }</span>
    </header>
    <nav class="seg av" [attr.aria-label]="tr('Views')">
      <a [class.on]="tab() === 'images'" (click)="tab.set('images')">{{ tr('Images') }}<small>{{ images().length }}</small></a>
      <a [class.on]="tab() === 'templates'" (click)="tab.set('templates')">{{ tr('Templates') }}<small>{{ templates().length }}</small></a>
      <a [class.on]="tab() === 'profiles'" (click)="tab.set('profiles')">{{ tr('Device pack') }}</a>
      <a [class.on]="tab() === 'onhosts'" (click)="tab.set('onhosts')">{{ tr('On the hosts') }}</a>
      <a [class.on]="tab() === 'public'" (click)="openPublic()">{{ tr('Public catalogue') }}</a>
    </nav>

    @switch (tab()) {
      @case ('images') {
        <div class="kpis">
          <div class="kpi"><span>{{ tr('Images') }}</span><b>{{ images().length }}</b><small>{{ families() === 1 ? tr('in 1 family') : tr('in {0} families', families()) }}</small></div>
          <div class="kpi"><span>{{ tr('On this computer') }}</span><b>{{ here().n }}</b><small>{{ mb(here().bytes) }}</small></div>
          <div class="kpi"><span>{{ tr('To download') }}</span><b>{{ toDownload().length + queue().length }}</b>
            <small>@if (queue().length) { {{ tr('{0} downloading or waiting', queue().length) }} } @else if (toDownload().length) { <a class="lnk" (click)="lab.download(toDownload())">{{ tr('Download all') }}</a> } @else { {{ tr('nothing to download') }} }</small></div>
          <div class="kpi"><span>{{ tr('On the host') }}</span><b>{{ onHost() }}</b><small>{{ hostName() || tr('no host yet') }}</small></div>
          <div class="kpi"><span>{{ tr('In use') }}</span><b>{{ inUse().n }}</b><small>{{ (inUse().sw === 1 ? tr('by 1 switch') : tr('by {0} switches', inUse().sw)) + ' · ' + (inUse().tp === 1 ? tr('1 template') : tr('{0} templates', inUse().tp)) }}</small></div>
        </div>
        <div class="body">
          <aside class="list">
            <div class="lh"><app-tv-bar [tv]="imView" /><app-tv-sorts [tv]="imView" [keys]="imKeys" /></div>
            <div class="chips">@for (q of quicks; track q.k) { <a [class.on]="qf() === q.k" (click)="qf.set(q.k)">{{ tr(q.name) }}<small>{{ quickN(q.k) }}</small></a> }</div>
            <div class="lsc">
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
              @if (installs().length) {
                <div class="grp">{{ tr('Installs from ISO') }}<i>{{ installs().length }}</i></div>
                <div class="row" [class.on]="iso()" (click)="openIso()"><b>{{ tr('{0} install(s)', installs().length) }}</b><span class="st" [class]="'st ' + (installsBusy() ? 'w' : 'm')">{{ installsBusy() ? tr('running') : tr('done') }}</span><span class="f">{{ installsLine() }}</span></div>
              }
              @for (g of listGroups(); track g.name) {
                <div class="grp">{{ g.name }}<i>{{ g.items.length }}</i></div>
                @for (im of g.items; track im.id) { @let bd = badge(im);
                  <div class="row" [class.on]="!iso() && edit()?.id === im.id" (click)="pick(im.id)">
                    <b>{{ im.version || im.name }}@if (im.persona) { <span class="pb">{{ tr('persona') }}</span> }</b><span class="st" [class]="'st ' + bd.cls">{{ bd.text }}</span>
                    <span class="f">{{ im.fileName || tr('no file chosen') }} · {{ mb(im.size) }} · {{ im.cpus }} vCPU · {{ gb(im.ram) }}</span>
                    @if (dl(im) && dl(im) !== 'waiting') { <div class="meter rm"><i [style.width]="pctOf(dl(im)) + '%'"></i></div> }
                  </div>
                }
              } @empty { <p class="muted small pad1">{{ images().length ? tr('No image matches the search or the filters.') : tr('No images yet: ＋ Add Image….') }}</p> }
            </div>
            <div class="lf"><button type="button" class="sm" (click)="importAppliance()">{{ tr('GNS3 Appliance…') }}</button><button type="button" class="sm" (click)="importProfiles()">{{ tr('Import Profiles…') }}</button></div>
          </aside>

          <div class="det">
            @if (iso()) {
              <div class="dh"><div class="ic">◉</div><div class="dt"><h3>{{ tr('Install from an ISO file') }}</h3><div class="muted small">{{ tr('Some distributions are published only as an installer ISO. Termwright runs the installer on a KVM host with a blank disk and keeps the installed disk as a new image.') }}</div></div></div>
              <div class="dscroll one">
                @for (j of installs(); track j.id) {
                  <div class="card">
                    <h4>{{ j.name }}<small>{{ j.isoName }} · {{ diskText(j) }} · {{ j.mode === 'alpine' ? tr('automatic') : tr('by hand') }}</small><span class="sp"></span>
                      @if (isoBusy(j) && j.stage !== 'copying' || j.stage === 'waiting') { <button type="button" class="sm" (click)="screenOf.set(j)" [title]="tr('What its display shows, and its keyboard')">{{ tr('Screen…') }}</button> }
                      @if (j.stage === 'waiting' && j.console) { <button type="button" class="sm" (click)="isoConsole(j)">{{ tr('Open console') }}</button> }
                      @if (j.stage === 'waiting') { <button type="button" class="sm primary" (click)="isoFinish(j)">{{ tr('Finish and save as image') }}</button> }
                      @if (j.stage === 'done' && j.image) { <button type="button" class="sm" (click)="isoShow(j)">{{ tr('Show image') }}</button> }
                      @if (isoBusy(j) || j.stage === 'waiting') { <button type="button" class="sm danger-ghost" (click)="isoCancel(j)">{{ tr('Cancel') }}</button> }
                      @if (!isoBusy(j) && j.stage !== 'waiting') { <button type="button" class="sm" (click)="isoRemove(j)">{{ tr('Remove') }}</button> }</h4>
                    <div class="small" [class.r]="j.stage === 'failed'" [class.g]="j.stage === 'done'" [class.w]="isoBusy(j)">{{ isoText(j) }}</div>
                    @if (j.stage === 'done' && (j.verified || j.verifyErr)) { <div class="small" [class.g]="!!j.verified" [class.w]="!!j.verifyErr">{{ j.verified ? '✓ ' + tr('Verified: {0}', tr(j.verified)) : '⚠ ' + tr('Not verified: {0}', tr(j.verifyErr || '')) }}</div> }
                    @if (isoBusy(j) || j.stage === 'waiting') {
                      <div class="meter"><i [style.width]="overall(j) + '%'"></i></div>
                      <div class="small muted">{{ tr('Step {0} of {1}', stepNo(j), stepKeys(j).length) }} · {{ tr('{0} since the start', dur(secs(j.started))) }}</div>
                      <ol class="stl">
                        @for (k of stepKeys(j); track k) {
                          <li [class.d]="stepState(j, k) === 'done'" [class.n]="stepState(j, k) === 'now'">
                            <span class="mk" [class.g]="stepState(j, k) === 'done'" [class.w]="stepState(j, k) === 'now'">{{ stepState(j, k) === 'done' ? '✓' : stepState(j, k) === 'now' ? '▶' : '○' }}</span><span>{{ stepLabel(k) }}</span><span class="tm">{{ stepTime(j, k) }}</span>
                            @if (stepState(j, k) === 'now' && stepNote(j)) { <small>{{ stepNote(j) }}</small> }
                          </li>
                        }
                      </ol>
                      @if (j.tail) { <div class="small muted" style="margin-top: 8px">{{ tr('The installer’s console (last lines)') }}</div><pre class="yaml wrap">{{ j.tail }}</pre> }
                      @if ((j.idle ?? 0) >= 90 && j.step !== 'convert') { <div class="small w" style="margin-top: 6px">{{ tr('The installer’s console has shown nothing for {0}. It may be busy (slow without KVM) or waiting for an answer: read the last line above.', dur(j.idle ?? 0)) }}</div> }
                    }
                  </div>
                }
                <div class="card">
                  <h4>{{ tr('New install') }}</h4>
                  <div class="fg">
                    <label>{{ tr('ISO file') }}</label><div class="row2"><input [(ngModel)]="isoPath" name="isop" readonly [placeholder]="tr('Choose the .iso file')" /><button type="button" class="sm" (click)="chooseIso()">{{ tr('Choose…') }}</button></div>
                    <label>{{ tr('Install') }}</label><div><span class="seg"><a [class.on]="isoMode === 'alpine'" (click)="isoMode = 'alpine'">{{ tr('Alpine Linux, automatic') }}</a><a [class.on]="isoMode === 'manual'" (click)="isoMode = 'manual'">{{ tr('Other, on its console') }}</a></span>
                      <div class="hint">{{ isoMode === 'alpine' ? tr('Termwright types the Alpine install on the installer’s console and sets the root password below.') : tr('The installer starts and its console opens as a session: you install by hand, then press Finish. It must be an installer that talks on a serial console.') }}</div></div>
                    <label>{{ tr('Image name') }}</label><input [(ngModel)]="isoNm" name="isonm" [placeholder]="isoNamePh()" />
                    <label>{{ tr('Root password') }}</label><div><input [(ngModel)]="isoPw" name="isopw" autocomplete="off" /><div class="hint">{{ isoMode === 'alpine' ? tr('The new system’s root password; Termwright logs in with it.') : tr('The password you set during the install (optional): Termwright logs in with it.') }}</div></div>
                    <label>{{ tr('Disk') }}</label><div><div class="row2 nar"><input type="number" min="1" [(ngModel)]="isoSize" name="isogb" /><select [(ngModel)]="isoUnit" (ngModelChange)="isoUnitChanged($event)" name="isounit"><option value="MB">MB</option><option value="GB">GB</option></select></div><div class="hint">{{ tr('The new disk’s largest size, not the space it takes: the qcow2 file grows only as the system is written, so the saved image stays small (a small Alpine system takes a few hundred MB). In MB or GB; at least 100 MB.') }}</div></div>
                    <label>{{ tr('Memory') }}</label><div class="row2 nar"><input type="number" min="256" step="256" [(ngModel)]="isoRam" name="isoram" /><span class="unit">MB</span></div>
                    <label>vCPU</label><div class="row2 nar"><input type="number" min="1" max="64" [(ngModel)]="isoCpus" name="isocpu" /><span></span></div>
                    <label>{{ tr('Check') }}</label><label class="ckl"><input type="checkbox" [(ngModel)]="isoVerify" name="isover" /> {{ tr('Boot the new image once at the end to see it comes up (on a throwaway copy)') }}</label>
                    <label>{{ tr('Run on') }}</label><select [(ngModel)]="isoHost" name="isohost">@for (h of hosts(); track h.session) { <option [value]="h.session">{{ h.name }}</option> }</select>
                  </div>
                  <div class="cf"><button type="button" class="primary" (click)="startIso()" [disabled]="!isoPath || lab.busy() || (isoMode === 'alpine' && !isoPw)">{{ tr('Start install') }}</button></div>
                </div>
              </div>
            } @else if (edit(); as im) { @let bd = badge(live(im));
              <div class="dh">
                <div class="ic">▦</div>
                <div class="dt"><h3>{{ im.name }}@if (im.version && !im.name.includes(im.version)) { <span>{{ im.version }}</span> }<span class="st" [class]="'st ' + bd.cls">{{ bd.text }}</span></h3>
                  <div class="m">{{ im.fileName || tr('no file chosen') }} · {{ mb(im.size) }}@if (im.sha256) { · sha256 {{ im.sha256.slice(0, 4) }}…{{ im.sha256.slice(-4) }} }</div>
                  <div class="facts"><span>{{ family(im) }}</span>@if (im.persona) { <span>{{ tr('persona: {0}', im.persona.name) }}</span> }<span>{{ usedBy(im.id) === 1 ? tr('used by 1 switch') : tr('used by {0} switches', usedBy(im.id)) }}</span><span>{{ tplBy(im.id) === 1 ? tr('1 template') : tr('{0} templates', tplBy(im.id)) }}</span></div></div>
                <div class="acts">
                  @if (hasLinks(im)) {
                    @if (!needs(live(im))) { <button type="button" class="dlb done" disabled>✓ {{ tr('Downloaded') }}</button> }
                    @else if (dl(live(im))) { <button type="button" class="dlb busy" disabled [style.--p]="pctOf(dl(live(im))) + '%'"><span>{{ dl(live(im)) === 'waiting' ? tr('Waiting…') : tr('Downloading {0}', dl(live(im))) }}</span></button> }
                    @else { <button type="button" class="primary" (click)="lab.download([im.id])" [disabled]="lab.busy()">⤓ {{ tr('Download ({0})', mb(dlSize(im))) }}</button> }
                  }
                  <button type="button" (click)="lab.upload(im.id)" [disabled]="!im.file || lab.busy()" [title]="tr('Copies it to the lab’s host now, before a switch needs it')">{{ tr('Copy to Host…') }}</button>
                  <button type="button" class="danger-ghost" (click)="removeImage(im)">{{ tr('Remove') }}</button>
                </div>
              </div>
              <div class="dscroll">
                <div class="card w2"><h4>{{ tr('File') }}<small>{{ tr('where Termwright takes it from') }}</small></h4>
                  @for (f of filesOf(im); track f.slot) {
                    <div class="file">@if (filesOf(im).length > 1) { <span class="slot">{{ slotName(f.slot) }}</span> }
                      <span class="p" [class.g]="!!f.file" [class.r]="!f.file && !f.url" [title]="f.url || f.page || ''">{{ f.file ? '✓ ' + f.file : f.url ? tr('{0} - downloads with Download', f.fileName || '-') : tr('{0} - get it from the maker, then Choose…', f.fileName || '-') }}</span>
                      @if (!f.file && !f.url && f.page) { <a class="lnk small" (click)="openPage(f.page)">{{ tr('Download page') }}</a> }
                      <button type="button" class="sm" (click)="chooseSlot(im.id, f.slot)">{{ tr('Choose…') }}</button></div>
                  }
                  <div class="hint">{{ where(live(im)) }}@if (hasLinks(im)) { · {{ tr('Downloads go to {0}', lab.emu()?.downloadDir ?? '') }} }</div></div>
                <div class="card"><h4>{{ tr('Hardware') }}<small>{{ tr('what each switch gets') }}</small></h4><div class="fg">
                  <label>vCPU</label><input type="number" min="1" [(ngModel)]="im.cpus" name="icpu" />
                  <label>{{ tr('Memory') }}</label><div class="row2"><input type="number" min="256" step="256" [(ngModel)]="im.ram" name="iram" /><span class="unit">MB = {{ gb(im.ram) }}</span></div>
                  <label>{{ tr('Disk type') }}</label><select [(ngModel)]="im.disk" name="idisk">@for (b of buses(im.disk); track b.v) { <option [value]="b.v">{{ b.l }}</option> }</select>
                  <label>{{ tr('Network ports') }}</label><input type="number" min="1" [(ngModel)]="im.nics" name="inics" />
                  <label>{{ tr('Adapter type') }}</label><select [(ngModel)]="im.nic" name="inic">@for (n of nics(im.nic); track n) { <option [value]="n">{{ n }}</option> }</select>
                  <label>{{ tr('Port names') }}</label><input class="mono" [(ngModel)]="im.portName" name="ipn" placeholder="{n} or 1/{n}" />
                  <label>{{ tr('Port MACs') }}</label><select [(ngModel)]="im.portMacs" name="ipm" [title]="tr('The data ports’ network cards: from the switch’s base MAC (port 1 = base + 1), or a range of their own apart from the switch’s addresses, as GNS3 gives them')"><option value="">{{ tr('base MAC + port number') }}</option><option value="own">{{ tr('their own range (like GNS3)') }}</option></select>
                </div><div class="hint">{{ tr('The disk is the image’s own size; a template or a switch can make it bigger.') }}</div></div>
                <div class="card"><h4>{{ tr('Console login') }}<small>{{ tr('how Termwright logs in') }}</small></h4><div class="fg">
                  <label>{{ tr('Ready when') }}</label><input class="mono" [(ngModel)]="im.ready" name="irdy" />
                  <label>{{ tr('Login') }}</label><div class="row2"><input [(ngModel)]="im.login" name="ilog" /><input type="password" [(ngModel)]="password" name="ipw" [placeholder]="tr('password (keychain)')" autocomplete="off" /></div>
                  <label>{{ tr('Prompt') }}</label><input class="mono" [(ngModel)]="im.prompt" name="iprm" placeholder="[>#$]\\s*$" />
                  <label>{{ tr('Answers') }}</label><textarea class="mono" rows="2" [(ngModel)]="answers" name="ians" [placeholder]="'[y/N]: => n'"></textarea>
                  <label>{{ tr('Device type') }}</label><input [(ngModel)]="im.deviceType" name="idt" />
                </div></div>
                <div class="card"><h4>{{ tr('First boot') }}<small>{{ tr('day 0, typed once') }}</small></h4>
                  <textarea class="mono big" rows="6" [(ngModel)]="day0" name="id0" placeholder="configure snmp sysName {hostname}" [attr.aria-label]="tr('Day 0')"></textarea>
                  <div class="hint">{{ tr('{hostname} {mgmtIp} {mgmtAddr} {mgmtMask} {gateway} {n} are filled in per switch.') }}</div></div>
                <div class="card"><h4>{{ tr('Management address') }}<small>{{ tr('DHCP or fixed') }}</small></h4><div class="fg">
                  <label>{{ tr('Read with') }}</label><input class="mono" [(ngModel)]="im.addrCmd" name="iadr" placeholder="show vlan Mgmt" [title]="tr('On the host’s network the switch gets its address by DHCP: Termwright types this on the console after each boot and takes the first address it shows.')" />
                  <label>{{ tr('DHCP on') }}</label><textarea class="mono" rows="2" [(ngModel)]="dhcpOn" name="idh" placeholder="enable dhcp vlan Mgmt" [title]="tr('Typed when the switch shows no address on the host’s network: some first-login questions leave its DHCP client off')"></textarea>
                  <label>{{ tr('Fixed address') }}</label><textarea class="mono" rows="2" [(ngModel)]="addrSet" name="iads" [placeholder]="'configure vlan Mgmt ipaddress {mgmtAddr}/{mgmtBits}'" [title]="tr('Typed at each start when the switches have fixed addresses (the host’s network routed, or a lab bridge): {mgmtAddr} {mgmtBits} {mgmtMask} {gateway}')"></textarea>
                </div></div>
                @if (im.persona; as p) {
                  <div class="card w2"><h4>{{ tr('Persona') }}<small>{{ p.name }}</small></h4><div class="fg">
                    <label>{{ tr('File in the VM') }}</label><span class="mono">{{ p.file }}</span>
                    <label>{{ tr('Check after boot') }}</label><input class="mono" [(ngModel)]="p.check" name="ichk" />
                    <label>{{ tr('Models') }}</label><div>@for (m of p.models ?? []; track m.name) { <span class="pill">{{ m.name }}</span> }</div>
                    <label>{{ p.modules || tr('Modules') }}</label><div>@for (m of mods(p); track m) { <span class="pill">{{ m }}</span> }@if (p.stack) { <span class="muted small">· {{ tr('stacks') }}</span> }</div>
                  </div></div>
                }
                <details class="card w2 adv"><summary><h4>{{ tr('Advanced') }}<small>{{ tr('name, version, serial number format, QEMU options') }}</small></h4></summary><div class="fg">
                  <label>{{ tr('Name') }}</label><input [(ngModel)]="im.name" name="iname" />
                  <label>{{ tr('Version') }}</label><input [(ngModel)]="im.version" name="iver" />
                  <label>{{ tr('Serial format') }}</label><input class="mono" [(ngModel)]="im.serial" name="isn" [placeholder]="'AB{YY}{WW}-{N:5}'" />
                  <label>{{ tr('More QEMU options') }}</label><input class="mono" [(ngModel)]="im.extra" name="iex" placeholder="-cpu host -machine q35" />
                </div><div class="hint">{{ tr('Added to the QEMU command line as they are, for images that need them. Examples: -cpu host (the host CPU’s features), -cpu Nehalem, -machine q35, -smp sockets=1,cores=2, -device virtio-rng-pci, -boot order=c, -rtc base=utc. Termwright sets the disk, network ports, console, memory and vCPU itself.') }}</div></details>
              </div>
              <div class="ft"><span class="hint">{{ tr('Changes apply to switches when they start next') }}</span><span class="sp"></span><button type="button" (click)="pick(im.id)">{{ tr('Revert') }}</button><button type="button" class="primary" (click)="saveImage()" [disabled]="lab.busy()">{{ tr('Save') }}</button></div>
            } @else { <div class="none muted">{{ tr('Pick an image on the left, or add one.') }}</div> }
          </div>
        </div>
      }
      @case ('templates') {
        <div class="body">
          <aside class="list">
            <div class="lh"><app-tv-bar [tv]="tplView" /><app-tv-sorts [tv]="tplView" [keys]="tplKeys" /></div>
            <div class="lsc">
              @for (t of tplView.rows(); track t.id) {
                <div class="row" [class.on]="tpl()?.id === t.id" (click)="pickTpl(t)"><b>{{ t.name }}</b><span class="st m">{{ tplUse(t.id) === 1 ? tr('1 switch') : tr('{0} switches', tplUse(t.id)) }}</span>
                  <span class="f">{{ imageName(t.image) }} · {{ t.cpus }} vCPU · {{ gb(t.ram) }}@if (t.module) { · {{ t.module }} }</span></div>
              } @empty { <p class="muted small pad1">{{ templates().length ? tr('No template matches the search or the filters.') : tr('No templates yet.') }}</p> }
            </div>
            <div class="lf"><button type="button" class="primary" (click)="newTpl()" [disabled]="!images().length">＋ {{ tr('New Template') }}</button></div>
          </aside>
          <div class="det">
            @if (tpl(); as t) {
              <div class="dh"><div class="ic">▤</div><div class="dt"><h3>{{ t.name || tr('New template') }}</h3><div class="m">{{ imageName(t.image) }}</div>
                <div class="facts"><span>{{ tplUse(t.id) === 1 ? tr('used by 1 switch') : tr('used by {0} switches', tplUse(t.id)) }}</span>@if (t.model) { <span>{{ t.model }}</span> }</div></div>
                <div class="acts">@if (t.id) { <button type="button" class="danger-ghost" (click)="removeTpl()">{{ tr('Remove') }}</button> }</div></div>
              <div class="dscroll">
                <div class="card"><h4>{{ tr('What it is') }}<small>{{ tr('the image and the model it acts as') }}</small></h4><div class="fg">
                  <label>{{ tr('Name') }}</label><input [(ngModel)]="t.name" name="tn" placeholder="M-48" />
                  <label>{{ tr('Image') }}</label><select [ngModel]="t.image" (ngModelChange)="tplImage($event)" name="ti">@for (im of images(); track im.id) { <option [value]="im.id">{{ im.name }}{{ im.version ? ' · ' + im.version : '' }}</option> }</select>
                  @if (tplDef(); as p) {
                    <label>{{ tr('Model') }}</label><select [ngModel]="t.model" (ngModelChange)="t.model = $event; suggestTpl(true, true)" name="tm"><option value="">-</option>@for (m of p.models ?? []; track m.name) { <option [value]="m.name">{{ m.name }}</option> }</select>
                    <label>{{ p.modules || tr('Module') }}</label><select [(ngModel)]="t.module" name="tx"><option value="">{{ tr('none') }}</option>@for (m of tplMods(); track m) { <option [value]="m">{{ m }}</option> }</select>
                  }
                </div></div>
                <div class="card"><h4>{{ tr('Hardware') }}<small>{{ tr('what each switch gets') }}</small></h4><div class="fg">
                  <label>vCPU</label><input type="number" min="1" [(ngModel)]="t.cpus" name="tc" />
                  <label>{{ tr('Memory') }}</label><div class="row2"><input type="number" min="256" step="256" [(ngModel)]="t.ram" name="tr" /><span class="unit">MB = {{ gb(t.ram) }}</span></div>
                  <label>{{ tr('Disk') }}</label><div class="row2"><input type="number" min="0" [(ngModel)]="t.diskGb" name="td" /><span class="unit">GB</span></div>
                </div><div class="hint">{{ tr('0 = the image’s size') }} · {{ diskName(imageOf(t.image)?.disk) }}</div></div>
                <div class="card w2"><h4>{{ tr('Identity') }}<small>{{ tr('where serial numbers and MAC addresses start') }}</small></h4><div class="fg">
                  <label>{{ tr('First serial number') }}</label><div class="row2"><input class="mono" [(ngModel)]="t.serialStart" name="ts" /><button type="button" class="sm" (click)="suggestTpl(true, false)">↻ {{ tr('New') }}</button></div>
                  <label>{{ tr('First MAC address') }}</label><div class="row2"><input class="mono" [(ngModel)]="t.macStart" name="tmac" /><button type="button" class="sm" (click)="suggestTpl(false, true)">↻ {{ tr('New') }}</button></div>
                </div></div>
              </div>
              <div class="ft"><span class="hint">{{ tr('A template is an image with resources, the model it acts as and where serial numbers and MACs start.') }}</span><span class="sp"></span><button type="button" class="primary" (click)="saveTpl()" [disabled]="!t.name || !t.image || lab.busy()">{{ tr('Save') }}</button></div>
            } @else { <div class="none muted">{{ tr('A template is an image with resources, the model it acts as and where serial numbers and MACs start.') }}</div> }
          </div>
        </div>
      }
      @case ('onhosts') { <div class="full"><app-lab-host-images (hostsPage)="hostsPage.emit($event)" /></div> }
      @case ('public') {
        <div class="full pub">
          <p class="muted small">{{ tr('Ready-made settings from the public GNS3 registry, with their second disks, CD-ROMs and BIOS files. Add one: its versions join the image list; then download the files (or choose them when the maker wants you to sign in).') }}</p>
          @for (g of catGroups(); track g) {
            <div class="grp">{{ g }}<i>{{ catCount(g) }}</i></div>
            <div class="catg">
              @for (c of catIn(g); track c.file) {
                <div class="card"><h4>{{ c.name }}@if (c.added) { <span class="st ok">{{ tr('added') }}</span> }</h4>
                  <p class="muted small cn">{{ c.note }}</p>
                  <div class="cf">@if (c.added) { <button type="button" class="sm" (click)="showAdded(c.file)">{{ tr('Show') }}</button><button type="button" class="sm danger-ghost" (click)="removeCat(c.file, c.name, c.count)" [disabled]="lab.busy()">{{ tr('Remove') }}</button><span class="muted small">{{ tr('{0} images', c.count) }}</span> }
                    @else { <button type="button" class="sm primary" (click)="addCat(c.file, c.name)" [disabled]="lab.busy()">＋ {{ tr('Add') }}</button> }</div></div>
              }
            </div>
          } @empty { <p class="muted">{{ catErr() || tr('Loading…') }}</p> }
          <p class="muted small">{{ tr('Container appliances are not in the list: they do not run on a KVM host.') }}</p>
        </div>
      }
      @case ('profiles') {
        <div class="full">
          <p class="muted small">{{ tr('An image file you add is recognised by its name; New Lab offers these devices.') }} {{ tr('The device pack is loaded or changed in the Simulated Lab tab (the flask at the top right): Load Device Pack.') }}</p>
          <div class="card tcard"><table class="t"><thead><tr><th>{{ tr('Device') }}</th><th>{{ tr('OS') }}</th><th>{{ tr('Modules') }}</th><th>{{ tr('Images') }}</th></tr></thead><tbody>
            @for (d of lab.state()?.deviceTypes ?? []; track d.key) { <tr><td>{{ d.model || d.kind }}</td><td>{{ d.osName || d.kind }}</td><td class="small">{{ (d.modules ?? []).join(', ') || '-' }}</td><td>{{ d.images.length || tr('no image yet') }}</td></tr> }
          </tbody></table></div>
        </div>
      }
    }
  </section>
  @if (screenOf(); as j) { <app-vm-screen kind="install" [id]="j.id" [name]="j.name" (closed)="screenOf.set(null)" /> }`,
  styles: [`
    :host { display: contents; }
    .apg { position: absolute; inset: 0; z-index: 25; background: var(--bg); display: flex; flex-direction: column; gap: 12px; padding: 16px 20px 16px; overflow: hidden; font-size: 13px; }
    .aph { display: flex; align-items: flex-start; gap: 8px; h2 { margin: 0; font-size: 17px; } .apt { display: flex; flex-direction: column; gap: 3px; min-width: 0; } .sp { flex: 1; } button { white-space: nowrap; } }
    .imw { position: relative; } .imm { position: absolute; right: 0; top: calc(100% + 4px); z-index: 5; display: flex; flex-direction: column; min-width: 180px; padding: 4px; border: 1px solid var(--line); border-radius: 8px; background: var(--surface); box-shadow: 0 10px 30px rgba(0,0,0,.35);
      button { border: 0; background: transparent; text-align: left; padding: 6px 10px; border-radius: 6px; } button:hover { background: var(--hover); } }
    .seg.av { align-self: flex-start; display: inline-flex; border: 1px solid var(--line); border-radius: 8px; overflow: hidden; flex: none;
      a { padding: 5px 13px; color: var(--muted); cursor: pointer; white-space: nowrap; border-right: 1px solid var(--line); &:last-child { border-right: 0; } small { margin-left: 5px; font-size: 11px; color: var(--muted); } }
      a.on { background: var(--accent-soft); color: var(--accent); } }
    .kpis { display: grid; grid-template-columns: repeat(5, minmax(0, 1fr)); gap: 10px; flex: none; }
    .kpi { border: 1px solid var(--line); border-radius: 10px; background: var(--surface); padding: 9px 12px; display: flex; flex-direction: column; gap: 2px; min-width: 0;
      > span { font-size: 11px; color: var(--muted); text-transform: uppercase; letter-spacing: .04em; } b { font-size: 19px; font-weight: 600; font-variant-numeric: tabular-nums; } small { font-size: 11px; color: var(--muted); white-space: nowrap; overflow: hidden; text-overflow: ellipsis; } }
    .lnk { color: var(--accent); cursor: pointer; }
    .body { display: grid; grid-template-columns: 330px minmax(0, 1fr); gap: 14px; flex: 1; min-height: 0; }
    .list, .det { border: 1px solid var(--line); border-radius: 10px; background: var(--surface); display: flex; flex-direction: column; min-height: 0; min-width: 0; }
    .lh { padding: 10px 10px 4px; display: flex; flex-direction: column; gap: 6px; }
    .chips { display: flex; flex-wrap: wrap; gap: 6px; padding: 0 10px 8px; a { font-size: 11.5px; border: 1px solid var(--line); border-radius: 999px; padding: 2px 9px; color: var(--muted); cursor: pointer; small { margin-left: 4px; opacity: .8; } } a.on { color: var(--accent); border-color: var(--accent); background: var(--accent-soft); } }
    .lsc { flex: 1; min-height: 0; overflow: auto; padding-bottom: 6px; } .pad1 { padding: 0 14px; }
    .grp { font-size: 11px; font-weight: 600; color: var(--muted); text-transform: uppercase; letter-spacing: .05em; padding: 10px 14px 4px; display: flex; i { font-style: normal; margin-left: auto; font-weight: 500; text-transform: none; letter-spacing: 0; } }
    .row { display: grid; grid-template-columns: minmax(0, 1fr) auto; gap: 2px 8px; padding: 7px 10px; margin: 0 6px; border-radius: 8px; border: 1px solid transparent; cursor: pointer; align-items: center;
      &:hover { background: var(--hover); } &.on { background: var(--accent-soft); border-color: color-mix(in srgb, var(--accent) 55%, transparent); }
      b { font-size: 13px; font-weight: 600; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; } .f { grid-column: 1 / 3; color: var(--muted); font-size: 11.5px; font-family: var(--mono, monospace); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; } .rm { grid-column: 1 / 3; margin: 2px 0 0; } }
    .st { font-size: 10.5px; font-weight: 600; padding: 0 7px; line-height: 17px; border-radius: 4px; justify-self: end; white-space: nowrap;
      &.ok { background: color-mix(in srgb, var(--success, #1fa45a) 16%, transparent); color: var(--success, #1fa45a); } &.h { background: color-mix(in srgb, var(--accent) 16%, transparent); color: var(--accent); }
      &.w { background: color-mix(in srgb, var(--warning, #e39b17) 16%, transparent); color: var(--warning, #e39b17); } &.m { background: color-mix(in srgb, var(--muted) 16%, transparent); color: var(--muted); } &.r { background: color-mix(in srgb, var(--danger) 16%, transparent); color: var(--danger); } }
    .pb { font-size: 10px; font-weight: 500; color: var(--success, #1fa45a); border: 1px solid color-mix(in srgb, var(--success, #1fa45a) 50%, transparent); border-radius: 4px; padding: 0 5px; margin-left: 6px; vertical-align: 1px; }
    .lf { border-top: 1px solid var(--line); padding: 10px; display: flex; gap: 8px; > button { flex: 1; } }
    .dq { margin: 0 10px 6px; border: 1px solid var(--line); border-radius: 8px; padding: 6px 8px; .dqh { display: flex; gap: 6px; align-items: center; .sp { flex: 1; } } .dqi { display: flex; gap: 4px; align-items: center; padding: 4px 0; cursor: pointer; } .dqn { flex: 1; min-width: 0; span { display: block; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; font-size: 12.5px; } small { color: var(--muted); font-size: 11px; } } button.xs { padding: 0 5px; height: 20px; font-size: 11px; } }
    .meter { height: 5px; border-radius: 4px; background: var(--surface-2); overflow: hidden; margin: 4px 0 2px; i { display: block; height: 100%; background: var(--accent); } }
    .dh { display: flex; gap: 14px; align-items: center; padding: 14px 18px; border-bottom: 1px solid var(--line); flex: none;
      .ic { width: 42px; height: 42px; flex: none; border-radius: 10px; background: var(--accent-soft); color: var(--accent); display: grid; place-items: center; font-size: 20px; }
      .dt { flex: 1; min-width: 0; } h3 { margin: 0; font-size: 16px; display: flex; gap: 8px; align-items: center; flex-wrap: wrap; > span:not(.st) { color: var(--muted); font-weight: 500; } }
      .m { color: var(--muted); font-size: 12px; margin-top: 3px; font-family: var(--mono, monospace); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
      .facts { display: flex; gap: 6px; margin-top: 6px; flex-wrap: wrap; span { font-size: 11.5px; color: var(--muted); border: 1px solid var(--line); border-radius: 7px; padding: 1px 8px; background: var(--surface-2); } }
      .acts { display: flex; gap: 8px; flex: none; align-items: center; button { white-space: nowrap; } } }
    .dscroll { flex: 1; min-height: 0; overflow: auto; padding: 14px 18px; display: grid; grid-template-columns: minmax(0, 1fr) minmax(0, 1fr); gap: 12px; align-content: start; &.one { grid-template-columns: minmax(0, 1fr); max-width: 900px; } }
    .card { border: 1px solid var(--line); border-radius: 10px; background: var(--surface-2); padding: 12px 14px; min-width: 0; &.w2 { grid-column: 1 / -1; }
      h4 { margin: 0 0 10px; font-size: 12.5px; display: flex; gap: 8px; align-items: center; flex-wrap: wrap; small { color: var(--muted); font-weight: 400; } .sp { flex: 1; } } }
    .fg { display: grid; grid-template-columns: 116px minmax(0, 1fr); gap: 8px 10px; align-items: center; > label { color: var(--muted); font-size: 12px; } input, select, textarea { width: 100%; box-sizing: border-box; min-width: 0; } textarea { resize: vertical; } }
    textarea.big { width: 100%; box-sizing: border-box; resize: vertical; }
    .row2 { display: flex; gap: 8px; align-items: center; > * { flex: 1; min-width: 0; } > button, > .unit { flex: none; } &.nar { max-width: 260px; } } .unit { color: var(--muted); font-size: 12px; white-space: nowrap; }
    .mono { font-family: var(--mono, monospace); font-size: 12px; }
    .hint { color: var(--muted); font-size: 11.5px; margin-top: 8px; line-height: 1.45; }
    .file { display: flex; gap: 10px; align-items: center; padding: 2px 0; .slot { flex: none; width: 90px; color: var(--muted); font-size: 12px; } .p { flex: 1; min-width: 0; font-family: var(--mono, monospace); font-size: 12px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; color: var(--muted); &.g { color: var(--success, #1fa45a); } &.r { color: var(--danger); } } }
    .g { color: var(--success, #1fa45a); } .r { color: var(--danger); } .w { color: var(--warning, #e39b17); }
    .pill { display: inline-block; padding: 1px 8px; border-radius: 9px; font-size: 11.5px; margin: 0 4px 4px 0; border: 1px solid var(--line); background: var(--surface); }
    details.adv { > summary { list-style: none; cursor: pointer; &::-webkit-details-marker { display: none; } h4 { margin: 0; } h4::after { content: '▸'; color: var(--muted); order: -1; } } &[open] > summary h4 { margin-bottom: 10px; &::after { content: '▾'; } } }
    .ft { display: flex; align-items: center; gap: 8px; padding: 10px 18px; border-top: 1px solid var(--line); flex: none; .sp { flex: 1; } .hint { margin: 0; } }
    .none { margin: auto; padding: 40px; text-align: center; }
    .cf { display: flex; gap: 8px; align-items: center; margin-top: 12px; }
    .full { flex: 1; min-height: 0; overflow: auto; display: flex; flex-direction: column; gap: 10px; > app-lab-host-images { flex: 1; min-height: 0; } }
    .pub .grp { padding: 10px 2px 4px; } .catg { display: grid; grid-template-columns: repeat(auto-fill, minmax(260px, 1fr)); gap: 10px; } .cn { margin: 0; }
    .tcard { padding: 0; overflow: hidden; table { width: 100%; border-collapse: collapse; } th { text-align: left; font-weight: 500; color: var(--muted); font-size: 11.5px; padding: 8px 12px; border-bottom: 1px solid var(--line); } td { padding: 6px 12px; border-bottom: 1px solid var(--line); } }
    .stl { list-style: none; padding: 0; margin: 8px 0 0; li { display: grid; grid-template-columns: 18px minmax(0, 1fr) auto; gap: 6px; padding: 2px 0; color: var(--muted); &.d, &.n { color: var(--text); } small { grid-column: 2 / 4; color: var(--muted); } } .tm { font-size: 11px; color: var(--muted); } }
    pre.yaml { margin: 6px 0 0; padding: 8px 10px; border-radius: 8px; background: var(--bg); font-size: 11.5px; max-height: 220px; overflow: auto; white-space: pre-wrap; }
    .ckl { display: flex; gap: 6px; align-items: center; color: var(--text) !important; }
    .dlb { white-space: nowrap; &.busy { background: linear-gradient(90deg, var(--accent-soft) var(--p, 0%), transparent var(--p, 0%)); } }
    @media (max-width: 1250px) { .kpis { grid-template-columns: repeat(3, minmax(0, 1fr)); } .dscroll { grid-template-columns: minmax(0, 1fr); } }
  `],
})
export class LabAppliances extends LabImages {
  constructor() {
    super();
    // opened again on another view (Images, Templates, On the hosts) or on an image while it shows
    effect(() => { const v = this.start(); const id = this.image0(); untracked(() => { this.tab.set(v); if (id) this.pick(id); }); });
  }
  protected readonly imp = signal(false);
  protected readonly qf = signal<Quick>('all');
  protected readonly quicks: { k: Quick; name: string }[] = [
    { k: 'all', name: 'All' }, { k: 'here', name: 'On this computer' }, { k: 'todo', name: 'To download' }, { k: 'host', name: 'On the host' }, { k: 'used', name: 'In use' },
  ];
  private x(im: EmuImage) { return im as EmuImage & { onHost?: boolean; missing?: boolean; upload?: string }; }
  family(im: EmuImage) { return im.persona?.osName || im.deviceType || tr('Other'); }
  private inQuick(im: EmuImage, k: Quick) {
    switch (k) {
      case 'here': return !!im.file && !this.x(im).missing;
      case 'todo': return !im.file || this.needs(im) || !!this.dl(im);
      case 'host': return !!this.x(im).onHost;
      case 'used': return this.usedBy(im.id) > 0 || this.tplBy(im.id) > 0;
      default: return true;
    }
  }
  quickN(k: Quick) { return this.images().filter(im => this.inQuick(im, k)).length; }
  /** The images by family, filtered by the quick filter too. */
  protected listGroups = computed(() => {
    const k = this.qf();
    return this.groups().map(g => ({ name: g.name, items: g.items.filter(im => this.inQuick(im, k)) })).filter(g => g.items.length);
  });
  /** One badge per image: where its file is, or what is happening to it. */
  badge(im: EmuImage): Badge {
    const d = this.dl(im), x = this.x(im);
    if (d) return d === 'waiting' ? { text: tr('waiting'), cls: 'm' } : { text: tr('downloading {0}', d), cls: 'w' };
    if (x.upload) return { text: tr('copying {0}', x.upload), cls: 'w' };
    if (x.missing) return { text: tr('file missing'), cls: 'r' };
    if (!im.file) return im.url ? { text: tr('to download'), cls: 'm' } : { text: tr('no file'), cls: 'r' };
    if (x.onHost) return { text: tr('on the host'), cls: 'h' };
    return { text: tr('this computer'), cls: 'ok' };
  }
  protected families = computed(() => new Set(this.images().map(im => this.family(im))).size);
  protected here = computed(() => { const l = this.images().filter(im => !!im.file && !this.x(im).missing); return { n: l.length, bytes: l.reduce((a, im) => a + (im.size || 0), 0) }; });
  protected onHost = computed(() => this.images().filter(im => this.x(im).onHost).length);
  protected hostName = computed(() => { const h = this.lab.emu()?.host ?? ''; return this.hosts().find(x => x.session === h)?.name || h.split('/').pop() || ''; });
  protected inUse = computed(() => {
    const ids = new Set(this.images().filter(im => this.usedBy(im.id) > 0 || this.tplBy(im.id) > 0).map(im => im.id));
    return { n: ids.size, sw: this.lab.devices().filter(d => ids.has(d.image ?? '')).length, tp: this.templates().filter(t => ids.has(t.image)).length };
  });
}
