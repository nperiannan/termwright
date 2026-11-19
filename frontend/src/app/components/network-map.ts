import { Component, ElementRef, OnDestroy, computed, effect, inject, input, signal, viewChild } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { NgTemplateOutlet } from '@angular/common';
import { DomSanitizer } from '@angular/platform-browser';
import { Backend, errText } from '../core/backend';
import { Session, TopoChange, TopoLink, TopoMap, TopoNode, TopoProgress } from '../core/models';
import { Tabs, TabView } from '../core/tabs.service';
import { Ui } from '../core/ui.service';
import { tr } from '../core/i18n';
import { COMPUTED_LOOKS, LOOKS, Look, MLink, MNode, Prog, THEME_DARK, THEME_LIGHT, View, Zone, dims, kindOf, mapBounds, mapDocument, mapMarkup } from '../core/map-svg';
import { Laid, flowLayout, radialLayout, zonesLayout } from '../core/map-layout';

interface Pos { x: number; y: number; pinned: boolean; }
/** One device while the map is read: where it is in the four reading steps. */
interface DevProg { session: string; name: string; os: string; state: 'queued' | 'running' | 'done' | 'error'; done: number; now: string; err: string; ms: number; }
const VIEWS: { id: View; label: string; hint: string }[] = [
  { id: 'speed', label: 'Speed', hint: 'Cables coloured by speed: 10 Mbps to 10 Gbps green, 25 to 100 Gbps blue, above 100 Gbps thick blue' },
  { id: 'health', label: 'Health', hint: 'Cables up (green) or down (red)' },
  { id: 'stp', label: 'STP', hint: 'The spanning-tree state of the port at each end of every cable' },
  { id: 'lag', label: 'LAG', hint: 'Link aggregation groups highlighted, everything else faded' },
  { id: 'vlan', label: 'VLAN', hint: 'Pick a VLAN: the cables and devices that carry it are highlighted, everything else faded' },
];
const TIERED: Look[] = ['ant', 'material', 'tier', 'focus'];

const STYLES: { id: string; label: string }[] = [
  { id: 'auto', label: 'Auto (best fit)' }, { id: 'layered', label: 'Layered' }, { id: 'spine-leaf', label: 'Spine-leaf' },
  { id: 'tree', label: 'Tree' }, { id: 'ring', label: 'Ring' }, { id: 'star', label: 'Star' },
  { id: 'mesh', label: 'Full mesh' }, { id: 'grid', label: 'Grid' }, { id: 'organic', label: 'Organic' },
];
const INTERVALS = [0, 30, 60, 120, 180, 300];

/** The Network Map: devices and the links between them, read live from the sessions (LLDP / CDP / EDP, port and
 *  STP state, LAGs). Read-only on the devices. */
