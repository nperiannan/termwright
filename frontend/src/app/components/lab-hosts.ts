import { Component, ElementRef, OnDestroy, OnInit, computed, effect, inject, input, output, signal, viewChild } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { LabService } from '../core/lab.service';
import { Backend, errText } from '../core/backend';
import { EmuHost, EmuMatrix, EmuMatrixImage, EmuRemoveInfo, EmuRunRow, SessionNode } from '../core/models';
import { Ui } from '../core/ui.service';
import { tr } from '../core/i18n';
import { PAGE, emuLinuxType } from './lab-pages';

type HostView = 'host' | 'images' | 'matrix';
interface AddRow { path: string; name: string; hostname: string; have: boolean; }

/**
 * KVM Hosts: the hosts on the left (they scroll; Add and Remove stay at the bottom), the chosen host on the right
 * with its numbers, checks and management options. Images shows the chosen host's images; Images (All Hosts) the
 * hosts × images matrix. Add lists the Linux SSH sessions as the Session Manager has them, searchable by name or IP.
 */
@Component({
  selector: 'app-lab-hosts',
  imports: [FormsModule],
  template: `
  <div class="ov" (click)="closed.emit()"></div>
  <div class="mdl kh" role="dialog" [attr.aria-label]="tr('KVM Hosts')">
    <div class="mh"><div><h2>{{ tr('KVM Hosts') }}</h2><div class="muted small">{{ tr('Linux machines the switches run on - reached with their SSH sessions') }}</div></div><span class="sp"></span></div>
    <div class="khb" [class.full]="view() === 'matrix'">
      @if (view() !== 'matrix') {
        <div class="khl">
          <div class="khs"><span class="mg">⌕</span><input [(ngModel)]="q" [placeholder]="tr('Search hosts by name or IP')" [attr.aria-label]="tr('Search hosts by name or IP')" /></div>
          <div class="khsc">
            @for (h of shown(); track h.session) { @let u = use(h);
              <div class="hi" [class.on]="cur()?.session === h.session" (click)="pickHost(h.session)">
                <div class="t"><i class="d" [class]="'d ' + dot(h)"></i><b>{{ h.name || h.session }}</b><span class="sp"></span><span class="pill" [class]="'pill ' + pill(h).cls">{{ pill(h).text }}</span></div>
                <div class="muted small ell">{{ folder(h) }}{{ folder(h) ? ' · ' : '' }}{{ h.info.os || '?' }} · {{ isVM(h) ? tr('VM') : tr('bare metal') }}</div>
                <div class="m"><div><span>CPU <em>{{ u.cpu }}</em></span><div class="bar"><i [style.width.%]="u.cpuPct"></i></div></div><div><span>RAM <em>{{ u.ram }}</em></span><div class="bar m"><i [style.width.%]="u.ramPct"></i></div></div></div>
              </div>
            } @empty { <p class="muted small" style="padding: 4px 12px">{{ hosts().length ? tr('No host matches the search.') : tr('No KVM host yet: ＋ Add below.') }}</p> }
          </div>
        </div>
      }
      <div class="khd">
        @switch (view()) {
          @case ('host') {
            @if (cur(); as h) {
              @if (rm(); as r) {
                <div class="card rmc"><div class="row2" style="justify-content: flex-start"><b style="flex: none">{{ tr('Remove {0}', h.name || h.session) }}</b><span class="muted small">{{ tr('choose what stays on the host; Termwright forgets the host either way') }}</span><span class="sp"></span><a class="muted" (click)="rm.set(null)">✕</a></div>
                  @if (r.error) { <p class="small r">{{ tr('The host cannot be reached ({0}): it can only be forgotten - nothing on it changes.', r.error) }}</p> }
                  @else {
                    @if (r.switches) { <p class="small r">{{ tr('{0} switches of this topology run here: move or remove them first.', r.switches) }}</p> }
                    <div class="rmg">
                      <label class="all"><input type="checkbox" [ngModel]="allOn(r)" (ngModelChange)="setAll(r, $event)" name="rall" /> <b>{{ tr('Everything Termwright put on this host') }}</b> <span class="muted">· {{ tr('its images, the switches’ disks and what Set Up did - or tick only some below') }}</span></label>
                      <b class="small">{{ tr('Files') }}</b>
                      <label><input type="checkbox" [(ngModel)]="ro.images" name="rimg" [disabled]="!r.imagesBytes" /> {{ tr('Delete its images') }} <span class="muted">· {{ gb2(r.imagesBytes) }}@if (r.imagesDir) { {{ tr('in') }} <code>{{ r.imagesDir }}</code> }</span></label>
                      <label><input type="checkbox" [(ngModel)]="ro.labs" name="rlab" [disabled]="!r.labDirs" /> {{ tr('Delete the switches’ own disks') }} <span class="muted">· {{ tr('{0} topologies, {1} - their configurations go with them', r.labDirs, gb2(r.labBytes)) }}</span></label>
                      <b class="small">{{ tr('What Set Up did') }}</b>
                      @if (!r.sudoRule && !r.group && !(r.pkgs ?? []).length) { <span class="muted small">{{ tr('Nothing recorded (set up by hand, or by an older Termwright): nothing to take back.') }}</span> }
                      @if (r.sudoRule) { <label><input type="checkbox" [(ngModel)]="ro.sudoRule" name="rsu" /> {{ tr('Remove its sudo rule') }} <span class="muted">· /etc/sudoers.d/termwright</span></label> }
                      @if (r.group) { <label><input type="checkbox" [(ngModel)]="ro.group" name="rgr" /> {{ tr('Take {0} out of the kvm group', h.info.user) }}</label> }
                      @if ((r.pkgs ?? []).length) { <label><input type="checkbox" [(ngModel)]="ro.pkgs" name="rpk" /> {{ tr('Uninstall the packages it installed') }} <span class="muted">· {{ (r.pkgs ?? []).join(', ') }}</span></label> }
                      @if ((ro.sudoRule || ro.group || ro.pkgs) && !r.fullSudo) { <input type="password" [(ngModel)]="ro.password" name="rpw" [placeholder]="tr('sudo password of {0}', h.info.user)" autocomplete="off" style="max-width: 260px" /> }
                    </div>
                  }
                  <div class="row2" style="justify-content: flex-end; margin-top: 10px"><button type="button" (click)="rm.set(null)">{{ tr('Cancel') }}</button>
                    <button type="button" class="danger" (click)="removeNow(h, r)" [disabled]="lab.busy() || !!r.switches || ((ro.sudoRule || ro.group || ro.pkgs) && !r.fullSudo && !ro.password && !r.error)">{{ removeLabel(r) }}</button></div>
                </div>
              }
              <div class="hd"><div class="f1"><h3>{{ h.name || h.session }} <span class="pill" [class]="'pill ' + stateCls(h)">{{ state(h) }}</span></h3>
                  <div class="chips"><span>{{ h.info.user }}&#64;{{ addr(h) }}</span><span>{{ h.info.os || '?' }}</span>@if (h.info.kernel) { <span>{{ tr('kernel') }} {{ h.info.kernel }}</span> }@if (h.info.arch) { <span>{{ h.info.arch }}</span> }<span>{{ isVM(h) ? tr('VM ({0})', h.info.virt ?? '') : tr('bare metal') }}</span>@if (h.checked) { <span>{{ tr('checked {0}', h.checked) }}</span> }</div></div>
                <div class="acts"><button type="button" (click)="openImages()" [title]="tr('The images on this host')">▤ {{ tr('Images') }} @if (m()) { <span class="muted">{{ haveN(h.session) }}</span> }</button>
                  <button type="button" (click)="lab.checkHost(h.session)" [disabled]="lab.busy() || (!!setupHere() && !setupHere()!.done)">{{ tr('Check Again') }}</button>
                  @if (!setupHere()) { <button type="button" [class.primary]="h.info.canSetup || !h.info.sudo" (click)="setupOpen.set(!setupOpen()); rm.set(null)" [title]="h.info.canSetup || !h.info.sudo ? tr('Install and set up what is missing') : tr('Everything is there: run every step again, for example after a change on the host')">{{ h.info.canSetup || !h.info.sudo ? tr('Set Up…') : tr('Set Up Again…') }}</button> }</div></div>
              @if (setupOpen() || setupHere()) {
                <div class="card setupc"><div class="row2" style="justify-content: flex-start"><b style="flex: none">{{ tr('Set Up the Host') }}</b><span class="muted small">{{ tr('installs what is missing, as root on the host') }}</span><span class="sp"></span>
                    @if (!setupHere() || setupHere()!.done) { <a class="muted" (click)="setupOpen.set(false); lab.setup.set(null)">✕</a> }</div>
                  @if (setupHere(); as s) {
                    <div class="stp">@for (st of steps; track st) { <span class="small" [class]="'small ' + stepCls(s, st)">{{ stepMark(s, st) }} {{ tr(stepLabel(st)) }}</span> }
                      <span class="sp"></span><b class="small" [class.r]="!!s.error" [class.g]="s.done && !s.error">{{ s.done ? (s.error ? tr('Set-up failed') + ': ' + s.error : tr('Set up') + ' ✓ · ' + tr('checked again')) : tr('Setting up…') }}</b></div>
                    <pre class="log full">{{ s.lines.join('\\n') }}</pre>
                  } @else {
                    @if ((h.info.plan ?? []).length) { <ul class="small">@for (p of h.info.plan ?? []; track p) { <li>{{ p }}</li> }</ul> }
                    @else { <p class="small muted">{{ tr('Nothing is missing. Set Up runs every step again - packages, the NAT network, KVM, the sudo rule and the kvm group - which is safe to repeat, for example after something changed on the host.') }}</p> }
                    <div class="row2" style="justify-content: flex-start">@if (!h.info.sudoAll) { <input type="password" [(ngModel)]="pw" name="spw" [placeholder]="tr('sudo password of {0}', h.info.user)" autocomplete="off" style="max-width: 260px" /> }
                      <button type="button" class="primary" (click)="setup(h)" [disabled]="!h.info.sudoAll && !pw">{{ tr('Set Up') }}</button></div>
                  }
                </div>
              }
              @let u = use(h); @let run = runOn(h.session);
              <div class="kpis">
                <div class="kpi"><span>vCPU</span><b>{{ u.vcpus }} <small>{{ tr('of {0} · load {1}', h.info.cpus, h.info.load) }}</small></b><div class="bar"><i [style.width.%]="u.cpuPct"></i></div></div>
                <div class="kpi"><span>{{ tr('Memory') }}</span><b>{{ gb(h.info.memFree) }} <small>{{ tr('free of {0}', gbN(h.info.memTotal)) }}</small></b><div class="bar m"><i [style.width.%]="pct(h.info.memTotal - h.info.memFree, h.info.memTotal)"></i></div></div>
                <div class="kpi"><span>{{ tr('Disk · ~/{0}', h.info.dir) }}</span><b>{{ gb(h.info.diskFree) }} <small>{{ tr('free') }}</small></b><div class="bar"><i [style.width.%]="diskPct(h)" style="background: var(--s3, #199e70)"></i></div></div>
                <div class="kpi"><span>{{ tr('Running here') }}</span><b>{{ runN(run) }} <small>{{ run.length === 1 ? tr('devices · 1 topology') : tr('devices · {0} topologies', run.length) }}</small></b><div class="bar"><i [style.width.%]="runN(run) ? 100 : 0" style="background: var(--success, #1fa45a)"></i></div></div>
              </div>
              <div class="card"><h4>{{ tr('Running here') }} @if (run.length) { <span class="muted small">{{ runSummary(run) }}</span> }</h4>
                @for (r of run; track r.topology) {
                  <div class="run"><b>{{ r.topology }}</b>@for (s of r.switches; track s) { <span class="dev">{{ s }}</span> }<span class="sp"></span>@if (!r.open && !r.topology.includes('(')) { <button type="button" class="sm" (click)="openTopo(r.topology)">{{ tr('Open') }}</button> }</div>
                } @empty { <p class="muted small" style="margin: 0">{{ running() ? tr('No switch runs here.') : tr('Reading…') }}</p> }
              </div>
              <div class="grid2">
                <div class="card"><h4>{{ tr('Ready to run switches') }} @if (fixN(h)) { <span class="tag wr">{{ tr('{0} to fix', fixN(h)) }}</span> } @else { <span class="tag">{{ tr('all set') }}</span> }</h4>
                  <div class="ck"><span [class]="lv('packages', h.info.qemu ? 'ok' : 'bad')">{{ lm('packages', h.info.qemu ? '✓' : '✕') }}</span><b>QEMU</b><span class="muted">{{ lt('packages') || (h.info.qemu || tr('not installed')) + (h.info.qemuImg ? ' · qemu-img' : '') }}</span></div>
                  <div class="ck"><span [class]="h.info.kvm === 'none' && !h.info.vt ? 'wr' : lv('group', h.info.kvm === 'yes' ? 'ok' : 'wr')">{{ h.info.kvm === 'none' && !h.info.vt ? '!' : lm('group', h.info.kvm === 'yes' ? '✓' : '!') }}</span><b>KVM</b><span class="muted">{{ h.info.kvm === 'yes' ? tr('/dev/kvm: full speed') : h.info.kvm === 'noaccess' ? tr('no access to /dev/kvm') : tr('no /dev/kvm: software emulation, slow') }}</span></div>
                  <div class="ck"><span [class]="lv('sudo', h.info.sudo ? 'ok' : 'wr')">{{ lm('sudo', h.info.sudo ? '✓' : '!') }}</span><b>sudo</b><span class="muted">{{ lt('sudo') || (h.info.sudo ? tr('without password: taps, bridges, NAT') : tr('only port forwarding')) }}</span></div>
                  <div class="ck"><span [class]="lv('packages', h.info.guestfs ? 'ok' : 'bad')">{{ lm('packages', h.info.guestfs ? '✓' : '✕') }}</span><b>libguestfs</b><span class="muted">{{ lt('packages') || tr('writes persona files') }}</span></div>
                  <div class="ck"><span [class]="lv('nat', h.info.nat ? 'ok' : 'bad')">{{ lm('nat', h.info.nat ? '✓' : '✕') }}</span><b>{{ tr('NAT network') }}</b><span class="muted">{{ lt('nat') || (h.info.nat ? 'virbr0 · ' + h.info.nat : tr('libvirt’s virbr0 with DHCP')) }}</span></div>
                  @for (p of h.info.problems ?? []; track p) { <p class="r small" style="margin: 8px 0 0">{{ p }}</p> }
                </div>
                <div class="card"><h4>{{ tr('Management of the switches') }} <span class="muted small">{{ tr('how you reach them') }}</span></h4>
                  <div class="mgs">
                    <div class="mo" [class.best]="best(h) === 'nat'"><span [class]="h.info.sudo && h.info.nat ? 'ok' : 'bad'">{{ h.info.sudo && h.info.nat ? '✓' : '✕' }}</span><div><b>{{ tr('NAT behind the host') }}</b><small>{{ h.info.nat ? tr('DHCP on virbr0') : tr('after Set Up') }}</small></div>@if (best(h) === 'nat') { <span class="tag">{{ tr('best here') }}</span> }</div>
                    <div class="mo"><span [class]="h.info.sudo && h.info.lanAddr ? 'ok' : 'bad'">{{ h.info.sudo && h.info.lanAddr ? '✓' : '✕' }}</span><div><b>{{ tr('Host network, routed (cloud)') }}</b><small>{{ h.info.lanAddr ? h.info.uplink + ' ' + h.info.lanAddr + ' · ' + tr('fixed addresses') : tr('no network port with an address') }}</small></div></div>
                    <div class="mo" [class.best]="best(h) === 'lan'"><span [class]="h.info.sudo && h.info.lanKind ? (isVM(h) ? 'wr' : 'ok') : 'bad'">{{ h.info.sudo && h.info.lanKind ? (isVM(h) ? '!' : '✓') : '✕' }}</span><div><b>{{ tr('Host network, bridged') }}</b><small>{{ h.info.lanIf ? h.info.lanIf + ' (' + h.info.lanKind + ')' : tr('no port to join') }}{{ isVM(h) ? ' · ' + tr('virtual switch must allow MAC changes') : '' }}</small></div>@if (best(h) === 'lan') { <span class="tag">{{ tr('best here') }}</span> }</div>
                    <div class="mo" [class.best]="best(h) === 'bridge' || best(h) === 'forward'"><span class="ok">✓</span><div><b>{{ tr('Lab bridge · Port forwarding') }}</b><small>{{ tr('from the host only · always works') }}</small></div>@if (best(h) === 'bridge' || best(h) === 'forward') { <span class="tag">{{ tr('best here') }}</span> }</div>
                  </div>
                  @for (n of h.info.notes ?? []; track n) { <div class="note">{{ n }}</div> }
                </div>
              </div>
            } @else { <p class="muted">{{ tr('No KVM host yet: ＋ Add below.') }}</p> }
          }
          @case ('images') {
            @if (cur(); as h) { @let list = hostImages(h.session);
              <div class="hd"><div class="f1"><a class="bk" (click)="view.set('host')">‹ {{ tr('Back to {0}', h.name || h.session) }}</a><h3 class="mt4">{{ tr('Images on {0}', h.name || h.session) }}</h3>
                  <div class="sum"><span><b>{{ list.length }}</b> {{ tr('images') }}</span><span><b>{{ size(usedBytes(list)) }}</b> {{ tr('used') }}</span><span><b>{{ size(mh(h.session)?.diskFree ?? 0) }}</b> {{ tr('free on {0}', mh(h.session)?.imagesDir || '~/' + h.info.dir) }}</span>@if (copyingN(h.session)) { <span><b>{{ copyingN(h.session) }}</b> {{ tr('copying') }}</span> }</div></div>
                <div class="acts"><input class="srch" [(ngModel)]="iq" [placeholder]="tr('Search images')" /><select [(ngModel)]="itype" [title]="tr('Type')"><option value="">{{ tr('Type') }}</option>@for (t of types(); track t) { <option [value]="t">{{ t }}</option> }</select></div></div>
              @if (!m()) { <p class="muted">{{ tr('Reading the hosts…') }}</p> }
              @else {
                <div class="tbl">
                  <div class="tr h it7"><span>{{ tr('Image') }}</span><span>{{ tr('Type') }}</span><span>{{ tr('Version') }}</span><span>{{ tr('Size') }}</span><span>{{ tr('Used by (running here)') }}</span><span>{{ tr('State') }}</span><span></span></div>
                  @for (im of list; track im.id) { @let st = cell(h.session, im.id); @let users = usedBy(h.session, im.id);
                    <div class="tr it7"><b [title]="im.name">{{ im.name }}</b><span>@if (typeOf(im.id); as t) { <span class="ty" [style.--c]="typeColor(t)">{{ t }}</span> }</span><span class="mono">{{ im.version || '-' }}</span><span>{{ size(im.size) }}</span><span [class.muted]="!users.length">{{ users.length ? users.join(', ') : '-' }}</span>
                      <span>@switch (kind(st)) {
                        @case ('ok') { <span class="c-ok">✓ {{ tr('on this host') }}</span> }
                        @case ('copying') { <span class="c-cp">{{ tr('copying {0} %', pctOf(st)) }}</span><div class="mini l"><i [style.width.%]="pctOf(st)"></i></div> }
                        @case ('queued') { <span class="muted">{{ tr('waiting to copy') }}</span> }
                        @case ('low') { <span class="wn">⚠ {{ tr('low space after copy') }}</span> }
                        @case ('failed') { <span class="bad" [title]="st.slice(8)">{{ tr('failed') }}</span> }
                        @case ('partial') { <span class="wn">{{ tr('copy broke off') }}</span> }
                        @case ('different') { <span class="wn">{{ tr('different file') }}</span> }
                        @default { <span class="muted">{{ st }}</span> } }</span>
                      <span class="ra">
                        <select class="cpb" [ngModel]="''" (ngModelChange)="copyTo(im, $event)" [title]="tr('Copy this image to another host')"><option value="" disabled>{{ tr('Copy to…') }}</option>@for (o of otherHosts(h.session, im.id); track o.session) { <option [value]="o.session">{{ o.name }}</option> }@if (otherHosts(h.session, im.id).length > 1) { <option value="*">{{ tr('All hosts without it') }}</option> }@if (!otherHosts(h.session, im.id).length) { <option value="" disabled>{{ tr('every host has it') }}</option> }</select>
                        @if (kind(st) === 'ok' && !users.length) { <button type="button" class="cpb danger-ghost" (click)="removeImage(h, im)">{{ tr('Remove') }}</button> }
                        @if (['low', 'failed', 'partial', 'different'].includes(kind(st))) { <button type="button" class="cpb" (click)="sync([h.session], [im.id], kind(st) !== 'partial')">{{ kind(st) === 'partial' ? tr('Go On') : tr('Retry') }}</button> }
                      </span></div>
                  } @empty { <p class="muted small pd">{{ iq || itype ? tr('No image matches.') : tr('No image on this host yet: Copy here from Images (All Hosts).') }}</p> }
                </div>
                <p class="muted small" style="margin: 0">{{ tr('Remove is offered only for images no device on this host uses. Copy to… sends the image to another host (it goes on where it stopped when the link breaks).') }}</p>
              }
            }
          }
          @case ('matrix') {
            <div class="hd"><div class="f1"><a class="bk" (click)="view.set('host')">‹ {{ tr('KVM Hosts') }}</a><h3 class="mt4">{{ tr('Images on all hosts') }}</h3><div class="muted small mt4">{{ tr('Which host has which image: a switch can run (or move) only where its image is.') }}</div></div>
              <div class="acts"><input class="srch" [(ngModel)]="iq" [placeholder]="tr('Search images')" /><select [(ngModel)]="itype" [title]="tr('Type')"><option value="">{{ tr('Type') }}</option>@for (t of types(); track t) { <option [value]="t">{{ t }}</option> }</select>
                <button type="button" (click)="copyMissing()" [disabled]="!missingN()">{{ tr('Copy Missing to All…') }}</button></div></div>
            @if (!m()) { <p class="muted">{{ tr('Reading the hosts…') }}</p> }
            @else {
              <div class="tbl mxw"><table class="mx">
                <tr><th>{{ tr('Image') }}</th>@for (x of m()?.hosts ?? []; track x.session) { <th>{{ x.name }}<small>{{ x.error ? tr('not reached') : tr('{0} free', size(x.diskFree)) }}</small></th> }</tr>
                @for (im of mImages(); track im.id) {
                  <tr><td><b>{{ im.name }}{{ im.version ? ' ' + im.version : '' }}</b>@if (typeOf(im.id); as t) { <span class="ty" [style.--c]="typeColor(t)">{{ t }}</span> }<span class="muted small" style="margin-left: 6px">{{ size(im.size) }}</span></td>
                    @for (x of m()?.hosts ?? []; track x.session) { @let st = cell(x.session, im.id);
                      <td>@switch (kind(st)) {
                        @case ('ok') { <span class="c-ok" [title]="tr('on the host')">✓</span> }
                        @case ('copying') { <span class="c-cp">{{ pctOf(st) }} %</span><div class="mini"><i [style.width.%]="pctOf(st)"></i></div> }
                        @case ('queued') { <span class="muted small">{{ tr('waiting') }}</span> }
                        @case ('unknown') { <span class="muted">?</span> }
                        @case ('low') { <button type="button" class="cpb" (click)="sync([x.session], [im.id], true)" [title]="st.slice(5)">⚠ {{ tr('Copy Anyway') }}</button> }
                        @case ('failed') { <button type="button" class="cpb" (click)="sync([x.session], [im.id], true)" [title]="st.slice(8)">{{ tr('Retry') }}</button> }
                        @default { <button type="button" class="cpb" (click)="sync([x.session], [im.id], false)" [disabled]="!!x.error">{{ tr('Copy here') }}</button> } }</td>
                    }</tr>
                } @empty { <tr><td [attr.colspan]="(m()?.hosts?.length ?? 0) + 1" class="muted small">{{ tr('No image matches.') }}</td></tr> }
              </table></div>
              <div class="leg"><span><span class="c-ok">✓</span> {{ tr('on the host') }}</span><span><span class="c-cp">64 %</span> {{ tr('copying') }}</span><span><button type="button" class="cpb" tabindex="-1" style="height: 20px">{{ tr('Copy here') }}</button> {{ tr('not there: copies it from a host that has it') }}</span></div>
            }
          }
        }
      </div>
    </div>
    <div class="khf">
      <div class="l">
        @if (view() === 'matrix') { <button type="button" (click)="view.set('host')">‹ {{ tr('Hosts') }}</button><span class="cnt">{{ tr('{0} hosts · {1} images', hosts().length, m()?.images?.length ?? 0) }}</span> }
        @else { <button type="button" class="primary" (click)="toggleAdd()" [attr.aria-expanded]="addOpen()">＋ {{ tr('Add') }}</button><button type="button" class="danger-ghost" (click)="remove()" [disabled]="!cur() || loadingRm()">{{ loadingRm() ? tr('Reading…') : '− ' + tr('Remove') }}</button><span class="cnt">{{ tr('{0} hosts', hosts().length) }}</span> }
      </div>
      <div class="r">
        @if (view() === 'matrix') { <span class="muted small">{{ tr('Copies run in the background; the Events tab shows them.') }}</span> }
        @else { <button type="button" (click)="openMatrix()" [title]="tr('Which host has which image')">▦ {{ tr('Images (All Hosts)') }}</button> }
        <span class="sp"></span><button type="button" (click)="closed.emit()">{{ tr('Close') }}</button>
      </div>
    </div>
    @if (addOpen()) {
      <div class="addp" role="dialog" [attr.aria-label]="tr('Add hosts')" (keydown.escape)="addOpen.set(false)">
        <div class="ph"><b>{{ tr('Add hosts') }}</b><div class="muted small">{{ tr('Your Linux SSH sessions, as in the Session Manager. Search by name or IP address.') }}</div></div>
        <div class="as"><span class="mg">⌕</span><input #aqi [(ngModel)]="aq" [placeholder]="tr('Name or IP address')" [attr.aria-label]="tr('Name or IP address')" /><span class="k">{{ tr('{0} of {1}', addShownN(), addAll().length) }}</span></div>
        <div class="tree">
          @for (f of addTree(); track f.path) {
            <div class="fo">▾ <span class="fi"></span>{{ f.name }}<span class="n">{{ tr('{0} Linux', f.rows.length) }}</span></div>
            @for (s of f.rows; track s.path) {
              <label class="se" [class.on]="picked().has(s.path)" [class.dis]="s.have"><input type="checkbox" [checked]="picked().has(s.path)" [disabled]="s.have" (change)="togglePick(s.path)" /><span class="ic">&gt;_</span>
                <span class="ell">@for (p of hl(s.name); track $index) { @if (p.m) { <mark>{{ p.t }}</mark> } @else { {{ p.t }} } }</span>
                <span class="mono ell">@for (p of hl(s.hostname); track $index) { @if (p.m) { <mark>{{ p.t }}</mark> } @else { {{ p.t }} } }</span><span class="muted small">{{ s.have ? tr('in the lab') : '' }}</span></label>
            }
          } @empty { <p class="muted small" style="padding: 8px">{{ addAll().length ? tr('No session matches.') : tr('No Linux SSH session yet: make one in the Session Manager (device type Linux).') }}</p> }
        </div>
        <div class="pf"><span class="muted small">{{ tr('{0} selected · Termwright checks QEMU, KVM and sudo first', picked().size) }}</span><span class="sp"></span><button type="button" (click)="addOpen.set(false)">{{ tr('Cancel') }}</button>
          <button type="button" class="primary" (click)="addPicked()" [disabled]="!picked().size || lab.busy()">{{ lab.busy() ? tr('Logging in and checking…') : tr('Check and Add') }}</button></div>
      </div>
    }
  </div>`,
  styles: [PAGE, `
  .mdl.kh { display: grid; grid-template-rows: 64px minmax(0, 1fr) 56px; padding: 0; overflow: hidden; }
  .khb { display: grid; grid-template-columns: 290px minmax(0, 1fr); min-height: 0; &.full { grid-template-columns: minmax(0, 1fr); } }
  .khl { border-right: 1px solid var(--line); background: var(--surface-2); display: flex; flex-direction: column; min-height: 0; }
  .khs { margin: 12px; height: 32px; border-radius: 8px; border: 1px solid var(--line); background: var(--surface); display: flex; align-items: center; gap: 6px; padding: 0 10px;
    .mg { color: var(--muted); } input { flex: 1; min-width: 0; border: 0; background: transparent; height: 28px; padding: 0; outline: none; font-size: 12.5px; } }
  .khsc { overflow: auto; flex: 1; min-height: 0; padding-bottom: 8px; }
  .hi { margin: 0 10px 8px; padding: 10px 12px; border-radius: 10px; border: 1px solid transparent; cursor: pointer;
    &:hover { background: var(--surface); } &.on { background: color-mix(in srgb, var(--accent) 14%, transparent); border-color: color-mix(in srgb, var(--accent) 55%, transparent); }
    .t { display: flex; align-items: center; gap: 8px; b { font-size: 13.5px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; } }
    .m { display: grid; grid-template-columns: 1fr 1fr; gap: 8px; margin-top: 7px; span { font-size: 10.5px; color: var(--muted); display: flex; justify-content: space-between; margin-bottom: 2px; em { font-style: normal; } } } }
  .d { width: 8px; height: 8px; border-radius: 50%; flex: none; background: var(--success, #1fa45a); &.wa { background: var(--warning, #e39b17); } &.bad { background: var(--danger); } }
  .bar { height: 4px; border-radius: 2px; background: color-mix(in srgb, var(--muted) 22%, transparent); overflow: hidden; i { display: block; height: 100%; background: var(--s1, #3987e5); transition: width .6s; } &.m i { background: var(--s7, #9085e9); } }
  .pill { font-size: 10.5px; padding: 1px 8px; border-radius: 9px; font-weight: 600; white-space: nowrap; background: color-mix(in srgb, var(--success, #1fa45a) 16%, transparent); color: var(--success, #1fa45a);
    &.run { background: color-mix(in srgb, var(--s1, #3987e5) 18%, transparent); color: var(--s1, #3987e5); } &.wr { background: color-mix(in srgb, var(--warning, #e39b17) 16%, transparent); color: var(--warning, #e39b17); } &.bad { background: color-mix(in srgb, var(--danger) 16%, transparent); color: var(--danger); } }
  .khd { overflow: auto; padding: 18px 22px; display: flex; flex-direction: column; gap: 14px; min-width: 0; .card { margin: 0; } }
  .hd { display: flex; align-items: flex-start; gap: 12px; .f1 { flex: 1; min-width: 0; } h3 { margin: 0; font-size: 18px; display: flex; align-items: center; gap: 8px; } }
  .mt4 { margin-top: 4px !important; }
  .chips { display: flex; flex-wrap: wrap; gap: 6px; margin-top: 6px; span { font-size: 11.5px; color: var(--muted); border: 1px solid var(--line); border-radius: 7px; padding: 2px 8px; background: var(--surface-2); } }
  .acts { display: flex; gap: 8px; align-items: center; flex-wrap: wrap; justify-content: flex-end; }
  .kpis { display: grid; grid-template-columns: repeat(4, minmax(0, 1fr)); gap: 10px; }
  .kpi { border: 1px solid var(--line); border-radius: 10px; padding: 10px 12px; background: var(--surface-2); > span { font-size: 10.5px; color: var(--muted); text-transform: uppercase; letter-spacing: .05em; } b { display: block; font-size: 20px; margin: 2px 0 6px; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; small { font-size: 12px; color: var(--muted); font-weight: 400; } } }
  h4 { margin: 0 0 8px; font-size: 12.5px; display: flex; align-items: center; gap: 8px; .muted { font-weight: 400; } }
  .run { display: flex; align-items: center; flex-wrap: wrap; gap: 8px; padding: 8px 0; border-top: 1px solid var(--line); &:first-of-type { border-top: 0; } .dev { font-size: 11px; border: 1px solid var(--line); border-radius: 7px; padding: 1px 7px; color: var(--muted); } }
  .grid2 { display: grid; grid-template-columns: 1fr 1.25fr; gap: 12px; }
  .ck { display: grid; grid-template-columns: 18px 110px minmax(0, 1fr); gap: 6px; padding: 5px 0; border-top: 1px solid var(--line); font-size: 12px; &:first-of-type { border-top: 0; } }
  .ok { color: var(--success, #1fa45a); font-weight: 700; } .wr { color: var(--warning, #e39b17); font-weight: 700; } .bad { color: var(--danger); font-weight: 700; } .run2 { color: var(--accent); }
  .tag { font-size: 10px; padding: 1px 7px; border-radius: 8px; border: 1px solid color-mix(in srgb, var(--success, #1fa45a) 60%, transparent); color: var(--success, #1fa45a); white-space: nowrap; font-weight: 500;
    &.wr { border-color: color-mix(in srgb, var(--warning, #e39b17) 60%, transparent); color: var(--warning, #e39b17); } }
  .mgs { display: grid; gap: 6px; }
  .mo { display: grid; grid-template-columns: 16px minmax(0, 1fr) auto; gap: 8px; align-items: start; padding: 8px 10px; border: 1px solid var(--line); border-radius: 9px; background: var(--surface);
    &.best { border-color: color-mix(in srgb, var(--success, #1fa45a) 60%, transparent); } b { font-size: 12.5px; } small { display: block; color: var(--muted); font-size: 11px; margin-top: 1px; } }
  .note { margin-top: 8px; border-radius: 9px; padding: 9px 11px; font-size: 11.5px; background: color-mix(in srgb, var(--warning, #e39b17) 10%, transparent); border: 1px solid color-mix(in srgb, var(--warning, #e39b17) 35%, transparent); }
  .khf { display: grid; grid-template-columns: 290px minmax(0, 1fr); border-top: 1px solid var(--line); background: var(--surface);
    .l { display: flex; align-items: center; gap: 8px; padding: 0 12px; border-right: 1px solid var(--line); background: var(--surface-2); } .r { display: flex; align-items: center; gap: 8px; padding: 0 18px; }
    .cnt { font-size: 11px; color: var(--muted); margin-left: auto; white-space: nowrap; } button { white-space: nowrap; } }
  .bk { color: var(--accent); cursor: pointer; font-size: 12.5px; }
  .sum { display: flex; gap: 8px; flex-wrap: wrap; margin-top: 8px; span { font-size: 11.5px; border: 1px solid var(--line); border-radius: 8px; padding: 3px 10px; background: var(--surface-2); color: var(--muted); } b { color: var(--text); } }
  .srch { width: 240px; height: 30px; }
  .tbl { border: 1px solid var(--line); border-radius: 10px; overflow: hidden; .pd { padding: 10px 14px; margin: 0; } }
  .tr { display: grid; align-items: center; gap: 10px; padding: 8px 14px; border-top: 1px solid var(--line); font-size: 12.5px; &.h { border-top: 0; background: var(--surface-2); font-size: 10.5px; color: var(--muted); text-transform: uppercase; letter-spacing: .05em; padding: 7px 14px; } }
  .it7 { grid-template-columns: minmax(0, 1.4fr) 76px 100px 70px minmax(0, 1.3fr) 130px 170px; }
  .ra { display: flex; gap: 6px; justify-content: flex-end; }
  .ty { font-size: 10.5px; padding: 1px 7px; border-radius: 7px; margin-left: 6px; border: 1px solid color-mix(in srgb, var(--c, var(--muted)) 50%, transparent); color: var(--c, var(--muted)); white-space: nowrap; }
  .tr .ty { margin-left: 0; }
  .c-ok { color: var(--success, #1fa45a); font-weight: 600; } .c-cp { color: var(--s1, #3987e5); } .wn { color: var(--warning, #e39b17); }
  .cpb { font-size: 11px; height: 26px !important; padding: 0 8px; width: auto; }
  select.cpb { width: 112px !important; padding: 0 6px; }
  .mini { height: 3px; border-radius: 2px; background: color-mix(in srgb, var(--muted) 22%, transparent); overflow: hidden; width: 70px; margin: 3px auto 0; i { display: block; height: 100%; background: var(--s1, #3987e5); } &.l { margin: 3px 0 0; } }
  .mxw { overflow: auto; }
  .mx { border-collapse: separate; border-spacing: 0; width: 100%;
    td, th { border-bottom: 1px solid var(--line); padding: 8px 10px; text-align: center; font-size: 12px; }
    tr:last-child td { border-bottom: 0; }
    th { background: var(--surface-2); font-weight: 600; position: sticky; top: 0; small { display: block; font-weight: 400; color: var(--muted); font-size: 10.5px; margin-top: 2px; } }
    td:first-child, th:first-child { text-align: left; position: sticky; left: 0; background: var(--surface); } th:first-child { background: var(--surface-2); } }
  .leg { display: flex; gap: 14px; font-size: 11.5px; color: var(--muted); align-items: center; flex-wrap: wrap; }
  .ell { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .addp { position: absolute; left: 12px; bottom: 64px; width: 520px; height: min(520px, calc(100% - 140px)); background: var(--surface); border: 1px solid color-mix(in srgb, var(--muted) 45%, var(--line)); border-radius: 12px; box-shadow: 0 18px 50px rgba(0,0,0,.5); display: grid; grid-template-rows: auto auto minmax(0, 1fr) auto; z-index: 5;
    .ph { padding: 12px 14px 6px; b { font-size: 14px; } }
    .as { margin: 4px 14px 8px; height: 34px; border: 1px solid var(--accent); border-radius: 8px; display: flex; align-items: center; padding: 0 10px; gap: 8px; .mg { color: var(--muted); } input { flex: 1; min-width: 0; border: 0; background: transparent; outline: none; height: 30px; padding: 0; font-size: 12.5px; } .k { font-size: 10.5px; color: var(--muted); border: 1px solid var(--line); border-radius: 5px; padding: 0 5px; white-space: nowrap; } }
    .tree { overflow: auto; padding: 0 8px 8px; }
    .fo { display: flex; align-items: center; gap: 7px; padding: 6px 8px; font-weight: 600; font-size: 12.5px; .fi { width: 16px; height: 12px; border-radius: 2px; background: #d6a33c; display: inline-block; } .n { margin-left: auto; color: var(--muted); font-weight: 400; font-size: 11px; } }
    .se { display: grid; grid-template-columns: 22px 26px minmax(0, 1fr) 150px 70px; align-items: center; gap: 6px; padding: 6px 8px 6px 22px; border-radius: 8px; font-size: 12.5px; cursor: pointer;
      &:hover { background: var(--surface-2); } &.on { background: color-mix(in srgb, var(--accent) 16%, transparent); } &.dis { opacity: .45; cursor: default; }
      input { width: auto; height: auto; margin: 0; } .ic { width: 22px; height: 22px; border-radius: 6px; background: #1f8a5b; display: grid; place-items: center; font: 600 10px var(--mono, monospace); color: #fff; }
      .mono { font-family: var(--mono, monospace); font-size: 11.5px; color: var(--muted); } mark { background: color-mix(in srgb, var(--warning, #e39b17) 35%, transparent); color: inherit; border-radius: 3px; padding: 0 1px; } }
    .pf { display: flex; align-items: center; gap: 8px; padding: 10px 14px; border-top: 1px solid var(--line); button { white-space: nowrap; } } }
  `],
})
export class LabHosts implements OnInit, OnDestroy {
  readonly closed = output<void>();
  readonly pick0 = input('', { alias: 'select' });
  protected lab = inject(LabService);
  private backend = inject(Backend);
  private ui = inject(Ui);
  protected tr = tr;
  protected view = signal<HostView>('host');
  protected q = '';
  protected iq = '';
  protected itype = '';
  protected aq = '';
  protected hosts = computed<EmuHost[]>(() => this.lab.emu()?.hosts ?? []);
  protected sel = signal('');
  protected cur = computed(() => this.hosts().find(h => h.session === this.sel()) ?? this.hosts()[0] ?? null);
  protected running = signal<EmuRunRow[] | null>(null);
  protected m = signal<EmuMatrix | null>(null);
  protected addOpen = signal(false);
  protected picked = signal(new Set<string>());
  private aqi = viewChild<ElementRef<HTMLInputElement>>('aqi');
  private runTimer = setInterval(() => void this.loadRunning(), 10000);
  private mTimer?: ReturnType<typeof setTimeout>;

