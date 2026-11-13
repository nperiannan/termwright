import { Component, HostListener, computed, effect, inject, input, output, signal } from '@angular/core';
import { Backend } from '../core/backend';
import { EmuField, EmuPacketDetail } from '../core/models';
import { Ui } from '../core/ui.service';
import { tr } from '../core/i18n';

/** A field as a row of the tree (depth, open state, its bytes). */
interface Row { f: EmuField; depth: number; key: string; kids: boolean; }

/** The fields of a frame as text (one per line, indented), for the clipboard. */
export function packetText(p: EmuPacketDetail): string {
  const out = [`#${p.no} · ${p.at} · ${p.len} bytes · ${p.proto} · ${p.src} → ${p.dst} · ${p.info}`];
  const walk = (fs: EmuField[] | null | undefined, ind: string) => { for (const f of fs ?? []) { out.push(ind + f.name + (f.value ? ': ' + f.value : '')); walk(f.kids, ind + '  '); } };
  walk(p.fields, '');
  return out.join('\n');
}

/** The bytes as a hex dump: offset, 16 bytes, their ASCII. */
export function hexDump(hex: string): string {
  const b = hexBytes(hex), out: string[] = [];
  for (let i = 0; i < b.length; i += 16) {
    const row = b.slice(i, i + 16);
    const h = row.map((x, j) => x.toString(16).padStart(2, '0') + (j === 7 ? ' ' : '')).join(' ');
    out.push(i.toString(16).padStart(4, '0') + '  ' + h.padEnd(49) + '  ' + row.map(x => x >= 32 && x < 127 ? String.fromCharCode(x) : '.').join(''));
  }
  return out.join('\n');
}
function hexBytes(hex: string) { const b: number[] = []; for (let i = 0; i + 1 < hex.length; i += 2) b.push(parseInt(hex.slice(i, i + 2), 16)); return b; }