@Component({
  selector: 'app-network-map',
  imports: [FormsModule, NgTemplateOutlet],
  template: `
    <ng-template #detTpl>
      @if (selLinks().length) {
        @for (l of selLinks(); track l.key) {
          <div class="dl">
            <div class="dl-h"><span class="dl-t">{{ selLinks().length > 1 ? t('Cable') + ' ' + ($index + 1) : t('Cable') }}</span><span [class]="'st ' + l.state">{{ l.state === 'up' ? t('Up') : l.state === 'blocked' ? t('STP blocked') : t('Down') }}</span></div>
            <div class="dl-ends">
              @for (e of linkEnds(l); track e.side) {
                <button type="button" class="dl-end" (click)="selectNode(e.id)" [title]="t('Show this device')">
                  <b>{{ e.name }}</b>
                  <span class="kv"><i>{{ t('Port') }}</i><em>{{ e.port || '—' }}</em></span>
                  <span class="kv"><i>STP</i><em [class]="'stp ' + (e.stp || 'none')"><span class="sd"></span>{{ e.stp ? t(stpText(e.stp)) : t('not read') }}</em></span>
                  @if (e.lag) { <span class="kv"><i>LAG</i><em>{{ e.lag }}</em></span> }
                </button>
                @if ($first) { <span class="dl-x">⇄</span> }
              }
            </div>
            <div class="kvs">
              <span class="kv"><i>{{ t('Speed') }}</i><em>{{ l.speed || t('not read') }}</em></span>
              <span class="kv"><i>VLAN</i><em>{{ vlanText(l.vlans) }}</em></span>
              <span class="kv"><i>{{ t('Seen by') }}</i><em>{{ (l.seen ?? []).join(', ').toUpperCase() || '—' }}</em></span>
            </div>
            @if (l.warn) { <div class="warn">{{ l.warn }}</div> }
          </div>
        }
      } @else if (selNode(); as n) {
        <div class="dd">
          <div class="dd-h"><div><b>{{ n.name }}</b><small>{{ [n.model, (n.os || '').toUpperCase()].filter(isStr).join(' · ') || (n.ghost ? t('Neighbour, not in Session Manager') : '') }}</small></div>
            @if (n.session) { <button type="button" class="nm-btn pri sm" (click)="openSession(n.session)">{{ t('Connect') }}</button> }</div>
          <div class="kvs col">
            <span class="kv"><i>{{ t('Address') }}</i><em>{{ n.ip || '—' }}{{ selSess() ? '  ·  ' + selSess()!.protocol + ' ' + selSess()!.port : '' }}</em></span>
            @if (selSess()?.folder) { <span class="kv"><i>{{ t('Folder') }}</i><em>{{ selSess()!.folder }}</em></span> }
            <span class="kv"><i>{{ t('Neighbours') }}</i><em>{{ nodePeers() }} · {{ nodeLinks().length }} {{ t(nodeLinks().length === 1 ? 'cable' : 'cables') }}</em></span>
            @if (n.portsTotal) { <span class="kv"><i>{{ t('Ports') }}</i><em>{{ n.portsUp }} {{ t('up of') }} {{ n.portsTotal }}</em></span> }
            <span class="kv"><i>{{ t('Management') }}</i><em>{{ mgmtText(n) }}</em></span>
            @if (n.vlans?.length) { <span class="kv"><i>VLAN</i><em>{{ vlanText(n.vlans) }}</em></span> }
            @if (n.chassis) { <span class="kv"><i>MAC</i><em>{{ n.chassis }}</em></span> }
          </div>
          @for (x of n.notes ?? []; track $index) { <div class="warn">{{ x }}</div> }
          @if (nodeLinks().length) {
            <div class="dd-t">{{ t('Connected through') }}</div>
            <table class="dd-p"><tr><th>{{ t('Port') }}</th><th>{{ t('Neighbour') }}</th><th>{{ t('Speed') }}</th><th>STP</th></tr>
              @for (x of nodeLinks(); track x.key) {
                <tr (click)="selectLink(x.key)" [class]="x.state"><td><b>{{ x.port }}</b></td><td>{{ x.peer }} <span class="mut">{{ x.peerPort }}</span></td><td>{{ x.speed || '—' }}</td><td><span [class]="'stp ' + (x.stp || 'none')"><span class="sd"></span>{{ x.stp ? t(stpText(x.stp)) : '—' }}</span></td></tr>
              }
            </table>
          }
        </div>
      }
    </ng-template>
    <div [class]="'nm nm-' + fam()" (click)="closeMenus($event)">
    <div class="nm-head">
      <div class="nm-tt">
        @if (fam() === 'mat') {
          <div class="h1r"><h1>{{ t('Network map') }}</h1><span class="nm-chip f">{{ tab().mapTitle }} · {{ realCount() || devs().length }} {{ t('devices') }}</span><span class="nm-chip">{{ t('Collect') }}: {{ collectLabel() }}</span><span class="nm-chip">{{ intervalLabel() }}</span></div>
        } @else {
          <div class="h1r"><h1>{{ tab().mapTitle }}</h1><span class="nm-tag b">{{ collectLabel() }}</span>@if (busy()) { <span class="nm-tag live"><i></i>{{ t('Reading') }}</span> }</div>
          <p>{{ subline() }}</p>
        }
      </div>
      <div class="nm-ctl">
        <div class="nm-mw">
          <button type="button" class="nm-dd" [class.on]="menu() === 'look'" (click)="openMenu('look', $event)" [title]="t('How devices and cables are drawn')"><span class="lbk">{{ t('Style') }}</span>{{ t(lookLabel()) }}<i class="cv"></i></button>
          @if (menu() === 'look') {
            <div class="nm-pop looks">@for (l of looks; track l.id) {
              <button type="button" class="lk" [class.on]="look() === l.id" (click)="setLook(l.id); menu.set('')">
                <svg class="th" viewBox="0 0 64 44" width="64" height="44">@switch (l.id) {
                  @case ('ant') { <path d="M32 11V18M14 18H50M14 18V26M32 18V26M50 18V26" fill="none" stroke="#3c89e8" stroke-width="1.4"/><rect x="24" y="4" width="16" height="8" rx="2" fill="#1f1f1f" stroke="#7c4dd8"/><rect x="6" y="26" width="16" height="8" rx="2" fill="#1f1f1f" stroke="#595959"/><rect x="24" y="26" width="16" height="8" rx="2" fill="#1f1f1f" stroke="#595959"/><rect x="42" y="26" width="16" height="8" rx="2" fill="#1f1f1f" stroke="#595959"/> }
                  @case ('material') { <path d="M32 13C32 20 16 18 16 26M32 13C32 20 48 18 48 26" fill="none" stroke="#CCC2DC" stroke-width="1.4" opacity=".7"/><rect x="21" y="3" width="22" height="11" rx="5" fill="#4F378B"/><rect x="5" y="26" width="22" height="11" rx="5" fill="#5B3D9E"/><rect x="37" y="26" width="22" height="11" rx="5" fill="#5B3D9E"/> }
                  @case ('focus') { <path d="M20 12L12 30M20 12L32 30M44 12L32 30M44 12L52 30M20 12H44" stroke="#8b97a8" stroke-width="1.2" opacity=".6"/><path d="M44 12L52 30" stroke="#f5a524" stroke-width="1.6" stroke-dasharray="3 2"/><circle cx="20" cy="12" r="6" fill="#1b212b" stroke="#4c9aff"/><circle cx="44" cy="12" r="6" fill="#1b212b" stroke="#8b97a8"/><circle cx="12" cy="31" r="5" fill="#1b212b" stroke="#8b97a8"/><circle cx="32" cy="31" r="5" fill="#1b212b" stroke="#8b97a8"/><circle cx="52" cy="31" r="5" fill="#1b212b" stroke="#8b97a8"/> }
                  @case ('constellation') { <circle cx="32" cy="22" r="18" fill="none" stroke="#3f6fb8" stroke-opacity=".5"/><circle cx="32" cy="22" r="9" fill="none" stroke="#3f6fb8" stroke-opacity=".5"/><path d="M28 22L14 10M36 22L50 34M32 22L32 4" stroke="#6b8fc9" stroke-width="1"/><circle cx="28" cy="22" r="4" fill="#12305e" stroke="#49aa19" stroke-width="1.5"/><circle cx="36" cy="22" r="4" fill="#12305e" stroke="#49aa19" stroke-width="1.5"/><circle cx="14" cy="10" r="3.5" fill="#12305e" stroke="#60a5fa" stroke-width="1.5"/><circle cx="50" cy="34" r="3.5" fill="#12305e" stroke="#49aa19" stroke-width="1.5"/><circle cx="32" cy="4" r="3" fill="#12305e" stroke="#60a5fa" stroke-width="1.5"/> }
                  @case ('zones') { <rect x="16" y="3" width="32" height="13" rx="3" fill="none" stroke="#2a3240"/><rect x="20" y="6" width="10" height="7" rx="2" fill="#1b212b" stroke="#4c9aff"/><rect x="34" y="6" width="10" height="7" rx="2" fill="#1b212b" stroke="#4c9aff"/><path d="M32 16V24" stroke="#4c9aff" stroke-width="1.4"/><rect x="4" y="24" width="56" height="15" rx="3" fill="none" stroke="#2a3240"/><rect x="8" y="28" width="11" height="7" rx="2" fill="#1b212b" stroke="#8b97a8"/><rect x="22" y="28" width="11" height="7" rx="2" fill="#1b212b" stroke="#8b97a8"/><rect x="36" y="28" width="11" height="7" rx="2" fill="#1b212b" stroke="#8b97a8"/> }
                  @case ('flow') { <path d="M20 22C27 22 25 9 32 9M20 22C27 22 25 35 32 35M46 9C50 9 50 5 54 5M46 9C50 9 50 14 54 14M46 35H54" fill="none" stroke="#4c9aff" stroke-width="1.3"/><rect x="4" y="18" width="16" height="8" rx="3" fill="#151a22" stroke="#4c9aff"/><rect x="32" y="5" width="14" height="8" rx="3" fill="#151a22" stroke="#2a3240"/><rect x="32" y="31" width="14" height="8" rx="3" fill="#151a22" stroke="#2a3240"/><rect x="54" y="2" width="8" height="6" rx="2" fill="#151a22" stroke="#2a3240"/><rect x="54" y="11" width="8" height="6" rx="2" fill="#151a22" stroke="#2a3240"/><rect x="54" y="32" width="8" height="6" rx="2" fill="#151a22" stroke="#2a3240"/> }
                  @case ('card') { <path d="M32 12L14 28M32 12L50 28" stroke="#34d399" stroke-width="1.4"/><rect x="20" y="4" width="24" height="9" rx="2" fill="#2a2b30" stroke="#36373d"/><rect x="22" y="6" width="7" height="5" fill="#8b8e96"/><rect x="3" y="27" width="24" height="9" rx="2" fill="#2a2b30" stroke="#36373d"/><rect x="5" y="29" width="7" height="5" fill="#8b8e96"/><rect x="37" y="27" width="24" height="9" rx="2" fill="#2a2b30" stroke="#36373d"/><rect x="39" y="29" width="7" height="5" fill="#8b8e96"/> }
                  @default { <path d="M2 20H62" stroke="#8b8e96" stroke-dasharray="1 3"/><path d="M32 12V24H14V28M32 24H50V28" fill="none" stroke="#34d399" stroke-width="1.4"/><rect x="20" y="4" width="24" height="9" rx="2" fill="#2a2b30" stroke="#36373d"/><rect x="2" y="28" width="24" height="9" rx="2" fill="#2a2b30" stroke="#36373d"/><rect x="38" y="28" width="24" height="9" rx="2" fill="#2a2b30" stroke="#36373d"/> }
                }</svg>
                <span class="tx"><b>{{ t(l.label) }}</b><small>{{ t(l.hint) }}</small></span>
              </button>
            }</div>
          }
        </div>
        <div class="nm-seg">@for (v of views; track v.id) { <button type="button" [class.on]="cableView() === v.id" (click)="setView(v.id)" [title]="t(v.hint)">{{ t(v.label) }}</button> }</div>
        @if (cableView() === 'vlan') {
          <div class="nm-mw">
            <button type="button" class="nm-dd" [class.on]="menu() === 'vlan'" (click)="openMenu('vlan', $event)" [title]="t('The VLAN to show')"><span class="lbk">VLAN</span>{{ vlanLabel() }}<i class="cv"></i></button>
            @if (menu() === 'vlan') {
              <div class="nm-pop list vl">
                <input class="nm-in vq" [ngModel]="vlanQ()" (ngModelChange)="vlanQ.set($event)" (keydown.enter)="vlanEnter()" (click)="$event.stopPropagation()" [placeholder]="t('VLAN ID or name, Enter to show')" spellcheck="false" autofocus />
                <div class="vls">@for (v of vlanShown(); track v.id) {
                  <button type="button" class="vr" [class.on]="vlanSel() === v.id" (click)="pickVlan(v.id); menu.set('')"><span class="vid">{{ v.id }}</span><span class="vn">{{ v.name }}</span><span class="cnt">{{ v.n }} {{ t(v.n === 1 ? 'cable' : 'cables') }}</span></button>
                }</div>
                @if (!vlanShown().length) { <div class="hd2">{{ vlanList().length ? t('No VLAN matches; press Enter to show VLAN {0}', vlanQ()) : t('No VLANs were read (EXOS and VOSS only)') }}</div> }
              </div>
            }
          </div>
        }
        <div class="nm-mw">
          <button type="button" class="nm-dd" [class.on]="menu() === 'layout'" (click)="openMenu('layout', $event)" [title]="t('Arrange the devices without clutter')"><span class="lbk">{{ t('Layout') }}</span>{{ layoutLabel() }}<i class="cv"></i></button>
          @if (menu() === 'layout') {
            <div class="nm-pop list">
              @if (computedLook()) { <button type="button" [class.on]="ownLayout()" (click)="pickArrange('own'); menu.set('')">{{ t('As the style draws it') }}</button><hr /> }
              @for (st of styles; track st.id) { <button type="button" [class.on]="(!computedLook() || !ownLayout()) && layoutStyle() === st.id" (click)="pickArrange(st.id); menu.set('')">{{ t(st.label) }}</button> }
            </div>
          }
        </div>
        <button type="button" class="nm-tog" [class.on]="ghosts()" (click)="ghosts.set(!ghosts())" [title]="t('Neighbours that are not in the Session Manager')"><i></i>{{ t('Unmanaged') }}</button>
        <button type="button" class="nm-tog" [class.on]="ports()" (click)="ports.set(!ports())" [title]="t('Port names on the cables')"><i></i>{{ t('Ports') }}</button>
        @if (fam() !== 'mat') { <button type="button" class="nm-btn pri" (click)="refresh(true)" [disabled]="busy()" [title]="t('Read the neighbours from the devices now')">{{ busy() ? t('Reading…') : t('Refresh') }}</button> }
        <div class="nm-mw">
          <button type="button" class="nm-btn ic" [class.on]="menu() === 'more'" (click)="openMenu('more', $event)" [title]="t('More')">⋯</button>
          @if (menu() === 'more') {
            <div class="nm-pop list more">
              <div class="hd2">{{ t('Read again') }}</div>
              <div class="chips">@for (iv of intervals; track iv) { <button type="button" [class.on]="interval() === iv" (click)="setInterval(iv)">{{ iv ? (iv >= 60 ? iv / 60 + ' min' : iv + ' s') : t('Manual') }}</button> }</div>
              <div class="hd2">{{ t('Read from') }}</div>
              <div class="chips"><button type="button" [class.on]="collect() === 'cli'" (click)="collect.set('cli'); refresh(true)">{{ t('Device CLI') }}</button><button type="button" [class.on]="collect() === 'snmp'" (click)="collect.set('snmp'); refresh(true)">SNMP</button><button type="button" [class.on]="collect() === 'both'" (click)="collect.set('both'); refresh(true)">{{ t('CLI, else SNMP') }}</button></div>
              @if (collect() !== 'cli') { <input class="nm-in" [(ngModel)]="community" placeholder="SNMP community" spellcheck="false" [title]="t('Read-only community. Not saved.')" /> }
              <hr />
              <button type="button" (click)="sideOpen.set(!sideOpen()); menu.set('')">{{ sideOpen() ? t('Hide details') : t('Show details') }}@if (failedCount()) { <em>{{ failedCount() }}</em> }</button>
              @if (fam() !== 'ant') { <button type="button" (click)="toggleTable(); menu.set('')">{{ tableOpen() ? t('Hide the devices table') : t('Show the devices table') }}</button> }
              <hr />
              <button type="button" (click)="exportAs('jpg'); menu.set('')">{{ t('Export') }} JPEG</button>
              <button type="button" (click)="exportAs('pdf'); menu.set('')">{{ t('Export') }} PDF</button>
              <button type="button" (click)="exportAs('html'); menu.set('')">{{ t('Export') }} HTML</button>
            </div>
          }
        </div>
      </div>
    </div>
    @if (showProg()) {
      @if (look() === 'flow') {
        <div class="nm-bars">@for (s of stepList(); track s.n) { <div [class]="'sb ' + s.st"><div class="sl"><b>{{ t(s.name) }}</b><span>{{ s.sub }}</span></div><div class="tr"><i [style.width.%]="s.pct"></i></div></div> }</div>
      } @else if (fam() !== 'mat' && fam() !== 'con' && look() !== 'zones') {
        <div class="nm-steps">@for (s of stepList(); track s.n) {
          <div [class]="'step ' + s.st"><span class="ic">@if (s.st === 'ok') { ✓ } @else if (s.st === 'run') { <i class="spin"></i> } @else { {{ s.n }} }</span><div><b>{{ t(s.name) }}</b><small>{{ s.sub }}</small></div></div>
        }</div>
      }
    }
    @if (fam() !== 'ant' && fam() !== 'mat' && fam() !== 'con' && look() !== 'focus' && kpis().length) {
      <div class="nm-pills">@for (k of kpis(); track k.l) { <button type="button" (click)="k.go()" [title]="t(k.h)" [class]="k.c"><b>{{ k.v }}</b> {{ t(k.l) }}</button> }</div>
    }
    @if (busy() && nodes().length && !showProg()) { <div class="busybar"></div> }
    @if (laid()?.err) { <div class="err">{{ laid()?.err }}</div> }
    @if (err()) { <button type="button" class="err" (click)="sideOpen.set(true)">{{ err() }}</button> }
    <div class="nm-main">
      <div class="nm-mapc">
        @if (fam() === 'ant') { <h3 class="nm-ch">{{ t('Topology') }}<span>{{ subTopo() }}</span></h3> }
      <div #stage [class]="'stage lk-' + look()" [class.drag]="panning" (wheel)="onWheel($event)" (pointerdown)="down($event)" (pointermove)="move($event)" (pointerup)="up($event)" (pointerleave)="hover.set(null); hoverId.set('')" (dblclick)="dbl($event)">
        @if (fam() === 'con' && busy() && radar(); as r) { <div class="con-radar" [style.left.px]="r.x - r.r" [style.top.px]="r.y - r.r" [style.width.px]="r.r * 2" [style.height.px]="r.r * 2"></div> }
        <svg #svg [attr.viewBox]="'0 0 ' + stageSize().w + ' ' + stageSize().h" [innerHTML]="markup()" font-family="-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,Helvetica,Arial,sans-serif" style="position:absolute;inset:0;width:100%;height:100%"></svg>
        @if (!nodes().length && !busy()) { <div class="empty">{{ t('No devices read yet. Neighbours appear here after the first read.') }}</div> }
        @if (!nodes().length && busy()) {
          <div class="skel">
            <svg viewBox="0 0 800 440" preserveAspectRatio="xMidYMid meet" [class]="'sk-' + skel().kind">
              @for (r of skel().rings; track $index) { <circle class="sr" cx="400" cy="220" [attr.r]="r" /> }
              @for (q of skel().panels; track $index) { <rect class="spn" [attr.x]="q[0]" [attr.y]="q[1]" [attr.width]="q[2]" [attr.height]="q[3]" rx="14" /> }
              @for (l of skel().links; track $index) { <path class="sl" pathLength="1" [attr.d]="l" [style.--i]="$index" /> }
              @for (n of skel().nodes; track $index) {
                <g class="sn" [style.--i]="$index" [attr.transform]="'translate(' + n[0] + ' ' + n[1] + ')'">
                  @switch (skel().kind) {
                    @case ('circle') { <circle r="20" /><circle class="sp" r="11" /><rect class="st" x="-22" y="28" width="44" height="6" rx="3" /> }
                    @case ('pill') { <rect x="-70" y="-17" width="140" height="34" rx="10" /><circle class="sp" cx="-54" cy="0" r="5" /><rect class="st" x="-42" y="-6" width="60" height="6" rx="3" /><rect class="st" x="-42" y="4" width="40" height="5" rx="2.5" /> }
                    @case ('tile') { <rect x="-36" y="-22" width="72" height="44" rx="9" /><rect class="st" x="-26" y="-12" width="40" height="6" rx="3" /><rect class="st" x="-26" y="0" width="28" height="5" rx="2.5" /><rect class="sp" x="-26" y="10" width="22" height="7" rx="3" /> }
                    @case ('card') { <rect x="-66" y="-26" width="132" height="52" rx="14" /><rect class="st" x="-54" y="-14" width="60" height="7" rx="3" /><rect class="st" x="-54" y="0" width="34" height="6" rx="3" /><rect class="st" x="-54" y="11" width="46" height="5" rx="2.5" /> }
                    @default { <rect x="-70" y="-24" width="140" height="48" rx="9" /><rect class="sp" x="-61" y="-13" width="34" height="26" rx="3" /><rect class="st" x="-16" y="-10" width="64" height="7" rx="3" /><rect class="st" x="-16" y="4" width="44" height="6" rx="3" /> }
                  }
                </g>
              }
            </svg>
            <div class="scap"><span class="dotp"></span>{{ t('Reading the devices and their neighbours…') }}</div>
          </div>
        }
        @if (fam() === 'mat') {
          <div class="nm-chips tl"><span><i class="d dev"></i>VOSS · EXOS</span><span><i class="d"></i>{{ t('Other') }}</span><span><i class="d run"></i>{{ t('Reading') }}</span><span><i class="d ok"></i>{{ t('Done') }}</span></div>
          <button type="button" class="nm-fab" (click)="refresh(true)" [disabled]="busy()">⟳ {{ busy() ? t('Reading…') : t('Refresh') }}</button>
        }
        @if (look() === 'focus') {
          <div class="nm-chips tl st">@for (c of statusChips(); track c.l) { <button type="button" (click)="c.go()"><i [class]="'d ' + c.c"></i><b>{{ c.v }}</b> {{ t(c.l) }}</button> }</div>
          @if (focusCard(); as fc) {
            <div class="nm-info"><b>{{ fc.name }}</b>@for (l of fc.lines; track $index) { <div>{{ l }}</div> }@if (fc.session) { <a (click)="openSession(fc.session)">{{ t('Double-click to open the session') }}</a> }</div>
          }
        }
        @if (fam() === 'con' && devs().length) {
          <div class="con-hud con-l"><h5>{{ t('Pipeline') }}</h5>@for (s of stepList(); track s.n) {
            <div class="con-st" [class]="'con-st ' + s.st"><svg viewBox="0 0 40 40" width="40" height="40"><circle cx="20" cy="20" r="16" class="g0"/>@if (s.pct) { <circle cx="20" cy="20" r="16" class="g1" [attr.stroke-dasharray]="(100.5 * s.pct / 100).toFixed(1) + ' 101'" transform="rotate(-90 20 20)"/> }<text x="20" y="24.5" text-anchor="middle">{{ s.st === 'ok' ? '✓' : s.n }}</text></svg><div><b>{{ t(s.name) }}</b><small>{{ s.sub }}</small></div></div>
          }</div>
          <div class="con-hud con-r"><h5>{{ busy() ? t('Parallel workers') + ' · ' + workers().length : t('Read') }}</h5>
            @if (busy()) { @for (w of workers(); track w.session; let i = $index) { <div class="con-w"><i>w{{ i + 1 }}</i><div><b>{{ w.name }}</b><small>{{ w.now || t('Connecting…') }}</small></div></div> }
              @if (!workers().length) { <div class="con-w"><div><small>{{ t('Joining…') }}</small></div></div> } }
            @else { @for (k of kpis(); track k.l) { @if (k.l !== 'Devices read') { <button type="button" class="con-k" (click)="k.go()" [class]="'con-k ' + k.c"><span>{{ t(k.l) }}</span><b>{{ k.v }}</b></button> } } }
          </div>
          <div class="con-big"><b>{{ finished() - failedCount() }}</b><span>{{ t('of') }} {{ devs().length }} {{ t('devices complete') }}</span></div>
        }
        @if (fam() === 'con') {
          <div class="con-leg"><span><i class="d dev"></i>EXOS / VOSS</span><span><i class="d ok"></i>{{ t('Complete') }}</span><span><i class="d gh"></i>{{ t('Neighbour') }}</span></div>
        }
        @if (showProg() && look() === 'zones') {
          <div class="dock">@for (s of stepList(); track s.n) { <div class="ds" [class]="s.st"><i>{{ s.st === 'ok' ? '✓' : s.n }}</i><div><b>{{ t(s.name) }}</b><small>{{ s.sub }}</small></div></div> }</div>
        }
        @if (fam() === 'ant' || look() === 'focus') {
          <div class="nm-lgs">@for (r of legend(); track r.t) { <span class="lr">
            <svg width="30" height="12" viewBox="0 0 46 18">@switch (r.k) {
              @case ('blocked') { <path d="M3 9H36" stroke-width="3.4" stroke-dasharray="9 6" stroke-linecap="round" [attr.stroke]="r.c" /><circle cx="40" cy="9" r="5" [attr.fill]="r.d" /> }
              @case ('sp') { <path d="M3 9H43" stroke-linecap="round" [attr.stroke-width]="r.w" [attr.stroke]="r.c" /> }
              @case ('down') { <path d="M3 9H43" stroke-width="3" stroke-dasharray="3 6" stroke-linecap="round" [attr.stroke]="r.c" /> }
              @case ('ghost') { <rect x="4" y="2" width="38" height="14" rx="4" fill="none" stroke-width="1.6" stroke-dasharray="4 3" [attr.stroke]="r.c" /> }
              @case ('stp') { <circle cx="9" cy="9" r="4" [attr.fill]="r.c" /><circle cx="23" cy="9" r="4" [attr.fill]="r.d" /><circle cx="37" cy="9" r="4" [attr.fill]="r.e" /> }
              @default { <path d="M3 9H43" [attr.stroke-width]="r.k === 'lag' || r.k === 'warn' ? 6 : 3.4" stroke-linecap="round" [attr.stroke]="r.c" /> }
            }</svg>{{ r.t }}</span> }@if (look() === 'focus') { <em>{{ t('Cables are neutral while healthy; colour means something needs a look. Click a device to focus it.') }}</em> }</div>
        } @else {
          <div class="lgf" [class.open]="legendOpen()">
            <button type="button" class="lgh" (click)="toggleLegend()">{{ t('Legend') }} <span>{{ legendOpen() ? '▾' : '▸' }}</span></button>
            @if (legendOpen()) {
            <div class="lg">
          @for (r of legend(); track r.t) {
            <div class="lr">
              <svg width="46" height="18" viewBox="0 0 46 18">
                @switch (r.k) {
                  @case ('up') { <path d="M3 9H43" stroke-width="3.4" stroke-linecap="round" [attr.stroke]="r.c" /> }
                  @case ('blocked') { <path d="M3 9H38" stroke-width="3.4" stroke-dasharray="9 6" stroke-linecap="round" [attr.stroke]="r.c" /><circle cx="40" cy="9" r="4.5" [attr.fill]="r.d" /> }
                  @case ('sp') { <path d="M3 9H43" stroke-linecap="round" [attr.stroke-width]="r.w" [attr.stroke]="r.c" /> }
                  @case ('stp') { <circle cx="9" cy="9" r="4" [attr.fill]="r.c" /><circle cx="23" cy="9" r="4" [attr.fill]="r.d" /><circle cx="37" cy="9" r="4" [attr.fill]="r.e" /> }
                  @case ('down') { <path d="M3 9H43" stroke-width="3" stroke-dasharray="3 6" stroke-linecap="round" [attr.stroke]="r.c" /> }
                  @case ('lag') { <path d="M3 9H43" stroke-width="6" stroke-linecap="round" [attr.stroke]="r.c" /><circle cx="23" cy="9" r="5.5" fill="none" stroke-width="2" [attr.stroke]="r.d" /> }
                  @case ('warn') { <path d="M3 9H43" stroke-width="6" stroke-linecap="round" [attr.stroke]="r.c" /><circle cx="23" cy="9" r="5.5" fill="none" stroke-width="2" [attr.stroke]="r.d" /> }
                  @case ('ghost') { <rect x="4" y="2" width="38" height="14" rx="4" fill="none" stroke-width="1.4" stroke-dasharray="4 3" [attr.stroke]="r.c" /> }
                }
              </svg>
              <span>{{ r.t }}</span>
            </div>
          }
            </div>
            }
          </div>
        }
        <div class="zoom zf">
          <button type="button" (click)="zoomBy(1 / 1.25)" [title]="t('Zoom out')">−</button>
          <span>{{ (view().k * 100).toFixed(0) }}%</span>
          <button type="button" (click)="zoomBy(1.25)" [title]="t('Zoom in')">+</button>
          <button type="button" (click)="fit(true)" [title]="t('Fit all devices')">⤢</button>
        </div>
        @if (hover(); as h) {
          @if (look() !== 'focus') {
            <div class="nm-hov" [style.left.px]="h.x + 14" [style.top.px]="h.y + 14"><b>{{ h.title }}</b>@for (l of h.lines; track $index) { <div>{{ l }}</div> }</div>
          }
        }
      </div>
      </div>
      @if (fam() === 'ant') {
        <div class="nm-side">
          <div class="nm-card">
            <h3>{{ selNode() || selLinks().length ? t('Details') : t('Statistics') }}@if (selNode() || selLinks().length) { <span><a (click)="closeSide()">✕</a></span> }</h3>
            @if (selNode() || selLinks().length) { <div class="nm-det">        <ng-container [ngTemplateOutlet]="detTpl" />
</div> }
            @else {
              <div class="nm-stats">@for (k of kpis(); track k.l) { <button type="button" (click)="k.go()" [title]="t(k.h)" [class]="k.c"><small>{{ t(k.l) }}</small><b>{{ k.v }}</b></button> }</div>
              <div class="nm-note">{{ t('Neighbours and ports are read at the same time on every device, then the map is built once all have answered.') }}</div>
            }
          </div>
        </div>
      } @else if (fam() === 'mat') {
        <div class="nm-sheet">
          @if ((selNode() || selLinks().length) && !busy()) {
            <h2>{{ t('Details') }} <a (click)="closeSide()">✕</a></h2>
            <div class="nm-det">        <ng-container [ngTemplateOutlet]="detTpl" />
</div>
          } @else {
            <h2>{{ busy() ? t('Reading the network') : t('The network') }}</h2>
            <p>{{ sheetLine() }}</p>
            <div class="ring"><svg viewBox="0 0 64 64"><circle cx="32" cy="32" r="27" class="rb" /><circle cx="32" cy="32" r="27" class="rf" [attr.stroke-dasharray]="ringDash()" transform="rotate(-90 32 32)" /></svg><div><b>{{ devs().length ? finished() : realCount() }}</b><small>{{ t('of') }} {{ devs().length || realCount() }} {{ t('devices') }}</small></div></div>
            <ol class="vs">@for (s of stepList(); track s.n) { <li [class]="s.st"><i>{{ s.st === 'ok' ? '✓' : s.n }}</i><div><b>{{ t(s.name) }}</b><small>{{ s.sub }}</small>@if (s.st === 'run') { <span class="pbar"><i [style.width.%]="s.pct"></i></span> }</div></li> }</ol>
            <div class="mstat">@for (k of kpis(); track k.l) { @if (k.l !== 'Devices read') { <span [class]="k.c">{{ t(k.l) }} <b>{{ k.v }}</b></span> } }</div>
          }
        </div>
      } @else if (sideOpen() || selNode() || selLinks().length) {
      <aside class="side">
        <div class="sp-hd"><b>{{ selNode() || selLinks().length ? t('Details') : t('Devices and changes') }}</b>
          <button type="button" class="sp-x" (click)="closeSide()" [title]="t('Close')" [attr.aria-label]="t('Close')"><svg viewBox="0 0 16 16" width="16" height="16"><path d="M4 4l8 8M12 4l-8 8" stroke="currentColor" stroke-width="1.8" stroke-linecap="round"/></svg></button></div>
        <div class="sidescroll">
        <ng-container [ngTemplateOutlet]="detTpl" />
        @if (devs().length) {
          <section class="sp-sec">
            <button type="button" class="sp-sh" (click)="spDev.set(!spDev())" [attr.aria-expanded]="spDev()"><span><i class="chev" [class.open]="spDev()"></i>{{ t('Devices') }}</span><em>{{ finished() - failedCount() }} / {{ devs().length }} {{ t('read') }}@if (failedCount()) { · <i class="bad">{{ failedCount() }} {{ t('failed') }}</i> }</em></button>
            @if (spDev()) {
            <table class="sp-t">
              <thead><tr><th></th><th>{{ t('Device') }}</th><th>{{ t('Status') }}</th><th class="r">{{ t('Time') }}</th></tr></thead>
              <tbody>@for (d of devRows(); track d.session) {
                <tr (click)="selectDev(d.session)" [class]="d.state">
                  <td class="dot"><i [class]="d.state"></i></td>
                  <td><b>{{ d.name }}</b>@if (d.ip) { <small>{{ d.ip }}</small> }</td>
                  <td [class]="'st-' + d.state">{{ d.state === 'error' ? d.err : d.state === 'done' ? t('Read') : d.state === 'running' ? (d.now || t('Connecting…')) : t('Waiting') }}</td>
                  <td class="r num">{{ d.ms ? (d.ms / 1000).toFixed(1) + ' s' : '' }}</td>
                </tr>
              }</tbody>
            </table>
            }
          </section>
        }
        <section class="sp-sec">
          <button type="button" class="sp-sh" (click)="spChg.set(!spChg())" [attr.aria-expanded]="spChg()"><span><i class="chev" [class.open]="spChg()"></i>{{ t('Changes') }}</span><em>{{ changes().length }}</em></button>
          @if (!spChg()) { } @else if (changes().length) {
            <div class="sp-cl">@for (c of changeRows(); track $index) {
              <div class="sp-c" [class.grp]="c.first"><div class="sp-c1"><span [class]="'ev ' + c.cls">{{ t(c.label) }}</span><span class="num">{{ c.time }}</span></div><div class="sp-c2">{{ c.text }}</div></div>
            }</div>
          } @else { <div class="sp-empty">{{ t('No changes seen yet.') }}</div> }
        </section>
        </div>
      </aside>
      }
    </div>
    @if ((tableOpen() || fam() === 'ant') && devs().length) {
      <div class="nm-card dtab">
        <button type="button" class="dth" (click)="toggleBody()" [title]="tableBody() ? t('Collapse') : t('Expand')"><b><i class="chev" [class.open]="tableBody()"></i>{{ t('Devices') }}</b><span class="mut">{{ devs().length }} · {{ t('Sorted by progress') }}</span></button>
        @if (tableBody()) {
        <div class="dts"><table>
          <thead><tr><th>{{ t('Device') }}</th><th>{{ t('Type') }}</th><th>{{ t('Address') }}</th><th>{{ t('Progress') }}</th><th>{{ t('Now') }}</th><th>{{ t('Status') }}</th><th>{{ t('Time') }}</th></tr></thead>
          <tbody>@for (d of devRows(); track d.session) {
            <tr [class]="d.state" (click)="selectDev(d.session)">
              <td><b>{{ d.name }}</b></td><td>@if (d.os) { <span class="osb">{{ d.os.toUpperCase() }}</span> }</td><td class="mono">{{ d.ip }}</td>
              <td><span class="pb">@for (k of [0, 1, 2, 3]; track k) { <i [class.ok]="d.state === 'done' || d.done > k" [class.run]="d.state === 'running' && d.done === k" [class.bad]="d.state === 'error'"></i> }</span></td>
              <td class="mut">{{ d.state === 'running' ? (d.now || t('Connecting…')) : d.state === 'done' ? t('Done') : d.state === 'queued' ? t('Waiting') : '' }}</td>
              <td [class]="d.state">{{ d.state === 'error' ? d.err : d.state === 'done' ? t('Done') : d.state === 'running' ? t('Reading') : t('Waiting') }}</td>
              <td class="mono">{{ d.ms ? (d.ms / 1000).toFixed(1) + ' s' : '' }}</td>
            </tr>
          }</tbody>
        </table></div>
        }
      </div>
    }
    </div>
  `,
  styles: [`

    .nm .side { display: flex; flex-direction: column; background: var(--nc); }
    .sp-hd { display: flex; align-items: center; justify-content: space-between; gap: 8px; padding: 12px 14px 12px 18px; border-bottom: 1px solid var(--nl); flex: none; }
    .sp-hd b { font-size: 15px; font-weight: 600; }
    .nm .sp-x { width: 32px; height: 32px; padding: 0; display: grid; place-items: center; border-radius: 8px; border: 1px solid var(--nl); background: var(--nc2); color: var(--nt); cursor: pointer; }
    .nm .sp-x:hover { border-color: var(--bad); color: var(--bad); }
    .nm .sidescroll { padding: 12px 14px 18px; gap: 14px; }
    .sp-sec { flex: none; border: 1px solid var(--nl); border-radius: 10px; background: var(--nb); overflow: hidden; }
    .nm .sp-sh { width: 100%; height: auto; border: 0; border-radius: 0; cursor: pointer; text-align: left; font: inherit; color: inherit; }
    .sp-sh span { display: inline-flex; align-items: center; gap: 8px; } .nm .sp-sh:hover { background: var(--nab); }
    .sp-sec .sp-sh:last-child { border-bottom: 0; }
    .sp-sh { display: flex; justify-content: space-between; align-items: center; padding: 10px 12px; border-bottom: 1px solid var(--nl); background: var(--nc2); }
    .sp-sh span { font-size: 12px; font-weight: 600; letter-spacing: .06em; text-transform: uppercase; color: var(--nm); } .sp-sh em { font-style: normal; font-size: 12px; color: var(--nm); } .sp-sh .bad { font-style: normal; color: var(--bad); }
    .sp-t { width: 100%; border-collapse: collapse; font-size: 12.5px; table-layout: auto; }
    .sp-t th { text-align: left; font-weight: 500; font-size: 11.5px; color: var(--nm); padding: 7px 10px; border-bottom: 1px solid var(--nl); white-space: nowrap; }
    .sp-t td { padding: 8px 10px; border-bottom: 1px solid var(--nl); vertical-align: top; color: var(--nt); line-height: 1.4; } .sp-t tr:last-child td { border-bottom: 0; }
    .sp-t tbody tr { cursor: default; } .sp-t:not(.chg) tbody tr { cursor: pointer; } .sp-t:not(.chg) tbody tr:hover td { background: var(--nc2); }
    .sp-t td b { display: block; font-weight: 600; } .sp-t td small { display: block; color: var(--nm); font-size: 11.5px; font-variant-numeric: tabular-nums; }
    .sp-t .r { text-align: right; } .sp-t .num { font-variant-numeric: tabular-nums; white-space: nowrap; color: var(--nm); }
    .sp-t td.dot { width: 10px; padding-right: 0; } .sp-t td.dot i { display: block; width: 8px; height: 8px; margin-top: 5px; border-radius: 50%; background: var(--nm); }
    .sp-t td.dot i.done { background: var(--ok); } .sp-t td.dot i.error { background: var(--bad); } .sp-t td.dot i.running { background: var(--na); }
    .sp-t .st-error { color: var(--bad); overflow-wrap: anywhere; } .sp-t .st-done { color: var(--nm); }
    .sp-t.chg tr.grp td { border-top: 1px solid var(--nl); } .sp-t.chg td.tm { width: 76px; } .sp-t.chg td.tx { overflow-wrap: anywhere; }
    .ev { display: inline-block; font-size: 11px; font-weight: 600; padding: 1px 7px; border-radius: 6px; white-space: nowrap; border: 1px solid var(--nl); color: var(--nm); }
    .ev.ok { color: var(--ok); border-color: color-mix(in srgb, var(--ok) 40%, transparent); background: color-mix(in srgb, var(--ok) 10%, transparent); }
    .ev.bad { color: var(--bad); border-color: color-mix(in srgb, var(--bad) 40%, transparent); background: color-mix(in srgb, var(--bad) 10%, transparent); }
    .ev.amb { color: var(--amb); border-color: color-mix(in srgb, var(--amb) 40%, transparent); background: color-mix(in srgb, var(--amb) 10%, transparent); }
    .ev.acc { color: var(--na); border-color: color-mix(in srgb, var(--na) 40%, transparent); background: var(--nab); }
    .sp-c { padding: 9px 12px; border-bottom: 1px solid var(--nl); } .sp-c:last-child { border-bottom: 0; }
    .sp-c1 { display: flex; justify-content: space-between; align-items: center; gap: 8px; margin-bottom: 4px; } .sp-c1 .num { font-size: 11.5px; color: var(--nm); font-variant-numeric: tabular-nums; }
    .sp-c2 { font-size: 12.5px; line-height: 1.45; color: var(--nt); overflow-wrap: anywhere; }
    .sp-empty { padding: 14px 12px; color: var(--nm); font-size: 12.5px; }
    .nm .side .dl, .nm .side .dd { padding: 4px 2px 0; }

    .nm-con { --nb: #070d18; --nc: #0d1522; --nc2: #111b2b; --nl: #2b3b57; --nt: #e6e1f2; --nm: #8fa3bf; --na: #60a5fa; --nab: #10264a; --ok: #49aa19; --nr: 14px; }
    :host-context(html.light) .nm-con { --nb: #eef4fd; --nc: #e9eff9; --nc2: #f4f8fe; --nl: #c9d6ea; --nt: #1b2330; --nm: #5f6b7a; --na: #2f6fd0; --nab: #dce8fb; --ok: #3a9a1a; }
    .nm-con .nm-mapc { border: 0; } .nm-con .stage { background: radial-gradient(ellipse at 50% 50%, #0e1726 0, #070d18 60%, #040810 100%) !important; }
    :host-context(html.light) .nm-con .stage { background: radial-gradient(ellipse at 50% 50%, #e3ecfa 0, #eef4fd 60%, #f5f8fe 100%) !important; }
    .con-radar { position: absolute; z-index: 0; pointer-events: none; border-radius: 50%; will-change: transform;
      background: conic-gradient(from 0deg, transparent 0 300deg, rgba(59,130,246,.22) 350deg, rgba(96,165,250,.5) 360deg);
      -webkit-mask: radial-gradient(circle, #000 0 70%, transparent 71%); mask: radial-gradient(circle, #000 0 70%, transparent 71%);
      animation: con-rot 6s linear infinite; }
    :host-context(html.light) .con-radar { background: conic-gradient(from 0deg, transparent 0 300deg, rgba(47,111,208,.12) 350deg, rgba(47,111,208,.32) 360deg); }
    @keyframes con-rot { to { transform: rotate(360deg); } }
    @media (prefers-reduced-motion: reduce) { .con-radar { animation: none; opacity: .5; } }
    .con-hud { position: absolute; top: 14px; z-index: 5; width: 300px; padding: 16px 18px; border-radius: 14px; background: var(--nc); border: 1px solid var(--nl); }
    .con-l { left: 14px; } .con-r { right: 14px; max-height: calc(100% - 120px); overflow: auto; }
    .con-hud h5 { margin: 0 0 12px; font-size: 11px; letter-spacing: .14em; text-transform: uppercase; color: var(--na); font-weight: 600; }
    .con-st { display: flex; align-items: center; gap: 12px; margin-bottom: 10px; } .con-st b { display: block; font-size: 14px; } .con-st small { color: var(--nm); font-size: 12px; }
    .con-st .g0 { fill: none; stroke: var(--nl); stroke-width: 3.5; } .con-st .g1 { fill: none; stroke: var(--na); stroke-width: 3.5; stroke-linecap: round; }
    .con-st.ok .g1 { stroke: var(--ok); } .con-st text { font-size: 13px; fill: var(--nt); } .con-st.ok text { fill: var(--ok); } .con-st.wait { opacity: .7; }
    .con-w { display: flex; gap: 12px; padding: 7px 0; border-bottom: 1px solid var(--nl); } .con-w i { font-style: normal; color: var(--na); font-weight: 700; font-family: var(--mono); width: 24px; }
    .con-w b { display: block; font-size: 13px; font-weight: 500; } .con-w small { color: var(--nm); font-size: 12px; }
    .nm .con-k { width: 100%; height: auto; display: flex; justify-content: space-between; padding: 7px 0; border: 0; border-bottom: 1px solid var(--nl); background: none; cursor: pointer; font-size: 13px; color: var(--nm); }
    .con-k b { color: var(--nt); } .con-k.amb b { color: var(--amb); } .con-k.bad b { color: var(--bad); }
    .con-big { position: absolute; left: 22px; bottom: 16px; z-index: 5; display: flex; align-items: baseline; gap: 10px; pointer-events: none; }
    .con-big b { font-size: 64px; font-weight: 200; line-height: 1; color: var(--nt); } .con-big span { color: var(--nm); font-size: 15px; }
    .con-leg { position: absolute; right: 18px; bottom: 18px; z-index: 5; display: flex; gap: 18px; font-size: 13px; color: var(--nt); pointer-events: none; }
    .con-leg span { display: inline-flex; align-items: center; gap: 6px; } .con-leg .d { width: 10px; height: 10px; border-radius: 50%; } .con-leg .dev { background: var(--na); } .con-leg .ok { background: var(--ok); } .con-leg .gh { border: 1.5px dashed var(--nm); }
    .nm-con .zf { bottom: 52px; } .nm-con .lgf { left: auto; right: 14px; bottom: 100px; }

    .dl { padding: 4px 0 12px; border-bottom: 1px solid var(--nl); margin-bottom: 8px; } .dl:last-child { border-bottom: 0; }
    .dl-h, .dd-h { display: flex; align-items: center; justify-content: space-between; gap: 8px; margin-bottom: 10px; }
    .dl-t { font-size: 11.5px; text-transform: uppercase; letter-spacing: .06em; color: var(--nm); }
    .st { font-size: 12px; padding: 1px 8px; border-radius: 10px; border: 1px solid var(--nl); } .st.up { color: var(--ok); border-color: color-mix(in srgb, var(--ok) 45%, transparent); }
    .st.blocked { color: var(--amb); border-color: color-mix(in srgb, var(--amb) 45%, transparent); } .st.down { color: var(--bad); border-color: color-mix(in srgb, var(--bad) 45%, transparent); }
    .dl-ends { display: grid; grid-template-columns: 1fr auto 1fr; align-items: stretch; gap: 6px; margin-bottom: 10px; }
    .dl-x { align-self: center; color: var(--nm); }
    .dl-end .kv { flex-direction: column; gap: 0; } .dl-end .kv i { font-size: 11px; } .dl-end .kv em { white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
    .nm .dl-end { height: auto; text-align: left; display: flex; flex-direction: column; gap: 4px; padding: 8px 10px; border-radius: 8px; border: 1px solid var(--nl); background: var(--nc2); cursor: pointer; min-width: 0; }
    .nm .dl-end:hover { border-color: var(--na); } .dl-end b { font-size: 13px; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
    .kv { display: flex; gap: 8px; font-size: 12.5px; line-height: 1.45; min-width: 0; } .kv i { font-style: normal; color: var(--nm); flex: none; min-width: 34px; }
    .kv em { font-style: normal; color: var(--nt); overflow-wrap: anywhere; }
    .kvs { display: flex; flex-direction: column; gap: 3px; } .kvs.col .kv i { min-width: 92px; }
    .stp { display: inline-flex; align-items: center; gap: 5px; } .stp .sd { width: 8px; height: 8px; border-radius: 50%; background: var(--nm); }
    .stp.forwarding .sd { background: var(--ok); } .stp.blocked .sd { background: var(--amb); } .stp.learning .sd { background: var(--na); } .stp.disabled .sd { background: var(--bad); }
    .dd-h b { display: block; font-size: 16px; } .dd-h small { color: var(--nm); font-size: 12px; } .nm .nm-btn.sm { height: 28px; padding: 0 12px; font-size: 12.5px; }
    .dd-t { margin: 14px 0 6px; font-size: 11.5px; text-transform: uppercase; letter-spacing: .06em; color: var(--nm); }
    .dd-p { width: 100%; border-collapse: collapse; font-size: 12.5px; } .dd-p th { text-align: left; font-weight: 500; color: var(--nm); padding: 4px 6px; border-bottom: 1px solid var(--nl); }
    .dd-p td { padding: 5px 6px; border-bottom: 1px solid var(--nl); white-space: nowrap; } .dd-p tr { cursor: pointer; } .dd-p tr:hover td { background: var(--nc2); }
    .dd-p tr.blocked td:first-child { box-shadow: inset 2px 0 var(--amb); } .dd-p tr.down td:first-child { box-shadow: inset 2px 0 var(--bad); }

    /* ---- look families: each takes its mock's colours, type and controls ---- */
    .nm { --nb: var(--bg); --nc: var(--panel); --nc2: var(--tab); --nl: var(--border); --nt: var(--fg, #dcdde1); --nm: var(--muted, #8b8e96); --na: var(--accent); --nab: color-mix(in srgb, var(--accent) 16%, transparent); --nr: 8px; --nf: var(--ui-font);
      --ok: #34d399; --amb: #f5a524; --bad: #f87171;
      display: flex; flex-direction: column; height: 100%; min-height: 0; background: var(--nb); color: var(--nt); font-family: var(--nf); font-size: 13px; }
    .nm-ant { --nb: #000; --nc: #141414; --nc2: #1f1f1f; --nl: #303030; --nt: rgba(255,255,255,.88); --nm: rgba(255,255,255,.45); --na: #3c89e8; --nab: #111d2c; --nr: 8px; --ok: #49aa19; font-size: 14px; }
    .nm-mat { --nb: #141218; --nc: #1D1B20; --nc2: #2B2930; --nl: #49454F; --nt: #E6E0E9; --nm: #CAC4D0; --na: #D0BCFF; --nab: #4F378B; --nr: 24px; --ok: #7DDCA4; --nf: Roboto, -apple-system, 'Segoe UI', sans-serif; }
    .nm-dk { --nb: #0e1116; --nc: #151a22; --nc2: #1b212b; --nl: #2a3240; --nt: #e6eaf0; --nm: #8b97a8; --na: #4c9aff; --nab: #12284a; --nr: 12px; --ok: #3fbf6f; }
    :host-context(html.light) .nm-ant { --nb: #f5f5f5; --nc: #fff; --nc2: #fafafa; --nl: #e5e5e5; --nt: rgba(0,0,0,.88); --nm: rgba(0,0,0,.45); --na: #1677ff; --nab: #e6f4ff; --ok: #52c41a; }
    :host-context(html.light) .nm-mat { --nb: #FEF7FF; --nc: #F7F2FA; --nc2: #ECE6F0; --nl: #CAC4D0; --nt: #1D1B20; --nm: #49454F; --na: #6750A4; --nab: #EADDFF; --ok: #1f8a52; }
    :host-context(html.light) .nm-dk { --nb: #f5f7fa; --nc: #fff; --nc2: #f7f9fc; --nl: #dde3ea; --nt: #1b2330; --nm: #5f6b7a; --na: #1f6fd6; --nab: #e3eefc; --ok: #1f9d55; }
    .nm button { font: inherit; color: inherit; }
    .nm-head { display: flex; align-items: center; gap: 16px; padding: 16px 24px 12px; flex-wrap: wrap; }
    .nm-ant .nm-head { background: var(--nc); border-bottom: 1px solid var(--nl); }
    .nm-tt { min-width: 0; flex: 1; } .h1r { display: flex; align-items: center; gap: 10px; flex-wrap: wrap; }
    .nm-tt h1 { margin: 0; font-size: 22px; font-weight: 600; letter-spacing: -.01em; } .nm-ant .nm-tt h1 { font-size: 20px; } .nm-mat .nm-tt h1 { font-weight: 400; margin-right: 6px; }
    .nm-tt p { margin: 4px 0 0; color: var(--nm); font-size: 13px; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
    .nm-tag { display: inline-flex; align-items: center; gap: 6px; font-size: 12px; line-height: 20px; padding: 0 7px; border-radius: 4px; border: 1px solid var(--nl); color: var(--nm); }
    .nm-tag.b { background: var(--nab); border-color: color-mix(in srgb, var(--na) 45%, transparent); color: var(--na); }
    .nm-tag.live i { width: 7px; height: 7px; border-radius: 50%; background: var(--na); animation: lv 1.2s ease-in-out infinite; } .nm-tag.live { color: var(--na); }
    .nm-chip { height: 32px; border-radius: 8px; border: 1px solid var(--nl); display: inline-flex; align-items: center; padding: 0 12px; color: var(--nm); font-size: 13px; } .nm-chip.f { background: var(--nab); border-color: transparent; color: var(--nt); }
    /* controls, drawn in the family's style */
    .nm-ctl { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; }
    .nm-mw { position: relative; }
    .nm-dd, .nm-btn, .nm-tog { height: 32px; display: inline-flex; align-items: center; gap: 8px; padding: 0 12px; border-radius: 6px; border: 1px solid var(--nl); background: var(--nc); cursor: pointer; font-size: 13px; white-space: nowrap; }
    .nm-dd .lbk { color: var(--nm); } .nm-dd .cv { width: 7px; height: 7px; border-right: 1.5px solid var(--nm); border-bottom: 1.5px solid var(--nm); transform: rotate(45deg) translateY(-2px); margin-left: 2px; }
    .nm-dd:hover, .nm-btn:hover, .nm-tog:hover, .nm-dd.on, .nm-btn.on { border-color: var(--na); }
    .nm-btn.pri { background: var(--na); border-color: var(--na); color: #fff; font-weight: 500; } .nm-btn.pri:disabled { opacity: .55; cursor: default; } .nm-btn.ic { width: 32px; justify-content: center; padding: 0; font-size: 16px; }
    .nm-tog i { width: 14px; height: 14px; border-radius: 4px; border: 1.5px solid var(--nm); display: grid; place-items: center; }
    .nm-tog.on i { background: var(--na); border-color: var(--na); } .nm-tog.on i::after { content: ''; width: 6px; height: 3px; border-left: 2px solid #fff; border-bottom: 2px solid #fff; transform: rotate(-45deg) translate(1px, -1px); }
    .nm-seg { display: inline-flex; height: 32px; padding: 2px; border-radius: 6px; background: var(--nc2); border: 1px solid var(--nl); }
    .nm-seg button { border: 0; background: none; padding: 0 12px; border-radius: 4px; cursor: pointer; font-size: 13px; color: var(--nm); }
    .nm-seg button.on { background: var(--na); color: #fff; } .nm-seg button:disabled { opacity: .4; cursor: default; }
    .nm-mat .nm-dd, .nm-mat .nm-btn, .nm-mat .nm-tog { border-radius: 8px; background: transparent; border-color: var(--nl); color: var(--nm); }
    .nm-mat .nm-seg { border-radius: 16px; } .nm-mat .nm-seg button { border-radius: 14px; } .nm-mat .nm-seg button.on, .nm-mat .nm-tog.on i { background: var(--nab); color: var(--nt); border-color: var(--nab); }
    .nm-dk .nm-dd, .nm-dk .nm-btn, .nm-dk .nm-tog { border-radius: 10px; } .nm-dk .nm-seg { border-radius: 10px; } .nm-dk .nm-seg button { border-radius: 8px; }
    .nm-pop { position: absolute; top: calc(100% + 6px); right: 0; z-index: 30; background: var(--nc); border: 1px solid var(--nl); border-radius: var(--nr); box-shadow: 0 12px 32px rgba(0,0,0,.4); padding: 6px; }
    .nm-mat .nm-pop { border-radius: 16px; background: var(--nc2); }
    .nm-pop.looks { left: 0; right: auto; width: 560px; display: grid; grid-template-columns: 1fr 1fr; gap: 4px; padding: 8px; }
    .nm-pop.looks .lk { height: auto !important; min-height: 64px; display: flex; align-items: center; gap: 12px; text-align: left; padding: 8px 10px; border-radius: 8px; border: 1px solid transparent; background: none; cursor: pointer; line-height: 1.3; }
    .nm-pop.looks .lk:hover { background: var(--nc2); } .nm-pop.looks .lk.on { border-color: var(--na); background: var(--nab); }
    .nm-pop.looks .th { flex: none; width: 64px; height: 44px; border-radius: 6px; background: #0e1116; border: 1px solid var(--nl); }
    .nm-pop.looks .tx { min-width: 0; display: flex; flex-direction: column; gap: 2px; } .nm-pop.looks .tx b { font-weight: 600; font-size: 13px; white-space: nowrap; }
    .nm-pop.looks .tx small { color: var(--nm); font-size: 11.5px; display: -webkit-box; -webkit-line-clamp: 2; -webkit-box-orient: vertical; overflow: hidden; }
    .nm-pop.vl { width: 340px; padding: 8px; } .nm-pop.vl .vq { margin: 0 0 6px; width: 100%; height: 32px; box-sizing: border-box; }
    .nm-pop.vl .vls { max-height: 320px; overflow: auto; display: flex; flex-direction: column; }
    .nm-pop.vl .vr { height: auto !important; display: grid; grid-template-columns: 44px minmax(0, 1fr) auto; align-items: center; gap: 8px; text-align: left; padding: 7px 8px; border: 0; border-radius: 6px; background: none; cursor: pointer; font-size: 13px; line-height: 1.3; }
    .nm-pop.vl .vr:hover { background: var(--nc2); } .nm-pop.vl .vr.on { background: var(--nab); color: var(--na); }
    .nm-pop.vl .vid { font-weight: 600; font-variant-numeric: tabular-nums; } .nm-pop.vl .vn { white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
    .nm-pop.vl .cnt { color: var(--nm); font-size: 12px; white-space: nowrap; font-variant-numeric: tabular-nums; }
    .nm-pop.list { min-width: 220px; display: flex; flex-direction: column; } .nm-pop.list > button { text-align: left; padding: 7px 10px; border: 0; background: none; border-radius: 6px; cursor: pointer; font-size: 13px; }
    .nm-pop.list > button:hover { background: var(--nc2); } .nm-pop.list > button.on { color: var(--na); font-weight: 600; } .nm-pop.list > button.on::before { content: '✓ '; }
    .nm-pop hr { border: 0; border-top: 1px solid var(--nl); margin: 4px 0; width: 100%; }
    .nm-pop .hd2 { font-size: 11px; text-transform: uppercase; letter-spacing: .06em; color: var(--nm); padding: 6px 10px 4px; }
    .nm-pop .chips { display: flex; flex-wrap: wrap; gap: 4px; padding: 0 8px 4px; } .nm-pop .chips button { border: 1px solid var(--nl); background: none; border-radius: 6px; padding: 3px 8px; font-size: 12px; cursor: pointer; }
    .nm-pop .chips button.on { border-color: var(--na); color: var(--na); background: var(--nab); }
    .nm-pop em { font-style: normal; margin-left: 6px; padding: 0 6px; border-radius: 8px; background: var(--bad); color: #fff; font-size: 11px; }
    .nm-in { margin: 0 8px 6px; height: 28px; border-radius: 6px; border: 1px solid var(--nl); background: var(--nc2); color: var(--nt); padding: 0 8px; }
    /* progress: antd steps (Clean boxes, Focus, classic) and the five bars (Flow) */
    .nm-steps { display: flex; padding: 16px 24px; border-bottom: 1px solid var(--nl); } .nm-ant .nm-steps { background: var(--nc); }
    .nm-steps .step { flex: 1; display: flex; gap: 12px; align-items: center; position: relative; min-width: 0; }
    .nm-steps .step:not(:last-child)::after { content: ''; flex: 1; height: 1px; background: var(--nl); margin: 0 14px; min-width: 16px; } .nm-steps .step.ok:not(:last-child)::after { background: var(--na); }
    .nm-steps .ic { flex: none; width: 34px; height: 34px; border-radius: 50%; border: 1px solid var(--nl); display: grid; place-items: center; color: var(--nm); font-size: 14px; }
    .nm-steps b { display: block; font-size: 15px; font-weight: 400; color: var(--nm); white-space: nowrap; } .nm-steps small { color: var(--nm); font-size: 12px; white-space: nowrap; }
    .nm-steps .ok .ic { border-color: var(--na); color: var(--na); } .nm-steps .ok b { color: var(--nt); }
    .nm-steps .run .ic { background: var(--na); border-color: var(--na); color: #fff; } .nm-steps .run b { color: var(--nt); font-weight: 500; }
    .nm-bars { display: grid; grid-template-columns: repeat(5, 1fr); gap: 14px; padding: 4px 24px 14px; }
    .nm-bars .sl { display: flex; justify-content: space-between; margin-bottom: 6px; font-size: 13px; } .nm-bars .sl span { color: var(--nm); }
    .nm-bars .tr { height: 6px; border-radius: 3px; background: var(--nl); overflow: hidden; } .nm-bars .tr i { display: block; height: 100%; background: var(--na); border-radius: 3px; }
    .nm-bars .ok .tr i { background: var(--ok); } .nm-bars .wait { opacity: .55; }
    .nm-bars .run .tr i { background-image: linear-gradient(45deg, rgba(255,255,255,.25) 25%, transparent 25%, transparent 50%, rgba(255,255,255,.25) 50%, rgba(255,255,255,.25) 75%, transparent 75%); background-size: 14px 14px; animation: st 1s linear infinite; } @keyframes st { to { background-position: 14px 0; } }
    .nm-pills { display: flex; gap: 8px; padding: 0 24px 10px; flex-wrap: wrap; }
    .nm-pills button, .nm-chips.st button { height: 30px; display: inline-flex; align-items: center; gap: 6px; padding: 0 12px; border-radius: 15px; border: 1px solid var(--nl); background: var(--nc); cursor: pointer; font-size: 13px; color: var(--nm); }
    .nm-pills b, .nm-chips.st b { color: var(--nt); font-weight: 600; } .nm-pills .amb b { color: var(--amb); } .nm-pills .bad b { color: var(--bad); }
    /* body: map card + the family's right column */
    .nm-main { flex: 1; display: flex; gap: 16px; min-height: 0; padding: 0 24px 16px; } .nm-ant .nm-main { padding-top: 16px; } .nm-cls .nm-main { padding: 0; gap: 0; }
    .nm-mapc { flex: 1; min-width: 0; display: flex; flex-direction: column; position: relative; border: 1px solid var(--nl); border-radius: var(--nr); background: var(--nb); overflow: hidden; }
    .nm-ant .nm-mapc { background: var(--nc); } .nm-mat .nm-mapc { background: var(--nc); border: 0; } .nm-cls .nm-mapc { border: 0; border-radius: 0; }
    .nm-ch { margin: 0; padding: 12px 16px; font-size: 16px; font-weight: 600; border-bottom: 1px solid var(--nl); display: flex; align-items: center; } .nm-ch span { margin-left: auto; color: var(--nm); font-weight: 400; font-size: 13px; }
    .nm-mapc .stage { flex: 1; }
    .nm-ant .stage { background-image: radial-gradient(#262626 1px, transparent 1px) !important; background-size: 18px 18px; }
    .nm-side { width: 360px; flex: none; display: flex; flex-direction: column; gap: 16px; min-height: 0; }
    .nm-card { background: var(--nc); border: 1px solid var(--nl); border-radius: var(--nr); overflow: hidden; min-height: 0; display: flex; flex-direction: column; }
    .nm-card h3 { margin: 0; padding: 12px 16px; font-size: 16px; font-weight: 600; border-bottom: 1px solid var(--nl); display: flex; align-items: center; } .nm-card h3 span { margin-left: auto; color: var(--nm); font-weight: 400; } .nm-card h3 a { cursor: pointer; }
    .nm-stats { display: grid; grid-template-columns: 1fr 1fr; gap: 12px; padding: 16px; }
    .nm-stats button { text-align: left; padding: 12px 16px; border-radius: 8px; border: 1px solid var(--nl); background: var(--nc2); cursor: pointer; height: auto; display: block; line-height: 1.3; }
    .nm-stats button:hover { border-color: var(--na); } .nm-stats small { display: block; color: var(--nm); font-size: 13px; } .nm-stats b { display: block; font-size: 28px; font-weight: 500; margin-top: 4px; }
    .nm-stats .amb b { color: var(--amb); } .nm-stats .bad b { color: var(--bad); }
    .nm-note { margin: 0 16px 16px; padding: 12px 16px; border-radius: 8px; background: var(--nab); border: 1px solid color-mix(in srgb, var(--na) 35%, transparent); color: var(--na); line-height: 1.6; }
    .nm-det { padding: 12px 16px; overflow: auto; display: flex; flex-direction: column; gap: 4px; font-size: 13px; } .nm-det h4 { margin: 10px 0 2px; font-size: 12px; text-transform: uppercase; letter-spacing: .04em; color: var(--nm); }
    .nm-det .mut, .nm-det .mut * { color: var(--nm); }
    .nm-sheet { width: 340px; flex: none; background: var(--nc2); border-radius: 24px; padding: 22px 20px; overflow: auto; min-height: 0; }
    .nm-sheet h2 { font-size: 16px; font-weight: 500; margin: 0 0 2px; display: flex; } .nm-sheet h2 a { margin-left: auto; cursor: pointer; } .nm-sheet p { margin: 0 0 14px; color: var(--nm); font-size: 12.5px; }
    .nm-sheet .ring { position: relative; width: 128px; height: 128px; margin: 6px auto 16px; } .nm-sheet .ring svg { width: 128px; height: 128px; }
    .nm-sheet .ring .rb { fill: none; stroke: var(--nl); stroke-width: 6; } .nm-sheet .ring .rf { fill: none; stroke: var(--na); stroke-width: 6; stroke-linecap: round; }
    .nm-sheet .ring div { position: absolute; inset: 0; display: grid; place-content: center; text-align: center; } .nm-sheet .ring b { font-size: 30px; font-weight: 500; } .nm-sheet .ring small { font-size: 11px; color: var(--nm); }
    .nm-sheet .vs { list-style: none; margin: 0; padding: 0; } .nm-sheet .vs li { display: flex; gap: 12px; padding: 7px 0; position: relative; }
    .nm-sheet .vs li i { flex: none; width: 24px; height: 24px; border-radius: 50%; display: grid; place-items: center; font-style: normal; font-size: 12px; font-weight: 600; background: var(--nc); color: var(--nm); }
    .nm-sheet .vs li.ok i { background: var(--ok); color: #00391f; } .nm-sheet .vs li.run i { background: var(--na); color: #381E72; }
    .nm-sheet .vs b { display: block; font-weight: 500; font-size: 14px; } .nm-sheet .vs small { color: var(--nm); font-size: 12.5px; } .nm-sheet .vs li.wait { opacity: .6; }
    .nm-sheet .pbar { display: block; width: 130px; height: 4px; border-radius: 2px; background: var(--nl); margin-top: 6px; overflow: hidden; } .nm-sheet .pbar i { display: block; height: 100%; background: var(--na); }
    .nm-sheet .mstat { display: flex; flex-wrap: wrap; gap: 6px; margin-top: 14px; } .nm-sheet .mstat span { border: 1px solid var(--nl); border-radius: 8px; padding: 4px 10px; font-size: 12px; color: var(--nm); } .nm-sheet .mstat b { color: var(--nt); }
    .nm-chips { position: absolute; z-index: 5; display: flex; gap: 14px; } .nm-chips.tl { left: 16px; top: 14px; }
    .nm-chips span { display: inline-flex; align-items: center; gap: 6px; font-size: 13px; color: var(--nm); }
    .nm-chips .d { width: 10px; height: 10px; border-radius: 50%; background: var(--nm); } .nm-chips .d.dev { background: #7c4dd8; } .nm-chips .d.run { background: var(--na); } .nm-chips .d.ok { background: var(--ok); }
    .nm-chips .d.amb { background: var(--amb); } .nm-chips .d.bad { background: var(--bad); } .nm-chips.st { gap: 8px; }
    .nm .nm-mat .zf, .nm-mat .zf { top: 12px; bottom: auto; }
    .nm .nm-fab { position: absolute; right: 16px; bottom: 16px; z-index: 6; height: 52px; padding: 0 22px; border-radius: 16px; border: 0; background: #EADDFF; color: #21005D !important; font-size: 15px !important; font-weight: 500; cursor: pointer; box-shadow: 0 3px 8px rgba(0,0,0,.35); }
    .nm-info { position: absolute; right: 16px; top: 14px; z-index: 6; min-width: 260px; padding: 12px 16px; border-radius: 10px; background: var(--nc); border: 1px solid var(--nl); font-size: 13px; color: var(--nm); line-height: 1.6; }
    .nm-info b { display: block; color: var(--nt); font-size: 16px; } .nm-info a { color: var(--na); cursor: pointer; }
    .nm-lgs { position: absolute; left: 12px; bottom: 12px; z-index: 5; display: flex; flex-wrap: wrap; align-items: center; gap: 6px 18px; padding: 6px 12px; border-radius: 8px; border: 1px solid var(--nl); background: var(--nc); font-size: 12.5px; color: var(--nm); max-width: calc(100% - 200px); }
    .nm-lgs .lr { display: inline-flex; align-items: center; gap: 6px; } .nm-lgs em { font-style: normal; color: var(--nm); }
    .nm-hov { position: absolute; z-index: 8; pointer-events: none; padding: 8px 12px; border-radius: 8px; background: var(--nc); border: 1px solid var(--nl); box-shadow: 0 8px 22px rgba(0,0,0,.35); font-size: 12.5px; color: var(--nm); max-width: 320px; } .nm-hov b { color: var(--nt); display: block; }
    .nm .zf { background: var(--nc); border-color: var(--nl); } .nm .lgf { background: var(--nc); border-color: var(--nl); }
    .nm .side { border-left-color: var(--nl); background: var(--nb); }
    .nm .dth { width: 100%; height: auto; border: 0; background: none; cursor: pointer; text-align: left; padding: 10px 16px; display: flex; justify-content: space-between; align-items: center; font-size: 14px; }
    .nm .dth b { display: inline-flex; align-items: center; gap: 8px; } .chev { width: 7px; height: 7px; border-right: 1.5px solid var(--nm); border-bottom: 1.5px solid var(--nm); transform: rotate(-45deg); transition: transform .15s; } .chev.open { transform: rotate(45deg); }
    .nm-card.dtab { margin: 0 24px 16px; max-height: 34%; flex: none; } .nm-cls .nm-card.dtab { margin: 0; border-radius: 0; }
    .nm .dtab th { background: var(--nc); color: var(--nm); } .nm .dtab td, .nm .dtab th { border-color: var(--nl); } .nm .dtab .mono { font-family: inherit; font-size: inherit; }
    .nm .osb { border-color: #531dab; background: #1f1230; color: #b37feb; } :host-context(html.light) .nm .osb { border-color: #d3adf7; background: #f9f0ff; color: #722ed1; }
    :host { display: flex; flex-direction: column; height: 100%; min-height: 0; background: var(--bg); }
    .h1r { display: flex; align-items: center; gap: 8px; }
    .chip { font-size: 11.5px; font-weight: 600; padding: 2px 8px; border-radius: 6px; border: 1px solid color-mix(in srgb, var(--accent) 45%, transparent); color: var(--accent); background: color-mix(in srgb, var(--accent) 10%, transparent); }
    .chip.live { display: inline-flex; align-items: center; gap: 6px; } .chip.live i { width: 7px; height: 7px; border-radius: 50%; background: var(--accent); animation: lv 1.2s ease-in-out infinite; }
    @keyframes lv { 50% { opacity: .25; } }
    .stepper { display: flex; align-items: center; gap: 10px; padding: 12px 18px; border-bottom: 1px solid var(--border); }
    .sp1 { display: flex; align-items: center; gap: 10px; flex: none; } .sp1 b { display: block; font-size: 13.5px; font-weight: 600; } .sp1 small { color: var(--muted, #8b8e96); font-size: 11.5px; }
    .sp1 .ic { width: 30px; height: 30px; border-radius: 50%; display: grid; place-items: center; border: 1.5px solid var(--border); color: var(--muted, #8b8e96); font-size: 12px; font-weight: 700; }
    .sp1.ok .ic { border-color: #34d399; color: #34d399; } .sp1.run .ic { background: var(--accent); border-color: var(--accent); } .sp1.wait { opacity: .6; }
    .spin { width: 14px; height: 14px; border-radius: 50%; border: 2px solid rgba(255,255,255,.35); border-top-color: #fff; animation: spn .9s linear infinite; } @keyframes spn { to { transform: rotate(360deg); } }
    .cn { flex: 1; height: 2px; min-width: 20px; background: var(--border); border-radius: 1px; } .cn.ok { background: var(--accent); }
    .kpis { display: grid; grid-template-columns: repeat(7, minmax(0, 1fr)); gap: 10px; padding: 12px 16px 4px; }
    .kpi { height: auto; line-height: 1.25; display: block; text-align: left; padding: 9px 12px; border-radius: 10px; border: 1px solid var(--border); background: var(--panel); cursor: pointer; min-width: 0; }
    .kpi:hover { border-color: var(--accent); } .kpi small { display: block; font-size: 11.5px; color: var(--muted, #8b8e96); white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
    .kpi b { display: block; font-size: 20px; font-weight: 650; margin-top: 2px; } .kpi.amb b { color: #f5a524; } .kpi.bad b { color: #f87171; }
    .tbar { display: flex; align-items: center; gap: 8px; padding: 8px 16px; flex-wrap: wrap; } .tbar .tl { font-size: 14px; margin-right: 6px; }
    .segv { display: inline-flex; border: 1px solid var(--border); border-radius: 8px; overflow: hidden; } .segv button { border: 0; border-radius: 0; background: none; padding: 4px 12px; font-size: 12.5px; }
    .segv button.on { background: var(--accent); color: #fff; } .segv button:disabled { opacity: .45; }
    .tg { display: flex; align-items: center; gap: 5px; font-size: 12.5px; }
    .dtab { flex: none; max-height: 32%; display: flex; flex-direction: column; border-top: 1px solid var(--border); min-height: 0; }
    .dth { display: flex; justify-content: space-between; align-items: center; padding: 8px 16px; font-size: 13px; }
    .dts { overflow: auto; min-height: 0; } .dtab table { width: 100%; border-collapse: collapse; font-size: 12.5px; }
    .dtab th { position: sticky; top: 0; background: var(--bg); text-align: left; font-weight: 500; color: var(--muted, #8b8e96); padding: 6px 16px; border-bottom: 1px solid var(--border); }
    .dtab td { padding: 6px 16px; border-bottom: 1px solid var(--border); white-space: nowrap; max-width: 360px; overflow: hidden; text-overflow: ellipsis; } .dtab tr { cursor: pointer; } .dtab tr:hover td { background: color-mix(in srgb, var(--accent) 7%, transparent); }
    .dtab td.error { color: #f87171; white-space: normal; } .mono { font-family: ui-monospace, Menlo, monospace; font-size: 12px; }
    .osb { font-size: 10.5px; font-weight: 700; padding: 1px 6px; border-radius: 5px; border: 1px solid #8b5cf6; color: #a78bfa; }
    .pb { display: inline-flex; gap: 3px; } .pb i { width: 34px; height: 5px; border-radius: 3px; background: var(--border); } .pb i.ok { background: #34d399; } .pb i.run { background: var(--accent); } .pb i.bad { background: #f87171; opacity: .6; }
    .hd { display: flex; align-items: center; gap: 8px; padding: 12px 16px 10px; border-bottom: 1px solid var(--border); flex-wrap: wrap; }
    .hd .tt { min-width: 0; } .hd h1 { margin: 0; font-size: 18px; font-weight: 650; line-height: 1.2; }
    .hd .sub { margin: 3px 0 0; font-size: 12px; color: var(--muted, #8b8e96); white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
    .hd .pri { background: var(--accent); border-color: var(--accent); color: #fff; font-weight: 600; }
    .ib.on { border-color: var(--accent); color: var(--accent); } .ib em { font-style: normal; margin-left: 6px; padding: 0 6px; border-radius: 8px; background: #f87171; color: #fff; font-size: 11px; }
    .morew { position: relative; } .more { position: absolute; right: 0; top: calc(100% + 6px); z-index: 20; min-width: 250px; padding: 10px 12px; display: flex; flex-direction: column; gap: 8px; background: var(--panel); border: 1px solid var(--border); border-radius: 8px; box-shadow: 0 10px 28px rgba(0,0,0,.35); font-size: 12px; }
    .more > label:not(.chk) { display: flex; align-items: center; justify-content: space-between; gap: 10px; } .more select, .more input.comm { width: 140px; }
    .zf { position: absolute; right: 12px; bottom: 12px; z-index: 5; padding: 4px; background: var(--panel); border: 1px solid var(--border); border-radius: 8px; }
    .lgf { position: absolute; left: 12px; bottom: 12px; z-index: 5; max-width: min(760px, calc(100% - 200px)); background: var(--panel); border: 1px solid var(--border); border-radius: 8px; padding: 4px 10px; font-size: 12px; box-shadow: 0 6px 18px rgba(0,0,0,.25); }
    .lgf.open { padding-bottom: 8px; } .lgf .lg { display: flex; flex-wrap: wrap; gap: 4px 16px; } .lgf .lr { display: flex; align-items: center; gap: 6px; } .lgf .lr svg { width: 30px; } .lgh { border: 0; background: none; padding: 4px 0; font-size: 11px; font-weight: 700; letter-spacing: .06em; text-transform: uppercase; color: var(--muted, #8b8e96); cursor: pointer; }
    .sx { align-self: flex-end; border: 0; background: none; color: var(--muted, #8b8e96); padding: 0 2px; cursor: pointer; }
    button.err { display: block; width: 100%; text-align: left; border: 0; border-radius: 0; background: none; cursor: pointer; }
    .bar { display: flex; align-items: center; gap: 8px; padding: 6px 10px; border-bottom: 1px solid var(--border); flex-wrap: wrap; }
    .ttl { margin-right: 6px; } .sp { flex: 1; } .mut { color: var(--muted, #8b8e96); font-size: 12px; }
    .chk { display: flex; align-items: center; gap: 4px; font-size: 12px; } .comm { width: 110px; }
    .zoom { display: flex; align-items: center; gap: 4px; } .zoom button { width: 28px; padding: 0; } .zoom span { min-width: 40px; text-align: center; font-size: 12px; }
    .err { padding: 6px 12px; color: #f87171; font-size: 12px; }
    .body { flex: 1; display: flex; min-height: 0; }
    .stage { flex: 1; position: relative; overflow: hidden; cursor: grab; touch-action: none; user-select: none; } .stage.drag { cursor: grabbing; }
    .side { width: 360px; border-left: 1px solid var(--border); font-size: 12.5px; display: flex; flex-direction: column; min-height: 0; }
    .sidescroll { flex: 1; min-height: 0; overflow: auto; padding: 8px 12px; display: flex; flex-direction: column; gap: 4px; }
    .legendbox { flex: none; border-top: 1px solid var(--border); padding: 4px 12px 10px; background: var(--panel); } .legendbox h4 { margin-top: 6px; }
    .side h4 { margin: 10px 0 2px; font-size: 12px; text-transform: uppercase; letter-spacing: .04em; color: var(--muted, #8b8e96); }
    .lg { display: flex; flex-direction: column; gap: 6px; } .lr { display: flex; align-items: center; gap: 10px; font-size: 12.5px; line-height: 1.3; } .lr svg { flex: none; }
    .warn { color: #f59e0b; } .chg { line-height: 1.35; }
    .empty { position: absolute; inset: 0; display: flex; align-items: center; justify-content: center; color: var(--muted, #8b8e96); pointer-events: none; }
    .skel { position: absolute; inset: 0; display: flex; flex-direction: column; align-items: center; justify-content: center; gap: 6px; pointer-events: none; }
    .skel svg { width: min(86%, 900px); height: auto; }
    .sn rect, .sn circle { fill: var(--panel); stroke: var(--border); stroke-width: 1; } .sr { fill: none; stroke: var(--border); stroke-dasharray: 2 6; } .spn { fill: none; stroke: var(--border); stroke-dasharray: 4 4; }
    .sk-card .sn rect:first-child { fill: color-mix(in srgb, #6f4fc4 55%, var(--panel)); stroke: none; } .sk-circle .sn circle:first-child { stroke: var(--accent); stroke-opacity: .5; }
    .sn rect { fill: var(--panel); stroke: var(--border); stroke-width: 1; } .sn .sp { fill: var(--tab) !important; stroke: none !important; } .sn .st { fill: var(--tab) !important; stroke: none !important; }
    .sn { opacity: 0; animation: skn 5.2s ease-in-out infinite; animation-delay: calc(var(--i) * .32s); }
    .sn .st, .sn .sp { animation: shim 1.4s ease-in-out infinite; }
    .sl { fill: none; stroke: var(--accent); stroke-width: 2.4; stroke-linecap: round; stroke-dasharray: 1; stroke-dashoffset: 1; opacity: 0; animation: skl 5.2s ease-in-out infinite; animation-delay: calc(1.4s + var(--i) * .3s); }
    .scap { display: flex; align-items: center; gap: 8px; color: var(--muted, #8b8e96); font-size: 13px; }
    .dotp { width: 8px; height: 8px; border-radius: 50%; background: var(--accent); animation: pul 1.2s ease-in-out infinite; }
    @keyframes skn { 0% { opacity: 0; transform-box: fill-box; } 12%, 88% { opacity: 1; } 100% { opacity: 0; } }
    @keyframes skl { 0% { stroke-dashoffset: 1; opacity: 0; } 8% { opacity: .9; } 30%, 80% { stroke-dashoffset: 0; opacity: .9; } 100% { stroke-dashoffset: 0; opacity: 0; } }
    @keyframes shim { 0%, 100% { opacity: .45; } 50% { opacity: 1; } }
    @keyframes pul { 0%, 100% { transform: scale(.7); opacity: .5; } 50% { transform: scale(1.1); opacity: 1; } }
    .busybar { height: 2px; background: linear-gradient(90deg, transparent, var(--accent), transparent); background-size: 40% 100%; background-repeat: no-repeat; animation: bar 1.3s linear infinite; }
    @keyframes bar { 0% { background-position: -40% 0; } 100% { background-position: 140% 0; } }
    @media (prefers-reduced-motion: reduce) { .sn, .sl, .sn .st, .sn .sp, .dotp, .busybar { animation: none; opacity: .8; } .sl { stroke-dashoffset: 0; } }
    .stage.lk-ant, .stage.lk-material, .stage.lk-flow, .stage.lk-zones { background-image: radial-gradient(rgba(128,128,128,.22) 1px, transparent 1px); background-size: 18px 18px; }
    .stage.lk-constellation { background-image: radial-gradient(ellipse at center, rgba(79,156,247,.10), transparent 62%); }
    .lk { padding: 4px 0; border-bottom: 1px solid var(--border); line-height: 1.35; }
    .dv { display: flex; gap: 8px; padding: 3px 0; line-height: 1.3; } .dv i { flex: none; width: 8px; height: 8px; margin-top: 5px; border-radius: 50%; background: var(--muted, #8b8e96); }
    .dv.done i { background: #34d399; } .dv.running i { background: var(--accent); animation: pul 1s ease-in-out infinite; } .dv.error i { background: #f87171; } .dv.error small { color: #f87171; white-space: normal; }
    .dv small { display: block; color: var(--muted, #8b8e96); font-size: 11.5px; overflow-wrap: anywhere; }
    .stepsbar, .seg5, .dock { position: absolute; z-index: 4; pointer-events: none; background: var(--panel); border: 1px solid var(--border); box-shadow: 0 6px 18px rgba(0,0,0,.3); }
    .stepsbar { top: 10px; left: 12px; right: 12px; display: flex; gap: 6px; padding: 8px 14px; border-radius: 8px; }
    .stp { flex: 1; display: flex; gap: 10px; align-items: center; opacity: .55; } .stp.ok, .stp.run { opacity: 1; }
    .stp i, .ds i, .vs i, .hs i { flex: none; width: 24px; height: 24px; border-radius: 50%; display: grid; place-items: center; font-style: normal; font-size: 12px; border: 1px solid var(--border); }
    .stp.ok i, .ds.ok i, .vs .ok i, .hs.ok i { background: #34d399; border-color: #34d399; color: #04140c; font-weight: 700; }
    .stp.run i, .ds.run i, .vs .run i, .hs.run i { border: 2px solid transparent; border-top-color: var(--accent); border-right-color: var(--accent); animation: rot 1s linear infinite; }
    .stp b, .ds b, .vs b, .hs b { display: block; font-size: 12.5px; } .stp small, .ds small, .vs small, .hs small { color: var(--muted, #8b8e96); font-size: 11.5px; }
    @keyframes rot { to { transform: rotate(360deg); } }
    .seg5 { display: grid; padding: 8px 16px 10px !important; border: 0 !important; border-bottom: 1px solid var(--border) !important; box-shadow: none !important; background: none !important; position: static !important; grid-template-columns: repeat(5, 1fr); gap: 14px; padding: 10px 14px; border-radius: 8px; }
    .seg5 .sl { display: flex; justify-content: space-between; margin-bottom: 5px; font-size: 12px; } .seg5 .sl span { color: var(--muted, #8b8e96); }
    .seg5 .tr { height: 6px; border-radius: 3px; background: var(--tab); overflow: hidden; } .seg5 .tr i { display: block; height: 100%; background: var(--accent); border-radius: 3px; }
    .seg5 .ok .tr i { background: #34d399; } .seg5 .wait { opacity: .55; }
    .seg5 .run .tr i { background-image: linear-gradient(45deg, rgba(255,255,255,.28) 25%, transparent 25%, transparent 50%, rgba(255,255,255,.28) 50%, rgba(255,255,255,.28) 75%, transparent 75%); background-size: 14px 14px; animation: stripe 1s linear infinite; }
    @keyframes stripe { to { background-position: 14px 0; } }
    .dock { left: 50%; transform: translateX(-50%); bottom: 14px; display: flex; gap: 26px; padding: 12px 26px; border-radius: 40px; } .ds { display: flex; gap: 10px; align-items: center; } .ds.wait { opacity: .55; }
    .pm { position: absolute; z-index: 4; pointer-events: none; top: 12px; right: 12px; width: 230px; padding: 14px; border-radius: 20px; background: var(--panel); border: 1px solid var(--border); box-shadow: 0 6px 18px rgba(0,0,0,.3); }
    .pring { position: relative; width: 92px; height: 92px; margin: 0 auto 10px; } .pring svg { width: 100%; height: 100%; } .pring .rb { fill: none; stroke: var(--tab); stroke-width: 6; } .pring .rf { fill: none; stroke: var(--accent); stroke-width: 6; stroke-linecap: round; transition: stroke-dasharray .4s; }
    .pring b { position: absolute; inset: 0; display: grid; place-items: center; font-size: 22px; font-weight: 600; padding-bottom: 10px; } .pring small { position: absolute; left: 0; right: 0; bottom: 22px; text-align: center; color: var(--muted, #8b8e96); font-size: 10.5px; }
    .vs { list-style: none; margin: 0; padding: 0; display: flex; flex-direction: column; gap: 10px; } .vs li { display: flex; gap: 10px; align-items: center; } .vs .wait { opacity: .55; }
    .hud { position: absolute; z-index: 4; pointer-events: none; top: 12px; width: 220px; padding: 12px 14px; border-radius: 12px; background: var(--panel); border: 1px solid var(--border); }
    .hl { left: 12px; } .hr { right: 12px; } .hud h5 { margin: 0 0 8px; font-size: 11px; letter-spacing: .12em; text-transform: uppercase; color: var(--accent); }
    .hs { display: flex; gap: 10px; align-items: center; margin-bottom: 9px; } .hs.wait { opacity: .55; } .hw { padding: 5px 0; border-bottom: 1px solid var(--border); font-size: 12px; } .hw small { display: block; color: var(--muted, #8b8e96); font-size: 11px; }
    .card { position: absolute; z-index: 5; background: var(--panel); border: 1px solid var(--border); border-radius: 8px; padding: 8px 10px; font-size: 12px; max-width: 280px; pointer-events: none; box-shadow: 0 6px 18px rgba(0,0,0,.35); }
  `],
})
export class NetworkMap implements OnDestroy {
  readonly tab = input.required<TabView>();
  readonly visible = input(true);

