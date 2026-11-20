import { Component, OnDestroy, OnInit, computed, effect, inject, input, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { Backend, errText } from '../core/backend';
import { Ui } from '../core/ui.service';
import { tr } from '../core/i18n';
import { Recorder } from '../core/recorder.service';
import { TestCase, TestCaseInfo, TestResult, TestRunReport, TestsGitInfo, TestsTree } from '../core/models';
import { expectLabel, stepText } from './recorder-panel';

type View = 'steps' | 'yaml' | 'go';

/** The Tests tab: the tests folder's tree, the selected test (steps, YAML, generated Go) and the last run. */
@Component({
  selector: 'app-tests-view',
  imports: [FormsModule],
  template: `
    <div class="tv">
      <aside class="side">
        <div class="row"><button type="button" (click)="openFolder()" title="Use another folder as the tests folder">Open…</button><button type="button" (click)="clone()" title="Clone a Git repository of tests">Clone…</button>
          <span class="sp"></span><button type="button" (click)="run([])" [disabled]="busy() || !tree()?.cases?.length" title="Run all tests">▶▶</button></div>
        <div class="git" [title]="tree()?.root ?? ''">@if (git()?.repo) { <span>⎇ {{ git()?.branch }}</span><span>{{ git()?.changed }} changed</span>
            @if (git()?.ahead) { <span title="Commits not pushed yet">↑{{ git()?.ahead }}</span> } @if (git()?.behind) { <span title="Commits on the remote not pulled yet">↓{{ git()?.behind }}</span> } } @else { <span>{{ folderName() }}</span> }
          <span class="sp"></span><button type="button" class="lnk" (click)="backend.testsOpenInEditor('')" title="Open the folder in VS Code (or the file manager)">⧉</button></div>
        @if (git()?.repo) {
          <div class="row gb">
            <button type="button" (click)="gitDo('pull')" [disabled]="busy() || !git()?.upstream" title="Get the remote's commits (fast-forward only)">Pull</button>
            <button type="button" (click)="gitDo('commit')" [disabled]="busy() || !git()?.changed" title="Commit the tests, testbeds and library (not the reports)">Commit…</button>
            <button type="button" (click)="gitDo('push')" [disabled]="busy() || (!!git()?.upstream && !git()?.ahead)" title="Send your commits to the remote">Push</button>
          </div>
        } @else if (git()?.hasGit) {
          <button type="button" class="lnk sm" (click)="gitDo('init')" title="Keep the tests' history with Git (reports are left out)">Make it a Git repository</button>
        }
        <div class="tree">
          <div class="h">CASES</div>
          @for (g of groups(); track g.dir) {
            <div class="grp">{{ g.dir || 'cases' }}</div>
            @for (c of g.cases; track c.file) {
              <button type="button" class="it" [class.sel]="c.file === sel()" (click)="select(c.file)" [title]="c.error || c.description || c.file">
                <span class="dot" [class]="dot(c)">●</span>{{ c.name }}<span class="n">{{ c.steps }}</span></button>
            }
          } @empty { <div class="mut">No tests yet. Tests → Record a Test, or ask the AI assistant to write one.</div> }
          <div class="h hb">TESTBEDS<button type="button" class="lnk" (click)="newTestbed()" title="New testbed from a session folder: a role for each session">+</button></div>
          @for (b of tree()?.testbeds ?? []; track b) { <button type="button" class="it" (click)="backend.testsOpen(b)">{{ b.slice(9) }}</button> } @empty { <div class="mut">none - add files in testbeds/</div> }
          <div class="h">REPORTS</div>
          @for (r of (tree()?.reports ?? []).slice(0, 12); track r.dir) {
            <button type="button" class="it" (click)="backend.testsOpen(r.dir)" title="Open the HTML report">{{ when(r.dir) }} <span [class]="r.failed ? 'bad' : 'ok'">{{ r.passed }}/{{ r.passed + r.failed }}</span></button>
          }
        </div>
      </aside>
      <section class="main">
        @if (sel(); as file) {
          <header>
            <b class="ttl">{{ test()?.name || file }}</b><span class="mut fl">{{ file }}</span><span class="sp"></span>
            <div class="seg">
              @for (v of views; track v.k) { <button type="button" [class.on]="view() === v.k" (click)="setView(v.k)">{{ v.l }}</button> }
            </div>
            <select [(ngModel)]="testbed" title="Testbed: its values fill the {{ '{{variables}}' }}"><option value="">No testbed</option>@for (b of tree()?.testbeds ?? []; track b) { <option [value]="b">{{ b.slice(9) }}</option> }</select>
            <button type="button" class="pri" (click)="run([file])" [disabled]="busy()">{{ busy() ? 'Running…' : '▶ Run' }}</button>
          </header>
          <div class="body">
            @switch (view()) {
              @case ('steps') {
                @if (test()?.description) { <p class="mut">{{ test()?.description }}</p> }
                @if (test()?.params?.length) { <p class="mut">Runs {{ test()?.params?.length }} times: {{ paramsText() }}</p> }
                @if (varsText()) { <p class="mut">Defaults: {{ varsText() }} - a testbed's values win.</p> }
                @for (s of test()?.steps ?? []; track $index; let i = $index) {
                  <div class="st" [class.f]="stepRes(i)?.passed === false && !stepRes(i)?.skipped">
                    <div><b>{{ i + 1 }}</b> <span class="mut">{{ s.run?.session ?? s.http?.session ?? s.snmp?.session ?? '' }} ›</span> <span class="cmd">{{ text(s) }}</span>
                      @if (stepRes(i); as r) { <span class="ms">{{ r.skipped ? 'skipped' : r.ms + ' ms' }}</span> }</div>
                    @for (e of s.expect ?? []; track $index; let j = $index) {
                      <span class="chip" [class.b]="stepRes(i)?.checks?.[j]?.passed === false">{{ stepRes(i)?.checks?.[j]?.passed === false ? '✗' : '✓' }} {{ label(e) }}@if (stepRes(i)?.checks?.[j]?.passed === false) { · got {{ stepRes(i)?.checks?.[j]?.got }} }</span>
                    }
                    @for (v of s.save ?? []; track $index) { <span class="chip v">{{ '{{' + v.var + '}}' }}</span> }
                    @if (stepRes(i)?.error) { <div class="bad">{{ stepRes(i)?.error }}</div> }
                  </div>
                }
                <div class="row"><button type="button" (click)="recordMore()">● Record a New Test</button><span class="mut">To change steps, edit the YAML.</span></div>
              }
              @case ('yaml') {
                <textarea class="code" [(ngModel)]="yaml" spellcheck="false" aria-label="Test YAML"></textarea>
                <div class="row"><span class="mut">Saving checks the test and writes its Go test again.</span><span class="sp"></span><button type="button" (click)="setView('yaml')">Revert</button><button type="button" class="pri" (click)="saveYaml()">Save</button></div>
              }
              @case ('go') {
                <pre class="code ro">{{ goSrc() }}</pre>
                <div class="row"><span class="mut">Generated from the YAML - edit the YAML, not this file.@if (git()?.hasGo) { Run outside Termwright: go test ./cases/... in the tests folder. }</span><span class="sp"></span><button type="button" (click)="backend.testsOpenInEditor(goFile())">Open in VS Code</button></div>
              }
            }
          </div>
        } @else { <div class="empty mut">Select a test, or record one: Tests → Record a Test.</div> }
        @if (report(); as rep) {
          <footer>
            <div class="row"><b>Last run</b><span class="ok">{{ rep.passed }} passed</span>@if (rep.failed) { <span class="bad">{{ rep.failed }} failed</span> }<span class="mut">{{ (rep.ms / 1000).toFixed(1) }} s</span><span class="sp"></span>
              <button type="button" (click)="backend.testsOpen(rep.dir)">Open Report</button><button type="button" (click)="backend.testsOpen(rep.dir + '/junit.xml')">JUnit XML</button></div>
            <div class="res">
              @for (r of rep.results; track $index) {
                <button type="button" class="rr" (click)="r.file && select(r.file)"><span [class]="r.passed ? 'ok' : 'bad'">{{ r.passed ? '✓' : '✗' }}</span>{{ r.file || r.case }}{{ paramOf(r.case) }}<span class="mut">{{ r.passed ? '' : failText(r) }}</span><span class="sp"></span><span class="mut">{{ (r.ms / 1000).toFixed(1) }} s</span></button>
              }
            </div>
          </footer>
        }
      </section>
    </div>`,
  styles: [`
    :host { display: block; height: 100%; min-height: 0; } .tv { display: flex; height: 100%; min-height: 0; }
    .side { width: 250px; flex: none; display: flex; flex-direction: column; gap: 8px; padding: 10px; border-right: 1px solid var(--border); background: var(--panel); min-height: 0; }
    .row { display: flex; align-items: center; gap: 6px; } .sp { flex: 1; } .mut { color: var(--muted); font-size: 12px; } .ok { color: var(--ok, #4ec07a); } .bad { color: var(--danger, #ef6161); }
    .git { display: flex; gap: 8px; font-size: 12px; color: var(--muted); align-items: center; } .lnk { border: 0; background: none; color: var(--accent); cursor: pointer; padding: 0 4px; }
    .gb button { flex: 1; padding: 3px 6px; } .sm { font-size: 12px; text-align: left; } .tree { flex: 1; overflow: auto; display: flex; flex-direction: column; gap: 1px; } .hb { display: flex; justify-content: space-between; align-items: center; } .h { font-size: 11px; letter-spacing: .06em; color: var(--muted); margin: 8px 0 3px; } .grp { font-size: 12px; color: var(--muted); padding: 2px 4px; }
    .it { display: flex; gap: 6px; align-items: center; text-align: left; border: 0; background: none; color: var(--text); padding: 4px 8px; border-radius: 6px; cursor: pointer; font: inherit; }
    .it:hover { background: var(--hover, rgba(255,255,255,.05)); } .it.sel { background: color-mix(in srgb, var(--accent) 20%, transparent); } .it .n { margin-left: auto; color: var(--muted); font-size: 11px; }
    .dot { font-size: 9px; color: var(--muted); } .dot.p { color: var(--ok, #4ec07a); } .dot.f { color: var(--danger, #ef6161); } .dot.e { color: var(--warn, #e0a030); }
    .main { flex: 1; min-width: 0; display: flex; flex-direction: column; }
    header { display: flex; gap: 8px; align-items: center; padding: 10px 12px; border-bottom: 1px solid var(--border); } .ttl { font-size: 15px; } .fl { white-space: nowrap; overflow: hidden; text-overflow: ellipsis; min-width: 0; }
    header select { height: 28px; max-width: 160px; }
    .seg { display: flex; gap: 2px; background: color-mix(in srgb, var(--text) 6%, transparent); border-radius: 7px; padding: 2px; } .seg button { border: 0; background: none; padding: 3px 10px; border-radius: 5px; color: var(--text); cursor: pointer; } .seg button.on { background: color-mix(in srgb, var(--accent) 28%, transparent); }
    .body p { margin: 0; } .body { flex: 1; overflow: auto; padding: 12px; display: flex; flex-direction: column; gap: 8px; min-height: 0; }
    .st { border: 1px solid var(--border); border-radius: 8px; padding: 7px 9px; } .st.f { border-color: color-mix(in srgb, var(--danger, #ef6161) 60%, transparent); } .cmd { font-family: var(--mono, monospace); margin-left: 4px; } .st > div > b { margin-right: 8px; } .ms { float: right; color: var(--muted); font-size: 11px; }
    .chip { display: inline-block; font-size: 11.5px; padding: 1px 8px; border-radius: 10px; margin: 4px 4px 0 0; background: color-mix(in srgb, var(--ok, #4ec07a) 15%, transparent); color: var(--ok, #4ec07a); }
    .chip.b { background: color-mix(in srgb, var(--danger, #ef6161) 15%, transparent); color: var(--danger, #ef6161); } .chip.v { background: color-mix(in srgb, var(--accent) 15%, transparent); color: var(--accent); }
    .code { flex: 1; min-height: 240px; font: 12.5px/1.5 var(--mono, monospace); resize: none; white-space: pre; overflow: auto; margin: 0; padding: 10px; border-radius: 8px; border: 1px solid var(--border); background: var(--term-bg, rgba(0,0,0,.25)); color: var(--text); }
    .pri { background: var(--accent); color: #fff; border-color: transparent; } .empty { margin: auto; }
    footer { border-top: 1px solid var(--border); padding: 8px 12px; max-height: 32%; display: flex; flex-direction: column; gap: 4px; min-height: 0; } .res { overflow: auto; }
    .rr { display: flex; gap: 8px; width: 100%; border: 0; background: none; color: var(--text); padding: 3px 4px; text-align: left; cursor: pointer; font: inherit; } .rr:hover { background: var(--hover, rgba(255,255,255,.05)); }
  `],
})
export class TestsView implements OnInit, OnDestroy {
  readonly visible = input(true);
  readonly backend = inject(Backend);
  private readonly ui = inject(Ui);
  private readonly rec = inject(Recorder);
  readonly tree = signal<TestsTree | null>(null);
  readonly git = signal<TestsGitInfo | null>(null);
  readonly sel = signal('');
  readonly test = signal<TestCase | null>(null);
  readonly view = signal<View>('steps');
  readonly goSrc = signal('');
  readonly report = signal<TestRunReport | null>(null);
  readonly busy = signal(false);
  readonly views: { k: View; l: string }[] = [{ k: 'steps', l: 'Steps' }, { k: 'yaml', l: 'YAML' }, { k: 'go', l: 'Go' }];
  yaml = '';
  testbed = '';
  private sig = '';
  private timer?: ReturnType<typeof setInterval>;
  private offs: (() => void)[] = [];
  /** The last result of each test file, for the tree's dots. */
  private readonly last = signal<Record<string, TestResult>>({});

  readonly groups = computed(() => {
    const m = new Map<string, TestCaseInfo[]>();
    for (const c of this.tree()?.cases ?? []) {
      const d = c.file.replace(/^cases\/?/, '').split('/').slice(0, -1).join('/');
      m.set(d, [...(m.get(d) ?? []), c]);
    }
    return [...m.entries()].map(([dir, cases]) => ({ dir, cases }));
  });
  readonly folderName = computed(() => (this.tree()?.root ?? '').split(/[\\/]/).pop() ?? '');
  readonly goFile = computed(() => this.sel().replace(/\.ya?ml$/, '') + '_test.go');

  constructor() {
    effect(() => { if (this.visible()) void this.poll(); });
  }

  ngOnInit() {
    void this.load();
    void this.backend.testLastRun().then(r => { if (r) this.took(r); }).catch(() => {});
    this.offs.push(this.backend.on('tests:progress', (r: TestResult) => { if (r.file) this.last.update(m => ({ ...m, [r.file!]: r })); }));
    this.timer = setInterval(() => { if (this.visible()) void this.poll(); }, 4000);
  }
  ngOnDestroy() { clearInterval(this.timer); this.offs.forEach(f => f()); }

  private async load() {
    try {
      this.tree.set(await this.backend.testsTree());
      this.git.set(await this.backend.testsGit());
      this.sig = await this.backend.testsSignature();
      const files = this.tree()?.cases.map(c => c.file) ?? [];
      if (this.sel() && files.includes(this.sel())) await this.select(this.sel(), true); else if (!this.sel() && files.length) await this.select(files[0]);
    } catch (e) { this.ui.toast(errText(e), 'error'); }
  }

  /** Files changed outside (an editor, git pull): read again. */
  private async poll() {
    const s = await this.backend.testsSignature().catch(() => this.sig);
    if (s !== this.sig) await this.load();
  }

  async select(file: string, keepView = false) {
    this.sel.set(file);
    try { this.test.set(await this.backend.testCase(file)); } catch (e) { this.test.set(null); this.ui.toast(errText(e), 'error'); }
    if (keepView && this.view() === 'yaml') return; // unsaved edits stay
    await this.setView(keepView ? this.view() : 'steps');
  }

  async setView(v: View) {
    this.view.set(v);
    const f = this.sel();
    if (v === 'yaml') this.yaml = await this.backend.testFile(f).catch(e => '# ' + errText(e));
    if (v === 'go') this.goSrc.set(await this.backend.testFile(this.goFile()).catch(() => tr('No Go test yet: save the test (YAML → Save) to write it.')));
  }

  async saveYaml() {
    try {
      await this.backend.testSaveYAML(this.sel(), this.yaml);
      this.ui.toast(tr('Saved'), 'success');
      await this.load();
    } catch (e) { this.ui.toast(errText(e), 'error'); }
  }

  async run(files: string[], confirmed = false): Promise<void> {
    this.busy.set(true);
    try {
      this.took(await this.backend.runTests({ files, testbed: this.testbed || undefined, confirmed }));
      await this.load();
    } catch (e) {
      const t = errText(e);
      if (!confirmed && /production sessions/i.test(t)) {
        this.busy.set(false);
        if (await this.ui.confirm(tr('Run on Production?'), t + '. ' + tr('Their commands and requests are sent to those devices.'), tr('Run'), true)) return this.run(files, true);
        return;
      }
      this.ui.toast(t, 'error');
    }
    this.busy.set(false);
  }

  private took(r: TestRunReport) {
    this.report.set(r);
    this.last.update(m => { const n = { ...m }; for (const x of r.results ?? []) if (x.file) n[x.file] = x; return n; });
  }

  async openFolder() { try { if (await this.backend.testsOpenFolder()) { this.sel.set(''); await this.load(); } } catch (e) { this.ui.toast(errText(e), 'error'); } }
  async clone() {
    const u = await this.ui.input(tr('Clone Tests'), tr('Git repository (uses this computer\'s git and its saved login)'), '');
    if (!u) return;
    this.busy.set(true);
    try { if (await this.backend.testsClone(u)) { this.sel.set(''); await this.load(); } } catch (e) { this.ui.toast(errText(e), 'error'); }
    this.busy.set(false);
  }
  recordMore() { this.rec.start(); }
  async gitDo(what: 'pull' | 'commit' | 'push' | 'init') {
    let msg = '';
    if (what === 'commit') {
      msg = await this.ui.input(tr('Commit Tests'), tr('What changed?'), '') ?? '';
      if (!msg.trim()) return;
    }
    this.busy.set(true);
    try {
      const out = what === 'pull' ? await this.backend.testsGitPull() : what === 'push' ? await this.backend.testsGitPush()
        : what === 'init' ? await this.backend.testsGitInit() : await this.backend.testsGitCommit(msg);
      this.ui.toast((out || tr('Done')).split('\n').slice(-3).join('\n'), 'success');
      await this.load();
    } catch (e) { this.ui.toast(errText(e), 'error'); }
    this.busy.set(false);
  }
  async newTestbed() {
    const folder = await this.ui.input(tr('New Testbed'), tr('Session folder whose sessions become roles (e.g. Lab)'), '');
    if (!folder) return;
    const name = await this.ui.input(tr('New Testbed'), tr('Testbed name'), folder.split('/').pop() ?? '');
    if (!name) return;
    try { const rel = await this.backend.testsNewTestbed(name, folder); this.ui.toast(tr('Wrote {0}', rel), 'success'); await this.load(); void this.backend.testsOpen(rel); }
    catch (e) { this.ui.toast(errText(e), 'error'); }
  }
  paramOf(name: string) { const i = name.lastIndexOf(' ['); return i > 0 && name.endsWith(']') ? name.slice(i) : ''; }
  paramsText() { return (this.test()?.params ?? []).map(p => Object.entries(p).map(([k, v]) => `${k}=${v}`).join(', ')).join(' · '); }
  varsText() { return Object.entries(this.test()?.vars ?? {}).map(([k, v]) => `{{${k}}} = ${v}`).join(', '); }

  dot(c: TestCaseInfo) { if (c.error) return 'dot e'; const r = this.last()[c.file]; return r ? (r.passed ? 'dot p' : 'dot f') : 'dot'; }
  stepRes(i: number) { return this.last()[this.sel()]?.steps?.[i]; }
  label = expectLabel;
  text = stepText;
  when(dir: string) { return dir.replace(/^reports\//, '').replace(/^(\d{4}-\d\d-\d\d)_(\d\d)(\d\d)(\d\d).*/, '$1 $2:$3'); }
  failText(r: TestResult) {
    if (r.error) return r.error;
    const i = r.steps.findIndex(s => !s.passed && !s.skipped), s = r.steps[i];
    if (!s) return '';
    const c = s.checks?.find(x => !x.passed);
    return `step ${i + 1}: ` + (s.error || (c ? `${c.what}, got "${c.got ?? ''}"` : 'failed'));
  }
}