/** One packet: its fields as a tree, and its bytes in hex; a field marks its bytes. */
@Component({
  selector: 'app-packet-view',
  standalone: true,
  template: `
    @if (pkt(); as p) {
      <div class="pv">
        <div class="tree" role="tree" [attr.aria-label]="tr('Fields')">
          @for (r of rows(); track r.key) {
            <div class="fr" role="treeitem" [class.on]="mark()?.key === r.key" [style.padding-left.px]="6 + r.depth * 14" (click)="pick(r)">
              @if (r.kids) { <span class="tw" (click)="$event.stopPropagation(); toggle(r.key)">{{ shut().has(r.key) ? '▸' : '▾' }}</span> } @else { <span class="tw"></span> }
              <b [class.lay]="r.depth === 0">{{ r.f.name }}</b>@if (r.f.value) { <span class="v">{{ r.f.value }}</span> }
            </div>
          }
        </div>
        <div class="hex" [attr.aria-label]="tr('Bytes')">
          @for (l of lines(); track l.off) {
            <div class="hl"><span class="o">{{ l.offText }}</span><span class="hb">@for (c of l.cells; track c.i) {<span [class.m]="c.m" [class.g]="c.i % 16 === 8" (click)="pickByte(c.i)">{{ c.h }}</span>}</span><span class="as">@for (c of l.cells; track c.i) {<span [class.m]="c.m">{{ c.a }}</span>}</span></div>
          }
        </div>
      </div>
    } @else { <div class="muted pe">{{ tr('Click a packet to see its fields and bytes.') }}</div> }
  `,
  styles: [`
    :host { display: block; min-height: 0; }
    .pv { display: grid; grid-template-columns: minmax(0, 1fr) minmax(0, 1.15fr); gap: 0; height: 100%; min-height: 0; }
    .tree, .hex { overflow: auto; min-height: 0; padding: 6px 4px; }
    .tree { border-right: 1px solid var(--line); font-size: 12px; }
    .fr { display: flex; gap: 6px; align-items: baseline; padding: 1px 6px; border-radius: 4px; cursor: pointer; white-space: nowrap; }
    .fr:hover { background: var(--surface-2); } .fr.on { background: color-mix(in srgb, var(--accent) 22%, transparent); }
    .tw { width: 10px; color: var(--muted); flex: none; }
    b { font-weight: 500; color: color-mix(in srgb, var(--accent) 70%, var(--text)); } b.lay { color: var(--text); font-weight: 600; }
    .v { color: var(--text); overflow: hidden; text-overflow: ellipsis; }
    .hex { font-family: var(--mono, ui-monospace, Menlo, monospace); font-size: 12px; line-height: 1.6; }
    .hl { display: flex; gap: 14px; white-space: pre; }
    .o { color: var(--muted); }
    .hb span { padding: 0 3px; cursor: pointer; border-radius: 2px; } .hb span.g { margin-left: 8px; }
    .as span { cursor: default; }
    .m { background: color-mix(in srgb, var(--accent) 45%, transparent); color: #fff; }
    .pe { padding: 14px; font-size: 12px; }
  `],
})
export class PacketView {
  readonly pkt = input<EmuPacketDetail | null>(null);
  protected tr = tr;
  protected shut = signal(new Set<string>());
  protected mark = signal<Row | null>(null);
  constructor() { effect(() => { this.pkt(); this.mark.set(null); }); }
  protected rows = computed(() => {
    const out: Row[] = [], shut = this.shut();
    const walk = (fs: EmuField[] | null | undefined, depth: number, pre: string) => (fs ?? []).forEach((f, i) => {
      const key = pre + i; out.push({ f, depth, key, kids: !!f.kids?.length });
      if (!shut.has(key)) walk(f.kids, depth + 1, key + '.');
    });
    walk(this.pkt()?.fields, 0, '');
    return out;
  });
  protected lines = computed(() => {
    const b = hexBytes(this.pkt()?.hex ?? ''), m = this.mark(), out: { off: number; offText: string; cells: { i: number; h: string; a: string; m: boolean }[] }[] = [];
    for (let i = 0; i < b.length; i += 16) out.push({ off: i, offText: i.toString(16).padStart(4, '0'), cells: b.slice(i, i + 16).map((x, j) => ({ i: i + j, h: x.toString(16).padStart(2, '0'), a: x >= 32 && x < 127 ? String.fromCharCode(x) : '.', m: !!m && i + j >= m.f.off && i + j < m.f.off + m.f.n })) });
    return out;
  });
  toggle(k: string) { this.shut.update(s => { const n = new Set(s); if (n.has(k)) n.delete(k); else n.add(k); return n; }); }
  pick(r: Row) { this.mark.set(this.mark()?.key === r.key ? null : r); }
  /** A byte clicked: the smallest field that holds it. */
  pickByte(i: number) { let best: Row | null = null; for (const r of this.rows()) if (i >= r.f.off && i < r.f.off + r.f.n && (!best || r.f.n <= best.f.n)) best = r; this.mark.set(best); }
}