  private backend = inject(Backend);
  private tabs = inject(Tabs);
  private ui = inject(Ui);
  private san = inject(DomSanitizer);
  private stage = viewChild.required<ElementRef<HTMLElement>>('stage');
  private svg = viewChild.required<ElementRef<SVGSVGElement>>('svg');

  protected t = tr;
  protected styles = STYLES;
  protected intervals = INTERVALS;
  protected busy = signal(false);
  /** The placeholder drawn while the first read runs (not real progress): devices appear, then the links are drawn one by one. */
  protected readonly skelNodes: [number, number][] = [[290, 70], [510, 70], [110, 250], [290, 250], [510, 250], [690, 250], [400, 390]];
  protected readonly skelLinks: string[] = ['M360 70H440', 'M250 94L130 226', 'M290 94V226', 'M510 94V226', 'M550 94L670 226', 'M300 94L500 226', 'M300 274L390 366', 'M510 274L410 366'];
  protected err = signal('');
  protected map = signal<TopoMap>({ nodes: [], links: [], at: '' });
  protected changes = signal<TopoChange[]>([]);
  protected pos = signal<Map<string, Pos>>(new Map());
  protected view = signal({ x: 0, y: 0, k: 1 });
  protected look = signal<Look>('flow');
  protected cableView = signal<View>('speed');
  protected looks = LOOKS;
  protected views = VIEWS;
  protected devs = signal<DevProg[]>([]);
  protected sideOpen = signal(false);
  /** Side panel sections: closed until the person opens them (the details of what is selected stay on top). */
  protected spDev = signal(false);
  protected spChg = signal(false);
  protected changeRows = computed(() => {
    const kinds: Record<string, [string, string]> = { 'node-new': ['New device', 'ok'], 'node-lost': ['Device gone', 'bad'], 'link-up': ['Link up', 'ok'], 'link-down': ['Link down', 'bad'], moved: ['Moved', 'amb'], blocked: ['Blocked', 'amb'], forwarding: ['Forwarding', 'ok'], lag: ['LAG', 'acc'] };
    let last = '';
    return this.changes().map(c => {
      const time = this.clock(c.at), [label, cls] = kinds[c.kind] ?? [c.kind, ''];
      const text = c.text.replace(/^[a-z ]+:\s*/i, ''), first = time !== last;
      last = time;
      return { time, label, cls, text, first };
    });
  });