  constructor() { effect(() => { if (this.addOpen()) setTimeout(() => this.aqi()?.nativeElement.focus(), 0); }); }
  ngOnInit() { if (this.pick0()) this.sel.set(this.pick0()); void this.loadRunning(); void this.loadMatrix(); }
  ngOnDestroy() { clearInterval(this.runTimer); clearTimeout(this.mTimer); }

  // the list
  protected shown() { const q = this.q.trim().toLowerCase(); return q ? this.hosts().filter(h => [h.name, h.session, h.info.lanAddr ?? '', this.addr(h)].some(x => x.toLowerCase().includes(q))) : this.hosts(); }
  protected pickHost(s: string) { this.sel.set(s); this.rm.set(null); this.setupOpen.set(false); }
  private sessMap = computed(() => { const out = new Map<string, SessionNode>(); const walk = (n?: SessionNode | null) => n?.children?.forEach(c => { if (c.isFolder) walk(c); else out.set(c.path, c); }); walk(this.ui.tree()); return out; });
  protected addr(h: EmuHost) { return this.sessMap().get(h.session)?.hostname || (h.info.lanAddr ?? '').split('/')[0] || h.session; }
  protected folder(h: EmuHost) { const p = h.session.split('/'); return p.length > 1 ? p[p.length - 2] : ''; }
  protected use(h: EmuHost) {
    const u = (this.lab.emu()?.hostUse ?? []).find(x => x.host === h.session), cpus = h.info.cpus || u?.cpus || 0, memMB = h.info.memTotal || u?.memTotal || 0;
    const vcpus = u?.vcpus ?? 0, ram = u?.ram ?? Math.max(0, memMB - (h.info.memFree || 0));
    return { vcpus, cpu: `${vcpus}/${cpus || '?'}`, cpuPct: this.pct(vcpus, cpus), ram: `${this.gbN(ram)}/${this.gbN(memMB)} GB`, ramPct: this.pct(ram, memMB) };
  }
  protected pill(h: EmuHost) {
    const n = this.runN(this.runOn(h.session));
    if (n) return { text: tr('{0} running', n), cls: 'run' };
    const p = (h.info.problems ?? []).length;
    return p ? { text: tr('{0} to fix', p), cls: 'bad' } : h.info.canSetup || !h.info.sudo ? { text: tr('needs set up'), cls: 'wr' } : { text: tr('ready'), cls: '' };
  }
  isVM(h: EmuHost) { return !!h.info.virt && h.info.virt !== 'none' && h.info.virt !== 'unknown'; }
  best(h: EmuHost) { const i = h.info; return i.sudo && i.lanKind && !this.isVM(h) ? 'lan' : i.sudo && i.nat ? 'nat' : i.sudo && i.lanKind ? 'lan' : i.sudo ? 'bridge' : 'forward'; }
  dot(h: EmuHost) { return (h.info.problems ?? []).length ? 'bad' : h.info.canSetup ? 'wa' : 'ok'; }
  state(h: EmuHost) { return (h.info.problems ?? []).length ? tr('{0} to fix', (h.info.problems ?? []).length) : h.info.canSetup ? tr('can be set up') : tr('ready'); }
  stateCls(h: EmuHost) { return (h.info.problems ?? []).length ? 'bad' : h.info.canSetup ? 'wr' : ''; }
  fixN(h: EmuHost) { const i = h.info; return [!i.qemu, i.kvm !== 'yes', !i.sudo, !i.guestfs, !i.nat].filter(Boolean).length; }
  gb(mb?: number) { return !mb ? '-' : mb >= 1024 ? (mb / 1024).toFixed(mb >= 10240 ? 0 : 1) + ' GB' : mb + ' MB'; }
  gbN(mb?: number) { if (!mb) return '0'; const g = mb / 1024; return g >= 10 ? String(Math.round(g)) : String(+g.toFixed(1)); }
  pct(a: number, b: number) { return b ? Math.max(0, Math.min(100, (a / b) * 100)) : 0; }
  diskPct(h: EmuHost) { const x = this.mh(h.session); return x?.diskTotal ? this.pct(x.diskTotal - x.diskFree, x.diskTotal) : 0; }

