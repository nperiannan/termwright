import { Component, ElementRef, OnDestroy, computed, effect, inject, input, signal, viewChild } from '@angular/core';
import { FormsModule } from '@angular/forms';
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
];
const TIERED: Look[] = ['ant', 'material', 'tier'];

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
  imports: [FormsModule],
  template: `
    <div class="bar">
      <b class="ttl">{{ tab().mapTitle }}</b>
      <button type="button" (click)="refresh(true)" [disabled]="busy()" [title]="t('Read the neighbours from the devices now')">{{ busy() ? t('Reading…') : t('Refresh') }}</button>
      <select [ngModel]="interval()" (ngModelChange)="setInterval(+$event)" [title]="t('How often the map reads the devices again (only while this tab is shown)')">
        @for (s of intervals; track s) { <option [value]="s">{{ s ? t('Every') + ' ' + (s >= 60 ? s / 60 + ' min' : s + ' s') : t('Manual only') }}</option> }
      </select>
      <span class="mut">{{ updated() }}</span>
      <span class="sp"></span>
      <select [ngModel]="collect()" (ngModelChange)="collect.set($event); refresh(true)" [title]="t('Where the neighbours are read from')">
        <option value="cli">{{ t('Device CLI') }}</option><option value="snmp">SNMP</option><option value="both">{{ t('CLI, else SNMP') }}</option>
      </select>
      @if (collect() !== 'cli') { <input class="comm" [(ngModel)]="community" placeholder="SNMP community" spellcheck="false" [title]="t('Read-only community. Not saved.')" /> }
      <label class="chk"><input type="checkbox" [ngModel]="ghosts()" (ngModelChange)="ghosts.set($event)" /> {{ t('Unknown neighbours') }}</label>
      <label class="chk"><input type="checkbox" [ngModel]="ports()" (ngModelChange)="ports.set($event)" /> {{ t('Port names') }}</label>
      @if (!computedLook()) {
        <select [ngModel]="''" (ngModelChange)="arrange($event)" [title]="t('Arrange the devices without clutter')">
          <option value="">{{ t('Arrange…') }}</option>
          @for (s of styles; track s.id) { <option [value]="s.id">{{ t(s.label) }}</option> }
        </select>
      }
      <select [ngModel]="look()" (ngModelChange)="setLook($event)" [title]="t('How devices and cables are drawn')">
        @for (l of looks; track l.id) { <option [value]="l.id" [title]="t(l.hint)">{{ t(l.label) }}</option> }
      </select>
      <select [ngModel]="cableView()" (ngModelChange)="setView($event)" [title]="t('What the cables show')">
        @for (v of views; track v.id) { <option [value]="v.id" [title]="t(v.hint)">{{ t('View') }}: {{ t(v.label) }}</option> }
      </select>
      <div class="zoom">
        <button type="button" (click)="zoomBy(1 / 1.25)" [title]="t('Zoom out')">−</button>
        <span>{{ (view().k * 100).toFixed(0) }}%</span>
        <button type="button" (click)="zoomBy(1.25)" [title]="t('Zoom in')">+</button>
        <button type="button" (click)="fit()" [title]="t('Fit all devices')">⤢</button>
      </div>
      <select [ngModel]="''" (ngModelChange)="exportAs($event)" [title]="t('Save the map')">
        <option value="">{{ t('Export…') }}</option><option value="jpg">JPEG</option><option value="pdf">PDF</option><option value="html">HTML</option>
      </select>
    </div>
    @if (busy() && nodes().length) { <div class="busybar"></div> }
    @if (err()) { <div class="err">{{ err() }}</div> }
    <div class="body">
      <div #stage [class]="'stage ' + look()" [class.drag]="panning" (wheel)="onWheel($event)" (pointerdown)="down($event)" (pointermove)="move($event)" (pointerup)="up($event)" (pointerleave)="hover.set(null)">
        <svg #svg [attr.viewBox]="'0 0 ' + stageSize().w + ' ' + stageSize().h" [innerHTML]="markup()" font-family="Inter,'Segoe UI',-apple-system,Helvetica,Arial,sans-serif" style="width:100%;height:100%"></svg>
        @if (!nodes().length && !busy()) { <div class="empty">{{ t('No devices read yet. Neighbours appear here after the first read.') }}</div> }
        @if (!nodes().length && busy()) {
          <div class="skel">
            <svg viewBox="0 0 800 440" preserveAspectRatio="xMidYMid meet">
              @for (l of skelLinks; track $index) { <path class="sl" pathLength="1" [attr.d]="l" [style.--i]="$index" /> }
              @for (n of skelNodes; track $index) {
                <g class="sn" [style.--i]="$index" [attr.transform]="'translate(' + (n[0] - 70) + ' ' + (n[1] - 24) + ')'">
                  <rect width="140" height="48" rx="9" />
                  <rect class="sp" x="9" y="11" width="34" height="26" rx="3" />
                  <rect class="st" x="54" y="14" width="64" height="7" rx="3" /><rect class="st" x="54" y="28" width="44" height="6" rx="3" />
                </g>
              }
            </svg>
            <div class="scap"><span class="dotp"></span>{{ t('Reading the devices and their neighbours…') }}</div>
          </div>
        }
        @if (showProg()) {
          @switch (look()) {
            @case ('material') {
              <div class="pm">
                <div class="pring"><svg viewBox="0 0 64 64"><circle cx="32" cy="32" r="27" class="rb" /><circle cx="32" cy="32" r="27" class="rf" [attr.stroke-dasharray]="ringDash()" transform="rotate(-90 32 32)" /></svg><b>{{ finished() }}</b><small>{{ t('of') }} {{ devs().length }}</small></div>
                <ol class="vs">@for (s of stepList(); track s.n) { <li [class]="s.st"><i>{{ s.st === 'ok' ? '✓' : s.n }}</i><div><b>{{ t(s.name) }}</b><small>{{ s.sub }}</small></div></li> }</ol>
              </div>
            }
            @case ('constellation') {
              <div class="hud hl"><h5>{{ t('Reading') }}</h5>@for (s of stepList(); track s.n) { <div class="hs" [class]="s.st"><i>{{ s.st === 'ok' ? '✓' : s.n }}</i><div><b>{{ t(s.name) }}</b><small>{{ s.sub }}</small></div></div> }</div>
              <div class="hud hr"><h5>{{ t('At the same time') }} · {{ workers().length }}</h5>@for (w of workers(); track w.session) { <div class="hw"><b>{{ w.name }}</b><small>{{ w.now || t('Connecting…') }}</small></div> }</div>
            }
            @case ('zones') {
              <div class="dock">@for (s of stepList(); track s.n) { <div class="ds" [class]="s.st"><i>{{ s.st === 'ok' ? '✓' : s.n }}</i><div><b>{{ t(s.name) }}</b><small>{{ s.sub }}</small></div></div> }</div>
            }
            @case ('flow') {
              <div class="seg5">@for (s of stepList(); track s.n) { <div [class]="s.st"><div class="sl"><b>{{ t(s.name) }}</b><span>{{ s.sub }}</span></div><div class="tr"><i [style.width.%]="s.pct"></i></div></div> }</div>
            }
            @default {
              <div class="stepsbar">@for (s of stepList(); track s.n) { <div class="stp" [class]="s.st"><i>{{ s.st === 'ok' ? '✓' : s.st === 'run' ? '' : s.n }}</i><div><b>{{ t(s.name) }}</b><small>{{ s.sub }}</small></div></div> }</div>
            }
          }
        }
        @if (hover(); as h) {
          <div class="card" [style.left.px]="h.x + 14" [style.top.px]="h.y + 14">
            <b>{{ h.title }}</b>
            @for (l of h.lines; track $index) { <div>{{ l }}</div> }
          </div>
        }
      </div>
      <aside class="side">
        <div class="sidescroll">
        @if (selLinks().length) {
          <h4>{{ selLinks().length > 1 ? t('Links') + ' (' + selLinks().length + ')' : t('Link') }}</h4>
          @for (l of selLinks(); track l.key) {
            <div class="lk">
              <div>{{ nodeName(l.a) }} <span class="mut">{{ l.aPort }}</span> ↔ {{ nodeName(l.b) }} <span class="mut">{{ l.bPort }}</span></div>
              <div class="mut">{{ l.state }}{{ l.speed ? ' · ' + l.speed : '' }}{{ l.stpA || l.stpB ? ' · STP ' + (l.stpA || '-') + '/' + (l.stpB || '-') : '' }}{{ l.lagA || l.lagB ? ' · LAG ' + (l.lagA?.name || '') + (l.lagB ? ' / ' + l.lagB.name : '') : '' }}</div>
              @if (l.warn) { <div class="warn">{{ l.warn }}</div> }
              @if (l.seen?.length) { <div class="mut">{{ t('Seen by') }} {{ l.seen!.join(', ') }}</div> }
            </div>
          }
        } @else if (selNode(); as n) {
          <h4>{{ n.name }}</h4>
          <div class="mut">{{ n.os || (n.ghost ? t('not in Session Manager') : '') }} {{ n.ip || '' }}</div>
          @if (n.chassis) { <div class="mut">{{ n.chassis }}</div> }
          @for (x of n.notes ?? []; track $index) { <div class="warn">{{ x }}</div> }
          @if (n.session) { <button type="button" (click)="openSession(n.session)">{{ t('Open session') }}</button> }
          @if (nodeLinks().length) {
            <h4>{{ t('Ports in use') }}</h4>
            @for (x of nodeLinks(); track x.key) { <div class="lk"><b>{{ x.port }}</b> → {{ x.peer }} <span class="mut">{{ x.peerPort }}</span><div class="mut">{{ x.info }}</div></div> }
          }
        }
        @if (devs().length) {
          <h4>{{ t('Devices') }} · {{ finished() }}/{{ devs().length }}</h4>
          @for (d of devs(); track d.session) {
            <div class="dv" [class]="d.state"><i></i><div><b>{{ d.name }}</b><small>{{ d.state === 'error' ? d.err : d.state === 'done' ? t('Read') + (d.ms ? ' · ' + (d.ms / 1000).toFixed(1) + ' s' : '') : d.state === 'running' ? (d.now || t('Connecting…')) : t('Waiting') }}</small></div></div>
          }
        }
        <h4>{{ t('Changes') }}</h4>
        @for (c of changes(); track $index) { <div class="chg"><span class="mut">{{ clock(c.at) }}</span> {{ c.text }}</div> }
        @if (!changes().length) { <div class="mut">{{ t('No changes seen yet.') }}</div> }
        </div>
        <div class="legendbox">
        <h4>{{ t('Legend') }}</h4>
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
        </div>
      </aside>
    </div>
  `,
  styles: [`
    :host { display: flex; flex-direction: column; height: 100%; min-height: 0; background: var(--bg); }
    .bar { display: flex; align-items: center; gap: 8px; padding: 6px 10px; border-bottom: 1px solid var(--border); flex-wrap: wrap; }
    .ttl { margin-right: 6px; } .sp { flex: 1; } .mut { color: var(--muted, #8b8e96); font-size: 12px; }
    .chk { display: flex; align-items: center; gap: 4px; font-size: 12px; } .comm { width: 110px; }
    .zoom { display: flex; align-items: center; gap: 4px; } .zoom button { width: 28px; padding: 0; } .zoom span { min-width: 40px; text-align: center; font-size: 12px; }
    .err { padding: 6px 12px; color: #f87171; font-size: 12px; }
    .body { flex: 1; display: flex; min-height: 0; }
    .stage { flex: 1; position: relative; overflow: hidden; cursor: grab; touch-action: none; user-select: none; } .stage.drag { cursor: grabbing; }
    .side { width: 270px; border-left: 1px solid var(--border); font-size: 12.5px; display: flex; flex-direction: column; min-height: 0; }
    .sidescroll { flex: 1; min-height: 0; overflow: auto; padding: 8px 12px; display: flex; flex-direction: column; gap: 4px; }
    .legendbox { flex: none; border-top: 1px solid var(--border); padding: 4px 12px 10px; background: var(--panel); } .legendbox h4 { margin-top: 6px; }
    .side h4 { margin: 10px 0 2px; font-size: 12px; text-transform: uppercase; letter-spacing: .04em; color: var(--muted, #8b8e96); }
    .lg { display: flex; flex-direction: column; gap: 6px; } .lr { display: flex; align-items: center; gap: 10px; font-size: 12.5px; line-height: 1.3; } .lr svg { flex: none; }
    .warn { color: #f59e0b; } .chg { line-height: 1.35; }
    .empty { position: absolute; inset: 0; display: flex; align-items: center; justify-content: center; color: var(--muted, #8b8e96); pointer-events: none; }
    .skel { position: absolute; inset: 0; display: flex; flex-direction: column; align-items: center; justify-content: center; gap: 6px; pointer-events: none; }
    .skel svg { width: min(86%, 900px); height: auto; }
    .sn rect { fill: var(--panel); stroke: var(--border); stroke-width: 1; } .sn .sp { fill: var(--tab); stroke: none; } .sn .st { fill: var(--tab); stroke: none; }
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
    .stage.ant, .stage.material, .stage.flow, .stage.zones { background-image: radial-gradient(rgba(128,128,128,.22) 1px, transparent 1px); background-size: 18px 18px; }
    .stage.constellation { background-image: radial-gradient(ellipse at center, rgba(79,156,247,.10), transparent 62%); }
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
    .seg5 { top: 10px; left: 12px; right: 12px; display: grid; grid-template-columns: repeat(5, 1fr); gap: 14px; padding: 10px 14px; border-radius: 8px; }
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
    .hud { position: absolute; z-index: 4; pointer-events: none; top: 12px; width: 220px; padding: 12px 14px; border-radius: 12px; background: color-mix(in srgb, var(--panel) 88%, transparent); border: 1px solid var(--border); backdrop-filter: blur(6px); }
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
  protected phase = signal<'' | 'reading' | 'build'>('');
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
    if (!COMPUTED_LOOKS.includes(l)) return null;
    const ids = new Set(this.shown().map(n => n.id)), D = dims(l);
    const ln = this.shown().map(n => ({ id: n.id, name: n.name, ghost: n.ghost })), le = (this.map().links ?? []).filter(e => ids.has(e.a) && ids.has(e.b)).map(e => ({ a: e.a, b: e.b }));
    return l === 'flow' ? flowLayout(ln, le, D.w, D.h) : l === 'constellation' ? radialLayout(ln, le, D.w) : zonesLayout(ln, le, D.w, D.h);
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
      return { id: n.id, name: n.name, sub: n.ghost ? tr('seen by neighbour discovery') : (n.os ?? '').toUpperCase(), ip: n.ghost ? '' : n.ip, kind: kindOf(n.os, n.caps, n.ghost), ghost: n.ghost, x: q.x, y: q.y, flag: (n.notes?.length ? 'warn' : ''), os: n.os, notes: n.notes?.length ?? 0, group: at.get(n.id)?.group, prog: this.progOf(n) };
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
    const inner = mapMarkup(this.nodes(), this.links(), { theme: th, showPorts: this.ports() && v.k >= .55, selLink: this.selLinkKey(), selNode: this.selNodeId(), fresh: this.fresh, look: this.look(), zones: this.zones(), view: this.cableView(), rings: this.rings(), sweep: this.busy() });
    const html = `<g transform="translate(${v.x} ${v.y}) scale(${v.k})">${inner}</g>`;
    return this.san.bypassSecurityTrustHtml(html); // drawn by map-svg.ts from escaped text only
  });
  protected legend = computed(() => {
    const th = document.documentElement.classList.contains('light') ? THEME_LIGHT : THEME_DARK, v = this.cableView();
    const rows: { k: string; c: string; d: string; e?: string; w?: number; t: string }[] = [];
    if (v === 'speed') rows.push({ k: 'sp', c: th.ok, d: '', w: 3.4, t: '10 Mbps – 10 Gbps' }, { k: 'sp', c: th.acc, d: '', w: 3.8, t: '25 – 100 Gbps' }, { k: 'sp', c: th.acc, d: '', w: 6.6, t: tr('Above 100 Gbps') });
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
    out.push({ n: 5, name: 'Build map', st: ph === 'build' ? 'run' : 'wait', sub: ph === 'build' ? tr('Joining…') : tr('Waiting'), pct: ph === 'build' ? 60 : 0 });
    return out;
  });
  protected finished = computed(() => this.devs().filter(x => x.state === 'done' || x.state === 'error').length);
  protected showProg = computed(() => this.busy() && this.devs().length > 0);
  protected ringDash = computed(() => { const n = this.devs().length; return n ? (170 * this.finished() / n).toFixed(0) + ' 170' : '0 170'; });
  protected workers = computed(() => this.devs().filter(x => x.state === 'running').slice(0, 8));
  protected selLinks = computed<TopoLink[]>(() => { const ks = new Set(this.selLinkKey().split(' ').filter(Boolean)); return ks.size ? (this.map().links ?? []).filter(l => ks.has(l.key)) : []; });
  protected nodeLinks = computed(() => {
    const id = this.selNodeId();
    if (!id) return [];
    return (this.map().links ?? []).filter(l => l.a === id || l.b === id).map(l => {
      const a = l.a === id, lag = a ? l.lagA : l.lagB;
      return { key: l.key, port: (a ? l.aPort : l.bPort) || '?', peer: this.nodeName(a ? l.b : l.a), peerPort: (a ? l.bPort : l.aPort) || '', info: [l.state, l.speed, lag ? 'LAG ' + lag.name : '', (a ? l.stpA : l.stpB) ? 'STP ' + (a ? l.stpA : l.stpB) : ''].filter(Boolean).join(' · ') };
    }).sort((x, y) => x.port.localeCompare(y.port, undefined, { numeric: true }));
  });
  protected selNode = computed<TopoNode | null>(() => (this.map().nodes ?? []).find(n => n.id === this.selNodeId()) ?? null);
  protected updated = computed(() => {
    if (!this.map().at) return '';
    const s = Math.max(0, Math.round((this.now() - new Date(this.map().at).getTime()) / 1000));
    return tr('Updated') + ' ' + (s < 90 ? s + ' s' : Math.round(s / 60) + ' min') + ' ' + tr('ago');
  });

  constructor() {
    try { const i = localStorage.getItem('map.interval'); if (i && INTERVALS.includes(+i)) this.interval.set(+i); const lk = localStorage.getItem('map.look') || this.ui.settings()?.mapLook || 'flow'; if (LOOKS.some(l => l.id === lk)) this.look.set(lk as Look); const vw = localStorage.getItem('map.view'); if (VIEWS.some(v => v.id === vw)) this.cableView.set(vw as View); } catch { /* storage may be blocked */ }
    this.unProg = this.backend.on('topo:progress', (p: TopoProgress) => this.onProgress(p));
    effect(() => { if (this.visible()) { this.restartTimer(); if (!this.lastRead) void this.refresh(false); else if (Date.now() - this.lastRead > this.interval() * 1000 && this.interval()) void this.refresh(false); } });
    this.timer = setInterval(() => this.tick(), 1000);
    effect(() => {
      const el = this.stage().nativeElement;
      this.ro?.disconnect();
      this.ro = new ResizeObserver(() => { const w = el.clientWidth, h = el.clientHeight; if (w && h) { const first = !this.sized; this.sized = true; this.stageSize.set({ w, h }); if (first && this.nodes().length) this.fit(); } });
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
    this.look.set(l); this.hover.set(null);
    try { localStorage.setItem('map.look', l); } catch { /* ignore */ }
    // the tiered looks need a layered arrangement; keep the user's own positions when there are some
    if (TIERED.includes(l) && !['layered', 'spine-leaf', 'tree'].includes(this.layoutStyle()) && ![...this.pos().values()].some(p => p.pinned)) void this.arrange('layered');
    else queueMicrotask(() => this.fit());
  }
  protected setView(v: View) { this.cableView.set(v); try { localStorage.setItem('map.view', v); } catch { /* ignore */ } }
  private onProgress(p: TopoProgress) {
    if (p.scope !== this.scope()) return;
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
    this.busy.set(true); this.err.set(''); this.devs.set([]); this.phase.set('reading');
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
    } finally { this.busy.set(false); this.phase.set(''); }
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
    if (!this.fitted && nodes.length) { this.fitted = true; queueMicrotask(() => this.fit()); }
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
    this.view.set({ k, x: x0 - (x0 - v.x) * r, y: y0 - (y0 - v.y) * r });
  }
  /** Show all devices: as large as fits (not above 100%), centred. */
  protected fit() {
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
    if (d && d.kind === 'pan') { this.view.update(v => ({ ...v, x: d.vx + e.clientX - d.sx, y: d.vy + e.clientY - d.sy })); return; }
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
        const doc = mapDocument(nodes, this.links(), { theme: th, showPorts: true, selLink: '', selNode: '', look: this.look(), zones: this.zones(), view: this.cableView(), rings: this.rings() }, title);
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
      const doc = mapDocument(nodes, this.links(), { theme: th, showPorts: true, selLink: '', selNode: '', look: this.look(), zones: this.zones(), view: this.cableView(), rings: this.rings() }, title);
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