  /** The placeholder drawn while the first read runs, in the chosen style's own shapes (not real devices). */
  protected skel = computed(() => {
    const l = this.look();
    const tiers = (ys: number[], counts: number[], gap: number) => counts.map((c, k) => Array.from({ length: c }, (_, i) => [400 + (i - (c - 1) / 2) * gap, ys[k]] as [number, number]));
    const curve = (a: [number, number], b: [number, number]) => `M${a[0]} ${a[1]}C${a[0]} ${(a[1] + b[1]) / 2} ${b[0]} ${(a[1] + b[1]) / 2} ${b[0]} ${b[1]}`;
    if (l === 'constellation') {
      const ring = (n: number, r: number, off = 0) => Array.from({ length: n }, (_, i) => { const a = -Math.PI / 2 + off + i * 2 * Math.PI / n; return [400 + Math.cos(a) * r, 220 + Math.sin(a) * r] as [number, number]; });
      const core: [number, number][] = [[376, 220], [424, 220]], r1 = ring(4, 100, Math.PI / 4), r2 = ring(6, 180, Math.PI / 6);
      const links = [...r1.map((p, i) => `M${core[i % 2][0]} 220L${p[0]} ${p[1]}`), ...r2.map((p, i) => `M${r1[Math.floor(i / 1.5) % 4][0]} ${r1[Math.floor(i / 1.5) % 4][1]}L${p[0]} ${p[1]}`)];
      return { kind: 'circle', nodes: [...core, ...r1, ...r2], links, panels: [] as number[][], rings: [100, 180] };
    }
    if (l === 'focus') {
      const [a, b, c] = tiers([70, 210, 350], [2, 4, 5], 150);
      const links = [...b.map((p, i) => `M${a[i % 2][0]} ${a[i % 2][1] + 20}L${p[0]} ${p[1] - 20}`), ...c.map((p, i) => `M${b[Math.min(3, Math.floor(i * 4 / 5))][0]} ${b[0][1] + 20}L${p[0]} ${p[1] - 20}`)];
      return { kind: 'circle', nodes: [...a, ...b, ...c], links, panels: [], rings: [] };
    }
    if (l === 'flow') {
      const col = (x: number, n: number, gap: number) => Array.from({ length: n }, (_, i) => [x, 220 + (i - (n - 1) / 2) * gap] as [number, number]);
      const a = col(110, 2, 80), b = col(400, 4, 90), c = col(690, 6, 64);
      const lk = (p: [number, number], q: [number, number]) => `M${p[0] + 70} ${p[1]}C${(p[0] + q[0]) / 2} ${p[1]} ${(p[0] + q[0]) / 2} ${q[1]} ${q[0] - 70} ${q[1]}`;
      return { kind: 'pill', nodes: [...a, ...b, ...c], links: [...b.map((q, i) => lk(a[i % 2], q)), ...c.map((q, i) => lk(b[Math.floor(i * 4 / 6)], q))], panels: [], rings: [] };
    }
    if (l === 'zones') {
      const [a, b] = tiers([75, 215], [2, 4], 84), c = [[110, 355], [194, 355], [278, 355], [522, 355], [606, 355], [690, 355]] as [number, number][];
      return { kind: 'tile', nodes: [...a, ...b, ...c], links: ['M400 112V170', 'M260 252V310', 'M540 252V310'], panels: [[300, 40, 200, 72], [210, 178, 380, 74], [60, 318, 300, 74], [440, 318, 300, 74]], rings: [] };
    }
    const [a, b, c] = tiers([60, 210, 360], [2, 4, l === 'material' ? 4 : 1], l === 'material' ? 170 : 175);
    const links = l === 'material' ? [...b.map((p, i) => curve([a[i % 2][0], a[i % 2][1] + 26], [p[0], p[1] - 26])), ...c.map((p, i) => curve([b[i][0], b[i][1] + 26], [p[0], p[1] - 26]))] : this.skelLinks;
    return { kind: l === 'material' ? 'card' : 'box', nodes: l === 'material' ? [...a, ...b, ...c] : this.skelNodes, links, panels: [], rings: [] };
  });