  // running here
  async loadRunning() { try { this.running.set((await this.backend.emuRunning()) ?? []); } catch { this.running.set([]); } }
  protected runOn(h: string) { return (this.running() ?? []).filter(r => r.host === h); }
  protected runN(rows: EmuRunRow[]) { return rows.reduce((a, r) => a + r.switches.length, 0); }
  protected runSummary(rows: EmuRunRow[]) { return rows.map(r => r.topology + (r.open ? ' · ' + tr('open') : '')).join(', '); }
  async openTopo(name: string) { try { await this.backend.emuOpenTopology(name, false); await this.lab.refresh(); this.closed.emit(); } catch (e) { this.ui.error(e); } }

  // images: the matrix of every host (read once, then while copies run)
  async loadMatrix() {
    clearTimeout(this.mTimer);
    try { this.m.set(await this.backend.emuImageMatrix()); } catch { /* the page says Reading */ }
    const busy = this.m()?.busy || Object.values(this.m()?.cells ?? {}).some(c => Object.values(c).some(s => s.startsWith('copying') || s === 'queued'));
    this.mTimer = setTimeout(() => void this.loadMatrix(), busy ? 2000 : 30000);
  }
  protected openImages() { this.view.set('images'); this.iq = ''; this.itype = ''; void this.loadMatrix(); }
  protected openMatrix() { this.view.set('matrix'); this.iq = ''; this.itype = ''; this.addOpen.set(false); void this.loadMatrix(); }
  protected mh(host: string) { return (this.m()?.hosts ?? []).find(x => x.session === host); }
  cell(host: string, id: string) { return this.m()?.cells?.[host]?.[id] ?? 'missing'; }
  kind(st: string) { return st.startsWith('copying') ? 'copying' : st.startsWith('low:') ? 'low' : st.startsWith('failed:') ? 'failed' : st; }
  pctOf(st: string) { const x = /(\d+) %/.exec(st); return x ? +x[1] : 0; }
  size(n: number) { return !n ? '-' : n >= 2 ** 40 ? (n / 2 ** 40).toFixed(1) + ' TB' : n >= 100 * 2 ** 30 ? Math.round(n / 2 ** 30) + ' GB' : n >= 2 ** 30 ? (n / 2 ** 30).toFixed(1) + ' GB' : n >= 2 ** 20 ? Math.round(n / 2 ** 20) + ' MB' : Math.round(n / 1024) + ' KB'; }
  protected typeOf(id: string) { return (this.lab.emu()?.images ?? []).find(i => i.id === id)?.deviceType ?? ''; }
  protected types() { return [...new Set((this.m()?.images ?? []).map(i => this.typeOf(i.id)).filter(Boolean))].sort(); }
  protected typeColor(t: string) { const pal = ['--s1', '--s7', '--s3', '--s4', '--s2', '--s5', '--s6', '--s8']; let h = 0; for (const c of t.toLowerCase()) h = (h * 31 + c.charCodeAt(0)) >>> 0; return `var(${pal[h % pal.length]})`; }
  private match(im: EmuMatrixImage) { const q = this.iq.trim().toLowerCase(); return (!q || (im.name + ' ' + im.version + ' ' + im.fileName).toLowerCase().includes(q)) && (!this.itype || this.typeOf(im.id) === this.itype); }
  protected mImages() { return (this.m()?.images ?? []).filter(im => this.match(im)); }
  protected hostImages(host: string) { return (this.m()?.images ?? []).filter(im => !['missing', 'unknown'].includes(this.cell(host, im.id)) && this.match(im)); }
  protected haveN(host: string) { return (this.m()?.images ?? []).filter(im => this.cell(host, im.id) === 'ok').length; }
  protected usedBytes(list: EmuMatrixImage[]) { return list.reduce((a, im) => a + (this.cell(this.cur()?.session ?? '', im.id) === 'ok' ? im.size : 0), 0); }
  protected copyingN(host: string) { return (this.m()?.images ?? []).filter(im => ['copying', 'queued'].includes(this.kind(this.cell(host, im.id)))).length; }
  protected usedBy(host: string, id: string) { const e = this.lab.emu(); return (e?.devices ?? []).filter(d => d.image === id && (d.host || e?.host) === host).map(d => d.hostname); }
  protected otherHosts(host: string, id: string) { return (this.m()?.hosts ?? []).filter(x => x.session !== host && !x.error && !['ok', 'copying', 'queued'].includes(this.kind(this.cell(x.session, id)))); }
  protected missingN() { const hs = this.m()?.hosts ?? [], ims = this.m()?.images ?? []; return hs.filter(x => !x.error).reduce((a, x) => a + ims.filter(im => !['ok', 'copying', 'queued', 'unknown'].includes(this.kind(this.cell(x.session, im.id)))).length, 0); }
  async sync(hosts: string[], images: string[], force: boolean) { try { this.m.set(await this.backend.emuSyncImages(hosts, images, force)); } catch (e) { this.ui.toast(errText(e), 'info'); } void this.loadMatrix(); }
  async copyTo(im: EmuMatrixImage, to: string) { if (!to) return; const hs = to === '*' ? this.otherHosts(this.cur()?.session ?? '', im.id).map(x => x.session) : [to]; await this.sync(hs, [im.id], false); }
  async copyMissing() {
    const n = this.missingN();
    if (!(await this.ui.confirm(tr('Copy missing images'), tr('Copy {0} missing images so every host has every image? The copies run in the background.', n), tr('Copy')))) return;
    await this.sync([], [], false);
  }
  async removeImage(h: EmuHost, im: EmuMatrixImage) {
    if (!(await this.ui.confirm(tr('Remove {0} from {1}', im.name, h.name || h.session), tr('Delete the image file on {0}? It stays in the list and on the other hosts, and can be copied here again.', h.name || h.session), tr('Remove'), true))) return;
    try { this.m.set(await this.backend.emuRemoveImageFrom(h.session, im.id)); this.ui.toast(tr('{0} removed from {1}', im.name, h.name || h.session), 'success'); } catch (e) { this.ui.error(errText(e)); }
  }

