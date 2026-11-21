import { Component, OnDestroy, OnInit, computed, effect, inject, input, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { Backend, errText } from '../core/backend';
import { Ui } from '../core/ui.service';
import { tr } from '../core/i18n';
import { Recorder } from '../core/recorder.service';
import { CleanupMode, OnCleanupFail, TestCase, TestCaseInfo, TestExpect, TestResult, TestRunReport, TestSchedule, TestScheduleView, TestStep, TestsGitInfo, TestsTree } from '../core/models';
import { expectLabel, parseTableCheck, stepText } from './recorder-panel';
import { AIChat } from '../core/ai-chat.service';

type View = 'steps' | 'yaml' | 'go' | 'py' | 'tcl';
type Phase = 'setup' | 'steps' | 'cleanup';

/** The Tests tab: the tests folder's tree, the selected test (steps, YAML, generated Go) and the last run. */
@Component({
  selector: 'app-tests-view',
  imports: [FormsModule],
  template: `
    <div class="tv">
      <aside class="side">
        <div class="row"><button type="button" (click)="openFolder()" title="Use another folder as the tests folder">Open…</button><button type="button" (click)="clone()" title="Clone a Git repository of tests">Clone…</button>
          <span class="sp"></span><button type="button" (click)="run([])" [disabled]="busy() || !tree()?.cases?.length" title="Run all tests">▶▶</button></div>
        <label class="ocf" title="Whether each test cleans up after itself. Off: the next test starts from what this one configured"><span>After each test</span>
          <select [(ngModel)]="cleanupMode" (ngModelChange)="keepOcf()"><option value="off">No cleanup - tests build on each other</option><option value="">Run its cleanup steps</option><option value="auto">Cleanup steps, else undo its configuration</option></select></label>
        @if (cleanupMode !== 'off') {
        <label class="ocf" title="What happens when a test's cleanup fails: the devices may not be clean for the next test"><span>On a failed cleanup</span>
          <select [(ngModel)]="onCleanupFail" (ngModelChange)="keepOcf()"><option value="stop">Stop the run</option><option value="restore">Restore the saved configuration</option><option value="factory">Factory default</option></select></label>
        }
        @if (picked().length) { <div class="row"><button type="button" class="pri" (click)="run(picked())" [disabled]="busy()">▶ Run Selected ({{ picked().length }})</button><button type="button" class="lnk" (click)="picked.set([])">Clear</button></div> }
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
        <button type="button" class="aiw" (click)="writeWithAI()" title="Describe a test; the AI assistant writes it as YAML, saves it and can run it">✦ Write a Test with AI</button>
        <div class="tree">
          <div class="h">CASES</div>
          @for (g of groups(); track g.dir) {
            <div class="grp">{{ g.dir || 'cases' }}<button type="button" class="lnk" (click)="run(filesOf(g))" [disabled]="busy()" [title]="'Run the ' + g.cases.length + ' tests in ' + (g.dir || 'cases')">▶</button></div>
            @for (c of g.cases; track c.file) {
              <div class="itr"><input type="checkbox" [checked]="picked().includes(c.file)" (change)="pick(c.file)" [attr.aria-label]="'Select ' + c.name" title="Select to run with ▶ Run Selected" />
              <button type="button" class="it" [class.sel]="c.file === sel() && !libSel()" (click)="select(c.file)" [title]="c.error || c.description || c.file">
                <span class="dot" [class]="dot(c)">●</span>{{ c.name }}<span class="n">{{ c.steps }}</span></button></div>
            }
          } @empty { <div class="mut">No tests yet. Tests → Record a Test, or ask the AI assistant to write one.</div> }
          <div class="h hb">TESTBEDS<button type="button" class="lnk" (click)="newTestbed()" title="New testbed from a session folder: a role for each session">+</button></div>
          @for (b of tree()?.testbeds ?? []; track b) { <button type="button" class="it" (click)="backend.testsOpen(b)">{{ b.slice(9) }}</button> } @empty { <div class="mut">none - add files in testbeds/</div> }
          <div class="h hb">SCHEDULES<button type="button" class="lnk" (click)="editSchedule(null)" title="Run tests by themselves while Termwright is open">+</button></div>
          @for (s of schedules(); track s.id) {
            <button type="button" class="it" [class.sel]="sched()?.id === s.id" (click)="editSchedule(s)" [title]="s.next ? 'Next: ' + s.next : 'Off'">
              <span class="dot" [class]="s.state.last ? (s.state.failed || s.state.error ? 'dot f' : 'dot p') : 'dot'">●</span>{{ s.name }}<span class="n">{{ s.off ? 'off' : s.when }}</span></button>
          } @empty { <div class="mut">none</div> }
          <div class="h">LIB</div>
          @for (l of tree()?.lib ?? []; track l) { <button type="button" class="it" [class.sel]="libSel() === l" (click)="openLib(l)" title="The helpers the generated tests use, and your own">{{ l.slice(4) }}</button> } @empty { <div class="mut">none</div> }
          <div class="h">REPORTS</div>
          @for (r of (tree()?.reports ?? []).slice(0, 12); track r.dir) {
            <button type="button" class="it" (click)="backend.testsOpen(r.dir)" title="Open the HTML report">{{ when(r.dir) }} <span [class]="r.failed ? 'bad' : 'ok'">{{ r.passed }}/{{ r.passed + r.failed }}</span></button>
          }
        </div>
      </aside>
      <section class="main">
        @if (sched(); as s) {
          <header><b class="ttl">{{ s.id ? 'Schedule' : 'New Schedule' }}</b><span class="mut">runs while Termwright is open; a failed run shows a notification</span></header>
          <div class="body sf">
            <label><span>Name</span><input [(ngModel)]="s.name" placeholder="Nightly smoke tests" /></label>
            <label><span>Tests</span><select [(ngModel)]="schedWhat"><option value="all">All tests</option>@if (sel()) { <option value="sel">{{ sel() }}</option> }@if (s.files?.length && schedWhat === 'keep') { <option value="keep">{{ s.files?.join(', ') }}</option> }</select></label>
            <label><span>Testbed</span><select [(ngModel)]="s.testbed"><option value="">None</option>@for (b of tree()?.testbeds ?? []; track b) { <option [value]="b">{{ b.slice(9) }}</option> }</select></label>
            <label><span>Repeat</span><select [(ngModel)]="s.repeat"><option value="daily">Daily</option><option value="weekdays">Weekdays</option><option value="weekly">Weekly</option><option value="hours">Every N hours</option></select></label>
            @if (s.repeat === 'hours') { <label><span>Every</span><input type="number" min="1" max="168" [(ngModel)]="s.hours" /> hours</label> }
            @else {
              @if (s.repeat === 'weekly') { <label><span>Day</span><select [(ngModel)]="s.day">@for (d of days; track $index) { <option [ngValue]="$index">{{ d }}</option> }</select></label> }
              <label><span>At</span><input type="time" [(ngModel)]="s.at" /></label>
            }
            <label><span>Cleanup</span><select [(ngModel)]="s.cleanup"><option [ngValue]="undefined">Run each test's cleanup steps</option><option value="off">No cleanup - tests build on each other</option><option value="auto">Cleanup steps, else undo its configuration</option></select></label>
            <label><span>Failed cleanup</span><select [(ngModel)]="s.onCleanupFail"><option [ngValue]="undefined">Stop the run</option><option value="restore">Restore the saved configuration</option><option value="factory">Factory default</option></select></label>
            <label class="ck"><input type="checkbox" [(ngModel)]="s.production" /> May use production sessions</label>
            <label class="ck"><input type="checkbox" [(ngModel)]="s.off" /> Off</label>
            @if (schedView(); as v) { @if (v.state.last) { <p class="mut">Last run {{ v.state.last.slice(0, 16).replace('T', ' ') }}: {{ v.state.error || (v.state.passed + ' passed, ' + v.state.failed + ' failed') }}@if (v.state.dir) { · <button type="button" class="lnk" (click)="backend.testsOpen(v.state.dir!)">report</button> }</p> } }
            <div class="row"><button type="button" (click)="sched.set(null)">Close</button><span class="sp"></span>
              @if (s.id) { <button type="button" (click)="removeSchedule(s)">Remove</button><button type="button" (click)="runScheduleNow(s)">Run Now</button> }
              <button type="button" class="pri" (click)="saveSchedule(s)">Save</button></div>
          </div>
        } @else if (libSel(); as lf) {
          <header><b class="ttl">{{ lf }}</b><span class="mut">library - shared by the tests</span><span class="sp"></span><button type="button" (click)="backend.testsOpenInEditor(lf)">Open in VS Code</button></header>
          <div class="body"><pre class="code ro">{{ goSrc() }}</pre>
            <div class="row"><span class="mut">Files in lib/ that Termwright writes (twtest, run_tests) are written again when it updates; keep your own helpers in other files.</span></div></div>
        } @else if (sel(); as file) {
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
                @if (test()?.restore?.length) { <p class="mut">Puts back afterwards: {{ restoreText() }}</p> }
                @for (r of restoreRes(); track $index) { <div class="rr2" [class.bad]="!r.passed"><span>{{ r.passed ? '✓' : '✗' }}</span> {{ r.name }} <span class="mut">{{ r.target }}</span>@if (r.error) { <span> - {{ r.error }}</span> }</div> }
                @if (last()[sel()]?.cleanupFailed) { <div class="warnbox">The cleanup failed: the device may not be clean for the next test.</div> }
                @if (draft(); as d) {
                  @for (ph of phases; track ph.k) {
                    <div class="phh">{{ ph.l }} <button type="button" class="lnk" (click)="addStep(ph.k)">+ Step</button></div>
                    @for (s of listOf(d, ph.k); track $index; let i = $index; let lastI = $last) {
                      <div class="st ed" [class.cl]="ph.k === 'cleanup'" [class.su]="ph.k === 'setup'">
                        <div class="row"><b>{{ i + 1 }}</b>
                          @if (s.run) { <input class="ses" [(ngModel)]="s.run.session" placeholder="session" aria-label="Session" /><input class="cmdi" [(ngModel)]="s.run.command" placeholder="command" aria-label="Command" /> }
                          @else if (s.http) { <select [(ngModel)]="s.http.method" aria-label="Method">@for (m of methods; track m) { <option [value]="m">{{ m }}</option> }</select><input class="cmdi" [(ngModel)]="s.http.url" placeholder="URL" aria-label="URL" /> }
                          @else { <span class="cmd fl">{{ text(s) }}</span><span class="sp"></span> }
                          <button type="button" class="lnk" (click)="moveStep(ph.k, i, -1)" [disabled]="i === 0" title="Move up">↑</button><button type="button" class="lnk" (click)="moveStep(ph.k, i, 1)" [disabled]="lastI" title="Move down">↓</button><button type="button" class="lnk" (click)="removeStep(ph.k, i)" title="Remove the step">✕</button>
                        </div>
                        <div class="row opts"><label>Timeout <input type="number" min="0" class="num" [(ngModel)]="s.timeout" /> s</label>
                          <label><input type="checkbox" [(ngModel)]="s.continueOnFail" /> Go on when it fails</label>
                          @if (s.run) { <label title="Do not fail the step when the device answers with an error message"><input type="checkbox" [(ngModel)]="s.ignoreErrors" /> Ignore device errors</label>
                            <label title="Turn the output into a table with a TextFSM template: auto (templates/index) or a file in templates/">Parse <input class="pin" [(ngModel)]="s.run.parse" placeholder="off" list="tw-parse" /></label> }</div>
                        <div>@for (e of s.expect ?? []; track $index; let j = $index) { <span class="chip">{{ label(e) }} <button type="button" class="x" (click)="removeCheck(s, j)" title="Remove the check">×</button></span> }
                          @if (ck(ph.k, i); as c) {
                            <span class="addck"><select [(ngModel)]="c.k" aria-label="Check">@for (k of checkKinds(s); track k.k) { <option [value]="k.k">{{ k.l }}</option> }</select>
                              <input [(ngModel)]="c.v" [placeholder]="checkHint(c.k)" aria-label="Value" (keydown.enter)="addCheck(s, c)" /><button type="button" (click)="addCheck(s, c)" [disabled]="!c.v.trim()">+ Check</button></span>
                          }</div>
                      </div>
                    } @empty { <div class="mut">no {{ ph.l.toLowerCase() }} steps</div> }
                  }
                  <datalist id="tw-parse"><option value="auto"></option></datalist>
                  <div class="row"><span class="mut">Saving writes the test's YAML and its Go, Python and TCL code again.</span><span class="sp"></span><button type="button" (click)="draft.set(null)">Cancel</button><button type="button" class="pri" (click)="saveDraft()">Save</button></div>
                } @else {
                @for (row of rows(); track $index) {
                  @if ($index === 0 || rows()[$index - 1].phase !== row.phase) { <div class="phh">{{ row.phase === 'setup' ? 'Setup' : row.phase === 'cleanup' ? (cleanupMode === 'off' ? 'Cleanup - off for runs from here (After each test)' : 'Cleanup - runs after the steps, also when one failed') : 'Steps' }}</div> }
                  <div class="st" [class.f]="res(row)?.passed === false && !res(row)?.skipped" [class.cl]="row.phase === 'cleanup'" [class.su]="row.phase === 'setup'">
                    <div><b>{{ row.i + 1 }}</b> <span class="mut">{{ row.s.run?.session ?? row.s.http?.session ?? row.s.snmp?.session ?? '' }} ›</span> <span class="cmd">{{ text(row.s) }}</span>
                      @if (res(row); as r) { <span class="ms">{{ r.skipped ? 'skipped' : r.ms + ' ms' }}</span> }</div>
                    @for (e of row.s.expect ?? []; track $index; let j = $index) {
                      <span class="chip" [class.b]="res(row)?.checks?.[j]?.passed === false">{{ res(row)?.checks?.[j]?.passed === false ? '✗' : '✓' }} {{ label(e) }}@if (res(row)?.checks?.[j]?.passed === false) { · got {{ res(row)?.checks?.[j]?.got }} }</span>
                    }
                    @for (v of row.s.save ?? []; track $index) { <span class="chip v">{{ '{{' + v.var + '}}' }}</span> }
                    @if (res(row); as r) { @if (r.unchecked && r.passed && !r.skipped) { <span class="chip u" title="The step has no checks: it passes when the command runs and the device does not answer with an error">○ ran, nothing checked</span> } }
                    @if (res(row)?.error) { <div class="bad">{{ res(row)?.error }}</div> }
                    @if (res(row); as r) { @if (r.table; as tb) { <details class="out"><summary>Table · {{ tb.rows.length }} rows</summary><div class="tbw"><table class="ptab"><tr>@for (h of tb.header; track $index) { <th>{{ h }}</th> }</tr>@for (x of tb.rows.slice(0, 200); track $index) { <tr>@for (c of x; track $index) { <td>{{ c }}</td> }</tr> }</table></div></details> } }
                    @if (res(row); as r) { @if (r.output && !r.skipped) { <details class="out"><summary>Output</summary><pre>{{ r.output }}</pre></details> } }
                  </div>
                }
                <div class="row"><button type="button" (click)="edit()" title="Change, add, move or remove steps and checks">✎ Edit Steps</button><button type="button" (click)="recordMore()">● Record a New Test</button>
                  @if (last()[file]?.passed === false) { <button type="button" (click)="explain(last()[file])" title="Ask the AI assistant why this test failed">✦ Explain the Failure</button> }
                  <span class="sp"></span><button type="button" (click)="rename(false)" title="Move the test to another name or folder">Rename…</button><button type="button" (click)="rename(true)" title="Copy the test under a new name">Duplicate…</button><button type="button" (click)="remove()" title="Move the test and its code to .removed/ in the tests folder">Delete</button></div>
                }
              }
              @case ('yaml') {
                <textarea class="code" [(ngModel)]="yaml" spellcheck="false" aria-label="Test YAML"></textarea>
                <div class="row"><span class="mut">Saving checks the test and writes its Go test again.</span><span class="sp"></span><button type="button" (click)="setView('yaml')">Revert</button><button type="button" class="pri" (click)="saveYaml()">Save</button></div>
              }
              @case ('go') {
                <pre class="code ro">{{ goSrc() }}</pre>
                <div class="row"><span class="mut">Generated from the YAML - edit the YAML, not this file.@if (git()?.hasGo) { Run outside Termwright: go test ./cases/... in the tests folder. }</span><span class="sp"></span><button type="button" (click)="backend.testsOpenInEditor(goFile())">Open in VS Code</button></div>
              }
              @case ('py') {
                <pre class="code ro">{{ goSrc() }}</pre>
                <div class="row"><span class="mut">Generated from the YAML - edit the YAML, not this file. Run: python lib/python/run_tests.py (or pytest cases) in the tests folder.</span><span class="sp"></span><button type="button" (click)="backend.testsOpenInEditor(pyFile())">Open in VS Code</button></div>
              }
              @case ('tcl') {
                <pre class="code ro">{{ goSrc() }}</pre>
                <div class="row"><span class="mut">Generated from the YAML - edit the YAML, not this file. Run: tclsh lib/tcl/run_tests.tcl in the tests folder (TAP output).</span><span class="sp"></span><button type="button" (click)="backend.testsOpenInEditor(tclFile())">Open in VS Code</button></div>
              }
            }
          </div>
        } @else { <div class="empty mut">Select a test, or record one: Tests → Record a Test.</div> }
        @if (report(); as rep) {
          <footer>
            <div class="row"><b>Last run</b><span class="ok">{{ rep.passed }} passed</span>@if (rep.failed) { <span class="bad">{{ rep.failed }} failed</span> }@if (rep.skipped) { <span class="warn">{{ rep.skipped }} skipped</span> }<span class="mut">{{ (rep.ms / 1000).toFixed(1) }} s</span><span class="sp"></span>
              <button type="button" (click)="backend.testsOpen(rep.dir)">Open Report</button><button type="button" (click)="backend.testsOpen(rep.dir + '/junit.xml')">JUnit XML</button></div>
            <div class="res">
              @for (s of rep.recovery ?? []; track $index) { <div class="rr2" [class.bad]="!s.passed">{{ s.passed ? '✓' : '✗' }} {{ s.name }} <span class="mut">{{ s.target }}</span>@if (s.error) { <span> - {{ s.error }}</span> }</div> }
              @for (r of rep.results; track $index) {
                <button type="button" class="rr" (click)="r.file && select(r.file)"><span [class]="r.skipped ? 'warn' : r.passed ? 'ok' : 'bad'">{{ r.skipped ? '○' : r.passed ? '✓' : '✗' }}</span>{{ r.file || r.case }}{{ paramOf(r.case) }}@if (r.cleanupFailed) { <span class="bad">failed (cleanup)</span> }<span class="mut">{{ r.passed ? '' : failText(r) }}</span><span class="sp"></span>@if (!r.passed && !r.skipped) { <span class="lnk ex" role="button" tabindex="0" (click)="$event.stopPropagation(); explain(r)" (keydown.enter)="$event.stopPropagation(); explain(r)" title="Ask the AI assistant why it failed">✦ Explain</span> }<span class="mut">{{ (r.ms / 1000).toFixed(1) }} s</span></button>
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
    .body p { margin: 0; } .phh { font-size: 11px; letter-spacing: .06em; color: var(--muted); text-transform: uppercase; margin-top: 4px; } .st.cl { border-color: color-mix(in srgb, var(--warn, #e0a030) 45%, var(--border)); } .st.su { border-style: dashed; } .rr2 { font-size: 12px; } .warnbox { font-size: 12px; color: var(--warn, #e0a030); border: 1px solid color-mix(in srgb, var(--warn, #e0a030) 45%, transparent); border-radius: 6px; padding: 6px 9px; } .body { flex: 1; overflow: auto; padding: 12px; display: flex; flex-direction: column; gap: 8px; min-height: 0; }
    .st { border: 1px solid var(--border); border-radius: 8px; padding: 7px 9px; } .st.f { border-color: color-mix(in srgb, var(--danger, #ef6161) 60%, transparent); } .cmd { font-family: var(--mono, monospace); margin-left: 4px; } .st > div > b { margin-right: 8px; } .ms { float: right; color: var(--muted); font-size: 11px; }
    .chip { display: inline-block; font-size: 11.5px; padding: 1px 8px; border-radius: 10px; margin: 4px 4px 0 0; background: color-mix(in srgb, var(--ok, #4ec07a) 15%, transparent); color: var(--ok, #4ec07a); }
    .chip.b { background: color-mix(in srgb, var(--danger, #ef6161) 15%, transparent); color: var(--danger, #ef6161); } .chip.v { background: color-mix(in srgb, var(--accent) 15%, transparent); color: var(--accent); }
    .code { flex: 1; min-height: 240px; font: 12.5px/1.5 var(--mono, monospace); resize: none; white-space: pre; overflow: auto; margin: 0; padding: 10px; border-radius: 8px; border: 1px solid var(--border); background: var(--term-bg, rgba(0,0,0,.25)); color: var(--text); }
    .sf label { display: flex; align-items: center; gap: 10px; } .sf label > span { width: 90px; color: var(--muted); font-size: 12px; } .sf input:not([type=checkbox]), .sf select { height: 28px; min-width: 220px; } .sf .ck { padding-left: 100px; font-size: 13px; } .aiw { text-align: left; } .ex { font-size: 12px; margin-right: 8px; cursor: pointer; } .pri { background: var(--accent); color: #fff; border-color: transparent; } .empty { margin: auto; }
    .ocf { display: flex; flex-direction: column; gap: 2px; font-size: 11px; color: var(--muted); } .ocf select { height: 26px; } .warn { color: var(--warn, #e0a030); }
    .itr { display: flex; align-items: center; gap: 2px; } .itr .it { flex: 1; min-width: 0; } .itr input { margin: 0 0 0 2px; } .grp { display: flex; justify-content: space-between; align-items: center; }
    .ed .row { gap: 6px; margin-bottom: 4px; } .ses { width: 150px; height: 26px; } .cmdi { flex: 1; min-width: 0; height: 26px; font-family: var(--mono, monospace); } .num { width: 60px; height: 24px; } .opts { font-size: 12px; color: var(--muted); gap: 14px; } .opts label { display: flex; align-items: center; gap: 4px; }
    .x { border: 0; background: none; color: inherit; cursor: pointer; padding: 0 0 0 4px; } .addck { display: inline-flex; gap: 4px; margin-top: 4px; } .addck select, .addck input { height: 24px; font-size: 12px; } .addck input { width: 200px; }
    .pin { width: 110px; height: 24px; } .tbw { max-height: 260px; overflow: auto; margin-top: 4px; border: 1px solid var(--border); border-radius: 6px; } .ptab { border-collapse: collapse; font: 12px var(--mono, monospace); } .ptab th { position: sticky; top: 0; background: var(--panel); text-align: left; padding: 2px 8px; color: var(--muted); font-weight: 500; } .ptab td { padding: 2px 8px; border-top: 1px solid var(--border); white-space: nowrap; }
    .chip.u { background: color-mix(in srgb, var(--warn, #e0a030) 15%, transparent); color: var(--warn, #e0a030); } .out summary { font-size: 11.5px; color: var(--muted); cursor: pointer; margin-top: 4px; } .out pre { font: 12px/1.45 var(--mono, monospace); max-height: 260px; overflow: auto; margin: 4px 0 0; padding: 6px 8px; border-radius: 6px; background: var(--term-bg, rgba(0,0,0,.25)); white-space: pre-wrap; }
    footer { border-top: 1px solid var(--border); padding: 8px 12px; max-height: 32%; display: flex; flex-direction: column; gap: 4px; min-height: 0; } .res { overflow: auto; }
    .rr { display: flex; gap: 8px; width: 100%; border: 0; background: none; color: var(--text); padding: 3px 4px; text-align: left; cursor: pointer; font: inherit; } .rr:hover { background: var(--hover, rgba(255,255,255,.05)); }
  `],
})
export class TestsView implements OnInit, OnDestroy {
  readonly visible = input(true);
  readonly backend = inject(Backend);
  private readonly ui = inject(Ui);
  private readonly rec = inject(Recorder);
  private readonly ai = inject(AIChat);
  readonly tree = signal<TestsTree | null>(null);
  readonly git = signal<TestsGitInfo | null>(null);
  readonly sel = signal('');
  readonly test = signal<TestCase | null>(null);
  readonly view = signal<View>('steps');
  readonly goSrc = signal('');
  readonly report = signal<TestRunReport | null>(null);
  readonly busy = signal(false);
  readonly schedules = signal<TestScheduleView[]>([]);
  readonly sched = signal<TestSchedule | null>(null);
  readonly schedView = computed(() => this.schedules().find(x => x.id === this.sched()?.id));
  readonly days = ['Sunday', 'Monday', 'Tuesday', 'Wednesday', 'Thursday', 'Friday', 'Saturday'];
  schedWhat = 'all';
  readonly views: { k: View; l: string }[] = [{ k: 'steps', l: 'Steps' }, { k: 'yaml', l: 'YAML' }, { k: 'go', l: 'Go' }, { k: 'py', l: 'Python' }, { k: 'tcl', l: 'TCL' }];
  yaml = '';
  testbed = '';
  private sig = '';
  private timer?: ReturnType<typeof setInterval>;
  private offs: (() => void)[] = [];
  readonly picked = signal<string[]>([]);
  readonly libSel = signal('');
  /** The test being edited in the Steps view (a copy; Save writes it). */
  readonly draft = signal<TestCase | null>(null);
  readonly phases: { k: Phase; l: string }[] = [{ k: 'setup', l: 'Setup' }, { k: 'steps', l: 'Steps' }, { k: 'cleanup', l: 'Cleanup - always runs' }];
  readonly methods = ['GET', 'POST', 'PUT', 'PATCH', 'DELETE'];
  private cks = new Map<string, { k: string; v: string }>();
  cleanupMode: CleanupMode = (() => { try { const v = localStorage.getItem('tw.tests.cleanup'); return (v === null ? 'off' : v) as CleanupMode; } catch { return 'off'; } })();
  onCleanupFail: OnCleanupFail = (() => { try { return (localStorage.getItem('tw.tests.ocf') as OnCleanupFail) || 'stop'; } catch { return 'stop'; } })();
  /** The last result of each test file, for the tree's dots. */
  readonly last = signal<Record<string, TestResult>>({});

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
  readonly tclFile = computed(() => this.sel().replace(/\.ya?ml$/, '') + '.test.tcl');
  /** As Termwright names it: cases/vlan/add.yaml → cases/vlan/test_vlan_add.py. */
  readonly pyFile = computed(() => {
    const f = this.sel(), i = f.lastIndexOf('/'), dir = f.slice(0, i), id = (s: string) => s.toLowerCase().replace(/[^a-z0-9]+/g, '_').replace(/^_+|_+$/g, '') || 'case';
    const sub = dir.replace(/^cases\/?/, ''), base = id(f.slice(i + 1).replace(/\.ya?ml$/, ''));
    return dir + '/test_' + (sub ? id(sub.replace(/\//g, '_')) + '_' : '') + base + '.py';
  });

  constructor() {
    effect(() => { if (this.visible()) void this.poll(); });
  }

  ngOnInit() {
    void this.load();
    void this.backend.testLastRun().then(r => { if (r) this.took(r); }).catch(() => {});
    this.offs.push(this.backend.on('tests:schedule', () => void this.load()));
    this.offs.push(this.backend.on('tests:progress', (r: TestResult) => { if (r.file) this.last.update(m => ({ ...m, [r.file!]: r })); }));
    this.timer = setInterval(() => { if (this.visible()) void this.poll(); }, 4000);
  }
  ngOnDestroy() { clearInterval(this.timer); this.offs.forEach(f => f()); }

  private async load() {
    try {
      this.tree.set(await this.backend.testsTree());
      this.git.set(await this.backend.testsGit());
      this.schedules.set(await this.backend.testSchedules().catch(() => []) ?? []);
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
    if (!keepView) {
      if (this.draft() && file !== this.sel() && !(await this.ui.confirm(tr('Discard the changes?'), tr('The steps you changed are not saved.'), tr('Discard'), true))) return;
      this.sched.set(null); this.libSel.set(''); this.draft.set(null);
    }
    this.sel.set(file);
    try { this.test.set(await this.backend.testCase(file)); } catch (e) { this.test.set(null); this.ui.toast(errText(e), 'error'); }
    if (keepView && this.view() === 'yaml') return; // unsaved edits stay
    await this.setView(keepView ? this.view() : 'steps');
  }

  async setView(v: View) {
    this.view.set(v);
    const f = this.sel();
    if (v === 'yaml') this.yaml = await this.backend.testFile(f).catch(e => '# ' + errText(e));
    if (v === 'go' || v === 'py' || v === 'tcl') this.goSrc.set(await this.backend.testFile(v === 'go' ? this.goFile() : v === 'py' ? this.pyFile() : this.tclFile()).catch(() => tr('No generated code yet: save the test (YAML → Save) to write it.')));
  }

  async saveYaml() {
    try {
      await this.backend.testSaveYAML(this.sel(), this.yaml);
      this.ui.toast(tr('Saved'), 'success');
      await this.load();
    } catch (e) { this.ui.toast(errText(e), 'error'); }
  }

  async run(files: string[], confirmed = false): Promise<void> {
    if (!confirmed && this.cleanupMode !== 'off' && this.onCleanupFail === 'factory' && !(await this.ui.confirm(tr('Factory Default on a Failed Cleanup?'),
      tr('When a cleanup fails, the devices of that test are reset to factory default and rebooted, then the run goes on. A factory default may remove the management address: use console sessions. Production sessions are never reset.'), tr('Run'), true))) return;
    this.busy.set(true);
    try {
      this.took(await this.backend.runTests({ files, testbed: this.testbed || undefined, confirmed, onCleanupFail: this.cleanupMode === 'off' ? undefined : this.onCleanupFail, cleanup: this.cleanupMode || undefined }));
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
  /** Sends a failed run with its test to the AI assistant and asks why it failed. */
  async explain(r: TestResult) {
    const yaml = r.file ? await this.backend.testFile(r.file).catch(() => '') : '';
    const cut = (s: string, n: number) => s.length > n ? s.slice(0, n) + '\n… (cut)' : s;
    const steps = r.steps.map((s, i) => {
      const head = `${i + 1}. ${s.skipped ? 'SKIPPED' : s.passed ? 'passed' : 'FAILED'} - ${s.phase ? s.phase + ': ' : ''}${s.name} (${s.kind} ${s.target ?? ''}: ${s.sent ?? ''}, ${s.ms} ms${s.status ? ', status ' + s.status : ''})`;
      if (s.passed || s.skipped) return head;
      const checks = (s.checks ?? []).map(c => `   ${c.passed ? 'ok' : 'FAILED'}: ${c.what}, got "${c.got ?? ''}"`).join('\n');
      return [head, s.error ? '   error: ' + s.error : '', checks, s.output ? '   output:\n' + cut(s.output, 6000) : ''].filter(Boolean).join('\n');
    }).join('\n');
    this.ai.currentId.set('');
    this.ai.start({});
    this.ai.addFile('Test ' + (r.file || r.case), `Test file ${r.file ?? ''}:\n${cut(yaml, 8000)}\n\nRun of "${r.case}" (${r.passed ? 'passed' : 'failed'}${r.error ? ': ' + r.error : ''}):\n${steps}`);
    void this.ai.send(tr('Why did this Termwright test fail? Say whether the device, the network or the test itself is wrong, and how to fix it. If the test should change, show the corrected YAML.'));
  }
  /** Asks the AI assistant to write a test from a description. */
  async writeWithAI() {
    const what = await this.ui.input(tr('Write a Test with AI'), tr('What should the test check? (e.g. VLAN 42 is up on core-sw1 and the REST API lists it)'), '');
    if (!what?.trim()) return;
    this.ai.currentId.set('');
    this.ai.start({});
    void this.ai.send(tr('Write a Termwright test that checks: {0}. Find the saved sessions it needs first, then save it with the tests tool (action save, a file under cases/), using roles ({{role}} variables with the sessions as defaults) and checks on what must be true. Then show the YAML and offer to run it.', what.trim()));
  }
  editSchedule(s: TestScheduleView | null) {
    this.libSel.set('');
    this.sched.set(s ? structuredClone({ ...s, state: undefined, next: undefined, when: undefined }) as TestSchedule : { id: '', name: '', repeat: 'daily', at: '06:00', day: 1, hours: 4, testbed: this.testbed });
    this.schedWhat = s?.files?.length ? 'keep' : 'all';
  }
  async saveSchedule(s: TestSchedule) {
    const files = this.schedWhat === 'sel' && this.sel() ? [this.sel()] : this.schedWhat === 'keep' ? s.files : undefined;
    const x: TestSchedule = { ...s, files, day: Number(s.day ?? 0), hours: Number(s.hours ?? 0) };
    if (x.production && !s.id && !(await this.ui.confirm(tr('Schedule on Production?'), tr('These tests then send their commands to production sessions without asking.'), tr('Schedule'), true))) return;
    try { this.sched.set(await this.backend.testSaveSchedule(x)); this.schedWhat = files?.length ? 'keep' : 'all'; this.ui.toast(tr('Saved'), 'success'); await this.load(); }
    catch (e) { this.ui.toast(errText(e), 'error'); }
  }
  async removeSchedule(s: TestSchedule) {
    if (!(await this.ui.confirm(tr('Remove the schedule?'), s.name, tr('Remove'), true))) return;
    try { await this.backend.testRemoveSchedule(s.id); this.sched.set(null); await this.load(); } catch (e) { this.ui.toast(errText(e), 'error'); }
  }
  async runScheduleNow(s: TestSchedule) {
    try { await this.backend.testRunScheduleNow(s.id); this.ui.toast(tr('Running {0}…', s.name), 'info'); } catch (e) { this.ui.toast(errText(e), 'error'); }
  }
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
  keepOcf() { try { localStorage.setItem('tw.tests.ocf', this.onCleanupFail); localStorage.setItem('tw.tests.cleanup', this.cleanupMode); } catch { /* not kept */ } }
  pick(f: string) { this.picked.update(l => l.includes(f) ? l.filter(x => x !== f) : [...l, f]); }
  filesOf(g: { cases: TestCaseInfo[] }) { return g.cases.map(c => c.file); }
  async openLib(f: string) {
    this.sched.set(null); this.libSel.set(f);
    this.goSrc.set(await this.backend.testFile(f).catch(e => errText(e)));
  }
  /** Rename (move) or duplicate the selected test. */
  async rename(copy: boolean) {
    const cur = this.sel().replace(/^cases\//, '').replace(/\.ya?ml$/, '');
    const to = await this.ui.input(copy ? tr('Duplicate Test') : tr('Rename Test'), tr('File in cases/ (a / makes a folder)'), copy ? cur + '-copy' : cur);
    if (!to?.trim() || to.trim() === cur) return;
    try { const rel = await this.backend.testRename(this.sel(), to.trim(), copy); this.ui.toast(tr('Wrote {0}', rel), 'success'); this.sel.set(rel); await this.load(); }
    catch (e) { this.ui.toast(errText(e), 'error'); }
  }
  async remove() {
    const f = this.sel();
    if (!(await this.ui.confirm(tr('Delete the test?'), tr('{0} and its Go, Python and TCL code are moved to .removed/ in the tests folder.', f), tr('Delete'), true))) return;
    try { await this.backend.testRemove(f); this.picked.update(l => l.filter(x => x !== f)); this.sel.set(''); this.draft.set(null); await this.load(); }
    catch (e) { this.ui.toast(errText(e), 'error'); }
  }
  edit() { const t = this.test(); if (t) { this.cks.clear(); const d = structuredClone({ ...t, setup: t.setup ?? [], steps: t.steps ?? [], cleanup: t.cleanup ?? [] }); for (const x of [...d.setup, ...d.steps, ...d.cleanup]) if (x.http) x.http.method ||= 'GET'; this.draft.set(d); } }
  listOf(d: TestCase, p: Phase): TestStep[] { return (p === 'steps' ? d.steps : p === 'setup' ? d.setup : d.cleanup) ?? []; }
  private touch() { const d = this.draft(); if (d) this.draft.set({ ...d }); this.cks.clear(); }
  addStep(p: Phase) {
    const d = this.draft(); if (!d) return;
    const all = [...(d.setup ?? []), ...d.steps, ...(d.cleanup ?? [])];
    const ses = [...all].reverse().find(s => s.run)?.run?.session ?? '';
    this.listOf(d, p).push({ run: { session: ses, command: '' } });
    this.touch();
  }
  moveStep(p: Phase, i: number, by: number) { const d = this.draft(); if (!d) return; const l = this.listOf(d, p); [l[i], l[i + by]] = [l[i + by], l[i]]; this.touch(); }
  removeStep(p: Phase, i: number) { const d = this.draft(); if (!d) return; this.listOf(d, p).splice(i, 1); this.touch(); }
  removeCheck(s: TestStep, j: number) { s.expect?.splice(j, 1); this.touch(); }
  ck(p: Phase, i: number) { const k = p + i; let c = this.cks.get(k); if (!c) { c = { k: 'contains', v: '' }; this.cks.set(k, c); } return c; }
  checkKinds(s: TestStep) {
    const k = [{ k: 'contains', l: 'contains' }, { k: 'notContains', l: 'does not contain' }, { k: 'matches', l: 'matches' }];
    return s.http ? [{ k: 'status', l: 'status' }, { k: 'json', l: 'JSON path =' }, ...k, { k: 'maxMs', l: 'within ms' }] : s.snmp ? [{ k: 'oid', l: 'OID =' }, ...k] : [...k, { k: 'table', l: 'table value' }];
  }
  checkHint(k: string) { return k === 'json' ? 'data.items[0].name = v42' : k === 'oid' ? 'ifOperStatus.4 = up' : k === 'status' ? '200' : k === 'maxMs' ? '500' : k === 'matches' ? 'v42\\s+42' : k === 'table' ? 'VLAN_ID=200: STATUS = active' : 'text'; }
  addCheck(s: TestStep, c: { k: string; v: string }) {
    const v = c.v.trim(); if (!v) return;
    let e: TestExpect;
    if (c.k === 'status' || c.k === 'maxMs') { const n = Number(v); if (!Number.isFinite(n)) return this.ui.toast(tr('Enter a number'), 'error'); e = { [c.k]: n }; }
    else if (c.k === 'table') {
      const t = parseTableCheck(v);
      if (!t) return this.ui.toast(tr('Write it as COLUMN=value: COLUMN = value (or rows N, missing, ~ pattern)'), 'error');
      e = { table: t };
      if (s.run && !s.run.parse) s.run.parse = 'auto';
    }
    else if (c.k === 'json' || c.k === 'oid') { const i = v.indexOf('='); e = i > 0 ? { [c.k]: v.slice(0, i).trim(), equals: v.slice(i + 1).trim() } : { [c.k]: v, exists: true }; }
    else e = { [c.k]: v };
    (s.expect ??= []).push(e); c.v = ''; this.touch();
  }
  async saveDraft() {
    const d = this.draft(); if (!d) return;
    const all = [...(d.setup ?? []), ...d.steps, ...(d.cleanup ?? [])];
    if (all.some(s => s.run && (!s.run.session.trim() || !s.run.command.trim()))) return this.ui.toast(tr('Every command step needs a session and a command.'), 'error');
    for (const s of all) { if (s.run && !s.run.parse) delete s.run.parse; if (!s.timeout) delete s.timeout; if (!s.continueOnFail) delete s.continueOnFail; if (!s.ignoreErrors) delete s.ignoreErrors; if (!s.expect?.length) delete s.expect; }
    const c: TestCase = { ...d, setup: d.setup?.length ? d.setup : undefined, cleanup: d.cleanup?.length ? d.cleanup : undefined };
    try { await this.backend.testSave(this.sel(), c); this.draft.set(null); this.ui.toast(tr('Saved'), 'success'); await this.load(); }
    catch (e) { this.ui.toast(errText(e), 'error'); }
  }
  paramOf(name: string) { const i = name.lastIndexOf(' ['); return i > 0 && name.endsWith(']') ? name.slice(i) : ''; }
  paramsText() { return (this.test()?.params ?? []).map(p => Object.entries(p).map(([k, v]) => `${k}=${v}`).join(', ')).join(' · '); }
  varsText() { return Object.entries(this.test()?.vars ?? {}).map(([k, v]) => `{{${k}}} = ${v}`).join(', '); }

  dot(c: TestCaseInfo) { if (c.error) return 'dot e'; const r = this.last()[c.file]; return r ? (r.passed ? 'dot p' : 'dot f') : 'dot'; }
  /** The test's setup, steps and cleanup in the order they run. */
  readonly rows = computed(() => {
    const t = this.test();
    const map = (l: TestStep[] | undefined, phase: '' | 'setup' | 'cleanup') => (l ?? []).map((s, i) => ({ s, i, phase }));
    return [...map(t?.setup, 'setup'), ...map(t?.steps, ''), ...map(t?.cleanup, 'cleanup')];
  });
  /** A row's result in the last run (results of the same phase, in order). */
  res(row: { i: number; phase: string }) { return this.last()[this.sel()]?.steps?.filter(x => (x.phase ?? '') === row.phase)[row.i]; }
  restoreRes() { return this.last()[this.sel()]?.steps?.filter(x => x.phase === 'restore') ?? []; }
  restoreText() { return (this.test()?.restore ?? []).map(k => k === 'lab' ? 'lab snapshot' : 'configuration').join(', '); }
  label = expectLabel;
  text = stepText;
  when(dir: string) { return dir.replace(/^reports\//, '').replace(/^(\d{4}-\d\d-\d\d)_(\d\d)(\d\d)(\d\d).*/, '$1 $2:$3'); }
  failText(r: TestResult) {
    if (r.error) return r.error;
    if (r.cleanupFailed) { const c = r.steps.find(s => s.phase === 'cleanup' && !s.passed && !s.skipped); if (c) return 'cleanup: ' + (c.error || c.checks?.find(x => !x.passed)?.what || c.name); }
    const i = r.steps.findIndex(s => !s.passed && !s.skipped), s = r.steps[i];
    if (!s) return '';
    const c = s.checks?.find(x => !x.passed);
    return `step ${i + 1}: ` + (s.error || (c ? `${c.what}, got "${c.got ?? ''}"` : 'failed'));
  }
}