  /** Constellation: where the radar sweep turns (the core is at the map's 0,0) and how far it reaches, on screen. */
  protected radar = computed(() => {
    // the sweep turns round the core device and reaches every device of the layout (any layout, not only the rings)
    const v = this.view(), sz = this.stageSize(), ns = this.nodes(), D = dims(this.look());
    if (!ns.length) return { x: sz.w / 2, y: sz.h / 2, r: Math.min(sz.w, sz.h) * .48 / .7 }; // around the placeholder
    let cx = 0, cy = 0;
    if (!this.laid()) { // an arranged layout: centre on the device with the most cables
      const deg = new Map<string, number>();
      for (const l of this.links()) { deg.set(l.a, (deg.get(l.a) ?? 0) + 1); deg.set(l.b, (deg.get(l.b) ?? 0) + 1); }
      const hub = ns.filter(n => !n.ghost).sort((p, q) => (deg.get(q.id) ?? 0) - (deg.get(p.id) ?? 0))[0] ?? ns[0];
      cx = hub.x; cy = hub.y;
    }
    const reach = Math.max(...ns.map(n => Math.hypot(n.x - cx, n.y - cy))) + D.w / 2 + 40;
    return { x: v.x + cx * v.k, y: v.y + cy * v.k, r: reach * v.k / .7 }; // the mask shows the inner 70 %
  });
  protected isStr = (x: unknown) => !!x;
  protected selSess = signal<Session | null>(null);
  private selSessFx = effect(() => {
    const n = this.selNode();
    this.selSess.set(null);
    if (!n?.session) return;
    const c = this.sessionCache.get(n.session);
    if (c !== undefined) { this.selSess.set(c); return; }
    void this.backend.getSession(n.session).then(x => { this.sessionCache.set(n.session!, x); if (this.selNodeId() === n.id) this.selSess.set(x); }).catch(() => this.sessionCache.set(n.session!, null));
  });
  protected selectNode(id: string) { this.selLinkKey.set(''); this.selNodeId.set(id); this.sideOpen.set(true); }
  protected selectLink(key: string) { this.selNodeId.set(''); this.selLinkKey.set(key); this.sideOpen.set(true); }
  protected linkEnds(l: TopoLink) {
    const lag = (g?: { name: string; mode: string } | null) => g ? g.name + (g.mode === 'static' ? ' (static)' : ' (LACP)') : '';
    return [
      { side: 'a', id: l.a, name: this.nodeName(l.a), port: l.aPort, stp: l.stpA ?? '', lag: lag(l.lagA) },
      { side: 'b', id: l.b, name: this.nodeName(l.b), port: l.bPort, stp: l.stpB ?? '', lag: lag(l.lagB) },
    ];
  }
  protected stpText(s: string) { return s === 'blocked' ? 'Blocking' : s === 'forwarding' ? 'Forwarding' : s === 'learning' ? 'Learning' : s === 'disabled' ? 'Disabled' : s; }
  protected vlanText(v?: number[] | null) {
    if (!v?.length) return '—';
    const nm = new Map((this.map().vlans ?? []).map(x => [x.id, x.name ?? '']));
    const list = v.map(id => id + (nm.get(id) ? ' ' + nm.get(id) : ''));
    return list.length > 8 ? list.slice(0, 8).join(', ') + ' ' + tr('and {0} more', list.length - 8) : list.join(', ');
  }
  protected nodePeers = computed(() => new Set(this.nodeLinks().map(x => x.peer)).size);
  protected mgmtText(n: TopoNode) {
    const parts: string[] = [];
    if (n.ip) parts.push(n.ip);
    if (n.mgmtVlan) parts.push('VLAN ' + n.mgmtVlan.id + (n.mgmtVlan.name ? ' ' + n.mgmtVlan.name : ''));
    if (n.mgmtVlan) parts.push(n.mgmtPorts?.length ? tr('port') + ' ' + n.mgmtPorts.join(', ') : tr('out-of-band port'));
    return parts.join(' · ') || tr('not read');
  }