  // add: the Linux SSH sessions as the Session Manager has them, by folder
  protected toggleAdd() { this.addOpen.set(!this.addOpen()); this.picked.set(new Set()); this.aq = ''; this.rm.set(null); }
  protected addAll = computed<(AddRow & { folder: string; fname: string })[]>(() => {
    const out: (AddRow & { folder: string; fname: string })[] = [], skip = [this.lab.emu()?.folder, this.lab.sim()?.folder].filter(Boolean) as string[];
    const have = new Set(this.hosts().map(h => h.session));
    const walk = (n: SessionNode | null | undefined, fpath: string, fname: string) => n?.children?.forEach(c => {
      if (c.isFolder) { if (!skip.some(f => c.path === f || c.path.startsWith(f + '/'))) walk(c, c.path, c.path); }
      else if (c.protocol === 'SSH2' && emuLinuxType(c.deviceType)) out.push({ path: c.path, name: c.name, hostname: c.hostname ?? '', have: have.has(c.path), folder: fpath, fname: fname || tr('Sessions') });
    });
    walk(this.ui.tree(), '', '');
    return out;
  });
  protected addTree() {
    const q = this.aq.trim().toLowerCase(), groups = new Map<string, { path: string; name: string; rows: AddRow[] }>();
    for (const r of this.addAll()) {
      if (q && !r.name.toLowerCase().includes(q) && !r.hostname.toLowerCase().includes(q) && !r.path.toLowerCase().includes(q)) continue;
      const g = groups.get(r.folder) ?? { path: r.folder, name: r.fname, rows: [] };
      g.rows.push(r); groups.set(r.folder, g);
    }
    return [...groups.values()];
  }
  protected addShownN() { return this.addTree().reduce((a, g) => a + g.rows.length, 0); }
  protected hl(s: string) {
    const q = this.aq.trim(); if (!q || !s) return [{ t: s, m: false }];
    const out: { t: string; m: boolean }[] = []; const low = s.toLowerCase(), ql = q.toLowerCase(); let i = 0;
    for (let j = low.indexOf(ql); j >= 0; j = low.indexOf(ql, i)) { if (j > i) out.push({ t: s.slice(i, j), m: false }); out.push({ t: s.slice(j, j + q.length), m: true }); i = j + q.length; }
    if (i < s.length) out.push({ t: s.slice(i), m: false });
    return out;
  }
  protected togglePick(p: string) { const s = new Set(this.picked()); if (s.has(p)) s.delete(p); else s.add(p); this.picked.set(s); }
  async addPicked() {
    const list = [...this.picked()]; let last = '';
    for (const p of list) { const h = await this.lab.checkHost(p); if (h) last = p; }
    if (last) { this.sel.set(last); this.addOpen.set(false); this.picked.set(new Set()); void this.loadMatrix(); }
  }

