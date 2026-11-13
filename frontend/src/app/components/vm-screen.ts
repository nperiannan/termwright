import { Component, OnDestroy, OnInit, inject, input, output, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { Backend } from '../core/backend';
import { Ui } from '../core/ui.service';
import { tr } from '../core/i18n';

/**
 * The screen of a VM (a lab device or an installer): what its display shows, refreshed every 1.5 s, and its
 * keyboard - type a line, or a key (Enter, Esc, arrows, F-keys, Ctrl+Alt+Del). An installer can also take
 * the line on its serial console.
 */
@Component({
  selector: 'app-vm-screen',
  imports: [FormsModule],
  template: `
    <div class="ov" (click)="closed.emit()">
      <div class="dl" role="dialog" [attr.aria-label]="tr('Screen of {0}', name())" (click)="$event.stopPropagation()">
        <div class="dh"><b>{{ tr('Screen') }} · {{ name() }}</b><span class="muted small">@if (err()) { <span class="er">{{ err() }}</span> } @else if (at()) { {{ tr('updated {0}', at()) }} · {{ size() }} }</span><span class="sp"></span>
          <label class="small muted"><input type="checkbox" [(ngModel)]="live" /> {{ tr('Live') }}</label><a class="x" (click)="closed.emit()" [title]="tr('Close')">✕</a></div>
        <div class="scr">@if (png()) { <img [src]="png()" [alt]="tr('Screen of {0}', name())" /> } @else { <span class="muted">{{ err() || tr('Reading its screen…') }}</span> }</div>
        <div class="kb">
          <input class="mono" [(ngModel)]="line" [placeholder]="console ? tr('a line for its serial console') : tr('type here, Enter sends it with Enter')" (keydown.enter)="send(true)" spellcheck="false" />
          <button type="button" class="sm primary" (click)="send(true)" [disabled]="busy()">{{ tr('Type ⏎') }}</button>
          @if (kind() === 'install') { <label class="small muted" [title]="tr('Type on the installer’s serial console instead of its keyboard')"><input type="checkbox" [(ngModel)]="console" /> {{ tr('Serial console') }}</label> }
          <span class="sp"></span>
          @for (k of keys; track k.k) { <button type="button" class="sm k" (click)="key(k.k)" [disabled]="busy() || console" [title]="k.k">{{ k.n }}</button> }
        </div>
      </div>
    </div>`,
  styles: [`
    .ov { position: fixed; inset: 0; z-index: 100; background: rgba(0,0,0,.5); display: grid; place-items: center; }
    .dl { width: min(1060px, 95vw); height: min(780px, 92vh); background: var(--surface); border: 1px solid var(--line); border-radius: 12px; box-shadow: 0 18px 50px rgba(0,0,0,.45); display: grid; grid-template-rows: auto minmax(0, 1fr) auto; overflow: hidden; }
    .dh { display: flex; align-items: center; gap: 10px; padding: 10px 14px; border-bottom: 1px solid var(--line); .x { cursor: pointer; color: var(--muted); } label { display: flex; align-items: center; gap: 4px; } }
    .sp { flex: 1; } .muted { color: var(--muted); } .small { font-size: 11.5px; } .mono { font-family: var(--mono, monospace); } .er { color: var(--danger); }
    .scr { background: #000; display: grid; place-items: center; min-height: 0; overflow: hidden; img { max-width: 100%; max-height: 100%; object-fit: contain; image-rendering: pixelated; } }
    .kb { display: flex; align-items: center; gap: 6px; padding: 8px 12px; border-top: 1px solid var(--line); background: var(--surface-2); flex-wrap: wrap;
      input.mono { flex: 1 1 280px; min-width: 200px; } label { display: flex; align-items: center; gap: 4px; } .k { min-width: 34px; } }`],
})
export class VmScreen implements OnInit, OnDestroy {
  readonly kind = input<'vm' | 'install'>('vm');
  readonly id = input.required<string>();
  readonly name = input('');
  readonly closed = output<void>();
  private backend = inject(Backend);
  private ui = inject(Ui);
  protected tr = tr;
  protected png = signal('');
  protected at = signal('');
  protected size = signal('');
  protected err = signal('');
  protected busy = signal(false);
  protected live = true;
  protected console = false;
  protected line = '';
  private timer: ReturnType<typeof setTimeout> | null = null;
  protected readonly keys = [{ k: 'ret', n: '⏎' }, { k: 'esc', n: 'Esc' }, { k: 'tab', n: 'Tab' }, { k: 'up', n: '↑' }, { k: 'down', n: '↓' }, { k: 'left', n: '←' }, { k: 'right', n: '→' }, { k: 'f2', n: 'F2' }, { k: 'f12', n: 'F12' }, { k: 'ctrl-alt-delete', n: 'Ctrl+Alt+Del' }];

  ngOnInit() { void this.tick(); }
  ngOnDestroy() { if (this.timer) clearTimeout(this.timer); this.timer = null; this.live = false; }
  private async tick() {
    if (this.live || !this.png()) {
      try { const s = await this.backend.emuScreen(this.kind(), this.id()); this.png.set(s.png); this.size.set(s.w + '×' + s.h); this.at.set(new Date(s.at).toLocaleTimeString([], { hour12: false })); this.err.set(''); } catch (e) { this.err.set(String((e as Error)?.message ?? e)); }
    }
    this.timer = setTimeout(() => void this.tick(), 1500);
  }
  protected async send(enter: boolean) {
    if (!this.line && !enter) return;
    this.busy.set(true);
    try {
      if (this.console && this.kind() === 'install') await this.backend.emuInstallType(this.id(), this.line, enter);
      else await this.backend.emuScreenKeys(this.kind(), this.id(), this.line, enter, []);
      this.line = '';
    } catch (e) { this.ui.error(e); } finally { this.busy.set(false); }
  }
  protected async key(k: string) {
    this.busy.set(true);
    try { await this.backend.emuScreenKeys(this.kind(), this.id(), '', false, [k]); } catch (e) { this.ui.error(e); } finally { this.busy.set(false); }
  }
}