  protected vlanSel = signal<number | null>(null);
  protected vlanList = computed(() => {
    const ls = this.map().links ?? [];
    return (this.map().vlans ?? []).map(v => ({ id: v.id, name: v.name ?? '', n: ls.filter(l => (l.vlans ?? []).includes(v.id)).length }));
  });
  protected vlanLabel = computed(() => { const id = this.vlanSel(), v = this.vlanList().find(x => x.id === id); return v ? v.id + (v.name ? ' · ' + v.name : '') : id != null ? String(id) : tr('Pick one'); });
  protected vlanQ = signal('');
  protected vlanShown = computed(() => {
    const q = this.vlanQ().trim().toLowerCase();
    return q ? this.vlanList().filter(v => String(v.id).startsWith(q) || v.name.toLowerCase().includes(q)) : this.vlanList();
  });
  protected vlanEnter() {
    const q = this.vlanQ().trim(), n = Number(q), hit = this.vlanShown();
    if (q && Number.isInteger(n) && n >= 1 && n <= 4094) this.pickVlan(n);
    else if (hit.length) this.pickVlan(hit[0].id);
    else return;
    this.menu.set(''); this.vlanQ.set('');
  }
  protected pickVlan(id: number) { this.vlanSel.set(id); try { localStorage.setItem('map.vlan.' + this.scope(), String(id)); } catch { /* ignore */ } }
  private vlanDefault = effect(() => {
    const l = this.vlanList();
    if (!l.length || (this.vlanSel() != null && l.some(v => v.id === this.vlanSel()))) return;
    let saved = NaN;
    try { saved = Number(localStorage.getItem('map.vlan.' + this.scope())); } catch { /* ignore */ }
    const pick = l.find(v => v.id === saved) ?? [...l].filter(v => v.id !== 1).sort((a, b) => b.n - a.n)[0] ?? l[0];
    this.vlanSel.set(pick.id);
  });

  protected menu = signal<'' | 'look' | 'layout' | 'more' | 'vlan'>('');
  protected fam = computed(() => { const l = this.look(); return l === 'ant' ? 'ant' : l === 'material' ? 'mat' : l === 'constellation' ? 'con' : l === 'card' || l === 'tier' ? 'cls' : 'dk'; });
  protected openMenu(m: 'look' | 'layout' | 'more' | 'vlan', e: Event) { e.stopPropagation(); this.menu.set(this.menu() === m ? '' : m); }
  protected closeMenus(e: Event) { if (this.menu() && !(e.target as HTMLElement).closest('.nm-pop')) this.menu.set(''); }
  protected lookLabel = computed(() => LOOKS.find(l => l.id === this.look())?.label ?? '');
  protected layoutLabel = computed(() => this.computedLook() && this.ownLayout() ? tr('As the style draws it') : tr(STYLES.find(x => x.id === this.layoutStyle())?.label ?? 'Auto (best fit)'));
  protected intervalLabel = computed(() => { const s = this.interval(); return s ? tr('Every') + ' ' + (s >= 60 ? s / 60 + ' min' : s + ' s') : tr('Manual only'); });
  protected subTopo = computed(() => { const n = this.realCount(), l = (this.map().links ?? []).length; return n ? n + ' ' + tr('devices') + ' · ' + l + ' ' + tr('links') : ''; });
  protected sheetLine = computed(() => {
    const d = this.devs(), done = d.filter(x => x.state === 'done' && x.ms), avg = done.length ? done.reduce((a, x) => a + x.ms, 0) / done.length / 1000 : 0;
    return tr('{0} sessions in parallel', d.length || this.realCount()) + (avg ? ' · ' + tr('{0} s per device on average', avg.toFixed(1)) : '');
  });
  protected statusChips = computed(() => {
    const ls = this.map().links ?? [], ns = this.map().nodes ?? [];
    const blocked = ls.filter(l => l.state === 'blocked').length, down = ls.filter(l => l.state === 'down').length;
    return [
      { l: 'healthy', v: ls.length - blocked - down, c: 'ok', go: () => this.setView('health') },
      { l: 'blocked', v: blocked, c: 'amb', go: () => this.setView('stp') },
      { l: 'down', v: down, c: 'bad', go: () => this.setView('health') },
      { l: 'unmanaged', v: ns.filter(n => n.ghost).length, c: 'mut', go: () => this.ghosts.set(!this.ghosts()) },
      ...(this.failedCount() ? [{ l: 'not read', v: this.failedCount(), c: 'bad', go: () => this.sideOpen.set(true) }] : []),
    ];
  });
  protected focusCard = computed(() => {
    const id = this.selNodeId() || this.hoverId();
    const n = (this.map().nodes ?? []).find(x => x.id === id);
    if (!n) return null;
    const ls = (this.map().links ?? []).filter(l => l.a === n.id || l.b === n.id).length;
    return { name: n.name, session: n.session ?? '', lines: [[(n.os ?? '').toUpperCase(), n.chassis, n.ip].filter(Boolean).join(' · ') || (n.ghost ? tr('not in Session Manager') : ''), ls + ' ' + tr(ls === 1 ? 'link' : 'links')].filter(Boolean) };
  });
  protected hoverId = signal('');
  protected dbl(e: MouseEvent) {
    const el = (e.target as Element).closest('[data-node]');
    const n = el && (this.map().nodes ?? []).find(x => x.id === el.getAttribute('data-node'));
    if (n?.session) this.openSession(n.session);
  }