  // set up
  protected pw = '';
  protected steps = ['packages', 'nat', 'kvm', 'sudo', 'group'];
  protected setupOpen = signal(false);
  protected setupHere = computed(() => { const s = this.lab.setup(); return s && s.host === this.cur()?.session ? s : null; });
  private live(st: string): '' | 'run' | 'done' {
    const s = this.setupHere(); if (!s || s.done) return '';
    const i = this.steps.indexOf(s.step), j = this.steps.indexOf(st);
    return j < i ? 'done' : j === i ? 'run' : '';
  }
  lv(st: string, cls: string) { const l = this.live(st); return l === 'done' ? 'ok' : l === 'run' ? 'run2' : cls; }
  lt(st: string) { const l = this.live(st); return l === 'done' ? tr('set up - checked again at the end') : l === 'run' ? tr('setting up…') : ''; }
  lm(st: string, mark: string) { const l = this.live(st); return l === 'done' ? '✓' : l === 'run' ? '…' : mark; }
  async setup(h: EmuHost) { const pw = this.pw; this.pw = ''; await this.lab.setupHost(h.session, pw); }
  stepLabel(s: string) { return ({ packages: 'Packages', nat: 'NAT network (libvirt, virbr0)', kvm: 'KVM module', sudo: 'Network commands without a password', group: 'KVM group' } as Record<string, string>)[s] ?? s; }
  stepMark(s: { step: string; done: boolean; error: string }, st: string) { const i = this.steps.indexOf(s.step), j = this.steps.indexOf(st); return s.done && !s.error ? '✓' : j < i ? '✓' : j === i ? (s.error ? '✕' : '…') : '○'; }
  stepCls(s: { step: string; done: boolean; error: string }, st: string) { const m = this.stepMark(s, st); return m === '✓' ? 'g' : m === '✕' ? 'r' : m === '…' ? 'w' : 'muted'; }

