import { isUnixType } from '../core/device-kind';
import { Component, OnDestroy, computed, inject, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { DEVICE_KINDS, DeviceIcon, DeviceKind, customDeviceKinds } from '../components/device-icon';
import { Backend, errText } from '../core/backend';
import { DeviceType, DiscoverResult, LocalNet, MovedDevice, SessionNode } from '../core/models';
import { Tabs } from '../core/tabs.service';
import { Ui } from '../core/ui.service';
import { Moved } from './moved';
import { FolderPicker } from '../components/folder-picker';
import { Icon } from '../components/icon';

interface Row extends DiscoverResult { selected: boolean; exists: boolean; deviceType: string; open?: boolean; touched?: boolean; }

const ACK_KEY = 'termwright.discoverAck';

/**
 * File → Discover Devices: scans an address range for devices (SSH, Telnet,
 * web, printing, phones, Windows ports; optional SNMP v2c system name and
 * description), shows them with an icon per kind, and adds the selected ones
 * as sessions. It never tries to log in.
 */
@Component({
  selector: 'app-discover-dialog',
  imports: [FormsModule, DeviceIcon, Moved, FolderPicker, Icon],
  template: `
  <div class="modal-backdrop">
    <div class="modal disc" [class.results]="total() || rows().length">
      <div class="modal-head"><span class="dh-ic"><app-icon name="radar" [size]="18" /></span>
        <span class="dh-t">Discover Devices<span class="dh-s">Find the devices on a network and add them as sessions - nothing logs in</span></span></div>
      <div class="modal-body">
        <div class="pre">
          <!-- before the first scan: the networks of this computer to pick from, then typed addresses; options folded away -->
          @if (!total() && groups().length) {
            <div class="sec"><span>Scan a network of this computer</span>
              <button type="button" class="linkbtn" [disabled]="running()" (click)="loadNets()">Refresh</button></div>
            <div class="nets" role="radiogroup" aria-label="Networks of this computer">
              @for (g of groups(); track g.scan) {
                <button type="button" class="net" role="radio" [class.sel]="spec.trim() === g.scan" [attr.aria-checked]="spec.trim() === g.scan"
                  [disabled]="running()" (click)="spec = g.scan" [title]="g.note">
                  <span class="rad"></span><b class="nn" translate="no">{{ g.scan }}</b><span class="via" translate="no">{{ g.via }}</span>
                  @if (g.vpn) { <span class="nv">VPN</span> }
                  <span class="nh">{{ g.hosts }} addresses</span>
                  @if (g.note) { <span class="nt">{{ g.note }}</span> }
                </button>
              }
            </div>
            <div class="sec">Or type addresses</div>
          }
          <div class="row specrow">
            <input name="spec" class="spec" [(ngModel)]="spec" [disabled]="running()" placeholder="192.168.1.0/24"
              (keydown.enter)="start()" spellcheck="false" />
            @if (running()) {
              <button type="button" (click)="stop()">Stop</button>
            } @else {
              <button type="button" class="primary" [disabled]="!spec.trim() || (!acked && !ackNow)" (click)="start()">Scan</button>
            }
          </div>
          @if (!total()) { <div class="hint">A network, a range (192.168.1.10-80) or single addresses - up to a /22</div> }
          <details class="opts" [open]="optsOpen" (toggle)="optsOpen = $any($event.target).open">
            <summary><b>Options</b> <span class="hint">{{ useSnmp ? '· SNMP v2c lookup on' : '· SNMP v2c lookup off' }}</span></summary>
            <div class="row">
              <label class="inline"><input type="checkbox" [(ngModel)]="useSnmp" [disabled]="running()" /> Look up name and model (SNMP v2c)</label>
              <input class="comm" [type]="showComm ? 'text' : 'password'" [(ngModel)]="community" [disabled]="running() || !useSnmp" placeholder="community"
                autocomplete="off" spellcheck="false" title="Read-only SNMP community. It is used for this scan only and not saved." />
              <button type="button" class="icon-btn eye" (click)="showComm = !showComm" [disabled]="!useSnmp"
                [title]="showComm ? 'Hide the community' : 'Show the community'">{{ showComm ? 'Hide' : 'Show' }}</button>
            </div>
          </details>
          @if (!acked) {
            <div class="warn">Scanning connects to many addresses and can be noticed by security monitoring. Scan only networks you are
              allowed to scan, such as your lab. Termwright never tries to log in; it checks which ports answer, reads the SSH/Telnet
              greeting, the start page of web interfaces, asks phones over SIP and, with a community, reads the SNMP system description.
              <label class="ack"><input type="checkbox" [(ngModel)]="ackNow" /> I am allowed to scan these addresses</label></div>
          }
        </div>
        <div class="bar">
          @if (total()) {
            <progress [max]="total()" [value]="done()"></progress>
            <span class="hint">{{ done() }} / {{ total() }} addresses{{ again().total ? (running() ? ' · checking ' + again().total + ' device' + (again().total === 1 ? '' : 's') + ' again, more slowly (' + again().done + ' / ' + again().total + ')' : ' · ' + again().total + ' checked again') : '' }} · {{ rows().length }} found{{ running() ? '' : stopped() ? ' · stopped' : ' · done' }}{{ rows().length ? ' · double-click a device to connect' : '' }}</span>
          }
          <span class="spacer"></span>
          @if (rows().length) {
            <label class="inline hint"><input type="checkbox" [(ngModel)]="loginOnly" /> Only devices you can connect to</label>
          }
        </div>
        @if (err()) { <p class="err">{{ err() }}</p> }
        @if (snmpSilent()) {
          <p class="note">No device answered SNMP with this community. Check the community, that SNMP is enabled, and that the devices' SNMP access list allows this computer - with SNMP the name, model and software are shown for every device.</p>
        }
        @if (!ouiDate() && total() && !running()) {
          <p class="note">MAC addresses are shown for devices on your own subnet. <a (click)="downloadOui()">{{ ouiBusy() ? 'Downloading…' : 'Download the MAC vendor list' }}</a>
            (IEEE registry, about 3 MB, saved in the data folder) to name their makers - this also identifies phones and printers with no open ports.</p>
        }
        @if (!running() && (moved().length || noMac().length || reused().length)) {
          <div class="movedbar">
            @if (moved().length) {
              <span>⚠ <b>{{ moved().length }} saved session{{ moved().length === 1 ? '' : 's' }}</b> answer{{ moved().length === 1 ? 's' : '' }} at a new address (same MAC address).</span>
              <button type="button" class="primary" (click)="reviewMoved()">Review…</button>
            }
            @for (u of reused().slice(0, 3); track u.path) {
              <div class="chg">⚠ <span><b>{{ u.ip }} is now another device.</b> Your session <b>{{ u.path }}</b> was for <span class="mono">{{ u.oldMac }}</span>;
                that device was not found, <span class="mono">{{ u.newMac }}</span> answers there now.</span>
                <span class="spacer"></span>
                <button type="button" (click)="keepReused(u.path)" title="Leave the session as it is (the device may be switched off)">Keep</button>
                <button type="button" (click)="updateReused(u)" title="Make the device now at this address the session's device (its MAC address is saved)">Update the session</button></div>
            }
            @if (reused().length > 3) { <span class="hint">and {{ reused().length - 3 }} more sessions whose address now belongs to another device</span> }
            @if (noMac().length) {
              <span class="hint">{{ noMac().length }} saved session{{ noMac().length === 1 ? '' : 's' }} found at {{ noMac().length === 1 ? 'its' : 'their' }} address without a saved MAC address.</span>
              <button type="button" (click)="rememberMacs()" title="Save the MAC addresses so Termwright finds these devices again when their IP address changes">Remember MAC Addresses</button>
            }
          </div>
        }
        @if (rows().length) {
          <div class="tbl">
            <table class="list">
              <thead><tr>
                <th class="c"><input type="checkbox" [checked]="allSelected()" (change)="selectAll(!allSelected())" title="Select all that can be added" /></th>
                <th></th><th>Name</th><th>Address</th><th>Type</th><th>Vendor / system</th><th>Services</th>
                <th>Device type</th><th></th>
              </tr></thead>
              <tbody>
                @for (r of shown(); track r.ip) {
                  <tr [class.dim]="!addable(r)" [class.opened]="r.open" (click)="toggle(r, $event)" (dblclick)="connect(r, $event)" (contextmenu)="rowMenu($event, r)"
                    [title]="r.protocol ? 'Double-click to connect now (' + r.protocol + ') without saving' : r.mac ? 'No login port answered - it can be added as a Wake-on-LAN session' : ''">
                    <td class="c"><input type="checkbox" [checked]="r.selected" [disabled]="!addable(r)"
                      [title]="r.protocol ? '' : r.mac ? 'No login port answered - added as a Wake-on-LAN session' : 'No SSH, Telnet, RDP or VNC port answered and no MAC address - cannot be added as a session'" /></td>
                    <td><app-device-icon [kind]="typeOf(r).kind" /></td>
                    <td><b>{{ r.name }}</b>@if (r.self) { <span class="tag me">this computer</span> }@if (r.exists) { <span class="tag">in sessions</span> }@if (!r.protocol && r.mac && r.selected) { <span class="tag wol" title="Added as a Wake-on-LAN session">Wake-on-LAN</span> }
                      @if (r.dns && r.dns !== r.name) { <div class="hint">{{ r.dns }}</div> }</td>
                    <td class="mono">{{ r.ip }}@if (r.mac) { <div class="hint mac" title="Right-click the row to copy the MAC address">{{ r.mac }}</div> }</td>
                    <td>{{ typeOf(r).label }}@if (typeOf(r).why) { <div class="hint likely">{{ typeOf(r).why }}</div> }</td>
                    <td class="sys">
                      <button type="button" class="info" [class.on]="r.open" (click)="details(r, $event)" title="What this is based on">ⓘ</button>
                      @if (r.vendor) { <b>{{ r.vendor }}</b> }{{ r.vendor && r.os ? ' · ' : '' }}{{ r.os }}
                      @if (r.open) { <ul class="ev">@for (e of r.evidence ?? []; track $index) { <li>{{ e }}</li> } @empty { <li>Only open ports were seen.</li> }</ul> }
                    </td>
                    <td>@for (s of r.services; track s) { <span class="svc" [class.login]="s === 'ssh' || s === 'telnet' || s === 'rdp' || s === 'vnc'">{{ svcLabel(s) }}</span> }</td>
                    <td (click)="$event.stopPropagation()">
                      <select #dt [ngModel]="r.deviceType" (ngModelChange)="pickType(r, $event, dt)" [disabled]="!addable(r)"
                        [title]="addable(r) ? 'The device type sets the icon and, with a login, the prompts and commands' : 'This device cannot be added as a session'">
                        <option value="">(none)</option>
                        @for (d of deviceTypes(); track d.name) { <option [value]="d.name">{{ d.name }}</option> }
                        <option value="+new">＋ New device type…</option>
                      </select>
                    </td>
                    <td class="cx" (click)="$event.stopPropagation()">
                      @if (actionsOf(r); as acts) {
                        @if (acts.length) {
                          <span class="split"><button type="button" (click)="run(r, acts[0])" [title]="acts[0].label + ' - without saving a session'">{{ acts[0].short }}</button><button type="button" class="dd" (click)="connectMenu($event, r)" title="All the ways to connect">▾</button></span>
                        }
                      }
                    </td>
                  </tr>
                }
              </tbody>
            </table>
          </div>
        }
      </div>
      @if (reviewing()) { <app-moved [embedded]="true" (closed)="reviewing.set(false)" /> }
      <div class="modal-foot">
        @if (rows().length) {
          <!-- what to add, and where: here at the bottom, next to Add; usable once devices are selected -->
          <div class="addopts" [class.off]="!selectedCount()">
            <label class="inline">Add
              <select [ngModel]="saveMode()" (ngModelChange)="saveMode.set($event); savePrefs()" [disabled]="!selectedCount()">
                <option value="all">all selected devices</option>
                <option value="types">only the selected devices of these device types</option>
              </select></label>
            @if (saveMode() === 'types' && selectedCount()) {
              @for (t of foundTypes(); track t) {
                <label class="chip" [class.on]="picked().includes(t)"><input type="checkbox" [checked]="picked().includes(t)" (change)="toggleType(t)" />
                  {{ t || '(no device type)' }} <span class="n">{{ countOf(t) }}</span></label>
              }
            }
            <label class="inline" title="For example Discovered/EXOS and Discovered/VOSS">
              <input type="checkbox" [ngModel]="byType()" (ngModelChange)="byType.set($event); savePrefs()" [disabled]="!selectedCount()" /> a subfolder per device type</label>
            <label class="inline">to</label>
            <app-folder-picker class="folder" [(value)]="folder" />
          </div>
        }
        <span class="spacer"></span>
        <button type="button" (click)="ui.close()">Close</button>
        @if (rows().length && (ui.settings()?.teams ?? []).length) {
          <button type="button" [disabled]="!selectedCount()" (click)="pushToTeam()" title="Offer the selected devices to your team's inventory (nothing is sent before you confirm)">Push to Team…</button>
        }
        @if (rows().length) {
          <button type="button" class="primary" [disabled]="!selectedCount() || adding()" (click)="add()">
            {{ adding() ? 'Adding…' : selectedCount() ? 'Add ' + selectedCount() + ' Session' + (selectedCount() === 1 ? '' : 's') : 'Add Sessions' }}</button>
        }
      </div>
      @if (newType(); as nt) {
        <div class="ntype-back" (click)="newType.set(null)"></div>
        <form class="ntype" (ngSubmit)="createType()" (click)="$event.stopPropagation()">
          <b>New device type</b>
          <label class="f"><span>Name</span><input name="ntname" [(ngModel)]="nt.name" placeholder="e.g. Phone, IoT, TV, Camera" required /></label>
          <span class="f"><span>Icon</span></span>
          <div class="icons">
            @for (k of iconKinds; track k) {
              <button type="button" [class.on]="nt.icon === k" (click)="nt.icon = k" [title]="kindLabel(k)"><app-device-icon [kind]="k" [size]="30" /></button>
            }
          </div>
          <p class="hint">Sets the icon of the device's session. Login steps, prompts and commands can be added later in Options → Device Types.</p>
          <div class="row end"><button type="button" (click)="newType.set(null)">Cancel</button><button type="submit" class="primary" [disabled]="!nt.name.trim()">Create</button></div>
        </form>
      }
    </div>
  </div>`,
  styles: [`
    .disc { width: min(1100px, 95vw); }
    .row { display: flex; gap: 10px; align-items: center; flex-wrap: wrap; }
    .inline { display: inline-flex; gap: 6px; align-items: center; white-space: nowrap; }
    .comm { flex: 0 0 220px; width: 220px !important; max-width: 220px; }
    .disc .specrow > input.spec { flex: 0 1 340px; width: 340px; min-width: 160px; }
    .eye { font-size: calc(11px * var(--ui-scale, 1)); width: auto; padding: 0 6px; }
    .ack { display: flex; gap: 6px; align-items: center; margin-top: 6px; font-weight: 600; }
    .bar { display: flex; gap: 10px; align-items: center; margin: 12px 0 8px; }
    .bar progress { width: 220px; accent-color: var(--accent); }
    .spacer { flex: 1; }
    .tbl { max-height: 52vh; overflow: auto; border: 1px solid var(--border); border-radius: 6px; }
    /* with results, only the list scrolls: the window has a fixed height, the parts above the list and the bar below stay */
    .disc.results { height: min(880px, calc(100vh - 40px)); position: relative; }
    .disc.results .modal-body { display: flex; flex-direction: column; overflow: hidden; overflow: clip; min-height: 0; }
    .disc.results .modal-body > * { flex: none; }
    /* the list takes the rest of the height and scrolls (as specific as the rule above, which would otherwise stop it growing) */
    .disc.results .modal-body > .tbl { flex: 1 1 0; min-height: 120px; max-height: none; overflow: auto; }
    .modal-foot { flex-wrap: wrap; row-gap: 8px; }
    .addopts { display: flex; flex-wrap: wrap; align-items: center; gap: 6px 10px; font-size: calc(12.5px * var(--ui-scale, 1)); }
    .addopts.off { opacity: .55; }
    .addopts select { max-width: 330px; }
    .likely { font-size: calc(11px * var(--ui-scale, 1)); }
    .chg { display: flex; align-items: center; gap: 10px; width: 100%; font-size: calc(12.5px * var(--ui-scale, 1)); .mono { font-family: var(--mono); font-size: calc(11.5px * var(--ui-scale, 1)); } button { flex: none; } }
    .cx { width: 1%; white-space: nowrap; }
    .split { display: inline-flex; border: 1px solid var(--border); border-radius: var(--r); overflow: hidden;
      button { height: 26px; min-height: 0; border: 0; border-radius: 0; padding: 0 10px; background: var(--surface); font-size: calc(12px * var(--ui-scale, 1)); font-weight: 500; }
      button:hover { background: var(--hover); } .dd { padding: 0 6px; border-left: 1px solid var(--border); } }
    .ntype-back { position: absolute; inset: 0; background: rgba(0, 0, 0, .18); z-index: 20; }
    .ntype { position: absolute; z-index: 21; left: 50%; top: 50%; transform: translate(-50%, -50%); width: 380px; display: flex; flex-direction: column; gap: 10px;
      padding: 16px 18px; background: var(--g-solid, var(--panel)); border: 1px solid var(--border); border-radius: 12px; box-shadow: var(--shadow);
      b { font-size: calc(14px * var(--ui-scale, 1)); } .f { display: flex; flex-direction: column; gap: 4px; > span { color: var(--muted); font-size: calc(12px * var(--ui-scale, 1)); } }
      .icons { display: grid; grid-template-columns: repeat(7, 1fr); gap: 6px; }
      .icons button { height: 44px; padding: 0; display: grid; place-items: center; border: 2px solid transparent; border-radius: 10px; background: transparent; }
      .icons button.on { border-color: var(--accent); background: var(--accent-soft); }
      .row.end { justify-content: flex-end; } }
    table { width: 100%; }
    th { position: sticky; top: 0; background: var(--panel); z-index: 1; }
    td, th { vertical-align: middle; }
    tr { cursor: default; }
    tr.dim td { opacity: .6; }
    .c { width: 28px; text-align: center; }
    /* narrow and wrapping, so the device type and Connect columns fit beside it */
    .sys { max-width: 250px; white-space: normal; overflow-wrap: anywhere; }

    .info { border: 0; background: none; padding: 0 4px 0 0; height: auto; color: var(--muted); cursor: pointer; font-size: calc(13px * var(--ui-scale, 1)); }
    .info.on, .info:hover { color: var(--accent); }
    .ev { margin: 4px 0 2px; padding-left: 16px; font-size: calc(11.5px * var(--ui-scale, 1)); color: var(--muted); white-space: normal; user-select: text; }
    .tag.me { background: color-mix(in srgb, var(--accent) 25%, transparent); color: var(--text); }
    .note { font-size: calc(12px * var(--ui-scale, 1)); color: var(--muted); margin: 0 0 8px; a { color: var(--accent); cursor: pointer; } }
    .svc { display: inline-block; font-size: calc(10.5px * var(--ui-scale, 1)); padding: 0 5px; margin: 1px 3px 1px 0; border-radius: 8px; border: 1px solid var(--border); color: var(--muted); }
    .svc.login { color: #fff; background: #0d9488; border-color: transparent; }
    .tag { white-space: nowrap; font-size: calc(10.5px * var(--ui-scale, 1)); margin-left: 6px; padding: 0 6px; border-radius: 8px; background: var(--hover); color: var(--muted); font-weight: 400; }
    .folder { width: 280px; }
    .dh-ic { display: none; }
    .dh-s { display: none; }
    /* ---- modern layout ---- */
    :host-context(.modern) {
      .disc { width: min(1160px, 95vw); }
      .modal-head { display: flex; align-items: center; gap: 12px; }
      .dh-ic { display: inline-flex; width: 34px; height: 34px; border-radius: 9px; align-items: center; justify-content: center; background: var(--accent-soft); color: var(--accent); }
      .dh-t { display: flex; flex-direction: column; } .dh-s { display: block; font-size: calc(12px * var(--ui-scale, 1)); font-weight: 400; color: var(--muted); }
      .grid { grid-template-columns: 90px 1fr; }
      .grid > label:empty { display: block !important; }
      .disc .specrow > input.spec { height: 36px; font-family: var(--mono); flex-basis: 380px; width: 380px; border-radius: var(--r); }
      .specrow button { height: 36px; padding: 0 20px; border-radius: var(--r); }
      .bar { margin: 16px 0 10px; }
      .bar progress { appearance: none; -webkit-appearance: none; height: 6px; width: 260px; border: 0; border-radius: 3px; overflow: hidden; background: var(--surface-2); }
      .bar progress::-webkit-progress-bar { background: var(--surface-2); border-radius: 3px; }
      .bar progress::-webkit-progress-value { background: var(--accent); border-radius: 3px; }
      .tbl { border-color: var(--line); border-radius: var(--r-lg); background: var(--surface); }
      table.list { margin: 0; border-collapse: separate; border-spacing: 0; }
      th { background: var(--surface-2); color: var(--faint); font-size: calc(11px * var(--ui-scale, 1)); font-weight: 600; letter-spacing: .05em; text-transform: uppercase;
        padding: 8px 8px; border-bottom: 1px solid var(--line); }
      td { padding: 7px 8px; border-bottom: 1px solid var(--line); }
      tbody tr:last-child td { border-bottom: 0; }
      tbody tr:hover td { background: var(--hover); }
      app-device-icon { transform: scale(1.35); transform-origin: center; display: inline-flex; margin: 0 4px; }
      td .hint { font-size: calc(11px * var(--ui-scale, 1)); }
      .mono { font-family: var(--mono); font-size: calc(12px * var(--ui-scale, 1)); }
      .svc { border-radius: 99px; padding: 1px 7px; font-weight: 600; }
      .svc.login { background: color-mix(in srgb, var(--ok) 22%, transparent); color: var(--ok); }
      .tag { border-radius: 99px; background: var(--surface-2); border: 1px solid var(--line); }
      td select { height: 28px; border-radius: var(--r-sm); }
      .save { margin-top: 12px; }
      .chip { border-radius: 99px; padding: 3px 10px; }
      .movedbar { border-radius: var(--r); }
    }
    .save { display: flex; flex-wrap: wrap; gap: 6px 8px; align-items: center; margin-top: 10px; font-size: calc(12.5px * var(--ui-scale, 1)); }
    .save select { max-width: 330px; }
    .chip { display: inline-flex; gap: 5px; align-items: center; padding: 2px 8px; border: 1px solid var(--border); border-radius: 12px; cursor: pointer; }
    .chip.on { border-color: var(--accent); background: color-mix(in srgb, var(--accent) 15%, transparent); }
    .chip .n { color: var(--muted); font-size: calc(11px * var(--ui-scale, 1)); }
    .err { color: var(--danger); }
    .movedbar .bad { color: var(--danger); }
    .movedbar { display: flex; flex-wrap: wrap; gap: 8px 12px; align-items: center; padding: 8px 10px; margin-bottom: 8px; border: 1px solid color-mix(in srgb, #f59e0b 55%, var(--border)); border-radius: 6px; background: color-mix(in srgb, #f59e0b 9%, transparent); font-size: calc(12.5px * var(--ui-scale, 1)); }
    select { max-width: 150px; }
    /* before a scan the window is narrow and calm; it widens for the results */
    .disc:not(.results) { width: min(700px, 95vw) !important; }
    .pre { display: flex; flex-direction: column; gap: 8px; }
    .sec { display: flex; align-items: center; justify-content: space-between; margin-top: 4px; font-size: calc(11px * var(--ui-scale, 1)); font-weight: 600;
      letter-spacing: .08em; text-transform: uppercase; color: var(--faint); }
    .disc .linkbtn { height: auto; min-height: 0; background: none; border: 0; padding: 0; color: var(--accent); cursor: pointer; font-size: calc(11.5px * var(--ui-scale, 1));
      text-transform: none; letter-spacing: 0; font-weight: 500; }
    .nets { display: flex; flex-direction: column; gap: 6px; margin-bottom: 6px; }
    .disc .nets .net { height: auto; min-height: 0; display: flex; flex-wrap: wrap; align-items: center; gap: 4px 12px; text-align: left; width: 100%; padding: 10px 14px;
      border: 1px solid var(--line); border-radius: var(--r-lg, 10px); background: var(--surface); color: inherit; cursor: pointer; font: inherit; }
    .disc .nets .net:hover:not([disabled]) { border-color: var(--border); background: var(--surface-2); }
    .disc .nets .net.sel { border-color: var(--accent); background: color-mix(in srgb, var(--accent) 10%, var(--surface)); }
    .net .rad { width: 14px; height: 14px; border-radius: 50%; border: 2px solid var(--faint); flex: none; box-sizing: border-box; }
    .net.sel .rad { border-color: var(--accent); background: var(--accent); box-shadow: inset 0 0 0 3px var(--surface); }
    .net .nn { font-family: var(--mono); font-size: calc(13.5px * var(--ui-scale, 1)); }
    .net .via { color: var(--muted); font-size: calc(12px * var(--ui-scale, 1)); }
    .net .nh { margin-left: auto; color: var(--faint); font-size: calc(12px * var(--ui-scale, 1)); }
    .net .nv { font-size: calc(10.5px * var(--ui-scale, 1)); font-weight: 700; color: #e5a93b; background: rgba(229, 169, 59, .15); border-radius: 5px; padding: 1px 6px; }
    .net .nt { flex-basis: 100%; padding-left: 26px; font-size: calc(11px * var(--ui-scale, 1)); color: var(--faint); }
    .disc .specrow > input.spec { flex: 1 1 auto; width: auto; min-width: 160px; }
    .disc.results .specrow > input.spec { flex: 0 1 380px; width: 380px; }
    .opts { margin-top: 2px; }
    .opts summary { cursor: pointer; color: var(--muted); font-size: calc(12.5px * var(--ui-scale, 1)); user-select: none; b { color: var(--text); font-weight: 500; } .hint { margin-left: 5px; } }
    .opts .row { margin-top: 8px; padding-left: 14px; }
    .warn { font-size: calc(12px * var(--ui-scale, 1)); }
  `],
})
export class DiscoverDialog implements OnDestroy {
  protected ui = inject(Ui);
  private backend = inject(Backend);
  private tabs = inject(Tabs);
  spec = '';
  useSnmp = true;
  community = 'public';
  showComm = false;
  acked = this.loadAck();
  ackNow = false;
  loginOnly = false;
  folder = 'Discovered';
  private readonly prefs = loadPrefs();
  /** Which selected devices "Add" saves: all, or only those of the picked device types. */
  readonly saveMode = signal<'all' | 'types'>(this.prefs.mode === 'types' ? 'types' : 'all');
  readonly picked = signal<string[]>(Array.isArray(this.prefs.types) ? this.prefs.types : []);
  readonly byType = signal(!!this.prefs.byType);
  readonly running = signal(false);
  readonly stopped = signal(false);
  /** The second, slower look at devices that showed no login port. */
  readonly again = signal({ done: 0, total: 0 });
  readonly total = signal(0);
  readonly done = signal(0);
  readonly rows = signal<Row[]>([]);
  readonly err = signal('');
  readonly adding = signal(false);
  readonly ouiDate = signal('');
  readonly ouiBusy = signal(false);
  private usedCommunity = false;
  private id = '';
  private unsub: (() => void)[] = [];

  readonly deviceTypes = computed<DeviceType[]>(() => this.ui.settings()?.deviceTypes ?? []);
  readonly folders = computed(() => {
    const out: string[] = [];
    const walk = (n: SessionNode | null | undefined) => n?.children?.forEach(c => { if (c.isFolder) { out.push(c.path); walk(c); } });
    walk(this.ui.tree());
    return out;
  });
  /** Host names and addresses already in the Session Manager. */
  private readonly known = computed(() => {
    const s = new Set<string>();
    const walk = (n: SessionNode | null | undefined) => n?.children?.forEach(c => { if (c.isFolder) walk(c); else if (c.hostname) s.add(c.hostname.toLowerCase()); });
    walk(this.ui.tree());
    return s;
  });

  /** Saved sessions (by MAC address) whose device answers at another address now. */
  readonly moved = computed<MovedDevice[]>(() => {
    const byMac = new Map<string, string>();
    for (const r of this.rows()) if (r.mac) byMac.set(r.mac.toLowerCase(), r.ip);
    const out: MovedDevice[] = [];
    for (const n of this.sessionNodes()) {
      const mac = normMac(n.mac);
      const now = mac ? byMac.get(mac) : undefined;
      if (now && n.hostname && now !== n.hostname) out.push({ path: n.path, old: n.hostname, new: now, mac });
    }
    return out;
  });
  private readonly knownMacs = computed(() => new Set(this.sessionNodes().map(n => normMac(n.mac)).filter(Boolean)));
  /** Saved sessions found at their address whose MAC address is not saved yet. */
  readonly noMac = computed(() => {
    const byIp = new Map(this.rows().filter(r => r.mac && !r.self).map(r => [r.ip, r.mac]));
    return this.sessionNodes().filter(n => !n.mac && n.hostname && byIp.has(n.hostname) && !n.jump && !n.proxy)
      .map(n => ({ path: n.path, mac: byIp.get(n.hostname!)! }));
  });
  private sessionNodes() {
    const out: SessionNode[] = [];
    const walk = (n: SessionNode | null | undefined) => n?.children?.forEach(c => { if (c.isFolder) walk(c); else out.push(c); });
    walk(this.ui.tree());
    return out;
  }

  readonly reviewing = signal(false);
  reviewMoved() { this.ui.movedInput.set({ items: this.moved() }); this.reviewing.set(true); }

  /** Saved sessions whose address now answers with another MAC, and whose own MAC was not seen. */
  readonly reused = computed(() => {
    const byIp = new Map(this.rows().filter(r => r.mac).map(r => [r.ip, r.mac]));
    const seen = new Set(this.rows().filter(r => r.mac).map(r => r.mac.toLowerCase()));
    const kept = this.keptReused();
    return this.sessionNodes().filter(n => {
      const mac = normMac(n.mac);
      const now = n.hostname ? byIp.get(n.hostname) : '';
      return !kept.has(n.path) && !!mac && !!now && normMac(now) !== mac && !seen.has(mac);
    }).map(n => ({ path: n.path, ip: n.hostname!, oldMac: n.mac!, newMac: byIp.get(n.hostname!)! }));
  });
  /** "Keep": sessions whose changed address the user has seen and left as they are (for this window). */
  private readonly keptReused = signal(new Set<string>());
  keepReused(path: string) { this.keptReused.update(s => new Set([...s, path])); }
  /** "Update the session": the device now at the address becomes the session's device. */
  async updateReused(u: { path: string; newMac: string }) {
    try {
      await this.backend.replaceMac(u.path, u.newMac);
      await this.ui.refreshTree();
      this.ui.toast(`${u.path.split('/').pop()} now belongs to the device ${u.newMac}`, 'success');
    } catch (e) { this.ui.error(e); }
  }

  async rememberMacs() {
    const list = this.noMac();
    if (!(await this.ui.confirm('Remember MAC Addresses', `Save the MAC address seen at the address of ${list.length === 1 ? 'this session' : 'these ' + list.length + ' sessions'}?\n\n` +
      list.slice(0, 12).map(x => `${x.path}  ${x.mac}`).join('\n') + (list.length > 12 ? '\n…' : '') +
      '\n\nDo this only when these addresses still belong to the right devices.', 'Save'))) return;
    const macs = Object.fromEntries(this.noMac().map(x => [x.path, x.mac]));
    try {
      const n = await this.backend.rememberMacs(macs);
      await this.ui.refreshTree();
      this.ui.toast(`MAC address saved in ${n} session${n === 1 ? '' : 's'}`, 'success');
    } catch (e) { this.ui.error(e); }
  }

  /** The networks of this computer's interfaces (wired, Wi-Fi, VPN). */
  readonly nets = signal<LocalNet[]>([]);
  loadNets() {
    this.backend.discoverNetworks().then(l => {
      this.nets.set(l || []);
      const first = (l || []).find(n => !n.vpn);
      if (!this.spec.trim() && first) this.spec = first.scan;
    }).catch(() => {});
  }
  /** The networks to pick from, one per network: a computer on the same network by wire and Wi-Fi shows both ("en8 · .38 and en0 · .44"). */
  readonly groups = computed(() => {
    const m = new Map<string, { scan: string; via: string[]; hosts: number; vpn: boolean; notes: string[] }>();
    for (const n of this.nets()) {
      const g = m.get(n.scan) ?? { scan: n.scan, via: [], hosts: n.hosts, vpn: false, notes: [] };
      g.via.push(n.interface + ' · .' + (n.address.split('.').pop() ?? n.address));
      g.vpn ||= n.vpn;
      if (n.note && !g.notes.includes(n.note)) g.notes.push(n.note);
      m.set(n.scan, g);
    }
    return [...m.values()].map(g => ({ scan: g.scan, via: g.via.join('  and  '), hosts: g.hosts, vpn: g.vpn, note: g.notes.join(' · ') }));
  });
  /** The SNMP options are folded away until opened. */
  optsOpen = false;

  constructor() { this.loadNets(); this.backend.discoverOuiStatus().then(d => this.ouiDate.set(d)).catch(() => {}); }

  /** SNMP was asked but no device answered (wrong community, ACL, SNMP off). */
  snmpSilent() { return this.usedCommunity && !this.running() && this.rows().length > 0 && !this.rows().some(r => r.services.includes('snmp')); }

  async downloadOui() {
    if (this.ouiBusy()) return;
    this.ouiBusy.set(true);
    try {
      const n = await this.backend.discoverDownloadOui();
      this.ouiDate.set(new Date().toISOString().slice(0, 10));
      this.ui.toast(`MAC vendor list downloaded (${n.toLocaleString()} vendors) - scan again to use it`, 'success');
    } catch (e) { this.err.set(errText(e)); }
    this.ouiBusy.set(false);
  }

  details(r: Row, ev: Event) {
    ev.stopPropagation();
    r.open = !r.open;
    this.rows.update(l => [...l]);
  }

  shown() { return this.loginOnly ? this.rows().filter(r => r.protocol) : this.rows(); }
  /** The devices "Add" saves. */
  toAdd() {
    const types = this.saveMode() === 'types' ? new Set(this.picked()) : null;
    return this.rows().filter(r => r.selected && this.addable(r) && (!types || types.has(r.deviceType ?? '')));
  }
  /** A device with a login port is added as that kind of session; one with only a MAC address as a Wake-on-LAN session. */
  addable(r: Row) { return !!r.protocol || !!r.mac; }
  selectedCount() { return this.toAdd().length; }
  /** Device types of the devices that can be added ("" = none), named ones first. */
  foundTypes() {
    const set = new Set(this.rows().filter(r => this.addable(r)).map(r => r.deviceType ?? ''));
    return [...set].sort((a, b) => (a === '') !== (b === '') ? (a === '' ? 1 : -1) : a.localeCompare(b));
  }
  countOf(t: string) { return this.rows().filter(r => r.selected && this.addable(r) && (r.deviceType ?? '') === t).length; }
  toggleType(t: string) {
    this.picked.update(l => l.includes(t) ? l.filter(x => x !== t) : [...l, t]);
    this.savePrefs();
  }
  savePrefs() {
    try { localStorage.setItem(PREFS_KEY, JSON.stringify({ mode: this.saveMode(), types: this.picked(), byType: this.byType() })); } catch { /* ignore */ }
  }
  allSelected() { const a = this.shown().filter(r => r.protocol); return a.length > 0 && a.every(r => r.selected); }
  kindLabel(k: string) { return DEVICE_KINDS[k as DeviceKind]?.label ?? 'Unknown device'; }

  /** What a device is: the scan's answer, or - for an unknown one - a best guess with its reason. */
  typeOf(r: Row): { kind: DeviceKind; label: string; why: string } {
    const own = customDeviceKinds().get((r.deviceType || '').toLowerCase()); // a device type given an icon (e.g. "Phone") decides
    if (own) return { kind: own, label: this.kindLabel(own), why: '' };
    const k = (r.kind || 'unknown') as DeviceKind;
    if (k !== 'unknown') return { kind: k, label: this.kindLabel(k), why: '' };
    const vendor = r.vendor || r.macVendor || '';
    const web = r.services.includes('http') || r.services.includes('https');
    const login = r.services.some(x => ['ssh', 'telnet', 'rdp', 'vnc', 'smb'].includes(x));
    const v = vendor.toLowerCase();
    if (/hikvision|dahua|axis comm|reolink|amcrest|uniview|wyze|foscam|hanwha/.test(v)) return { kind: 'camera', label: 'Camera', why: 'likely: ' + vendor };
    if (/\blg electronics|samsung electronics|sony|roku|vizio|tcl|hisense|philips|sharp|panasonic|chromecast/.test(v) && !login)
      return { kind: 'tv', label: 'TV / media player', why: 'likely: ' + vendor + (web ? ', web service' : '') };
    if (web && !login && /tp-?link|netgear|asus|ubiquiti|mikrotik|d-?link|linksys|huawei|zte|tenda|cisco|aruba|ruckus|avm|fritz|draytek|zyxel|sagemcom|technicolor|arris|actiontec|cambium|edimax|buffalo|totolink|mercusys|xiaomi/.test(v))
      return { kind: 'router', label: 'Router / access point', why: 'likely: ' + vendor + ', web admin only' };
    if (/\.1$/.test(r.ip) && (web || r.services.includes('ssh')))
      return { kind: 'router', label: 'Router / gateway', why: 'likely: the gateway address' + (web ? ', web admin' : '') };
    return { kind: 'unknown', label: this.kindLabel('unknown'), why: '' };
  }

  // ---- device types made here ----
  readonly newType = signal<{ row: Row; name: string; icon: DeviceKind } | null>(null);
  readonly iconKinds: DeviceKind[] = ['mobile', 'iot', 'tv', 'camera', 'router', 'wireless', 'printer', 'computer', 'server', 'nas', 'phone', 'switch', 'firewall', 'unknown'];
  pickType(r: Row, v: string, el: HTMLSelectElement) {
    if (v !== '+new') { r.deviceType = v; r.touched = true; return; }
    // the list keeps showing "+ New…": put the device's type back, then ask for the new one
    queueMicrotask(() => { el.value = r.deviceType || ''; });
    const t = this.typeOf(r);
    const names: Partial<Record<DeviceKind, string>> = { mobile: 'Phone', iot: 'IoT', tv: 'TV', camera: 'Camera', router: 'Router', wireless: 'Access Point', printer: 'Printer', unknown: 'Unknown' };
    this.rows.update(l => [...l]);
    this.newType.set({ row: r, name: names[t.kind] ?? '', icon: t.kind });
  }
  async createType() {
    const nt = this.newType();
    if (!nt || !nt.name.trim()) return;
    const name = nt.name.trim();
    const list = this.ui.settings()?.deviceTypes ?? [];
    const existing = list.find(d => d.name.toLowerCase() === name.toLowerCase());
    const types: DeviceType[] = existing
      ? list.map(d => (d === existing ? { ...d, icon: nt.icon } : d))
      : [...list, { name, icon: nt.icon, protocol: nt.row.protocol === 'Telnet' ? 'Telnet' : 'SSH2', port: 0, username: '', credential: '', initialCr: false,
          loginSteps: null, afterLogin: null, prompt: '' }];
    try {
      await this.ui.patchSettings({ deviceTypes: types });
      nt.row.deviceType = existing?.name ?? name;
      nt.row.touched = true;
      this.rows.update(l => [...l]);
      this.newType.set(null);
    } catch (e) { this.ui.error(e); }
  }

  // ---- connecting from the list: every way the device answered ----
  actionsOf(r: Row): { id: string; label: string; short: string }[] {
    const has = (x: string) => r.services.includes(x);
    const out: { id: string; label: string; short: string }[] = [];
    if (has('ssh')) out.push({ id: 'ssh', label: 'SSH', short: 'SSH' }, { id: 'sftp', label: 'SFTP (files)', short: 'SFTP' });
    if (has('telnet')) out.push({ id: 'telnet', label: 'Telnet', short: 'Telnet' });
    if (has('rdp')) out.push({ id: 'rdp', label: 'Remote Desktop (RDP)', short: 'RDP' });
    if (has('vnc')) out.push({ id: 'vnc', label: 'VNC', short: 'VNC' });
    if (has('https')) out.push({ id: 'https', label: 'Web interface (HTTPS)', short: 'Web' });
    if (has('http')) out.push({ id: 'http', label: 'Web interface (HTTP)', short: 'Web' });
    // the recommended one first: the login the scan chose (SSH before Telnet; RDP for Windows), else the web interface
    const best = ({ SSH2: 'ssh', Telnet: 'telnet', RDP: 'rdp', VNC: 'vnc' } as Record<string, string>)[r.protocol] ?? '';
    const i = out.findIndex(a => a.id === best);
    if (i > 0) out.unshift(...out.splice(i, 1));
    return out;
  }
  connectMenu(ev: MouseEvent, r: Row) {
    ev.stopPropagation();
    const acts = this.actionsOf(r);
    this.ui.showMenu(ev, acts.map((a, i) => ({ label: a.label + (i === 0 ? ' - recommended' : ''), action: () => void this.run(r, a) })));
  }
  /** Opens the device the chosen way without saving a session (the window stays open). */
  async run(r: Row, a: { id: string }) {
    if (a.id === 'http' || a.id === 'https') { this.backend.openURL(`${a.id}://${r.ip}/`); return; }
    const proto: Record<string, [string, number]> = { ssh: ['SSH2', 22], sftp: ['SSH2', 22], telnet: ['Telnet', 23], rdp: ['RDP', 3389], vnc: ['VNC', 5900] };
    const [p, port] = proto[a.id];
    try {
      const s = await this.sessionFor(r);
      s.protocol = p;
      s.port = r.protocol === p && r.port ? r.port : port;
      if (a.id === 'sftp') s.openAs = 'sftp';
      await this.tabs.openAdHoc(s);
      this.ui.toast(`Connecting to ${r.name || r.ip} - the tab opens behind this window`, 'info');
    } catch (e) { this.ui.error(e); }
  }
  svcLabel(s: string) {
    return ({ ssh: 'SSH', telnet: 'Telnet', http: 'HTTP', https: 'HTTPS', smb: 'SMB', rdp: 'RDP', printer: 'Print', ipp: 'IPP', sip: 'SIP', 'sip-udp': 'SIP', mdns: 'mDNS', snmp: 'SNMP', esphome: 'ESPHome', mqtt: 'MQTT', modbus: 'Modbus', coap: 'CoAP', vnc: 'VNC' } as Record<string, string>)[s] ?? s;
  }

  private loadAck() { try { return localStorage.getItem(ACK_KEY) === '1'; } catch { return false; } }

  async start() {
    if (this.running() || !this.spec.trim() || (!this.acked && !this.ackNow)) return;
    if (!this.acked) { this.acked = true; try { localStorage.setItem(ACK_KEY, '1'); } catch { /* ignore */ } }
    this.err.set('');
    this.rows.set([]);
    this.stopped.set(false);
    this.again.set({ done: 0, total: 0 });
    this.unsub.forEach(u => u());
    this.unsub = [];
    try {
      this.usedCommunity = this.useSnmp && !!this.community.trim();
      const r = await this.backend.discoverStart(this.spec.trim(), this.useSnmp ? this.community.trim() : '');
      this.id = r.id;
      this.total.set(r.total);
      this.done.set(0);
      this.running.set(true);
      this.unsub.push(
        this.backend.on('discover:found:' + r.id, (d: DiscoverResult) => this.found(d)),
        this.backend.on('discover:progress:' + r.id, (p: { done: number }) => this.done.set(p.done)),
        this.backend.on('discover:again:' + r.id, (p: { done: number; total: number }) => this.again.set(p)),
        this.backend.on('discover:done:' + r.id, (stopped: boolean) => { this.running.set(false); this.stopped.set(!!stopped); }),
      );
    } catch (e) { this.err.set(errText(e)); }
  }

  stop() { if (this.id) void this.backend.discoverStop(this.id); }

  ngOnDestroy() { this.stop(); this.unsub.forEach(u => u()); }

  private found(d: DiscoverResult) {
    const k = this.known();
    // a saved session of this device - also when it moved to this address (same MAC)
    const exists = [d.ip, d.dns, d.name].some(x => !!x && k.has(x.toLowerCase())) || (!!d.mac && this.knownMacs().has(normMac(d.mac)));
    const prev = this.rows().find(r => r.ip === d.ip);
    // a device can be reported again at the end of the scan with its MAC address
    const row: Row = { ...d, services: d.services ?? [], exists, open: prev?.open, touched: prev?.touched,
      selected: prev?.touched ? prev.selected : !!d.protocol && !exists && !d.self,
      deviceType: prev?.touched ? prev.deviceType : this.guessType(d) };
    this.rows.update(l => [...l.filter(r => r.ip !== d.ip), row].sort((a, b) => ipNum(a.ip) - ipNum(b.ip)));
  }

  /** Picks a device type whose name matches what the device reports (VOSS, EXOS, Linux, …). */
  private guessType(d: DiscoverResult): string {
    const types = this.deviceTypes();
    if (!types.length || !d.protocol || d.protocol === 'RDP' || d.protocol === 'VNC') return '';
    const named = (words: string[]) => types.find(t => words.some(w => t.name.toLowerCase().includes(w)));
    // the network OS any evidence named wins over a generic "Linux" from OpenSSH
    if (d.platform) {
      const t = named(PLATFORM_WORDS[d.platform] ?? [d.platform.toLowerCase()]);
      if (t) return t.name;
    }
    const text = `${d.sysDescr} ${d.os} ${d.vendor} ${d.web?.cert ?? ''} ${d.telnet} ${d.web?.title ?? ''} ${(d.web?.hints ?? []).join(' ')} ${d.sip} ${d.banner}`.toLowerCase();
    const direct = types.find(t => t.name.length > 2 && text.includes(t.name.toLowerCase()));
    if (direct) return direct.name;
    const aliases: [RegExp, string[]][] = [
      [/voss|vsp|fabric ?engine/, ['voss', 'fabric']], [/exos|extremexos|switch ?engine|summit/, ['exos', 'switch engine']],
      [/nx-os/, ['nx-os', 'nxos', 'cisco']], [/cisco/, ['ios', 'cisco']], [/junos|juniper/, ['junos', 'juniper']],
      [/arista|\beos\b/, ['eos', 'arista']], [/linux|ubuntu|debian|centos|red ?hat|rocky|alma|amazon|openssh/, ['linux', 'server']],
      [/windows/, ['windows']],
    ];
    for (const [re, words] of aliases) {
      if (!re.test(text)) continue;
      const t = named(words);
      if (t) return t.name;
    }
    // The Telnet login prompt against each device type's first login step
    // (EXOS "login:", VOSS "Login:" …); for servers prefer a Linux-like type.
    const prompt = (d.telnet ?? '').trim().split(' / ').pop() ?? '';
    if (prompt) {
      const fits = types.filter(t => { const e = t.loginSteps?.[0]?.expect?.trim(); return !!e && prompt.endsWith(e); });
      const pick = d.kind === 'server' ? fits.find(t => /linux|unix|server/i.test(t.name)) : fits.find(t => !/linux|unix|server/i.test(t.name));
      if (pick ?? fits.length === 1) return (pick ?? fits[0]).name;
    }
    return '';
  }

  /** A session for a device, as Add saves it. */
  private async sessionFor(r: Row, folder = '') {
    const s = await this.backend.newSessionDefaults();
    Object.assign(s, {
      name: (r.name || r.ip).replace(/[\\/:*?"<>|]+/g, '-').trim() || r.ip, folder, path: '', protocol: r.protocol || 'WOL', hostname: r.ip, port: r.protocol ? r.port : 0, mac: r.mac || '',
      deviceType: r.deviceType, description: [r.vendor, r.os].filter(Boolean).length ? [[r.vendor, r.os].filter(Boolean).join(' · ')] : [],
    });
    return s;
  }

  /** Opens a tab to the device without saving a session; the dialog stays open. */
  async connect(r: Row, ev?: Event) {
    if ((ev?.target as HTMLElement | undefined)?.closest('select, button, input')) return;
    if (!r.protocol) return;
    try {
      await this.tabs.openAdHoc(await this.sessionFor(r));
      this.ui.toast(`Connecting to ${r.name || r.ip} - the tab opens behind this window`, 'info');
    } catch (e) { this.ui.error(e); }
  }

  rowMenu(ev: MouseEvent, r: Row) {
    this.ui.showMenu(ev, [
      ...(this.actionsOf(r).length ? this.actionsOf(r).map((a, i) => ({ label: 'Connect: ' + a.label + (i === 0 ? ' - recommended' : ''), action: () => void this.run(r, a) }))
        : [{ label: 'No SSH, Telnet, RDP, VNC or web service answered', disabled: true }]),
      { separator: true },
      { label: 'Copy IP Address', action: () => this.copy(r.ip, 'IP address') },
      { label: r.mac ? 'Copy MAC Address (' + r.mac + ')' : 'Copy MAC Address', disabled: !r.mac, action: () => this.copy(r.mac, 'MAC address') },
      { label: 'Copy Vendor / System Info', disabled: !this.sysText(r), action: () => this.copy(this.sysText(r), 'Vendor / system info') },
      { label: 'Copy All Details', action: () => this.copy(this.allText(r), 'Details') },
      { separator: true },
      { label: r.selected ? 'Do Not Add' : r.protocol ? 'Add as a Session' : 'Add as a Wake-on-LAN Session', disabled: !this.addable(r), action: () => { r.selected = !r.selected; r.touched = true; this.rows.update(l => [...l]); } },
    ]);
  }

  /** Vendor and system of a device with everything it is based on, as text. */
  sysText(r: Row): string {
    const head = [r.vendor, r.os].filter(Boolean).join(' · ');
    const ev = (r.evidence ?? []).map(e => '- ' + e);
    return [head, ...ev].filter(Boolean).join('\n');
  }

  allText(r: Row): string {
    return [
      `Name: ${r.name}${r.dns && r.dns !== r.name ? ' (' + r.dns + ')' : ''}`,
      `IP address: ${r.ip}`,
      r.mac ? `MAC address: ${r.mac}` : '',
      `Type: ${this.kindLabel(r.kind)}`,
      r.services?.length ? `Services: ${r.services.map(x => this.svcLabel(x)).join(', ')}` : '',
      this.sysText(r) ? 'Vendor / system: ' + this.sysText(r) : '',
    ].filter(Boolean).join('\n');
  }

  private copy(text: string, what: string) {
    void this.backend.clipboardSet(text).then(() => this.ui.toast(what + ' copied', 'info', 2500)).catch(e => this.ui.error(e));
  }

  toggle(r: Row, ev: Event) {
    // selecting text (to copy a MAC address or the system text) must not tick the row
    if (window.getSelection()?.toString()) return;
    if (!this.addable(r) || (ev.target as HTMLElement).tagName === 'SELECT') return;
    r.selected = !r.selected;
    r.touched = true;
    this.rows.update(l => [...l]);
  }

  selectAll(v: boolean) {
    const ips = new Set(this.shown().map(r => r.ip));
    this.rows.update(l => l.map(r => ips.has(r.ip) && r.protocol ? { ...r, selected: v } : r));
  }

  /** Push to Team… with the selected devices (as they would be added). */
  pushToTeam() {
    const sub = (r: { deviceType: string }) => this.byType() ? (r.deviceType || 'Other').replace(/[\\/:*?"<>|]+/g, '-') : '';
    const items = this.toAdd().filter(r => r.ip).map(r => ({
      path: '', name: (r.name || r.ip).replace(/[\\/:*?"<>|]+/g, '-').trim() || r.ip, folder: sub(r), host: r.ip,
      port: r.protocol ? r.port : 0, protocol: r.protocol || 'SSH2', deviceType: r.deviceType, mac: r.mac || '',
      description: [r.vendor, r.os].filter(Boolean).join(' · '),
    }));
    this.ui.teamPush.set({ items });
   
  }

  async add() {
    const pick = this.toAdd();
    if (!pick.length) return;
    this.adding.set(true);
    const folder = this.folder.trim().replace(/^\/+|\/+$/g, '');
    let added = 0;
    const failed: string[] = [];
    try {
      const made = new Set(this.folders());
      const ensure = async (f: string) => {
        if (f && !made.has(f)) { made.add(f); await this.backend.createFolder(f).catch(() => {}); }
      };
      await ensure(folder);
      for (const r of pick) {
        const sub = this.byType() ? (r.deviceType || 'Other').replace(/[\\/:*?"<>|]+/g, '-') : '';
        const dest = [folder, sub].filter(Boolean).join('/');
        await ensure(dest);
        const s = await this.backend.newSessionDefaults();
        const base = (r.name || r.ip).replace(/[\\/:*?"<>|]+/g, '-').trim() || r.ip;
        Object.assign(s, {
          name: base, folder: dest, path: '', protocol: r.protocol || 'WOL', hostname: r.ip, port: r.protocol ? r.port : 0, mac: r.mac || '',
          deviceType: r.deviceType, description: [r.vendor, r.os].filter(Boolean).length ? [[r.vendor, r.os].filter(Boolean).join(' · ')] : [],
          filesPanel: r.protocol === 'SSH2' && isUnixType(r.deviceType),
        });
        try {
          await this.backend.saveSession(s, '');
        } catch {
          try { s.name = `${base} (${r.ip})`; await this.backend.saveSession(s, ''); } catch (e) { failed.push(`${r.ip}: ${errText(e)}`); continue; }
        }
        added++;
        r.selected = false;
        r.exists = true;
      }
    } finally {
      this.adding.set(false);
      this.rows.update(l => [...l]);
      await this.ui.refreshTree();
    }
    if (failed.length) this.err.set('Not added: ' + failed.join('; '));
    this.ui.toast(`Added ${added} session${added === 1 ? '' : 's'}${folder ? ' to ' + folder : ''}`, 'success');
  }
}

/** Words in device type names for each network OS the scan recognises. */
const PLATFORM_WORDS: Record<string, string[]> = {
  EXOS: ['exos', 'switch engine'], VOSS: ['voss', 'fabric'], BOSS: ['boss'], 'NX-OS': ['nx-os', 'nxos'], 'IOS XR': ['ios xr', 'ios-xr', 'xr'],
  'IOS XE': ['ios xe', 'ios-xe', 'ios'], IOS: ['ios', 'cisco'], Junos: ['junos', 'juniper'], EOS: ['eos', 'arista'], FortiOS: ['forti'],
  'PAN-OS': ['pan-os', 'palo'], RouterOS: ['routeros', 'mikrotik'], Comware: ['comware', 'h3c'], ArubaOS: ['aruba', 'aos'],
};

const PREFS_KEY = 'termwright.discover.save';
function loadPrefs(): { mode?: string; types?: string[]; byType?: boolean } {
  try { return JSON.parse(localStorage.getItem(PREFS_KEY) ?? '{}') ?? {}; } catch { return {}; }
}

function ipNum(ip: string) { return ip.split('.').reduce((n, p) => n * 256 + Number(p), 0); }

function normMac(s: string | undefined) {
  const bare = (s ?? '').toLowerCase().replace(/[^0-9a-f]/g, '');
  return bare.length === 12 ? bare.match(/../g)!.join(':') : '';
}