  protected ownLayout = signal(true);
  private layRestored = false;
  protected tableOpen = signal(false);
  protected tableBody = signal(true);
  protected toggleBody() { const v = !this.tableBody(); this.tableBody.set(v); try { localStorage.setItem('map.tbody', v ? '1' : '0'); } catch { /* ignore */ } }
  protected headerSteps = computed(() => ['ant', 'card', 'tier', 'focus'].includes(this.look()));
  protected collectLabel = computed(() => this.collect() === 'snmp' ? 'SNMP' : this.collect() === 'both' ? tr('CLI, else SNMP') : tr('Device CLI'));
  protected subline = computed(() => {
    const n = this.devs().length;
    if (this.busy()) return tr('Reading {0} devices at the same time (up to {1}). Only read-only show commands run.', n || '…', 16);
    const u = this.updated();
    return tr(this.lookHint()) + (u ? ' · ' + u : '');
  });
  protected arrangeSel = computed(() => this.computedLook() ? (this.ownLayout() ? 'own' : this.layoutStyle()) : '');
  private layKey() { return 'map.lay.' + this.scope(); }
  protected pickArrange(v: string) {
    try { localStorage.setItem(this.layKey(), JSON.stringify({ look: this.look(), style: v })); } catch { /* ignore */ }
    if (v === 'own') { this.ownLayout.set(true); queueMicrotask(() => this.fit()); return; }
    if (!v) return;
    this.ownLayout.set(false);
    void this.arrange(v);
  }
  protected toggleTable() { const v = !this.tableOpen(); this.tableOpen.set(v); try { localStorage.setItem('map.table', v ? '1' : '0'); } catch { /* ignore */ } }
  protected kpis = computed(() => {
    const ns = this.map().nodes ?? [], ls = this.map().links ?? [], d = this.devs();
    if (!ns.length && !d.length) return [] as { l: string; v: string | number; c: string; h: string; go: () => void }[];
    const real = ns.filter(n => !n.ghost).length, ghosts = ns.length - real;
    const lags = new Set(ls.flatMap(l => [l.lagA ? l.a + '|' + l.lagA.name : '', l.lagB ? l.b + '|' + l.lagB.name : '']).filter(Boolean)).size;
    const blocked = ls.filter(l => l.state === 'blocked').length, down = ls.filter(l => l.state === 'down').length, failed = this.failedCount();
    const open = () => this.sideOpen.set(true);
    return [
      { l: 'Devices read', v: d.length ? this.finished() - failed + ' / ' + d.length : real, c: '', h: 'Devices of this map that answered', go: () => this.tableOpen.set(true) },
      { l: 'Links found', v: ls.length, c: '', h: 'Cables between the devices', go: () => this.setView('health') },
      { l: 'Blocked by STP', v: blocked, c: blocked ? 'amb' : '', h: 'Ports blocked by spanning tree', go: () => this.setView('stp') },
      { l: 'Links down', v: down, c: down ? 'bad' : '', h: 'Cables whose port is down', go: () => this.setView('health') },
      { l: 'LAGs', v: lags, c: '', h: 'Link aggregation groups', go: () => this.setView('lag') },
      { l: 'Unmanaged', v: ghosts, c: '', h: 'Neighbours that are not in the Session Manager (click to show or hide them)', go: () => this.ghosts.set(!this.ghosts()) },
      { l: 'Could not read', v: failed, c: failed ? 'bad' : '', h: 'Devices that did not answer; the reason is in Details', go: open },
    ];
  });
  protected devRows = computed(() => {
    const ip = new Map((this.map().nodes ?? []).filter(n => n.session).map(n => [n.session!, n.ip ?? '']));
    const rank = (x: DevProg) => x.state === 'running' ? 0 : x.state === 'queued' ? 1 : x.state === 'error' ? 2 : 3;
    return this.devs().map(x => ({ ...x, ip: ip.get(x.session) ?? '' })).sort((a, b) => rank(a) - rank(b) || a.name.localeCompare(b.name, undefined, { numeric: true }));
  });
  protected selectDev(session: string) {
    const n = (this.map().nodes ?? []).find(x => x.session === session);
    if (n) { this.selNodeId.set(n.id); this.selLinkKey.set(''); }
    this.sideOpen.set(true);
  }

  protected more = signal(false);
  protected legendOpen = signal(false);
  protected lookHint = computed(() => LOOKS.find(l => l.id === this.look())?.hint ?? '');
  protected realCount = computed(() => this.nodes().filter(n => !n.ghost).length);
  protected failedCount = computed(() => this.devs().filter(d => d.state === 'error').length);
  private progQ: TopoProgress[] = [];
  private progT = 0;
  protected toggleLegend() { const v = !this.legendOpen(); this.legendOpen.set(v); try { localStorage.setItem('map.legend', v ? '1' : '0'); } catch { /* ignore */ } }
  protected closeSide() { this.sideOpen.set(false); this.selNodeId.set(''); this.selLinkKey.set(''); }
  protected phase = signal<'' | 'reading' | 'build' | 'done'>('');
  protected computedLook = computed(() => COMPUTED_LOOKS.includes(this.look()));
  private unProg?: () => void;
  protected layoutStyle = signal('auto');
  protected interval = signal(120);
  protected collect = signal<'cli' | 'snmp' | 'both'>('cli');
  protected community = 'public';
  protected ghosts = signal(true);
  protected ports = signal(true);
  protected selLinkKey = signal('');
  protected selNodeId = signal('');
  protected hover = signal<{ x: number; y: number; title: string; lines: string[] } | null>(null);
  protected now = signal(Date.now());
  protected panning = false;

  private timer: ReturnType<typeof setInterval> | undefined;
  private lastRead = 0;
  private fresh = new Set<string>();
  private confirmedProduction = false;
  private drag: { kind: 'node'; id: string; dx: number; dy: number; moved: boolean } | { kind: 'pan'; sx: number; sy: number; vx: number; vy: number } | null = null;
  private fitted = false;
  private touched = false; // the person zoomed or panned: stop fitting by itself
  private sessionCache = new Map<string, Session | null>();

  /** Rows of devices (by height) and what they are called, when the layout is in tiers. */
  private rows = computed(() => {
    const D = dims(this.look()), p = this.pos(), vis = (this.map().nodes ?? []).filter(n => this.ghosts() || !n.ghost);
    const list = vis.map(n => ({ n, y: p.get(n.id)?.y ?? 0, x: p.get(n.id)?.x ?? 0 })).sort((a, b) => a.y - b.y);
    const rows: { n: TopoNode; y: number; x: number }[][] = [];
    for (const e of list) { const r = rows[rows.length - 1]; if (r && e.y - r[r.length - 1].y < D.h * .6) r.push(e); else rows.push([e]); }
    const layered = ['layered', 'spine-leaf', 'tree'].includes(this.layoutStyle());
    const name = (i: number, r: { n: TopoNode }[]) => r.every(e => e.n.ghost) ? 'NEIGHBOURS' : i === 0 ? 'CORE' : i === rows.length - 1 ? 'ACCESS' : rows.length === 3 ? 'DISTRIBUTION' : 'TIER ' + (i + 1);
    return { tiers: layered && rows.length >= 2, rows, name };
  });
  /** The devices of the map that are shown (neighbours without a session only when asked for). */
  private shown = computed(() => (this.map().nodes ?? []).filter(n => this.ghosts() || !n.ghost));
  /** The looks that arrange themselves (Constellation, Flow, Zones) get their positions from the links. */
  protected laid = computed<Laid | null>(() => {
    const l = this.look();
    if (!COMPUTED_LOOKS.includes(l) || !this.ownLayout()) return null;
    const ids = new Set(this.shown().map(n => n.id)), D = dims(l);
    const ln = this.shown().map(n => ({ id: n.id, name: n.name, ghost: n.ghost })), le = (this.map().links ?? []).filter(e => ids.has(e.a) && ids.has(e.b)).map(e => ({ a: e.a, b: e.b }));
    try {
      return l === 'flow' ? flowLayout(ln, le, D.w, D.h) : l === 'constellation' ? radialLayout(ln, le, D.w) : zonesLayout(ln, le, D.w, D.h);
    } catch (e) {
      // never leave the map blank: draw it as Flow and say why
      const r = flowLayout(ln, le, dims('flow').w, dims('flow').h);
      return { ...r, err: tr('This look could not be drawn ({0}); shown as Flow.', String((e as Error)?.message ?? e)) };
    }
  });
  private progOf(n: TopoNode): Prog | undefined {
    if (!this.busy() || !n.session) return undefined;
    const d = this.devs().find(x => x.session === n.session);
    return d ? { state: d.state, done: d.done } : undefined;
  }
  protected nodes = computed<MNode[]>(() => {
    const p = this.pos(), lay = this.laid(), at = new Map((lay?.nodes ?? []).map(q => [q.id, q]));
    return this.shown().map(n => {
      const q = at.get(n.id) ?? p.get(n.id) ?? { x: 0, y: 0 };
      return { vlans: n.vlans, id: n.id, name: n.name, sub: n.ghost ? tr('seen by neighbour discovery') : (n.os ?? '').toUpperCase(), ip: n.ghost ? '' : n.ip, kind: kindOf(n.os, n.caps, n.ghost), ghost: n.ghost, x: q.x, y: q.y, flag: (n.notes?.length ? 'warn' : ''), os: n.os, notes: n.notes?.length ?? 0, group: at.get(n.id)?.group, prog: this.progOf(n) };
    });
  });
  /** Zones behind the devices: tier guides (Classic tiers), role panels (Zones), column headings (Flow). */
  private zones = computed<Zone[]>(() => {
    const D = dims(this.look()), ns = this.nodes(), rw = this.rows(), lay = this.laid(), l = this.look();
    if (lay && l !== 'constellation') return lay.panels.map(q => ({ x: q.x, y: q.y, w: q.w, h: q.h, label: tr(q.label), right: l === 'zones' ? q.right + ' ' + tr(q.right === '1' ? 'device' : 'devices') : q.right, id: q.id, ghost: q.ghost }));
    if (!ns.length || l !== 'tier' || !rw.tiers) return [];
    const x0 = Math.min(...ns.map(n => n.x)) - D.w / 2 - 20, x1 = Math.max(...ns.map(n => n.x)) + D.w / 2 + 20;
    return rw.rows.map((r, i) => ({ x: x0, y: Math.min(...r.map(e => e.y)) - D.h / 2 - 22, w: x1 - x0, h: 0, label: rw.name(i, r), right: r.length + ' ' + tr(r.length === 1 ? 'device' : 'devices') }));
  });
  private rings = computed<number[]>(() => {
    if (this.look() !== 'constellation') return [];
    const rs = new Set(this.nodes().filter(n => !n.ghost).map(n => Math.round(Math.hypot(n.x, n.y) / 4) * 4).filter(r => r > 0));
    return [...rs].sort((a, b) => a - b);
  });
  private links = computed<MLink[]>(() => {
    const ids = new Set(this.nodes().map(n => n.id));
    return (this.map().links ?? []).filter(l => ids.has(l.a) && ids.has(l.b));
  });
  protected stageSize = signal({ w: 1000, h: 600 });
  private ro?: ResizeObserver;
  private bounds = computed(() => mapBounds(this.nodes(), 60, this.look(), this.zones()));
  protected markup = computed(() => {
    const v = this.view();
    const th = document.documentElement.classList.contains('light') ? THEME_LIGHT : THEME_DARK;
    void v;
    let inner = '';
    try {
      inner = mapMarkup(this.nodes(), this.links(), { theme: th, showPorts: this.ports() && v.k >= .55, selLink: this.selLinkKey(), selNode: this.selNodeId(), fresh: this.fresh, look: this.look(), zones: this.zones(), view: this.cableView(), vlan: this.vlanSel(), rings: this.rings(), sweep: this.busy() });
    } catch (e) { inner = `<text x="20" y="30" fill="#f87171" font-size="13">${String((e as Error)?.message ?? e).replace(/[<&>]/g, '')}</text>`; }
    const html = `<g transform="translate(${v.x} ${v.y}) scale(${v.k})">${inner}</g>`;
    return this.san.bypassSecurityTrustHtml(html); // drawn by map-svg.ts from escaped text only
  });
  protected legend = computed(() => {
    const th = document.documentElement.classList.contains('light') ? THEME_LIGHT : THEME_DARK, v = this.cableView();
    const rows: { k: string; c: string; d: string; e?: string; w?: number; t: string }[] = [];
    if (v === 'vlan') rows.push({ k: 'sp', c: th.acc, d: '', w: 4.6, t: tr('Carries VLAN {0}', this.vlanLabel()) }, { k: 'sp', c: th.mut, d: '', w: 2, t: tr('Does not carry it (faded)') });
    else if (this.look() === 'focus') rows.push({ k: 'sp', c: th.mut, d: '', w: 1.4, t: '1G' }, { k: 'sp', c: th.mut, d: '', w: 2.4, t: '10G' }, { k: 'sp', c: th.mut, d: '', w: 3.6, t: '40G+' });
    else if (v === 'speed') rows.push({ k: 'sp', c: th.ok, d: '', w: 3.4, t: '10 Mbps – 10 Gbps' }, { k: 'sp', c: th.acc, d: '', w: 3.8, t: '25 – 100 Gbps' }, { k: 'sp', c: th.acc, d: '', w: 6.6, t: tr('Above 100 Gbps') });
    else rows.push({ k: 'up', c: th.ok, d: '', t: tr('Link is up') });
    rows.push({ k: 'blocked', c: th.ok, d: th.warn, t: tr('Blocked by spanning tree (amber dot = the blocking port)') },
      { k: 'down', c: th.bad, d: '', t: tr('Port is down') });
    if (v === 'stp') rows.push({ k: 'stp', c: th.ok, d: th.acc, e: th.mut, t: tr('Port state: forwarding, learning, disabled') });
    rows.push({ k: 'lag', c: v === 'lag' ? th.acc : th.ok, d: th.ink, t: tr('LAG: the ring is where a switch has it') },
      { k: 'warn', c: th.warn, d: th.warn, t: tr('LAG whose two ends disagree') },
      { k: 'ghost', c: th.mut, d: '', t: tr('Neighbour without a session') });
    return rows;
  });
  /** The steps of reading, from what the devices have reported. */
  protected stepList = computed(() => {
    const d = this.devs(), n = d.length, err = d.filter(x => x.state === 'error').length, ph = this.phase();
    const names = ['Connect', 'Identify', 'Neighbours', 'Ports · STP · LAG'];
    const out = names.map((name, k) => {
      const c = d.filter(x => x.state === 'done' || (x.state === 'running' && x.done >= k + 1)).length;
      const ok = n > 0 && c + err >= n, started = c > 0 || d.some(x => x.state === 'running' && x.done >= k);
      return { n: k + 1, name, st: ok ? 'ok' : started ? 'run' : 'wait', sub: c + ' / ' + n + (err && ok ? ' · ' + err + ' ' + tr('failed') : ''), pct: n ? Math.round(100 * (c + err) / n) : 0 };
    });
    out.push(ph === 'done' ? { n: 5, name: 'Build map', st: 'ok', sub: tr('Done') + (this.lastMs() ? ' · ' + (this.lastMs() / 1000).toFixed(1) + ' s' : ''), pct: 100 }
      : { n: 5, name: 'Build map', st: ph === 'build' ? 'run' : 'wait', sub: ph === 'build' ? tr('Joining…') : tr('Waiting'), pct: ph === 'build' ? 60 : 0 });
    return out;
  });
  protected finished = computed(() => this.devs().filter(x => x.state === 'done' || x.state === 'error').length);
  protected showProg = computed(() => this.devs().length > 0 && (this.busy() || this.phase() === 'done'));
  protected lastMs = signal(0);
  private readStart = 0;
  protected ringDash = computed(() => { const n = this.devs().length; return n ? (170 * this.finished() / n).toFixed(0) + ' 170' : '0 170'; });
  protected workers = computed(() => this.devs().filter(x => x.state === 'running').slice(0, 8));
  protected selLinks = computed<TopoLink[]>(() => { const ks = new Set(this.selLinkKey().split(' ').filter(Boolean)); return ks.size ? (this.map().links ?? []).filter(l => ks.has(l.key)) : []; });
  protected nodeLinks = computed(() => {
    const id = this.selNodeId();
    if (!id) return [];
    return (this.map().links ?? []).filter(l => l.a === id || l.b === id).map(l => {
      const a = l.a === id, lag = a ? l.lagA : l.lagB;
      return { speed: l.speed ?? '', stp: (a ? l.stpA : l.stpB) ?? '', state: l.state, key: l.key, port: (a ? l.aPort : l.bPort) || '?', peer: this.nodeName(a ? l.b : l.a), peerPort: (a ? l.bPort : l.aPort) || '', info: [l.state, l.speed, lag ? 'LAG ' + lag.name : '', (a ? l.stpA : l.stpB) ? 'STP ' + (a ? l.stpA : l.stpB) : ''].filter(Boolean).join(' · ') };
    }).sort((x, y) => x.port.localeCompare(y.port, undefined, { numeric: true }));
  });
  protected selNode = computed<TopoNode | null>(() => (this.map().nodes ?? []).find(n => n.id === this.selNodeId()) ?? null);
  protected updated = computed(() => {
    if (!this.map().at) return '';
    const s = Math.max(0, Math.round((this.now() - new Date(this.map().at).getTime()) / 1000));
    return tr('Updated') + ' ' + (s < 90 ? s + ' s' : Math.round(s / 60) + ' min') + ' ' + tr('ago');
  });

  constructor() {
    try { const i = localStorage.getItem('map.interval'); if (i && INTERVALS.includes(+i)) this.interval.set(+i); const lk = localStorage.getItem('map.look2') || this.ui.settings()?.mapLook || 'flow'; if (LOOKS.some(l => l.id === lk)) this.look.set(lk as Look); if (localStorage.getItem('map.legend') === '1') this.legendOpen.set(true); if (localStorage.getItem('map.table') === '1') this.tableOpen.set(true); if (localStorage.getItem('map.tbody') === '0') this.tableBody.set(false); const vw = localStorage.getItem('map.view'); if (VIEWS.some(v => v.id === vw)) this.cableView.set(vw as View); } catch { /* storage may be blocked */ }
    this.unProg = this.backend.on('topo:progress', (p: TopoProgress) => this.onProgress(p));
    effect(() => { if (this.selLinks().length || this.selNode()) this.sideOpen.set(true); });
    effect(() => { if (this.visible()) { this.restartTimer(); if (!this.lastRead) void this.refresh(false); else if (Date.now() - this.lastRead > this.interval() * 1000 && this.interval()) void this.refresh(false); } });
    this.timer = setInterval(() => this.tick(), 1000);
    effect(() => {
      const el = this.stage().nativeElement;
      this.ro?.disconnect();
      this.ro = new ResizeObserver(() => { const w = el.clientWidth, h = el.clientHeight; if (w && h) { const cur = this.stageSize(); if (this.sized && Math.abs(cur.w - w) < 2 && Math.abs(cur.h - h) < 2) return; const first = !this.sized; this.sized = true; this.stageSize.set({ w, h }); if ((first || !this.touched) && this.nodes().length) requestAnimationFrame(() => this.fit()); } });
      this.ro.observe(el);
    });
  }
  private sized = false;
  ngOnDestroy() { clearInterval(this.timer); this.ro?.disconnect(); this.unProg?.(); void this.backend.topoForget(this.scope()).catch(() => undefined); }