  // remove
  protected rm = signal<EmuRemoveInfo | null>(null);
  protected loadingRm = signal(false);
  protected ro = { images: false, labs: false, sudoRule: true, group: false, pkgs: false, password: '' };
  async remove() {
    const h = this.cur(); if (!h) return;
    this.view.set('host'); this.setupOpen.set(false); this.addOpen.set(false); this.loadingRm.set(true);
    this.ro = { images: false, labs: false, sudoRule: true, group: false, pkgs: false, password: '' };
    try { this.rm.set(await this.backend.emuRemoveHostInfo(h.session)); } catch (e) { this.ui.error(errText(e)); } finally { this.loadingRm.set(false); }
  }
  allOn(r: EmuRemoveInfo) { const o = this.ro; return (!r.imagesBytes || o.images) && (!r.labDirs || o.labs) && (!r.sudoRule || o.sudoRule) && (!r.group || o.group) && (!(r.pkgs ?? []).length || o.pkgs); }
  setAll(r: EmuRemoveInfo, on: boolean) { this.ro = { ...this.ro, images: on && !!r.imagesBytes, labs: on && !!r.labDirs, sudoRule: on && r.sudoRule, group: on && r.group, pkgs: on && !!(r.pkgs ?? []).length }; }
  removeLabel(r: EmuRemoveInfo) { const o = this.ro; return r.error ? tr('Forget the Host') : o.images || o.labs || (o.sudoRule && r.sudoRule) || (o.group && r.group) || (o.pkgs && (r.pkgs ?? []).length) ? tr('Remove and Clean Up') : tr('Remove, Keep Everything There'); }
  async removeNow(h: EmuHost, r: EmuRemoveInfo) {
    const o = this.ro, what = [o.images ? tr('its images ({0})', this.gb2(r.imagesBytes)) : '', o.labs ? tr('the switches’ disks ({0})', this.gb2(r.labBytes)) : ''].filter(Boolean);
    if (what.length && !(await this.ui.confirm(tr('Delete on {0}', h.name || h.session), tr('Delete {0} on the host? This cannot be undone.', what.join(tr(' and '))), tr('Delete and Remove'), true))) return;
    const req = { host: h.session, images: !r.error && o.images, labs: !r.error && o.labs, sudoRule: !r.error && o.sudoRule && r.sudoRule, group: !r.error && o.group && r.group, pkgs: !r.error && o.pkgs && !!(r.pkgs ?? []).length, password: o.password, forgetOff: !!r.error };
    this.ro.password = '';
    try { this.lab.emu.set(await this.backend.emuRemoveHostWith(req)); this.rm.set(null); this.sel.set(''); this.ui.toast(tr('{0} is removed', h.name || h.session), 'success'); } catch (e) { this.ui.error(errText(e)); }
  }
  gb2(n: number) { return !n ? '0' : n >= 2 ** 30 ? (n / 2 ** 30).toFixed(1) + ' GB' : Math.round(n / 2 ** 20) + ' MB'; }
}