/** A packet in a window of its own: ◀ ▶ through the capture, copy it as text or hex, save it as a pcap file. */
@Component({
  selector: 'app-packet-window',
  standalone: true,
  imports: [PacketView],
  template: `
    <div class="back" (click)="closed.emit()">
      <div class="win" role="dialog" [attr.aria-label]="tr('Packet')" (click)="$event.stopPropagation()">
        <div class="hd">
          <b>#{{ no() }}@if (pkt(); as p) { · {{ p.proto }} · {{ p.info }} }</b><span class="sp"></span>
          <button type="button" class="sm" (click)="go(-1)" [disabled]="no() <= 1" [title]="tr('Previous packet')">◀</button>
          <button type="button" class="sm" (click)="go(1)" [disabled]="!!pkt() && no() >= pkt()!.total" [title]="tr('Next packet')">▶</button>
          <button type="button" class="sm x" (click)="closed.emit()" [attr.aria-label]="tr('Close')">✕</button>
        </div>
        @if (pkt(); as p) {
          <div class="meta"><span><i>{{ tr('Time') }}</i> {{ p.time.toFixed(6) }} s ({{ p.at }})</span><span><i>{{ tr('Length') }}</i> {{ p.len }} {{ tr('bytes') }}</span><span><i>{{ tr('From') }}</i> {{ p.src }}</span><span><i>{{ tr('To') }}</i> {{ p.dst }}</span><span><i>{{ tr('Capture') }}</i> {{ title() }}</span></div>
        }
        <div class="tb"><button type="button" class="sm" (click)="copy('text')" [disabled]="!pkt()">{{ tr('Copy as Text') }}</button><button type="button" class="sm" (click)="copy('hex')" [disabled]="!pkt()">{{ tr('Copy as Hex') }}</button><button type="button" class="sm" (click)="save()" [disabled]="!pkt()">{{ tr('Save as .pcap…') }}</button>
          @if (err()) { <span class="err">{{ err() }}</span> }</div>
        <app-packet-view class="body" [pkt]="pkt()" />
      </div>
    </div>
  `,
  styles: [`
    .back { position: fixed; inset: 0; background: rgba(0, 0, 0, .45); z-index: 900; display: flex; align-items: center; justify-content: center; }
    .win { width: min(1100px, calc(100vw - 32px)); height: min(720px, calc(100vh - 48px)); background: var(--bg); border: 1px solid var(--line); border-radius: 12px; box-shadow: 0 20px 60px rgba(0, 0, 0, .5); display: grid; grid-template-rows: auto auto auto minmax(0, 1fr); overflow: hidden; }
    .hd { display: flex; gap: 6px; align-items: center; padding: 10px 12px; border-bottom: 1px solid var(--line); b { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; } }
    .sp { flex: 1; } .x { margin-left: 6px; }
    .meta { display: flex; flex-wrap: wrap; gap: 4px 18px; padding: 8px 12px 0; font-size: 12px; i { font-style: normal; color: var(--muted); margin-right: 4px; } }
    .tb { display: flex; gap: 6px; align-items: center; padding: 8px 12px; border-bottom: 1px solid var(--line); }
    .err { color: var(--danger); font-size: 12px; }
    .body { min-height: 0; }
  `],
})
export class PacketWindow {
  /** The capture (its key or file) and where to start. */
  readonly cap = input.required<string>();
  readonly start = input.required<number>();
  readonly title = input('');
  readonly closed = output<void>();
  private backend = inject(Backend);
  private ui = inject(Ui);
  protected tr = tr;
  protected no = signal(1);
  protected pkt = signal<EmuPacketDetail | null>(null);
  protected err = signal('');
  constructor() { effect(() => { this.no.set(this.start()); }); effect(() => { void this.load(this.cap(), this.no()); }); }
  private async load(cap: string, no: number) {
    try { this.pkt.set(await this.backend.emuCapturePacket(cap, no)); this.err.set(''); } catch (e) { this.err.set(String((e as Error)?.message ?? e)); }
  }
  go(d: number) { const t = this.pkt()?.total ?? Infinity; this.no.set(Math.min(t, Math.max(1, this.no() + d))); }
  async copy(how: 'text' | 'hex') {
    const p = this.pkt(); if (!p) return;
    try { await navigator.clipboard.writeText(how === 'hex' ? hexDump(p.hex) : packetText(p) + '\n\n' + hexDump(p.hex)); this.ui.toast(tr('Copied'), 'success'); } catch (e) { this.ui.error(e); }
  }
  async save() {
    try {
      const f = await this.backend.chooseSaveFile(tr('Save the Packet'), `packet-${this.no()}.pcap`, [{ displayName: 'pcap', pattern: '*.pcap' }]); if (!f) return;
      await this.backend.emuCaptureSavePackets(this.cap(), [this.no()], f); this.ui.toast(tr('Saved'), 'success');
    } catch (e) { this.ui.error(e); }
  }
  @HostListener('document:keydown', ['$event']) key(e: KeyboardEvent) {
    if (e.key === 'Escape') this.closed.emit(); else if (e.key === 'ArrowLeft') this.go(-1); else if (e.key === 'ArrowRight') this.go(1);
  }
}