  private scope() { return this.tab().mapScope ?? this.tab().id; }
  private tick() {
    this.now.set(Date.now());
    const s = this.interval();
    if (this.visible() && s && !this.busy() && Date.now() - this.lastRead >= s * 1000) void this.refresh(false);
  }
  private restartTimer() { /* the 1 s tick covers the interval; nothing else to restart */ }
  protected setLook(l: Look) {
    this.look.set(l); this.hover.set(null); this.ownLayout.set(true);
    try { localStorage.setItem('map.look2', l); } catch { /* ignore */ }
    // a new style starts from the layout last picked for it on this map, else Auto (best fit); then the map is fitted
    let sv: { look: string; style: string } | null = null;
    try { sv = JSON.parse(localStorage.getItem(this.layKey()) ?? 'null'); } catch { /* ignore */ }
    this.touched = false;
    if (sv && sv.look === l && sv.style && sv.style !== 'own') { if (COMPUTED_LOOKS.includes(l)) this.ownLayout.set(false); void this.arrange(sv.style); return; }
    if (!COMPUTED_LOOKS.includes(l) && this.nodes().length) { void this.arrange('auto'); return; }
    queueMicrotask(() => this.fit());
  }
  protected setView(v: View) { this.cableView.set(v); try { localStorage.setItem('map.view', v); } catch { /* ignore */ } }
  private onProgress(p: TopoProgress) {
    // many devices send many events: redraw at most 4 times a second
    this.progQ.push(p);
    if (!this.progT) this.progT = window.setTimeout(() => { this.progT = 0; const q = this.progQ; this.progQ = []; for (const x of q) this.applyProgress(x); }, 250);
  }
  private applyProgress(p: TopoProgress) {
    if (p.scope !== this.scope() || !this.busy()) return; // late events after the read are dropped
    if (p.phase === 'build') { this.phase.set('build'); return; }
    if (p.phase || !p.session) return;
    this.devs.update(list => {
      const i = list.findIndex(d => d.session === p.session), prev = i >= 0 ? list[i] : null;
      const next: DevProg = { session: p.session!, name: p.name ?? prev?.name ?? p.session!, os: p.os ?? prev?.os ?? '', state: p.state ?? 'queued', done: p.state === 'done' ? 4 : Math.max(p.done ?? 0, prev && p.state === 'running' ? prev.done : 0), now: p.now ?? '', err: p.err ?? '', ms: p.elapsedMs ?? prev?.ms ?? 0 };
      if (prev && prev.state === 'done' && next.state === 'running') return list; // a late step event after the end
      return i >= 0 ? list.map((d, k) => (k === i ? next : d)) : [...list, next];
    });
  }
  protected setInterval(s: number) { this.interval.set(s); try { localStorage.setItem('map.interval', String(s)); } catch { /* ignore */ } }

  protected async refresh(manual: boolean) {
    if (this.busy()) return;
    const tb = this.tab();
    const targets = tb.mapOpened ? this.tabs.openedSessionPaths() : (tb.mapTargets ?? []);
    if (!targets.length) { this.err.set(tr('No sessions are open. Open device sessions and press Refresh - this map follows the opened sessions.')); this.lastRead = Date.now(); return; }
    this.busy.set(true); this.err.set(''); this.devs.set([]); this.phase.set('reading'); this.readStart = Date.now(); this.lastMs.set(0);
    try {
      let r = await this.backend.topoRead({ targets, scope: this.scope(), production: this.confirmedProduction, collect: this.collect(), community: this.community.trim() || 'public' });
      if (r.needsConfirm?.length) {
        const ok = await this.ui.confirm(tr('Production devices'), tr('These sessions are marked Production: {0}. The map only runs read-only "show" commands. Read them?', r.needsConfirm.join(', ')), tr('Read'));
        if (!ok) { this.lastRead = Date.now(); this.busy.set(false); return; }
        this.confirmedProduction = true;
        r = await this.backend.topoRead({ targets, scope: this.scope(), production: true, collect: this.collect(), community: this.community.trim() || 'public' });
      }
      this.lastRead = Date.now();
      await this.apply(r.map, r.changes ?? [], manual);
      if (r.failed) this.err.set(tr('{0} device(s) could not be read; their last links are kept.', r.failed));
    } catch (e) {
      this.lastRead = Date.now();
      this.err.set(errText(e));
    } finally {
      // events still queued belong to this read: apply them, then mark the read finished
      if (this.progT) { clearTimeout(this.progT); this.progT = 0; }
      const q = this.progQ; this.progQ = []; for (const x of q) this.applyProgress(x);
      this.lastMs.set(Date.now() - this.readStart);
      this.phase.set(this.devs().length ? 'done' : ''); this.busy.set(false);
    }
  }

  private async apply(m: TopoMap, ch: TopoChange[], manual: boolean) {
    const old = new Set((this.map().nodes ?? []).map(n => n.id));
    this.fresh = new Set(old.size ? (m.nodes ?? []).filter(n => !old.has(n.id)).map(n => n.id) : []);
    this.changes.set(ch.slice().reverse());
    const nodes = m.nodes ?? [];
    const cur = this.pos(), next = new Map(cur);
    for (const id of [...next.keys()]) if (!nodes.some(n => n.id === id)) next.delete(id);
    const fresh = nodes.filter(n => !next.has(n.id));
    if (fresh.length) {
      // first draw: arrange everything; later: only the new nodes are placed, the others stay where they are
      const saved = this.loadPinned();
      for (const n of nodes) { const s = saved[n.id]; if (s && !next.has(n.id)) next.set(n.id, { x: s.x, y: s.y, pinned: true }); }
      const todo = nodes.filter(n => !next.has(n.id));
      if (todo.length) {
        const res = await this.backend.layoutGraph({
          style: TIERED.includes(this.look()) ? 'layered' : 'auto',
          nodes: nodes.map(n => { const p = next.get(n.id); return { id: n.id, w: dims(this.look()).w, h: dims(this.look()).h, x: p?.x ?? 0, y: p?.y ?? 0, pinned: !!p, ghost: n.ghost }; }),
          edges: (m.links ?? []).map(l => ({ a: l.a, b: l.b })),
        });
        for (const r of res.nodes ?? []) if (!next.has(r.id)) next.set(r.id, { x: r.x, y: r.y, pinned: false });
        this.layoutStyle.set(res.style || 'auto');
      }
    }
    this.pos.set(next);
    this.map.set(m);
    if (!this.layRestored && nodes.length) {
      // the layout picked last time for this map (and style) comes back
      this.layRestored = true;
      try {
        const sv = JSON.parse(localStorage.getItem(this.layKey()) ?? 'null') as { look: string; style: string } | null;
        if (sv && sv.look === this.look() && sv.style && sv.style !== 'own') {
          if (this.computedLook()) { this.ownLayout.set(false); await this.arrange(sv.style); } else this.layoutStyle.set(sv.style);
        }
      } catch { /* ignore */ }
    }
    if ((!this.fitted || !this.touched) && nodes.length) { this.fitted = true; queueMicrotask(() => this.fit()); }
    void manual;
  }

  protected async arrange(style: string) {
    if (!style) return;
    const nodes = this.nodes();
    if (!nodes.length) return;
    try {
      const res = await this.backend.layoutGraph({
        style, nodes: nodes.map(n => ({ id: n.id, w: dims(this.look()).w, h: dims(this.look()).h, x: n.x, y: n.y, ghost: n.ghost })),
        edges: this.links().map(l => ({ a: l.a, b: l.b })),
      });
      const next = new Map<string, Pos>();
      for (const r of res.nodes ?? []) next.set(r.id, { x: r.x, y: r.y, pinned: false });
      this.pos.set(next);
      this.layoutStyle.set(res.style || style);
      this.savePinned();
      this.fit();
    } catch (e) { this.ui.toast(errText(e), 'error'); }
  }

  // ---- pinned positions (dragged nodes stay where the user put them) ----
  private pinKey() { return 'map.pins.' + this.scope(); }
  private loadPinned(): Record<string, { x: number; y: number }> {
    try { return JSON.parse(localStorage.getItem(this.pinKey()) ?? '{}'); } catch { return {}; }
  }
  private savePinned() {
    const o: Record<string, { x: number; y: number }> = {};
    for (const [id, p] of this.pos()) if (p.pinned) o[id] = { x: Math.round(p.x), y: Math.round(p.y) };
    try { localStorage.setItem(this.pinKey(), JSON.stringify(o)); } catch { /* ignore */ }
  }

  // ---- zoom / pan ----
  protected zoomBy(f: number, cx?: number, cy?: number) {
    const sz = this.stageSize(), v = this.view();
    const k = Math.min(4, Math.max(.1, v.k * f)), x0 = cx ?? sz.w / 2, y0 = cy ?? sz.h / 2, r = k / v.k;
    this.touched = true;
    this.view.set({ k, x: x0 - (x0 - v.x) * r, y: y0 - (y0 - v.y) * r });
  }
  /** Show all devices: as large as fits (not above 100%), centred. */
  protected fit(manual = false) {
    if (manual) this.touched = false;
    const sz = this.stageSize(), b = this.bounds();
    const k = Math.min(1, sz.w / b.w, sz.h / b.h);
    this.view.set({ k, x: (sz.w - b.w * k) / 2 - b.x * k, y: (sz.h - b.h * k) / 2 - b.y * k });
  }
  protected onWheel(e: WheelEvent) {
    e.preventDefault();
    const r = this.stage().nativeElement.getBoundingClientRect();
    this.zoomBy(e.deltaY < 0 ? 1.12 : 1 / 1.12, e.clientX - r.left, e.clientY - r.top);
  }
  private toMap(e: PointerEvent) {
    const r = this.stage().nativeElement.getBoundingClientRect(), v = this.view();
    return { x: (e.clientX - r.left - v.x) / v.k, y: (e.clientY - r.top - v.y) / v.k };
  }
  protected down(e: PointerEvent) {
    if ((e.target as Element).closest('button, a, input, .zf, .lgf, .nm-lgs, .nm-info, .nm-chips, .dock, .seg5, .pm, .hud, .con-hud')) return; // the map's own buttons and panels
    const el = (e.target as Element).closest('[data-node]') as HTMLElement | SVGElement | null;
    (e.currentTarget as HTMLElement).setPointerCapture(e.pointerId);
    if (el) {
      const id = el.getAttribute('data-node')!, p = this.pos().get(id), m = this.toMap(e);
      if (p) this.drag = { kind: 'node', id, dx: p.x - m.x, dy: p.y - m.y, moved: false };
      return;
    }
    const lk = (e.target as Element).closest('[data-link]');
    if (lk) { this.selLinkKey.set(lk.getAttribute('data-link')!); this.selNodeId.set(''); return; }
    this.selLinkKey.set(''); this.selNodeId.set('');
    this.panning = true;
    const v = this.view();
    this.drag = { kind: 'pan', sx: e.clientX, sy: e.clientY, vx: v.x, vy: v.y };
  }
  protected move(e: PointerEvent) {
    const d = this.drag;
    if (d && d.kind === 'node') {
      if (this.laid()) return; // the arranging looks place the devices themselves
      const m = this.toMap(e), nx = new Map(this.pos()), p = nx.get(d.id);
      if (p) { d.moved = true; nx.set(d.id, { x: m.x + d.dx, y: m.y + d.dy, pinned: true }); this.pos.set(nx); this.hover.set(null); }
      return;
    }
    if (d && d.kind === 'pan') { this.touched = true; this.view.update(v => ({ ...v, x: d.vx + e.clientX - d.sx, y: d.vy + e.clientY - d.sy })); return; }
    const el = (e.target as Element).closest?.('[data-node]');
    if (!el) { this.hover.set(null); return; }
    const r = this.stage().nativeElement.getBoundingClientRect(), id = el.getAttribute('data-node')!;
    void this.showCard(id, e.clientX - r.left, e.clientY - r.top);
  }
  protected up(e: PointerEvent) {
    const d = this.drag;
    this.drag = null; this.panning = false;
    if (d && d.kind === 'node') {
      if (d.moved) this.savePinned(); else { this.selNodeId.set(d.id); this.selLinkKey.set(''); }
    }
    try { (e.currentTarget as HTMLElement).releasePointerCapture(e.pointerId); } catch { /* not captured */ }
  }

  private async showCard(id: string, x: number, y: number) {
    this.hoverId.set(id);
    const n = (this.map().nodes ?? []).find(q => q.id === id);
    if (!n) return;
    const lines: string[] = [];
    if (n.session) {
      let s = this.sessionCache.get(n.session);
      if (s === undefined) { try { s = await this.backend.getSession(n.session); } catch { s = null; } this.sessionCache.set(n.session, s); }
      if (s) {
        lines.push(`${s.hostname}:${s.port} · ${s.protocol}`);
        const meta = [s.deviceType, s.environment].filter(Boolean).join(' · ');
        if (meta) lines.push(meta);
        if (s.folder) lines.push(tr('Folder') + ': ' + s.folder);
        for (const d of (s.description ?? []).slice(0, 3)) lines.push(d);
      }
    } else {
      lines.push(tr('Not in Session Manager. Seen by LLDP / CDP / EDP.'));
      if (n.ip) lines.push(n.ip);
      if (n.chassis) lines.push(n.chassis);
    }
    for (const x2 of n.notes ?? []) lines.push('⚠ ' + x2);
    const ls = (this.map().links ?? []).filter(l => l.a === id || l.b === id);
    if (ls.length) lines.push(tr('{0} cables', ls.length) + ' · ' + tr('{0} blocked', ls.filter(l => l.state === 'blocked').length) + ' · ' + tr('{0} down', ls.filter(l => l.state === 'down').length));
    this.hover.set({ x, y, title: n.name, lines });
  }

  protected nodeName(id: string) { return (this.map().nodes ?? []).find(n => n.id === id)?.name ?? id; }
  protected clock(at: string) { const d = new Date(at); return isNaN(+d) ? '' : d.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit', second: '2-digit' }); }
  protected openSession(path: string) { void this.tabs.openSession(path); }

  // ---- exports ----
  protected async exportAs(kind: string) {
    if (!kind) return;
    const title = (this.tab().mapTitle ?? 'Network Map') + ' - ' + new Date().toISOString().slice(0, 16).replace('T', ' ');
    const th = document.documentElement.classList.contains('light') ? THEME_LIGHT : THEME_DARK;
    const nodes = this.nodes();
    const base = (this.tab().mapTitle ?? 'network-map').replace(/[^\w.-]+/g, '-');
    try {
      if (kind === 'html') {
        const doc = mapDocument(nodes, this.links(), { theme: th, showPorts: this.ports(), selLink: '', selNode: '', look: this.look(), zones: this.zones(), view: this.cableView(), vlan: this.vlanSel(), rings: this.rings() }, title);
        const det: string[] = [];
        for (const n of this.map().nodes ?? []) {
          const s = n.session ? await this.backend.getSession(n.session).catch(() => null) : null;
          det.push(`<tr><td>${esc(n.name)}</td><td>${esc(s ? s.hostname + ':' + s.port + ' ' + s.protocol : n.ip ?? '')}</td><td>${esc(s ? [s.deviceType, s.environment, s.folder].filter(Boolean).join(' · ') : 'not in Session Manager')}</td><td>${esc((s?.description ?? []).join(' '))}</td></tr>`);
        }
        const html = `<!doctype html><meta charset="utf-8"><title>${esc(title)}</title><body style="margin:0;background:${th.bg};color:${th.ink};font-family:sans-serif">${doc.svg}<table style="margin:16px;border-collapse:collapse;font-size:13px" cellpadding="6"><tr><th align="left">Device</th><th align="left">Address</th><th align="left">Type / environment / folder</th><th align="left">Notes</th></tr>${det.join('')}</table></body>`;
        const p = await this.backend.saveTextAs(tr('Save map'), base + '.html', html);
        if (p) this.ui.toast(tr('Saved') + ' ' + p, 'success');
        return;
      }
      const doc = mapDocument(nodes, this.links(), { theme: th, showPorts: this.ports(), selLink: '', selNode: '', look: this.look(), zones: this.zones(), view: this.cableView(), vlan: this.vlanSel(), rings: this.rings() }, title);
      const jpeg = await this.rasterize(doc.svg, doc.w, doc.h);
      if (kind === 'pdf') {
        const p = await this.backend.saveMapPDF(tr('Save map'), base + '.pdf', jpeg);
        if (p) this.ui.toast(tr('Saved') + ' ' + p, 'success');
      } else {
        // JPEG goes through the same save dialog as text, as a data URL download
        const a = document.createElement('a');
        a.href = 'data:image/jpeg;base64,' + jpeg; a.download = base + '.jpg'; a.click();
      }
    } catch (e) { this.ui.toast(errText(e), 'error'); }
  }

  /** The SVG as a JPEG (base64, no prefix), drawn at 2x. */
  private rasterize(svg: string, w: number, h: number): Promise<string> {
    return new Promise((res, rej) => {
      const img = new Image();
      img.onload = () => {
        const c = document.createElement('canvas'), k = Math.min(2, 8000 / Math.max(w, h));
        c.width = Math.round(w * k); c.height = Math.round(h * k);
        const g = c.getContext('2d');
        if (!g) { rej(new Error('no canvas')); return; }
        g.scale(k, k); g.drawImage(img, 0, 0, w, h);
        res(c.toDataURL('image/jpeg', .92).split(',')[1]);
      };
      img.onerror = () => rej(new Error(tr('The map could not be drawn to an image.')));
      img.src = 'data:image/svg+xml;charset=utf-8,' + encodeURIComponent(svg);
    });
  }
}

const esc = (s: string) => s.replace(/[&<>"']/g, c => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c]!));
