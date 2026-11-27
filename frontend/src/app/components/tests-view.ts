import { Component, OnDestroy, OnInit, computed, effect, inject, input, signal } from '@angular/core';
import { SizePick } from './size-pick';
import { TestBrowsers } from '../core/test-browsers';
import { FormsModule } from '@angular/forms';
import { NgTemplateOutlet } from '@angular/common';
import { Backend, errText } from '../core/backend';
import { Ui } from '../core/ui.service';
import { tr } from '../core/i18n';
import { Recorder, escapeRe, fieldPattern, isRead } from '../core/recorder.service';
import { Tabs } from '../core/tabs.service';
import { TestbedMeta, TestRequirement, TestStepOptions, TestSuiteDefaults, TestWeb, TestWebAction, WebResult, TestbedDevice, WebStatus, TestBooking, TestTrends, TestTable, TestProc, TestProcInfo, TemplateSpec, SessionNode, Session, TestApproval, CleanupMode, OnCleanupFail, TestCase, TestCaseInfo, TestExpect, TestHistory, TestStepResult, TestResult, TestRunReport, TestSchedule, TestScheduleView, TestStep, TestsGitInfo, TestsTree } from '../core/models';
import { expectLabel, parseTableCheck, stepText } from './recorder-panel';
import { AIChat } from '../core/ai-chat.service';
import { Icon } from './icon';
import { highlight, Lang } from '../core/highlight';

type View = 'steps' | 'yaml' | 'go' | 'py' | 'tcl' | 'history';
type Phase = 'setup' | 'steps' | 'cleanup';
type Section = 'tests' | 'runs' | 'schedules' | 'testbeds' | 'procs' | 'library';

/** The Tests tab: the tests folder's tree, the selected test (steps, YAML, generated Go) and the last run. */
type LiveItem = { k: 'head' | 'out' | 'res'; t?: string; c?: string; s?: TestStepResult };

/** A recorded action from the browser into a list: the toolbar's Undo drops the last one, Done is not an action. */
function recInto(acts: TestWebAction[], a: TestWebAction): boolean {
  if (a.do === '__done') return false;
  if (a.do === '__undo') return acts.pop() !== undefined;
  acts.push({ do: a.do, target: a.target, value: a.value, alt: a.alt?.length ? a.alt : undefined });
  return true;
}

@Component({
  selector: 'app-tests-view',
  imports: [SizePick, FormsModule, NgTemplateOutlet, Icon],
  template: `
    <div class="tv" [style.grid-template-columns]="'64px ' + sideW() + 'px minmax(0, 1fr)'">
      <nav class="rail" aria-label="Tests sections">
        @for (r of rails; track r.k) {
          <button type="button" [class.on]="section() === r.k" (click)="go(r.k)" [title]="r.t"><app-icon [name]="r.i" [size]="18" />{{ r.l }}</button>
        }
        <span class="sp"></span>
        <button type="button" class="gitb" (click)="gitMenu($event)" [title]="git()?.repo ? 'Git: ' + git()?.changed + ' changed' : 'Git'"><app-icon name="tree" [size]="17" />{{ git()?.repo ? (git()?.branch || 'git') : 'Git' }}@if (git()?.ahead) { <i>↑{{ git()?.ahead }}</i> }@if (git()?.behind) { <i>↓{{ git()?.behind }}</i> }</button>
      </nav>
      <aside class="side">
        <div class="sgrip" (mousedown)="dragSide($event)" (dblclick)="sideW.set(360); keepSide()" title="Drag to change the width (double-click: default)" role="separator" aria-orientation="vertical"></div>
        @switch (section()) {
          @case ('tests') {
            <div class="stop">
              <div class="recs"><button type="button" class="recb rc" (click)="recordMore()" title="Record what you do in terminal tabs, the API Client and the SNMP Browser as a test: devices, APIs, SNMP"><i class="rdot"></i><span>Record a Device Test<small>terminal · API · SNMP</small></span></button><button type="button" class="recb rc" (click)="openWebRec()" title="Record a test of any web app in a browser (Playwright) - no device needed. A test can have both: add device steps to a web test, or web steps to a device test"><i class="rdot"></i><span>Record a Web Test<small>Playwright · web apps</small></span></button></div>
              <div class="srch"><input class="search" type="search" placeholder="Search tests" [ngModel]="q()" (ngModelChange)="q.set($event)" aria-label="Search tests" /><button type="button" (click)="moreMenu($event)" title="Open, clone, import, export, write with AI, defaults, folder">⋯</button></div>
              <div class="tags">@for (t of tagList(); track t.k) { <button type="button" class="tag" [class.on]="tag() === t.k" (click)="tag.set(t.k)">{{ t.l }} <i>{{ t.n }}</i></button> }</div>
            </div>
            <div class="list">
              @if (tree()?.suites?.length) {
                <div class="fold"><button type="button" class="lnk fl" (click)="toggleFold('#suites')">{{ folded().includes('#suites') ? '▸' : '▾' }} Plans</button><span class="cnt">{{ tree()?.suites?.length }}</span></div>
                @if (!folded().includes('#suites')) {
                  @for (s of tree()?.suites ?? []; track s.file) {
                    <div class="case" (click)="openRun([s.file], 'plan ' + s.name)" (keydown.enter)="openRun([s.file], 'plan ' + s.name)" tabindex="0" role="button" [title]="s.error || 'Run the plan ' + s.name">
                      <span class="sui">▶</span><span class="cn">{{ s.name }}</span><span class="n">{{ s.tests }}</span>
                      <button type="button" class="lnk" (click)="$event.stopPropagation(); editSuite(s.file)" title="Open the suite file">✎</button></div>
                  }
                }
              }
              @for (g of groups(); track g.dir) {
                <div class="fold suite"><button type="button" class="lnk fl" (click)="toggleFold(g.dir)">{{ folded().includes(g.dir) ? '▸' : '▾' }} {{ g.dir || 'No suite' }}</button><span class="cnt">{{ g.cases.length }}</span>
                  <button type="button" class="lnk" (click)="openDefaults('cases' + (g.dir ? '/' + g.dir : ''))" [title]="'Defaults of the tests in ' + (g.dir || 'cases') + ': values and step options'">⚙</button>
                  <button type="button" class="lnk" (click)="openRun(filesOf(g), g.dir || 'cases')" [title]="'Run the tests in ' + (g.dir || 'cases')">▶</button></div>
                @if (!folded().includes(g.dir)) {
                  @for (c of g.cases; track c.file) {
                    <div class="case in" [class.sel]="c.file === sel() && section() === 'tests'" (click)="select(c.file)" (keydown.enter)="select(c.file)" tabindex="0" role="button" [title]="c.error || c.description || c.file">
                      <input type="checkbox" [checked]="picked().includes(c.file)" (click)="$event.stopPropagation()" (change)="pick(c.file)" [attr.aria-label]="'Select ' + c.name" />
                      <span class="dot" [class]="dot(c)"></span><span class="cn">{{ caseName(c) }}</span><span class="n">{{ c.steps }}</span></div>
                  }
                }
              } @empty {
                @if (!q() && tag() === 'all') {
                  <div class="onb">
                    <b>Automate a test in three steps</b>
                    <ol><li><b>Record</b> - work on the device as you always do; commands, API requests and SNMP queries become steps, and the checks for what you configured are added for you.</li>
                      <li><b>Run</b> - on the same devices or a testbed, with or without cleanup.</li>
                      <li><b>Report</b> - results with expected vs got, history, qTest and Jira.</li></ol>
                    <button type="button" class="recb" (click)="recordMore()"><i class="rdot"></i>Record a Device Test</button><button type="button" class="recb" (click)="openWebRec()"><i class="rdot"></i>Record a Web Test · Playwright</button>
                    <button type="button" (click)="importManual()">Import Test Cases…</button>
                    <button type="button" (click)="writeWithAI()">✦ Write a Test with AI</button>
                    <span class="mut">Import reads your test case sheet (Excel or CSV): Suite, Test, Step, Device, Expected - and Type web for web actions on a web app (click text=Log In, fill label=Email = {{ '{{' }}email{{ '}}' }}, expect Networks).</span>
                  </div>
                } @else { <div class="empty-s">No tests match.</div> }
              }
            </div>
            @if (picked().length) {
              <div class="selbar"><b>{{ picked().length }} selected</b><span class="sp"></span><button type="button" class="sbtn" (click)="picked.set([])">Clear</button><button type="button" class="sbtn" (click)="saveSuite()" title="Keep the selected tests as a plan (suites/…) to run them together again">Save as plan…</button><button type="button" class="go" (click)="openRun(picked(), (picked().length === 1 ? '1 test' : picked().length + ' tests'))"><app-icon name="play" [size]="12" /> Run {{ picked().length === 1 ? 'Test' : picked().length + ' Tests' }}</button></div>
            } @else if (filtering()) {
              <div class="selbar"><span class="mut">{{ filtered().length }} of {{ tree()?.cases?.length ?? 0 }} shown</span><span class="sp"></span><button type="button" (click)="openRun(shownFiles(), filtered().length + ' shown tests')" [disabled]="!filtered().length"><app-icon name="play" [size]="13" /> Run shown…</button></div>
            } @else {
              <div class="selbar"><span class="mut">{{ tree()?.cases?.length ?? 0 }} tests</span><span class="sp"></span><button type="button" (click)="openRun([], 'all tests')" [disabled]="!tree()?.cases?.length"><app-icon name="play" [size]="13" /> Run all…</button></div>
            }
          }
          @case ('runs') {
            <div class="stop"><div class="row"><b class="grow">Runs</b><button type="button" [class.on]="trendsOn()" (click)="openTrends()" title="Pass rate over the runs, the tests that fail most, results per software version">📈 Trends</button></div><span class="mut">past runs of this tests folder, newest first</span></div>
            <div class="list">
              @if (approvals().length) {
                <div class="fold"><span>Approvals</span></div>
                @for (a of approvals().slice(0, 8); track a.id) {
                  <div class="appr" [title]="a.files.join('\n')">
                    <div class="cn">{{ a.files.length === 1 ? base(a.files[0]) : a.files.length + ' tests' }} <span class="mut">on {{ a.testbed || 'no testbed' }}</span></div>
                    <div class="mut">by {{ a.requestedBy }}{{ a.reason ? ' - ' + a.reason : '' }}</div>
                    @if (!a.approvedBy) { <div class="aact"><button type="button" class="okbtn" (click)="decide(a.id, true)">Approve</button><button type="button" (click)="decide(a.id, false)">Reject</button></div> }
                    @else { <span class="pill" [class.p]="!a.rejected && !a.expired" [class.f]="a.rejected">{{ a.rejected ? 'rejected' : a.expired ? 'expired' : 'approved' }} by {{ a.approvedBy }}</span> }
                  </div>
                }
              }
              @for (r of runList(); track r.dir) {
                @if (r.day) { <div class="fold"><span>{{ r.day }}</span></div> }
                <div class="case" [class.sel]="runSel() === r.dir" (click)="openRunReport(r.dir)" (keydown.enter)="openRunReport(r.dir)" tabindex="0" role="button">
                  <span class="dot" [class]="r.failed ? 'dot f' : 'dot p'"></span><span class="cn">{{ r.time }}@if (r.testbed) { <span class="mut"> · {{ base(r.testbed) }}</span> }</span><span class="n">{{ r.passed }}/{{ r.passed + r.failed + (r.skipped ?? 0) }}</span></div>
              } @empty { <div class="empty-s">No runs yet.</div> }
            </div>
          }
          @case ('schedules') {
            <div class="stop"><div class="row"><b class="grow">Schedules</b><button type="button" (click)="editSchedule(null)">+ New</button></div><span class="mut">run by themselves while Termwright is open</span></div>
            <div class="list">
              @for (s of schedules(); track s.id) {
                <div class="case" [class.sel]="sched()?.id === s.id" (click)="editSchedule(s)" (keydown.enter)="editSchedule(s)" tabindex="0" role="button" [title]="s.next ? 'Next: ' + s.next : 'Off'">
                  <span class="dot" [class]="s.state.last ? (s.state.failed || s.state.error ? 'dot f' : 'dot p') : 'dot'"></span><span class="cn">{{ s.name }}</span><span class="n">{{ s.off ? 'off' : s.when }}</span></div>
              } @empty { <div class="empty-s">No schedules.</div> }
            </div>
          }
          @case ('testbeds') {
            <div class="stop"><div class="row"><b class="grow">Testbeds</b><button type="button" [class.on]="calOn()" (click)="openCal()" title="Book a testbed so other people's runs and schedules leave it alone">📅 Calendar</button><button type="button" (click)="openTbPick()" title="A role for each session of a session folder">+ New</button></div><span class="mut">which devices a run uses</span></div>
            <div class="list">
              @for (b of tree()?.testbeds ?? []; track b) { <div class="case" [class.sel]="fileSel() === b" (click)="calOn.set(false); openFile(b)" (keydown.enter)="openFile(b)" (contextmenu)="fileMenu($event, b)" tabindex="0" role="button"><span class="cn">{{ base(b) }}</span>@if (bookedNow(b); as k) { <span class="n" [title]="'Booked by ' + k.who + ' until ' + hm(k.to) + (k.why ? ' - ' + k.why : '')">📅 {{ k.mine ? 'yours' : k.who }}</span> }@if (lockOf(b); as l) { <span class="n" [title]="'Locked by ' + l.user + ' on ' + l.host + ' since ' + l.since.slice(11, 16)">🔒 {{ l.mine ? 'running' : l.user }}</span> }</div> } @empty { <div class="empty-s">No testbeds.</div> }
            </div>
          }
          @case ('procs') {
            <div class="stop"><div class="row"><b class="grow">Procedures</b><button type="button" (click)="newProcMenu($event)" title="A device procedure (CLI, API, SNMP steps) or a web procedure (browser actions), shared by tests">+ New ▾</button></div><span class="mut">steps several tests share - change once, every test follows</span></div>
            <div class="list">
              @for (p of procs(); track p.file) {
                <div class="case" [class.sel]="procSel() === p.file" (click)="openProc(p.file)" (keydown.enter)="openProc(p.file)" (contextmenu)="fileMenu($event, p.file)" tabindex="0" role="button" [title]="p.error || p.description || p.file">
                  <span class="pk" [class.web]="p.kind === 'web'">{{ p.kind === 'web' ? '🌐 web' : '⌨ device' }}</span><span class="cn">{{ p.name }}@if (p.params?.length) { <span class="mut">({{ p.params!.join(', ') }})</span> }</span><span class="n" [title]="'Used by ' + p.usedBy + (p.usedBy === 1 ? ' test' : ' tests') + ' - a change here changes them'">{{ p.kind === 'web' ? p.actions + ' actions' : p.steps + (p.steps === 1 ? ' step' : ' steps') }} · {{ p.usedBy }} {{ p.usedBy === 1 ? 'test' : 'tests' }}</span></div>
              } @empty { <div class="empty-s"><b>No procedures yet</b> - steps several tests share, changed in one place.<br>⌨ <b>Device procedure</b>: + New → Device procedure, or ⋯ on a test → Make a Procedure from This Test.<br>🌐 <b>Web procedure</b>: + New → Web procedure (record it in the browser), or select actions in a web step → <b>Save as Procedure…</b></div> }
            </div>
          }
          @case ('library') {
            <div class="stop"><b>Library</b><span class="mut">helpers of the generated code, and output templates</span>
              <button type="button" (click)="fetchTemplates()" [disabled]="fetching()" title="Download the community ntc-templates: TextFSM templates for hundreds of commands of many vendors">{{ fetching() ? 'Downloading…' : 'Get Community Templates' }}</button>
              <button type="button" class="lnk" (click)="openFile('reports/audit.log')" title="Who ran, saved, deleted or reported what, and when">Audit log</button>
              <button type="button" class="lnk" (click)="openRoles()" title="Who may edit, run and approve tests here, and which runs need an approval">Team roles</button></div>
            <div class="list">
              <div class="fold"><span>suites</span><span class="cnt">{{ tree()?.suites?.length ?? 0 }}</span></div>
              @for (l of tree()?.suites ?? []; track l.file) { <div class="case" [class.sel]="fileSel() === l.file" (click)="openFile(l.file)" (keydown.enter)="openFile(l.file)" tabindex="0" role="button"><span class="cn">{{ l.name }}</span><span class="n">{{ l.tests }}</span></div> }
              <div class="fold"><span>templates</span><span class="cnt">{{ tree()?.templates?.length ?? 0 }}</span></div>
              @for (l of tree()?.templates ?? []; track l) { <div class="case" [class.sel]="fileSel() === l" (click)="openFile(l)" (keydown.enter)="openFile(l)" tabindex="0" role="button"><span class="cn">{{ l.slice(10) }}</span></div> }
              <div class="fold"><span>lib</span><span class="cnt">{{ tree()?.lib?.length ?? 0 }}</span></div>
              @for (l of tree()?.lib ?? []; track l) { <div class="case" [class.sel]="fileSel() === l" (click)="openFile(l)" (keydown.enter)="openFile(l)" tabindex="0" role="button"><span class="cn">{{ l.slice(4) }}</span></div> }
            </div>
          }
        }
      </aside>
      <section class="main" [class.conmax]="conMax() && showLive() && liveLog().length > 0">
        @if (busy()) {
          <div class="live" role="status">
            <span class="spin"></span><b>Running</b>
            @if (liveStep(); as ls) { <span class="fl">{{ ls.case }} · {{ ls.step.name }}</span><span [class]="ls.step.skipped ? 'mut' : ls.step.passed ? 'ok' : 'bad'">{{ ls.step.skipped ? 'skipped' : ls.step.passed ? '✓' : '✗' }}</span> } @else { <span class="mut">starting…</span> }
            <span class="mut">{{ liveCount() }} steps done</span><span class="sp"></span>
            <button type="button" (click)="showLive.set(!showLive())">{{ showLive() ? 'Hide log' : 'Show log' }}</button>
            <button type="button" (click)="stopRun()" title="Stop after the step that runs now (cleanups still run)">■ Stop</button>
          </div>
          @if (paused(); as pz) {
            <div class="pausebar" [class.bp]="pz.breakpoint" role="alert">
              <div class="pzrow">
                <span class="pzi">{{ pz.breakpoint ? '●' : '❚❚' }}</span><div class="pzt"><b>{{ pz.breakpoint ? 'Stopped at a breakpoint' : 'Paused at a failed step' }}</b><span class="mut">{{ pz.case }} · {{ pz.step.name }}</span>
                  @if (!pz.breakpoint) { <div class="bad">{{ pz.step.error || failedCheck(pz.step) || 'a check failed' }}</div> } @else { <div class="mut">The step has not run yet. Change a variable below if you like, then run it.</div> }</div>
                <span class="sp"></span>
                @if (pz.step.target) { <button type="button" (click)="openDevice(pz.step.target)" title="Open the device in a terminal tab to look around (a new login)">Open Device</button> }
                @if (pz.breakpoint) {
                  <button type="button" class="go" (click)="resume('step')" title="Run this step, then stop before the next one">▶ Run Step</button>
                  <button type="button" (click)="resume('continue')" title="Run on to the next breakpoint">Continue</button>
                  <button type="button" (click)="resume('skip')" title="Leave this step out and stop before the next one">Skip</button>
                } @else {
                  <button type="button" class="go" (click)="resume('retry')" title="Run the step again - after you fixed the device or the test">↻ Retry Step</button>
                  <button type="button" (click)="resume('continue')" title="Go on with the next step (the test still fails)">Continue</button>
                }
                <button type="button" class="badbtn" (click)="resume('stop')" title="End the test; its cleanup runs">Stop Test</button>
              </div>
              @if (pauseVars().length) {
                <details class="pzvars" [open]="pz.breakpoint"><summary>Variables ({{ pauseVars().length }})</summary>
                  <div class="vars">@for (x of pauseVars(); track x.k) { <span class="vk">{{ '{{' + x.k + '}}' }}</span><input class="vin" [(ngModel)]="x.v" [attr.aria-label]="x.k" /> }</div>
                </details>
              }
            </div>
          }
        }
        @if (showLive() && liveLog().length) {
          <div class="console" [class.max]="conMax()">
            <div class="ch"><b>{{ busy() ? 'Live log' : 'Log of the last run' }}</b><span class="mut">what is sent to the devices and what comes back</span><span class="sp"></span>
              <button type="button" class="lnk" (click)="copyLive()">Copy</button>
              <button type="button" class="lnk" (click)="conMax.set(!conMax())" [title]="conMax() ? 'Back to the log above the test' : 'Show the log over the whole page'"><app-icon [name]="conMax() ? 'minimize' : 'maximize'" [size]="12" /> {{ conMax() ? 'Minimise' : 'Expand' }}</button>
              @if (!busy()) { <button type="button" class="lnk" (click)="showLive.set(false); conMax.set(false)">Close</button> }</div>
            <div class="con" #con [style.height.px]="conMax() ? null : conH()">
              @for (it of liveLog(); track $index) {
                @switch (it.k) {
                  @case ('head') { <div class="lhd"><span class="lar">▶&nbsp;</span><span class="lcase">{{ it.c }}</span> · <b>{{ it.t }}</b></div> }
                  @case ('out') { <pre class="lout">{{ it.t }}</pre> }
                  @case ('res') {
                    @if (it.s!.checks?.length) {
                      <table class="lchk"><thead><tr><th></th><th>Expected</th><th>Found</th></tr></thead><tbody>
                        @for (c of it.s!.checks!; track $index) { <tr [class.bad]="!c.passed"><td class="lm">{{ c.passed ? '✓' : '✗' }}</td><td>{{ c.what }}</td><td>{{ found(c) }}</td></tr> }
                      </tbody></table>
                    }
                    <div class="lres" [class.ok]="it.s!.passed && !it.s!.skipped" [class.bad]="!it.s!.passed && !it.s!.skipped" [class.skip]="it.s!.skipped">
                      {{ it.s!.skipped ? '– skipped' : it.s!.passed ? '✓ passed' : '✗ failed' }}<span class="lms">{{ dur(it.s!.ms) }}</span>
                      @if (it.s!.error) { <span class="lerr">{{ it.s!.error }}</span> }
                      @else if (!it.s!.skipped && !it.s!.checks?.length) { <span class="lnote">no checks - passes when the device does not answer with an error</span> }
                    </div>
                    @if (!it.s!.passed) { @for (p of failPics(it.s!.output); track p.path) {
                      <button type="button" class="lshot" (click)="showPic(p.path)" title="The page when it failed - click to enlarge">@if (picUrl(p.path); as u) { <img [src]="u" alt="The page when it failed" /> } @else { <span class="mut">{{ p.label }}</span> }</button>
                    } }
                  }
                }
              }
            </div>
            @if (!conMax()) { <div class="cgrip" (mousedown)="dragCon($event)" (dblclick)="conH.set(240); keepCon()" title="Drag to change the height (double-click: default)" role="separator" aria-orientation="horizontal"></div> }
          </div>
        }
        @switch (section()) {
          @case ('runs') {
            @if (trendsOn()) {
              <div class="head"><div class="crumb">reports/history.jsonl</div><div class="title"><h1>Trends</h1><span class="sp"></span>
                <select [(ngModel)]="trWhat" (ngModelChange)="loadTrends()" aria-label="Which tests"><option value="">Every test</option>@for (p of tree()?.suites ?? []; track p.file) { <option [value]="p.file">Plan: {{ p.name }}</option> }</select>
                <select [(ngModel)]="trTestbed" (ngModelChange)="loadTrends()" aria-label="Testbed"><option value="">Any testbed</option>@for (b of tree()?.testbeds ?? []; track b) { <option [value]="b">{{ base(b) }}</option> }</select>
                <button type="button" (click)="trendsOn.set(false)">Close</button></div></div>
              <div class="body">
                @if (trends(); as tr) {
                  @if (!tr.runs.length) { <div class="empty mut">No runs recorded yet{{ trWhat || trTestbed ? ' for this choice' : '' }}.</div> } @else {
                    <div class="kpis">
                      <div class="kpi"><b>{{ trRate(tr) }}%</b><span>passed (last {{ tr.runs.length }} runs)</span></div>
                      <div class="kpi"><b class="bad">{{ trSum(tr, 'failed') }}</b><span>failures</span></div>
                      <div class="kpi"><b class="kwarn">{{ trSum(tr, 'flaky') }}</b><span>flaky (passed on a rerun)</span></div>
                      <div class="kpi"><b>{{ trAvgMin(tr) }}</b><span>average run</span></div></div>
                    <div class="sec"><div class="sh">Pass rate per run<span class="sp"></span><span class="norm">oldest left · click a bar to open the run</span></div>
                      <div class="trbars">
                        @for (r of tr.runs; track r.dir) {
                          <div class="trb" (click)="trendsOn.set(false); openRunReport(r.dir)" [title]="when(r.dir) + ' · ' + r.passed + ' passed, ' + r.failed + ' failed, ' + r.skipped + ' skipped' + (r.version ? ' · ' + r.version : '') + (r.testbed ? ' · ' + base(r.testbed) : '')">
                            <i class="tf" [style.flex-grow]="r.failed"></i><i class="ts" [style.flex-grow]="r.skipped"></i><i class="tp" [style.flex-grow]="r.passed"></i></div>
                        }
                      </div></div>
                    <div class="trgrid">
                      <div class="sec"><div class="sh">Tests that fail most</div>
                        @for (t of tr.tests; track t.file) { <div class="trrow" (click)="trendsOn.set(false); section.set('tests'); select(t.file)" role="button" tabindex="0"><span class="cn">{{ t.case }}</span><span class="bad">{{ t.failures }}/{{ t.runs }} failed</span>@if (t.flips) { <span class="kwarn">{{ t.flips }} flips</span> }<span class="mut ell" [title]="t.last ?? ''">{{ t.last }}</span></div> }
                        @empty { <div class="none">No failures in these runs.</div> }</div>
                      <div class="sec"><div class="sh">Per software version</div>
                        @for (v of tr.versions; track v.version) { <div class="trrow"><span class="cn">{{ v.version }}</span><span class="bar trv"><i class="okb" [style.width.%]="100 * v.passed / v.total"></i></span><span>{{ (100 * v.passed / v.total).toFixed(0) }}%</span><span class="mut">{{ v.total }} results</span></div> }
                        @empty { <div class="none">No versions recorded (the runs read them from the devices).</div> }</div>
                    </div>
                  }
                }
              </div>
            } @else if (runRep(); as rep) {
              <div class="head"><div class="crumb">{{ rep.dir }} · {{ rep.testbed ? 'testbed ' + base(rep.testbed) : "the tests' own sessions" }} · {{ rep.cleanup === 'off' ? 'no cleanup' : rep.cleanup === 'auto' ? 'cleanup or undo' : 'cleanup steps' }} · {{ dur(rep.ms) }}@if (verText(rep.versions)) { · {{ verText(rep.versions) }} }@if (rep.runBy) { · run by {{ rep.runBy }} }</div>
                <div class="title"><h1>Run · {{ when(rep.dir) }}</h1>
                  <span class="pill" [class]="rep.failed ? 'pill f' : rep.skipped ? 'pill s' : 'pill p'">{{ rep.failed ? rep.failed + ' failed' : rep.skipped ? rep.skipped + ' skipped' : 'all passed' }}</span>
                  <span class="sp"></span>
                  <button type="button" (click)="backend.testsOpen(rep.dir)">HTML report</button><button type="button" (click)="backend.testsOpen(rep.dir + '/report.pdf')" title="The report as a PDF: summary, tests, every step with expected and found">PDF</button><button type="button" (click)="backend.testsOpen(rep.dir + '/junit.xml')">JUnit</button>
                  <button type="button" (click)="sendQTest(rep)" [disabled]="sending()" title="Send each test's result to its qTest test case (Global Options → Integrations)">{{ sending() ? 'Sending…' : 'Send to qTest' }}</button>
                  @if (failedFiles(rep).length) { <button type="button" class="go" (click)="openRun(failedFiles(rep), 'the failed tests')"><app-icon name="play" [size]="12" /> Run Failed Again</button> }
                </div></div>
              <div class="body">
                <div class="kpis">
                  <div class="kpi"><b class="kok">{{ rep.passed }}</b><span>passed</span></div><div class="kpi"><b class="kbad">{{ rep.failed }}</b><span>failed</span></div>
                  <div class="kpi"><b class="kwarn">{{ rep.skipped ?? 0 }}</b><span>skipped</span></div><div class="kpi"><b>{{ unchecked(rep) }}</b><span>steps not checked</span></div></div>
                <div class="bar"><i class="okb" [style.flex]="rep.passed"></i><i class="badb" [style.flex]="rep.failed"></i><i class="warnb" [style.flex]="rep.skipped ?? 0"></i></div>
                @if (rep.recovery?.length) { <div class="sec"><div class="sh">Bringing the devices back</div>@for (s of rep.recovery; track $index) { <div class="trow" [class.bad]="!s.passed"><span>{{ s.passed ? '✓' : '✗' }}</span><span>{{ s.name }}</span><span class="mut">{{ s.target }} {{ s.error }}</span></div> }</div> }
                <div class="sec"><div class="rhd rq"><span></span><span>Test</span><span>First problem</span><span>qTest / Jira</span><span class="r">Time</span></div>
                  @for (r of rep.results; track $index) {
                    <div class="rrow rq" (click)="r.file && openTest(r.file)" (keydown.enter)="r.file && openTest(r.file)" tabindex="0" role="button">
                      <span [class]="r.skipped ? 'warn' : r.passed ? 'ok' : 'bad'">{{ r.skipped ? '○' : r.passed ? '✓' : '✗' }}</span>
                      <span>{{ r.case }}@if (r.flaky) { <span class="pill w sm" [title]="'Failed first, passed on attempt ' + r.attempts">flaky</span> }@if (r.cleanupFailed) { <span class="pill f sm">cleanup</span> }</span>
                      <span class="mut">{{ r.passed ? notChecked(r) : failText(r) }}@if (!r.passed && !r.skipped) { · <button type="button" class="lnk" (click)="$event.stopPropagation(); explain(r)">✦ Explain</button> }</span>
                      <span class="mut">{{ r.qtestRun }}@if (r.defect) { @if (r.qtestRun) { · } <button type="button" class="lnk" (click)="$event.stopPropagation(); backend.openURL(r.defectUrl!)">{{ r.defect }}</button> } @else if (!r.passed && !r.skipped) { @if (r.qtestRun) { · } <button type="button" class="lnk" (click)="$event.stopPropagation(); createDefect(rep.dir, r)" title="Open a Jira defect with the steps, the failed output and the report">+ defect</button> }</span>
                      <span class="r mut">{{ dur(r.ms) }}@if (r.log) { <button type="button" class="lnk" (click)="$event.stopPropagation(); openLog(r.log!, r.case)" title="Every step: what was sent, the full answer, the checks">Log</button> }</span></div>
                  }
                </div>
              </div>
            } @else { <div class="empty mut">Select a run.</div> }
          }
          @case ('schedules') {
            @if (sched(); as s) {
              <div class="head"><div class="crumb">schedules</div><div class="title"><h1>{{ s.id ? s.name || 'Schedule' : 'New Schedule' }}</h1><span class="mut">runs while Termwright is open; a failed run shows a notification</span></div></div>
              <div class="body"><div class="form">
                <label><span>Name</span><input [(ngModel)]="s.name" placeholder="Nightly smoke tests" /></label>
                <label><span>Tests</span><select [(ngModel)]="schedWhat"><option value="all">All tests</option>@if (sel()) { <option value="sel">{{ sel() }}</option> }@if (picked().length) { <option value="picked">The {{ picked().length }} selected tests</option> }@for (su of tree()?.suites ?? []; track su.file) { <option [value]="'suite:' + su.file">Suite {{ su.name }}</option> }@if (s.files?.length && schedWhat === 'keep') { <option value="keep">{{ s.files?.join(', ') }}</option> }</select></label>
                <label><span>Only tags</span><input [ngModel]="(s.tags ?? []).join(', ')" (ngModelChange)="s.tags = splitTags($event)" placeholder="all - or e.g. smoke, !slow" /></label>
                <label><span>Testbed</span><select [(ngModel)]="s.testbed"><option value="">The tests' own sessions</option>@for (b of tree()?.testbeds ?? []; track b) { <option [value]="b">{{ base(b) }}</option> }</select></label>
                <label><span>Repeat</span><select [(ngModel)]="s.repeat"><option value="daily">Daily</option><option value="weekdays">Weekdays</option><option value="weekly">Weekly</option><option value="hours">Every N hours</option></select></label>
                @if (s.repeat === 'hours') { <label><span>Every</span><input type="number" min="1" max="168" [(ngModel)]="s.hours" /> hours</label> }
                @else {
                  @if (s.repeat === 'weekly') { <label><span>Day</span><select [(ngModel)]="s.day">@for (d of days; track $index) { <option [ngValue]="$index">{{ d }}</option> }</select></label> }
                  <label><span>At</span><input type="time" [(ngModel)]="s.at" /></label>
                }
                <label><span>After each test</span><select [(ngModel)]="s.cleanup"><option [ngValue]="undefined">Run its cleanup steps</option><option value="off">No cleanup - tests build on each other</option><option value="auto">Cleanup steps, else undo its configuration</option></select></label>
                <label><span>If a cleanup fails</span><select [(ngModel)]="s.onCleanupFail"><option [ngValue]="undefined">Stop the run - skip the rest</option><option value="restore">Restore the saved configuration</option><option value="factory">Factory default</option></select></label>
                <label title="A failed test runs again up to this many times; passing on a rerun marks it flaky"><span>Rerun a failed test</span><select [(ngModel)]="s.rerun"><option [ngValue]="undefined">No</option><option [ngValue]="1">Once</option><option [ngValue]="2">Twice</option><option [ngValue]="3">3 times</option></select></label>
                <label title="Tests not started by then are skipped and the run reports them"><span>Stop the run after (minutes)</span><input type="number" min="0" [(ngModel)]="s.runTimeout" placeholder="no limit" /></label>
                <label class="ck"><input type="checkbox" [(ngModel)]="s.production" /> May use production sessions</label>
                <label class="ck"><input type="checkbox" [(ngModel)]="s.off" /> Off</label>
                @if (schedView(); as v) { @if (v.state.last) { <p class="mut">Last run {{ v.state.last.slice(0, 16).replace('T', ' ') }}: {{ v.state.error || (v.state.passed + ' passed, ' + v.state.failed + ' failed') }}@if (v.state.dir) { · <button type="button" class="lnk" (click)="openRunReport(v.state.dir!)">open</button> }</p> } }
                <div class="row"><span class="sp"></span>@if (s.id) { <button type="button" (click)="removeSchedule(s)">Remove</button><button type="button" (click)="runScheduleNow(s)">Run Now</button> }<button type="button" class="pri" (click)="saveSchedule(s)">Save</button></div>
              </div></div>
            } @else { <div class="empty mut">Select a schedule, or + New.</div> }
          }
          @case ('testbeds') {
            @if (calOn()) {
              <div class="head"><div class="crumb">bookings.yaml - shared with the team through Git</div><div class="title"><h1>Testbed Calendar</h1><span class="sp"></span>
                <button type="button" (click)="calMove(-7)" aria-label="Previous week">‹</button><button type="button" (click)="calToday()">This Week</button><button type="button" (click)="calMove(7)" aria-label="Next week">›</button>
                <button type="button" class="pri" (click)="newBooking()">+ Book</button></div></div>
              <div class="body">
                <div class="cal">
                  <div class="calh"><span></span>@for (d of calDays(); track d.getTime()) { <span [class.today]="isToday(d)">{{ calDay(d) }}</span> }</div>
                  @for (b of tree()?.testbeds ?? []; track b) {
                    <div class="calr"><span class="caln" [title]="b">{{ base(b) }}</span>
                      <div class="calt" (dblclick)="newBooking(b, $event)" title="Double-click to book">
                        @for (d of calDays(); track d.getTime()) { <i class="cald" [class.today]="isToday(d)"></i> }
                        @if (nowPct() >= 0) { <i class="caln-now" [style.left.%]="nowPct()"></i> }
                        @for (k of weekBookings(b); track k.id) {
                          <div class="calb" [class.mine]="k.b.mine" [style.left.%]="k.l" [style.width.%]="k.w" (click)="editBooking(k.b)" [title]="k.b.who + ': ' + fmtWhen(k.b.from) + ' → ' + fmtWhen(k.b.to) + (k.b.why ? '\n' + k.b.why : '')"><b>{{ k.b.mine ? 'You' : k.b.who }}</b> {{ k.b.why }}</div>
                        }
                      </div></div>
                  } @empty { <div class="empty mut">No testbeds yet. Make one with + New.</div> }
                </div>
                @if (bookings().length) {
                  <div class="sec bkl"><div class="sh">Coming up</div>
                    @for (k of bookings(); track k.id) { <div class="trrow" role="button" tabindex="0" (click)="editBooking(k)" (keydown.enter)="editBooking(k)"><span class="cn">{{ base(k.testbed) }}</span><span>{{ fmtWhen(k.from) }} → {{ fmtWhen(k.to) }}</span><b [class.accent]="k.mine">{{ k.mine ? 'You' : k.who }}</b><span class="mut ell">{{ k.why }}</span></div> }
                  </div>
                }
                <p class="mut calnote">While a testbed is booked, runs and schedules by other people are refused (a run can still take it after asking). Bookings are in bookings.yaml in the tests folder - commit it so the team sees them.</p>
                @if (bk(); as k) {
                  <div class="sec bkf"><div class="sh">{{ k.id ? 'Booking' : 'New Booking' }}@if (k.id && !k.mine) { <span class="norm"> - by {{ k.who }}</span> }</div>
                    <div class="form">
                      <label><span>Testbed</span><select [(ngModel)]="k.testbed">@for (b of tree()?.testbeds ?? []; track b) { <option [value]="b">{{ base(b) }}</option> }</select></label>
                      <label><span>From</span><input type="datetime-local" [(ngModel)]="bkFrom" /></label>
                      <label><span>To</span><input type="datetime-local" [(ngModel)]="bkTo" /></label>
                      <label><span>Why</span><input [(ngModel)]="k.why" placeholder="Upgrade to the new release" /></label>
                      <div class="row"><span class="sp"></span>@if (k.id) { <button type="button" class="badbtn" (click)="cancelBooking(k)">Cancel Booking</button> }<button type="button" (click)="bk.set(null)">Close</button><button type="button" class="pri" (click)="saveBooking(k)">Save</button></div>
                    </div></div>
                }
              </div>
            } @else { <ng-container *ngTemplateOutlet="fileView" /> }
          }
          @case ('procs') {
            @if (proc(); as p) {
              <div class="head"><div class="crumb">{{ procSel() }}</div><div class="title"><h1>{{ p.name }}</h1><span class="pill">procedure</span><span class="sp"></span>
                <button type="button" (click)="copyText(callSnippet(p))" title="Copy the YAML of a step that calls it"><app-icon name="copy" [size]="13" /> Copy Call</button>
                <button type="button" (click)="editProcYaml()">✎ Edit YAML</button>
                <button type="button" class="badbtn" (click)="removeFile(procSel())"><app-icon name="trash" [size]="13" /> Delete</button></div></div>
              <div class="body">
                @if (p.description) { <p class="mut">{{ p.description }}</p> }
                <div class="meta"><span>Parameters <b>@for (x of p.params ?? []; track x) { <span class="tag">{{ '{{' }}{{ x.split('=')[0] }}{{ '}}' }}{{ x.includes('=') ? ' = ' + x.split('=')[1] : '' }}</span> } @empty { none }</b></span>
                  <span>Used by <b>{{ procUsers(p.name ?? '').length }} {{ procUsers(p.name ?? '').length === 1 ? 'test' : 'tests' }}</b></span></div>
                <div class="sec">
                  <div class="sh">Steps<span class="sp"></span><span class="norm">{{ p.steps.length }} steps</span></div>
                  <div class="srow hd"><span>#</span><span>Device</span><span>Command</span><span>Checks</span><span></span><span></span></div>
                  @for (st of p.steps; track $index; let i = $index) {
                    <div class="srow"><span class="mut">{{ i + 1 }}</span><span class="dev">{{ shortDev(target(st)) }}</span><span class="cmd">{{ text(st) }}</span>
                      <span class="chips">@for (e of st.expect ?? []; track $index) { <span class="chip">{{ label(e) }}</span> } @empty { @if (!isReadStep(st)) { <span class="chip n">✓ device errors</span> } @else { <span class="chip w">○ not checked</span> } }</span><span></span><span></span></div>
                  }
                </div>
                <p class="mut">In a test: <b>+ More ▾ → Call a Procedure</b>. Its steps run in place of the call with the parameters filled in, in Termwright and in the generated Go, Python and TCL.</p>
              </div>
            } @else { <ng-container *ngTemplateOutlet="fileView" /> }
          }
          @case ('library') { <ng-container *ngTemplateOutlet="fileView" /> }
          @default {
            @if (sel() && test(); as t) {
              <div class="head">
                <div class="crumb">{{ sel() }}</div>
                <div class="title"><h1>{{ t.name || sel() }}</h1>
                  @if (busy() && runningNow(sel())) { <span class="pill run"><i class="spin sm"></i>running</span> } @else if (last()[sel()]; as lr) { <span class="pill" [class]="lr.skipped ? 'pill s' : lr.passed ? 'pill p' : 'pill f'">{{ lr.skipped ? 'skipped' : lr.passed ? 'passed' : lr.cleanupFailed ? 'failed (cleanup)' : 'failed' }} · {{ ago(lr.start) }}</span> } @else { <span class="pill">not run yet</span> }
                  @for (g of t.tags ?? []; track g) { <span class="tag">{{ g }}</span> }
                  <span class="sp"></span>
                  @if (last()[sel()]?.log; as lg) { <button type="button" (click)="openLog(lg, t.name || sel())" title="The last run's log: every step, what was sent and what came back">Run Log</button> }
                  @if (!draft()) { <button type="button" (click)="edit()">✎ Edit</button> } @else { <button type="button" (click)="draft.set(null)">Cancel</button><button type="button" class="savb" (click)="saveDraft()" title="Save the test (its YAML, Go, Python and TCL)">Save</button> }
                  <button type="button" (click)="testMenu($event)" title="Rename, duplicate, delete, open">⋯</button>
                  <button type="button" class="go" (click)="openRun([sel()], t.name || sel())" [disabled]="busy()"><app-icon name="play" [size]="12" /> {{ busy() ? 'Running…' : 'Run Test' }}</button>
                </div>
                <div class="tabs">
                  <button type="button" [class.on]="view() === 'steps'" (click)="setView('steps')">Steps</button>
                  <button type="button" [class.on]="view() === 'history'" (click)="setView('history')">History</button>
                  <button type="button" [class.on]="view() !== 'steps' && view() !== 'history'" (click)="setView(view() === 'steps' || view() === 'history' ? 'yaml' : view())">Code</button>

                </div>
              </div>
              <div class="body">
                @if (view() === 'steps') {
                  @if (t.description) { <p class="mut">{{ t.description }}</p> }
                  <div class="meta">
                    @if (varsText()) { <span>Defaults <b>{{ varsText() }}</b></span> }
                    @if (t.params?.length) { <span>Runs {{ t.params?.length }}× <b>{{ paramsText() }}</b></span> }
                    @if (t.data) { <span>Data <b>{{ t.data }}</b> <span class="mut">- one run per row</span></span> }
                    @if (t.restore?.length) { <span>Afterwards <b>{{ restoreText() }} put back</b></span> }
                    @if (t.qtest?.testCase) { <span>qTest <b>{{ t.qtest?.testCase }}</b></span> }
                    @if (t.dependsOn?.length) { <span>Depends on <b>{{ t.dependsOn.join(', ') }}</b></span> }
                    @if (t.watch?.syslog?.length || t.watch?.traps?.length) { <span>Fails on <b>{{ watchText(t) }}</b></span> }
                    @if (t.configDiff) { <span>Configuration <b>compared before and after</b></span> }
                    <span>After each test <b>{{ cleanupLabel() }}</b></span>
                  </div>
                  @for (r of restoreRes(); track $index) { <div class="trow" [class.bad]="!r.passed"><span>{{ r.passed ? '✓' : '✗' }}</span><span>{{ r.name }}</span><span class="mut">{{ r.target }} {{ r.error }}</span></div> }
                  @if (draft(); as d) {
                    <div class="elay">
                    <div class="emain">
                    @if (setsOpen()) {
                      <div class="setsh"><b>Settings</b><span class="mut">for the whole test</span><span class="sp"></span><button type="button" class="lnk" (click)="setsOpen.set(false)">Done</button></div>
                    <div class="sec devsec"><div class="sh">Step options <span class="mut norm">for the steps set to Default</span></div>
                      <div class="ed"><div class="row opts">
                        <label>Timeout <input type="number" min="0" class="num2" [ngModel]="d.defaults?.timeout" (ngModelChange)="setDef(d, 'timeout', $event)" placeholder="60" /> s</label>
                        <label title="Seconds to wait before each step">Wait <input type="number" min="0" class="num2" [ngModel]="d.defaults?.wait" (ngModelChange)="setDef(d, 'wait', $event)" /> s</label>
                        <label title="Run a step again until its checks pass"><input type="checkbox" [ngModel]="!!d.defaults?.retry" (ngModelChange)="setDef(d, 'retry', $event ? { timeout: 60, interval: 5 } : undefined)" /> Retry until it passes</label>
                        @if (d.defaults?.retry; as rt) { <label>for <input type="number" min="1" class="num2" [(ngModel)]="rt.timeout" /> s</label> }
                        <label><input type="checkbox" [ngModel]="!!d.defaults?.continueOnFail" (ngModelChange)="setDef(d, 'continueOnFail', $event || undefined)" /> Go on when a step fails</label>
                        <label><input type="checkbox" [ngModel]="!!d.defaults?.ignoreErrors" (ngModelChange)="setDef(d, 'ignoreErrors', $event || undefined)" /> Ignore device errors</label>
                      </div></div></div>
                    <div class="sec"><div class="sh">Test data</div>
                      <div class="ed"><div class="row opts"><span class="mut">Run the test once per row of a CSV or Excel file - its column names are variables ({{ '{{' }}vlan{{ '}}' }}).</span><span class="sp"></span>
                        @if (d.data) { <span class="tag">{{ d.data }}</span><button type="button" class="lnk" (click)="d.data = undefined; touch()">Remove</button> }
                        <button type="button" (click)="pickData(d)">{{ d.data ? 'Change…' : 'Choose a File…' }}</button></div></div></div>
                    <div class="sec"><div class="sh">While the test runs</div>
                      <div class="ed">
                        <div class="row opts"><label class="wide" title="Words, or /regex/; one per line or comma-separated. The syslog server must be on.">Fail on syslog <input class="cmdi" [ngModel]="(d.watch?.syslog ?? []).join(', ')" (ngModelChange)="setWatch(d, 'syslog', $event)" placeholder="e.g. LINK-3-UPDOWN, /%.*-[0-2]-/" /></label></div>
                        <div class="row opts"><label class="wide" title="Notification names, OIDs or text. The trap receiver must be on.">Fail on SNMP notifications <input class="cmdi" [ngModel]="(d.watch?.traps ?? []).join(', ')" (ngModelChange)="setWatch(d, 'traps', $event)" placeholder="e.g. linkDown, coldStart" /></label></div>
                        <div class="row opts"><label title="Reads each device's configuration (its device type's backup commands) before the test and after the cleanup; a difference fails the test"><input type="checkbox" [(ngModel)]="d.configDiff" /> Compare the configuration before and after (needs the cleanup on)</label>
                          @if (d.configDiff) { <label class="wide">ignore lines <input class="cmdi" [ngModel]="(d.configIgnore ?? []).join(', ')" (ngModelChange)="d.configIgnore = splitTags($event)" placeholder="regular expressions, e.g. ^! Last configuration change" /></label> }</div>
                      </div></div>
                    }
                    @for (ph of phases; track ph.k) {
                      <div class="sec ph" [class]="'ph-' + ph.k">
                        <div class="sh"><span class="pht">{{ ph.l }}</span><span class="cnt">{{ listOf(d, ph.k).length }}</span><span class="mut norm">{{ phDesc(ph.k) }}</span><span class="sp"></span><button type="button" class="lnk" (click)="addStep(ph.k)">+ Command</button><button type="button" class="lnk" (click)="addMenu($event, ph.k)" title="An API request, a curl command, a manual step, a wait, a loop, an upgrade or a traffic test">+ More ▾</button></div>
                        @for (s of listOf(d, ph.k); track $index; let i = $index; let lastI = $last) {
                          <div class="ed">
                            <div class="row"><b class="num">{{ i + 1 }}</b>
                              @if (s.run) { @if (!oneDevice(d, s)) { <input class="ses" [(ngModel)]="s.run.session" placeholder="device" aria-label="Device" list="twroles" title="The device: {{ '{{' }}role{{ '}}' }} from Devices above, or a saved session" /> }<input class="cmdi" [(ngModel)]="s.run.command" placeholder="command" aria-label="Command" (contextmenu)="varMenu($event, s)" title="Right-click: insert a variable ({{ '{{' }}port1{{ '}}' }}…), or select a part and make it a variable" /> }
                              @else if (s.http) { <input class="ses" [(ngModel)]="s.http.session" placeholder="API session (or a full URL)" aria-label="API session" /><select [(ngModel)]="s.http.method" aria-label="Method">@for (m of methods; track m) { <option [value]="m">{{ m }}</option> }</select><input class="cmdi" [(ngModel)]="s.http.url" placeholder="URL or /path" aria-label="URL" /> }
                              @else if (s.manual) { <span class="mut">manual</span><input class="cmdi" [(ngModel)]="s.manual.instruction" placeholder="what the person does or checks" aria-label="Instruction" /> }
                              @else if (s.upgrade) { <span class="mut">upgrade</span><input class="ses" [(ngModel)]="s.upgrade.session" placeholder="session" aria-label="Session" /><input class="cmdi" [(ngModel)]="s.upgrade.image" placeholder="image file or URL" aria-label="Image" /> }
                              @else if (s.call) { <span class="mut">call</span><select class="ses" [ngModel]="s.call.proc" (ngModelChange)="setCallProc(s, $event)" aria-label="Procedure"><option value="">choose a procedure</option>@for (p of procs(); track p.file) { <option [value]="p.name">{{ p.name }}</option> }</select><span class="cmd fl">{{ procParamsText(s.call.proc) }}</span> }
                              @else if (s.netconf) { <span class="mut">netconf</span><input class="ses" [(ngModel)]="s.netconf.session" placeholder="SSH session" aria-label="Session" /><select [(ngModel)]="s.netconf.op" aria-label="Operation"><option value="get">get</option><option value="get-config">get-config</option><option value="edit-config">edit-config</option><option value="rpc">rpc</option></select>
                                @if (s.netconf.op === 'get-config') { <select [(ngModel)]="s.netconf.source" aria-label="Source"><option [ngValue]="undefined">running</option><option value="candidate">candidate</option><option value="startup">startup</option></select> }
                                @if (s.netconf.op === 'edit-config') { <select [(ngModel)]="s.netconf.target" aria-label="Target"><option [ngValue]="undefined">running</option><option value="candidate">candidate (then commit)</option></select> }<span class="sp"></span> }
                              @else if (s.tgen) { <span class="mut">traffic</span><select [(ngModel)]="s.tgen.kind" aria-label="Traffic generator"><option value="stc">Spirent TestCenter</option><option value="ixnetwork">IxNetwork</option></select><input class="cmdi" [(ngModel)]="s.tgen.server" placeholder="API session or https://api-server" aria-label="Server" list="tw-api-sessions" /><label class="mut">for <input type="number" class="num2" [(ngModel)]="s.tgen.duration" placeholder="10" /> s</label> }
                              @else if (s.web) { <button type="button" class="wapp" (click)="webAppMenu($event, s.web)" [title]="'The web app the step works on: ' + (s.web.login || 'none - the address is a full URL')"><span class="wic">🌐</span><span class="ell">{{ webAppName(s.web.login) }}</span>@if (s.web.login?.startsWith('{{')) { <code>{{ s.web.login }}</code> }<span class="cv">▾</span></button>
                                <input class="cmdi wurl" [(ngModel)]="s.web.url" [placeholder]="isWebApp(s.web.login) ? '/ (the start page) or /devices' : 'https://device-or-app/login'" aria-label="Page address" />
                                <button type="button" class="wpill" [class.on]="webSetOpen(ph.k, i)" (click)="toggleWebSet(ph.k, i)" title="Browser, page size, certificates, pictures, step options">{{ webPill(s.web) }} ⚙</button>
                                @if (resOf(ph.k, i); as lr) { <span class="wlast" [class.ok]="lr.passed" [class.bad]="!lr.passed && !lr.skipped">{{ lr.skipped ? '– skipped' : lr.passed ? '✓ passed' : '✗ failed' }} · {{ dur(lr.ms) }}</span> } }
                              @else if (s.traffic) { <span class="mut">traffic test</span><input class="cmdi" [(ngModel)]="s.traffic.test" placeholder="saved lab traffic test" aria-label="Traffic test" /> }
                              @else { <span class="cmd fl">{{ s.loop ? 'loop' : text(s) }}</span><span class="sp"></span> }
                              <button type="button" class="lnk" (click)="moveStep(ph.k, i, -1)" [disabled]="i === 0" title="Move up">↑</button><button type="button" class="lnk" (click)="moveStep(ph.k, i, 1)" [disabled]="lastI" title="Move down">↓</button><button type="button" class="lnk" (click)="removeStep(ph.k, i)" title="Remove the step">✕</button>
                            </div>
                            @if (s.call) {
                              <div class="row opts">@for (pn of procParams(s.call.proc); track pn.k) { <label>{{ pn.k }} <input class="pin" [ngModel]="s.call.with?.[pn.k] ?? ''" (ngModelChange)="setCallArg(s, pn.k, $event)" [placeholder]="pn.d || varHint" /></label> } @empty { <span class="mut">{{ s.call.proc ? 'no parameters' : 'Choose a procedure (Procedures in the left bar).' }}</span> }</div>
                            }
                            @if (s.tgen; as tg) {
                              <div class="row opts"><label class="wide" title="The saved configuration in the tests folder: STC .xml or .tcc, IxNetwork .ixncfg or .json (empty: what the server has loaded)">Configuration <input class="cmdi" [(ngModel)]="tg.config" placeholder="tgen/l3-traffic.xml" /></label></div>
                              <div class="row opts"><label class="wide" title="Chassis ports in the order of the configuration's ports (empty: the configuration's own)">Chassis ports <input class="cmdi" [ngModel]="(tg.ports ?? []).join(', ')" (ngModelChange)="tg.ports = splitPorts($event); touch()" placeholder="10.1.1.5/1/1, 10.1.1.5/1/2" /></label>
                                <label title="Start the configuration's protocols (devices, BGP…) before the traffic"><input type="checkbox" [(ngModel)]="tg.protocols" /> Start protocols</label>
                                @if (tg.kind === 'ixnetwork') { <label title="An existing session (Windows API servers: 1); empty makes one">Session <input class="pin" [(ngModel)]="tg.session" placeholder="new" /></label> }</div>
                              <div class="row opts"><span class="mut">The result is JSON: check <b>loss &lt; 0.01</b>, <b>streams[0].loss &lt; 0.1</b>, <b>streams[1].latencyUs &lt; 50</b> or <b>rx &gt; 0</b> (Result checks below). The API session holds the server's address and login.</span></div>
                            }
                            @if (s.web; as w) {
                              @if (webSt(); as ws) { @if (!ws.ready) { <div class="row opts webst"><span>🌐 {{ ws.node ? 'Playwright is not set up on this computer yet - recording, step-by-step runs and ⌖ Pick need it (once, about 150 MB).' : 'Node.js is not installed - web steps need it.' }} <small class="mut">{{ ws.hint }}</small></span><span class="sp"></span>@if (ws.node) { <button type="button" class="pri" (click)="webSetup()" [disabled]="webBusy()">{{ webBusy() ? 'Setting up…' : 'Set Up Playwright' }}</button> }</div>@if (webBusy()) { <div class="row opts"><code class="mut">{{ webLine() }}</code></div> } } }
                              @let dw = dbgOn(w);
                              @let lw = webRun(ph.k, i, w);
                              @if (dw) {
                                <div class="wdbg"><span class="ok">● browser open</span><button type="button" (click)="dbgRunAll(w)" [disabled]="dbgBusy()" title="Run all the actions in the open browser, from its page">▶ Run All</button><button type="button" (click)="dbgRestart()" [disabled]="dbgBusy()" title="Back to the step's page">↺ Start Over</button><button type="button" (click)="dbgClose()">■ Close Browser</button>
                                  @if (dbgMsg()) { <span class="mut">{{ dbgMsg() }}</span> }<span class="sp"></span>@if (selActs().size) { <span class="mut">{{ selActs().size }} selected</span> }</div>
                              }
                              @if (webSetOpen(ph.k, i)) {
                                <div class="wset">
                                  <label title="A web app session (its address, login and settings), or any saved session whose user name and password fill {{ '{{' }}username}} and {{ '{{' }}password}}. A role works too: {{ '{{' }}app}}. Passwords never go in the test.">Web app <input class="pin lg" [(ngModel)]="w.login" list="twsess" placeholder="none: a full address" /></label>
                                  @if (!isWebApp(w.login)) { <label title="Keep the login (cookies) under this name: the next web steps with the same name start logged in">Keep login as <input class="pin" [(ngModel)]="w.state" placeholder="optional" /></label> }
                                  <label>Browser <select [(ngModel)]="w.browser" aria-label="Browser"><option [ngValue]="undefined">Chromium</option><option value="firefox">Firefox</option><option value="webkit">WebKit (Safari)</option></select></label>
                                  <label title="The page size: a screen resolution or a custom one, e.g. 390x844 for a phone">Size <app-size-pick [(value)]="w.viewport" [empty]="isWebApp(w.login) ? 'As the app' : 'Browser default'" /></label>
                                  <label title="Accept self-signed certificates (device web pages)"><input type="checkbox" [(ngModel)]="w.insecure" /> Self-signed certificate OK</label>
                                  <label title="Show the browser window while it runs (default: hidden)"><input type="checkbox" [(ngModel)]="w.show" /> Show the browser</label>
                                  <label title="Record a Playwright trace (every action with the page before and after) - open it with npx playwright show-trace"><input type="checkbox" [(ngModel)]="w.trace" /> Trace</label>
                                  <label title="Pictures of the page in the run report: of a failure (the default), after every action as well (they show next to the actions here), or none">Pictures <select [ngModel]="w.pictures === 'all' || w.pictures === 'none' ? w.pictures : ''" (ngModelChange)="setPictures(w, $event, $any($event.target))"><option value="">of a failure</option><option value="all">after every action</option><option value="none">none</option></select></label>
                                  <button type="button" class="lnk" (click)="backend.openURL(webGuide)">Web testing guide</button>
                                </div>
                              }
                              <div class="wacts">
                              @for (a of w.actions ?? []; track $index; let k = $index) {
                                @let ar = dw ? dbg()?.res?.[k] : undefined;
                                @let st = ar ? (ar.ok ? (ar.healed ? 'hw' : 'ok') : 'bad') : lw.st[k];
                                <div class="wa" [class.open]="actOpen(w, k)" [class.cur]="dw && dbg()?.at === k" [class.drop]="dropAt() === k" [class.chk]="a.do.startsWith('expect')"
                                  draggable="true" (dragstart)="dragAct(w, k, $event)" (dragover)="overAct(w, k, $event)" (dragleave)="dropAt.set(-1)" (drop)="dropAct(w, k, $event)" (dragend)="dropAt.set(-1)">
                                  <div class="war" (click)="toggleAct(w, k)">
                                    <span class="wh" title="Drag to move">⋮⋮</span>
                                    <button type="button" class="wn" [class.sel]="isSelAct(w, k)" (click)="selAct(w, k, $event); $event.stopPropagation()" title="Select (Shift: a range) - to save as a procedure or record after it">{{ k + 1 }}</button>
                                    @if (lw.pic[k] && picUrl(lw.pic[k]); as u) { <img class="wth" [src]="u" alt="" (click)="bigPic.set(u); $event.stopPropagation()" title="The page after this action in the last run - click to enlarge" /> }
                                    <span class="wsay">@if (a.do.startsWith('expect')) { <b class="ck">✓</b> }{{ actWords(a) }}@if (a.do === 'block') { @for (p of blockParams(a.target); track p) { <span class="wv">{{ p }} = {{ a.with?.[p] || '…' }}</span> } }</span>
                                    <span class="wsts" [class]="'wsts ' + (st ?? '')" [title]="ar ? (ar.ok ? (ar.note || 'done') + (ar.healed ? ' - healed: ' + ar.healed : '') : ar.error || 'failed') : st ? 'in the last run' : ''">{{ st === 'ok' ? '✓' : st === 'hw' ? '⚠' : st === 'bad' ? '✗' : dw && dbg()?.at === k ? '➜' : '' }}</span>
                                    <span class="wtools">
                                      <button type="button" class="lnk" (click)="dbgRunOne(w, k); $event.stopPropagation()" [disabled]="dbgBusy() || !webSt()?.ready" [title]="needPw('Run just this action in the browser (it opens at the step page when it is not open)')">▶</button>
                                      <button type="button" class="lnk" (click)="rmWeb(w, k); $event.stopPropagation()" title="Delete the action">✕</button></span>
                                  </div>
                                  @if (actOpen(w, k)) {
                                    <div class="wae">
                                      <select [(ngModel)]="a.do" aria-label="Action" [title]="webAct(a.do)?.t ?? ''">@for (g of webGroups; track g.g) { <optgroup [label]="g.g">@for (o of g.a; track o.k) { <option [value]="o.k">{{ o.l }}</option> }</optgroup> }</select>
                                      @if (a.do === 'block') {
                                        <input class="cmdi" [(ngModel)]="a.target" list="twblocks" placeholder="procedure name" aria-label="Procedure" (focus)="loadBlocks()" />
                                        @for (p of blockParams(a.target); track p) { <label class="bp">{{ p }} <input class="pin sm" [ngModel]="a.with?.[p] ?? ''" (ngModelChange)="setWith(a, p, $event)" [placeholder]="p" /></label> }
                                      } @else {
                                        @if (webAct(a.do)?.tg !== false) { <span class="wtg"><input class="cmdi" [(ngModel)]="a.target" [attr.list]="'twtgt'" [placeholder]="webTgtHint(a.do)" aria-label="Target" /><button type="button" class="lnk" (click)="dbgPick(w, k)" [disabled]="dbgBusy() || !webSt()?.ready" [title]="needPw('Pick on the page: click the element in the browser and its target is filled in')">⌖ Pick</button></span> }
                                        @if (webAct(a.do)?.v !== false) { <input class="pin" [(ngModel)]="a.value" [type]="a.secret || ((a.do === 'fill' || a.do === 'type') && isSecretTarget(a.target) && a.value !== '{{password}}') ? 'password' : 'text'" [placeholder]="webValHint(a.do)" aria-label="Value" /> }
                                      }
                                    </div>
                                    <div class="walt">@if (a.alt?.length) { <span class="mut">Also found by: </span>@for (x of a.alt; track x) { <code>{{ x }}</code> } <span class="mut">(used when the GUI changes)</span> · }
                                      <button type="button" class="lnk" (click)="dbgRunTo(w, k)" [disabled]="dbgBusy() || !webSt()?.ready">▶ Run to here</button>
                                      <button type="button" class="lnk" (click)="dupWeb(w, k)">Duplicate</button>
                                      <button type="button" class="lnk" [disabled]="k === 0" (click)="mvWeb(w, k, -1); openAct(w, k - 1)">Move up</button>
                                      <button type="button" class="lnk" [disabled]="k === (w.actions?.length ?? 0) - 1" (click)="mvWeb(w, k, 1); openAct(w, k + 1)">Move down</button>
                                      <button type="button" class="lnk bad" (click)="rmWeb(w, k)">Delete</button></div>
                                  }
                                </div>
                              } @empty { <div class="wempty mut">No actions yet: <b>● Record</b> them in the browser, <b>✦ Ask AI</b>, or <b>+ Action</b>.</div> }
                              </div>
                              @let hl = healedOf(ph.k, i, w);
                              @if (hl.length) { <div class="row opts heal">⚠ <span><b>Healed in the last run:</b> @for (h of hl; track h.n) { action {{ h.n }} <code>{{ h.from }}</code> was not found, <code>{{ h.to }}</code> was. }</span><span class="sp"></span><button type="button" (click)="useHealed(w, hl)">Use the New Targets</button></div> }
                              <div class="wfoot">
                                <button type="button" class="rec" (click)="dw ? dbgRecord(w) : webRecord(w)" [disabled]="(webRec() && !dw) || dbgBusy() || !webSt()?.ready" [title]="!webSt()?.ready ? needPw('') : dw ? 'Record in the open browser: the actions go after the selected one (or at the end)' : 'Opens the page in a browser with the recording toolbar: use the app, Check what must be true, then Done'">{{ webRec() ? (dw ? '■ Stop Recording' : '● Recording…') : (w.actions?.length ? '● Record More' : '● Record') }}</button>
                                <button type="button" (click)="(w.actions ??= []).push({ do: 'click', target: '' }); openAct(w, w.actions!.length - 1); touch()">+ Action</button>
                                <button type="button" (click)="procMenu($event, w)" title="Insert a web procedure (shared actions with values) - or record a new one">+ Procedure ▾</button>
                                <button type="button" class="ib" (click)="webMore($event, w)" title="More: Debug in Browser, Save as Procedure">⋯</button>
                              </div>
                              <div class="wfoot2">
                                <span class="wai" [class.busy]="aiWebBusy() === w"><span class="aic">✦</span><input [(ngModel)]="aiWebText" (keydown.enter)="askAIWeb(w, ph.k, i)" [disabled]="!!aiWebBusy()" placeholder="Ask AI - describe the next actions, e.g. open the device and check it is Connected" aria-label="Ask AI" />
                                  @if (aiWebBusy() === w) { <span class="mut">{{ aiWebCount() }} recorded</span><button type="button" class="pri" (click)="takeAIWeb(w)">Add Them</button><button type="button" (click)="cancelAIWeb()">Cancel</button> }
                                  @else { <button type="button" (click)="askAIWeb(w, ph.k, i)" [disabled]="!aiWebText.trim() || !!aiWebBusy() || !webSt()?.ready">Ask</button> }</span>
                              </div>
                              <datalist id="twsess">@for (p of webSessions(); track p) { <option [value]="p"></option> }</datalist>
                              <datalist id="twblocks">@for (b of blocks(); track b.file) { <option [value]="b.file">{{ b.name }}</option> }</datalist>
                              <datalist id="twtgt"><option value="text="></option><option value="label="></option><option value="placeholder="></option><option value="role=button[name=&quot;&quot;]"></option><option value="testid="></option><option value="title="></option><option value="frame=iframe >> "></option></datalist>
                            }
                            @if (s.netconf; as nc) {
                              @if (nc.op === 'get' || nc.op === 'get-config' || !nc.op) { <div class="row opts"><label class="wide" title="A subtree filter: only these parts of the data come back">Filter <textarea class="cmdi body" [(ngModel)]="nc.filter" rows="2" placeholder="<vlans xmlns=&quot;http://openconfig.net/yang/vlan&quot;/>"></textarea></label></div> }
                              @if (nc.op === 'edit-config') { <div class="row opts"><label class="wide">Config <textarea class="cmdi body" [(ngModel)]="nc.config" rows="4" placeholder="<vlans xmlns=&quot;...&quot;><vlan><vlan-id>200</vlan-id>…</vlan></vlans>"></textarea></label></div> }
                              @if (nc.op === 'rpc') { <div class="row opts"><label class="wide">RPC <textarea class="cmdi body" [(ngModel)]="nc.rpc" rows="3" placeholder="<get-schema xmlns=&quot;…&quot;>…</get-schema>"></textarea></label></div> }
                              <div class="row opts"><span class="mut">Check the answer with <b>XML path =</b> (data.vlans.vlan[0].name = v200) or contains. Port <input type="number" class="num2" [(ngModel)]="nc.port" placeholder="830" /></span></div>
                            }
                            @if (s.http && s.http.method !== 'GET' && s.http.method !== 'DELETE') { <div class="row opts"><label class="wide">Body <textarea class="cmdi body" [(ngModel)]="s.http.body" rows="2" placeholder='{"name": "v200", "vid": 200}'></textarea></label></div> }
                            @if (s.loop; as lp) {
                              <div class="row opts"><label>For each <input class="pin" [(ngModel)]="lp.var" placeholder="port" /></label>
                                <label class="wide" title="Values separated by commas, a range like 1:1-1:8, or a variable like {{ '{{' }}list{{ '}}' }}">in <input class="cmdi" [ngModel]="loopSrc(lp)" (ngModelChange)="setLoopSrc(lp, $event)" placeholder="1:1-1:8  or  10, 20, 30  or  {{ '{{' }}list{{ '}}' }}" /></label></div>
                              @for (ls of lp.steps; track $index; let k = $index) {
                                <div class="row opts inner"><span class="mut">{{ k + 1 }}</span>
                                  @if (ls.run) { <input class="ses" [(ngModel)]="ls.run.session" placeholder="session" /><input class="cmdi" [(ngModel)]="ls.run.command" placeholder="command - use {{ '{{' }}{{ lp.var }}{{ '}}' }}" /> } @else { <span class="cmd fl">{{ text(ls) }}</span> }
                                  <button type="button" class="lnk" (click)="lp.steps.splice(k, 1); touch()" title="Remove">✕</button></div>
                              }
                              <div class="row opts inner"><button type="button" class="lnk" (click)="lp.steps.push({ run: { session: lastSession(), command: '' } }); touch()">+ Step in the loop</button></div>
                            }
                            @if (!s.call) {
                            @if (!s.web || webSetOpen(ph.k, i)) {
                            @let om = optMode(s);
                            <div class="row opts optbar">
                              <span class="seg sm" role="radiogroup" aria-label="Step options">
                                <button type="button" [class.on]="om === 'default'" (click)="setOptMode(s, 'default')" [title]="'Step options of the test: ' + defaultsText(d)">Default</button>
                                <button type="button" [class.on]="om === 'custom'" (click)="setOptMode(s, 'custom')" title="Its own timeout, wait, retry…">Custom</button>
                                <button type="button" [class.on]="om === 'none'" (click)="setOptMode(s, 'none')" title="No step options: run it once, stop the test when it fails">None</button></span>
                              <span class="mut ell">{{ om === 'default' ? defaultsText(d) : om === 'custom' ? stepOptText(s) : 'runs once; a failure stops the test' }}</span><span class="sp"></span>
                              <button type="button" class="lnk sm" [class.on]="isMore(ph.k, i) || moreSet(s)" (click)="toggleMore(ph.k, i)" title="Run only if…, parse into a table, sample over time, what is expected in words">⚙ {{ moreText(s) || 'More' }}</button>
                            </div>
                            @if (om === 'custom') {
                              <div class="row opts optpanel"><label>Timeout <input type="number" min="0" class="num2" [(ngModel)]="s.timeout" /> s</label>
                                <label title="Seconds to wait before the step">Wait <input type="number" min="0" class="num2" [(ngModel)]="s.wait" /> s</label>
                                <label title="Run the step again until its checks pass, for at most this many seconds"><input type="checkbox" [ngModel]="!!s.retry" (ngModelChange)="s.retry = $event ? { timeout: 60, interval: 5 } : undefined" /> Retry until it passes</label>
                                @if (s.retry) { <label>for <input type="number" min="1" class="num2" [(ngModel)]="s.retry.timeout" /> s</label> }
                                <label><input type="checkbox" [(ngModel)]="s.continueOnFail" /> Go on when it fails</label>
                                @if (s.run) { <label title="Do not fail the step when the device answers with an error message"><input type="checkbox" [(ngModel)]="$any(s.run).ignoreErrors" /> Ignore device errors</label> }</div>
                            }
                            @if (isMore(ph.k, i)) {
                              <div class="row opts optpanel">
                                <label title="Run the step only when this holds, e.g. {{ '{{' }}dut.device_type{{ '}}' }} == VOSS (==, !=, contains, ~, >, <)">If <input class="pin" [(ngModel)]="s.if" placeholder="always" /></label>
                                @if (s.run || s.http || s.snmp || s.netconf) { <label title="Run the step several times and check the minimum, maximum, average, change or rate of a value (CPU, counters, errors)"><input type="checkbox" [ngModel]="!!s.sample" (ngModelChange)="s.sample = $event ? { count: 5, interval: 10 } : undefined; touch()" /> Sample over time</label>
                                  @if (s.sample) { <label><input type="number" min="2" class="num2" [(ngModel)]="s.sample.count" /> times, every <input type="number" min="0" class="num2" [(ngModel)]="s.sample.interval" /> s</label> } }
                                @if (s.run) { <label title="Turn the output into a table with a TextFSM template: auto (templates/index) or a file in templates/">Parse <input class="pin" [(ngModel)]="s.run.parse" placeholder="off" list="tw-parse" /></label> }
                                <label class="wide" title="What should happen, in words - shown with the step and in the reports">Expected <input class="cmdi exp" [(ngModel)]="s.expected" placeholder="in words, optional - e.g. VLAN 200 lists port 1:2 tagged" /></label></div>
                            }
                            }
                            <div class="chips">@for (e of s.expect ?? []; track $index; let j = $index) { <span class="chip"><button type="button" class="lnk ckedit" (click)="openCk(ph.k, i); editCheck(s, j, ck(ph.k, i))" title="Change the check (it moves to the box on the right; + Check puts it back)">{{ label(e) }}</button> <button type="button" class="x" (click)="removeCheck(s, j)" title="Remove the check">×</button></span> }
                              @if (!s.loop && !s.manual && !s.web && ck(ph.k, i); as c) { @if (!ckOpen().has(ph.k + i) && !c.v) { <button type="button" class="addchip" (click)="openCk(ph.k, i)" title="Add a check: the output contains, does not contain, matches…">+ check</button> } @else {
                                <span class="addck"><select [(ngModel)]="c.k" aria-label="Check">@for (k of checkKinds(s); track k.k) { <option [value]="k.k">{{ k.l }}</option> }</select>
                                  <input [(ngModel)]="c.v" [placeholder]="checkHint(c.k)" aria-label="Value" (keydown.enter)="addCheck(s, c)" (contextmenu)="varMenu($event, s)" title="Right-click: insert a variable" /><button type="button" (click)="addCheck(s, c)" [disabled]="!c.v.trim()">+ Check</button></span>
                              } }</div>
                            } @else { <div class="row opts"><label><input type="checkbox" [(ngModel)]="s.continueOnFail" /> Go on when it fails</label><label title="Run the procedure only when this holds">If <input class="pin" [(ngModel)]="s.if" placeholder="always" /></label><span class="mut">The procedure's own steps have their checks.</span></div> }
                          </div>
                        } @empty { <div class="none">no {{ ph.l.toLowerCase() }} steps</div> }
                      </div>
                    }
                    <datalist id="tw-parse"><option value="auto"></option></datalist>
                    <div class="row"><span class="mut">Saving writes the test's YAML and its Go, Python and TCL code again.</span></div>
                    </div>
                    <aside class="eside">
                      <div class="sbox"><h4>Devices</h4>
                        @for (r of testRoles(d); track r) {
                          <div class="srole"><code class="rolec">{{ '{{' }}{{ r }}{{ '}}' }}</code><span class="sk">{{ roleKind(d, r) }}</span><span class="sp"></span>
                            @if (roleKind(d, r) !== 'web app') { <button type="button" class="lnk sm" [class.on]="needsOpen().has(r)" (click)="toggleNeeds(r)" title="What the test needs of this device (a testbed without it skips the test)">Needs</button> }
                            <button type="button" class="lnk sm" (click)="renameTestRole(d, r)" title="Rename it in every step of the test">Rename</button></div>
                          <input class="cmdi srin" [ngModel]="d.vars?.[r] ?? ''" (ngModelChange)="d.vars![r] = $event; touch()" [attr.list]="roleKind(d, r) === 'web app' ? 'twsess' : 'twsesslist'" [attr.aria-label]="r" />
                          @if (roleKind(d, r) !== 'web app') { <span class="mut sm">{{ portsHint(r) }}</span> }
                          @if (needsOpen().has(r)) {
                          @let q = d.requires?.[r] ?? {};
                          <div class="row needs" title="What the test needs of this device: a testbed that does not have it skips the test with the reason (not a failure)"><span class="mut">Needs</span>
                            <select [ngModel]="q.deviceType ?? ''" (ngModelChange)="setReq(d, r, 'deviceType', $event)"><option value="">any device type</option>@for (t of deviceTypeNames(); track t) { <option [value]="t">{{ t }}</option> }</select>
                            <select [ngModel]="q.stack === undefined ? '' : q.stack ? 'yes' : 'no'" (ngModelChange)="setReq(d, r, 'stack', $event === '' ? undefined : $event === 'yes')"><option value="">stack or not</option><option value="yes">a stack</option><option value="no">not a stack</option></select>
                            <label>at least <input type="number" min="0" class="num2" [ngModel]="q.ports" (ngModelChange)="setReq(d, r, 'ports', $event || undefined)" placeholder="0" /> ports</label>
                            <input class="pin" [ngModel]="(q.tags ?? []).join(', ')" (ngModelChange)="setReq(d, r, 'tags', splitTags($event))" placeholder="testbed tags, e.g. l3" /></div>
                          }
                        } @empty { <div class="mut sm">The steps name their sessions themselves.</div> }
                        <div class="mut sm sfoot">A testbed or the Run panel can put other ones in.</div>
                      <datalist id="twroles">@for (r of testRoles(d); track r) { <option [value]="'{{' + r + '}}'"></option> }</datalist>
                      <datalist id="twsesslist">@for (p of allSessions(); track p) { <option [value]="p"></option> }</datalist>
                      </div>
                      <div class="sbox"><h4>Values</h4>
                        @for (k of testValues(d); track k) {
                          <div class="sval"><code>{{ '{{' }}{{ k }}{{ '}}' }}</code><input class="pin" [ngModel]="d.vars?.[k] ?? ''" (ngModelChange)="d.vars![k] = $event; touch()" [attr.aria-label]="k" /><button type="button" class="lnk" (click)="rmValue(d, k)" title="Remove">✕</button></div>
                        } @empty { <div class="mut sm">None - {{ '{{name}}' }} in a step is a value the run or a testbed can change.</div> }
                        <button type="button" class="lnk" (click)="addValue(d)">+ Value</button>
                      </div>
                      <div class="sbox"><h4>Settings</h4>
                        @for (x of settingsSummary(d); track x[0]) { <div class="sset"><span>{{ x[0] }}</span><b>{{ x[1] }}</b></div> }
                        <button type="button" class="lnk" (click)="setsOpen.set(!setsOpen())">{{ setsOpen() ? 'Hide' : 'Change…' }}</button>
                      </div>
                    </aside>
                    </div>
                  } @else {
                    @for (ph of phases; track ph.k) {
                      @if (ph.k === 'steps' || listOf(t, ph.k).length) {
                        <div class="sec">
                          <div class="sh">{{ ph.k === 'cleanup' ? 'Cleanup' : ph.l }}<span class="sp"></span><span class="norm">{{ secSummary(ph.k) }}</span></div>
                          <div class="srow hd"><span>#</span><span>Device</span><span>Command</span><span>Checks</span><span>Result</span><span class="r">Time</span></div>
                          @for (s of listOf(t, ph.k); track $index; let i = $index) {
                            @let r = resOf(ph.k, i);
                            <div class="srow" [attr.aria-expanded]="isOpen(ph.k, i)" [class.f]="r && !r.passed && !r.skipped" [class.open]="isOpen(ph.k, i)" (click)="toggleRow(ph.k, i)" (keydown.enter)="toggleRow(ph.k, i)" (contextmenu)="stepMenu($event, ph.k, i)" tabindex="0" role="button">
                              @if (ph.k === 'steps') { <span class="bpn" [class.on]="hasBreak(i)" (click)="toggleBreak(i); $event.stopPropagation()" [title]="hasBreak(i) ? 'Breakpoint: the run stops before this step - click to remove' : 'Click to stop before this step (breakpoint)'">{{ i + 1 }}</span> } @else { <span class="mut">{{ i + 1 }}</span> }<span class="dev" [title]="target(s)">{{ s.loop ? 'loop' : s.manual ? 'you' : shortDev(target(s)) }}</span><span class="cmd" [class.manual]="!!s.manual">@if (s.web; as w) { <span class="wflow"><span class="wpath">{{ w.url || '/' }}</span>@for (x of webFlow(w).words; track $index) { <i>›</i>{{ x }} }</span> } @else { {{ text(s) }} }@if (logicText(s)) { <span class="logic">{{ logicText(s) }}</span> }@if (s.expected) { <span class="expd">Expected: {{ s.expected }}</span> }</span>
                              <span class="chips">@for (e of s.expect ?? []; track $index; let j = $index) { <span class="chip" [class.b]="r?.checks?.[j]?.passed === false">{{ r?.checks?.[j]?.passed === false ? '✗' : r ? '✓' : '' }} {{ label(e) }}</span> }
                                @if (s.web; as w) { @for (c of webFlow(w).checks; track $index) { <span class="chip">{{ c }}</span> } @empty { @if (!(s.expect?.length)) { <span class="chip w" title="Add a check: record with the toolbar's Check buttons, or + Action → Check">○ not checked</span> } } }
                                @else if (!(s.expect?.length) && !s.manual) { @if (isReadStep(s)) { <span class="chip w" title="A read step without checks passes whatever it shows: select text in its output and right-click to add one">○ not checked</span> } @else { <span class="chip n" title="Fails when the device answers with an error message (the device type's error patterns)">✓ device errors</span> } }@if (s.manual) { <span class="chip m">you decide</span> }</span>
                              <span [class]="r ? (r.skipped ? 'mut' : r.passed ? 'ok' : 'bad') : 'mut'">{{ r ? (r.skipped ? '– skipped' : r.passed ? '✓ passed' : '✗ failed') : '' }}</span>
                              <span class="r mut">{{ r && !r.skipped ? dur(r.ms) : '' }}</span>
                            </div>
                            @if (isOpen(ph.k, i)) {
                              <div class="exp">
                                <div>
                                  @if (s.loop) {
                                    <h4>Each value</h4>
                                    <div class="ev">@for (x of resList(ph.k)[i] ?? []; track $index) { <span [class]="x.skipped ? 'mut' : x.passed ? 'ok' : 'bad'">{{ x.skipped ? '–' : x.passed ? '✓' : '✗' }}</span><span>{{ x.name }}</span><span [class.bad]="!x.passed">{{ x.error || failedCheck(x) }}</span> } @empty { <span></span><span class="mut">not run yet</span><span></span> }</div>
                                    <p class="mut">Change the loop in the YAML (Code → YAML).</p>
                                  }
                                  <h4>Checks - expected vs got</h4>
                                  @if (r?.checks?.length) { <div class="ev">@for (c of r?.checks ?? []; track $index) { <span [class]="c.passed ? 'ok' : 'bad'">{{ c.passed ? '✓' : '✗' }}</span><span>{{ c.what }}</span><span [class.bad]="!c.passed">{{ c.got }}</span> }</div> }
                                  @else { <p class="mut">{{ s.expect?.length ? 'Run the test to see what came back.' : 'No checks: select text in the output and right-click to add one.' }}</p> }
                                  @if (r?.error) { <p class="bad">{{ r?.error }}</p> }
                                  @if (varList(r); as vl) { @if (vl.length) { <h4>Variables</h4><div class="vars">@for (x of vl; track x[0]) { <span class="vk">{{ '{{' + x[0] + '}}' }}</span><span class="vv" [class.mut]="x[1] === '(not set)'">{{ x[1] }}</span> }</div> } }
                                  @if (ph.k === 'steps' && !busy()) { <div class="dbg"><button type="button" (click)="runStep(i, 'only')" title="Run only this step on the devices (no setup, no cleanup)"><app-icon name="play" [size]="11" /> Run This Step</button><button type="button" (click)="runStep(i, 'from')" title="Run the setup, then this step and the rest">Run From Here</button></div> }
                                  @if (r && !r.passed && !r.skipped && last()[sel()]; as lr) { <div class="row"><button type="button" (click)="explain(lr)">✦ Explain</button>
                                    @if (lr.defect) { <button type="button" class="lnk" (click)="backend.openURL(lr.defectUrl!)">{{ lr.defect }}</button> } @else if (report()?.dir) { <button type="button" (click)="createDefect(report()!.dir, lr)">Create Jira Defect</button> }</div> }
                                </div>
                                <div>
                                  @if (r?.table; as tb) { <h4>Table · {{ tb.rows.length }} rows</h4><div class="tbw"><table class="ptab"><tr>@for (h of tb.header; track $index) { <th>{{ h }}</th> }</tr>@for (x of tb.rows.slice(0, 200); track $index) { <tr>@for (c of x; track $index) { <td>{{ c }}</td> }</tr> }</table></div> }
                                  @if (diffOf(ph.k, i); as df) {
                                    <h4>Changes since the last pass ({{ df.when }})</h4>
                                    @if (df.lines.length) { <pre class="diff">@for (l of df.lines; track $index) { <span [class]="l[0] === '+' ? 'da' : 'dr'">{{ l }}</span>
} </pre> } @else { <p class="mut">The output is the same as in that run.</p> }
                                  } @else if (r && !r.skipped && r.output) { <button type="button" class="lnk cmp" (click)="compare(ph.k, i)" title="Show which lines of the output differ from the last run where this test passed">Compare with the last pass</button> }
                                  <h4 class="oh">Output · select text, right-click to check it<span class="sp"></span>@if (s.run && r?.output) { <button type="button" class="sbtn" (click)="openTable(ph.k, i, s, r!.output!)" title="Turn this output into a table (or fields) by clicking - no template to write">▦ Make a Table…</button> }</h4>
                                  @if (picsOf(r?.output); as ps) { @if (ps.length) { <div class="pics">@for (p of ps; track p.path) { <button type="button" class="pic" [class.fail]="p.fail" (click)="showPic(p.path)" [title]="p.label">@if (picUrl(p.path); as u) { <img [src]="u" alt="" /> } @else { <span class="mut">{{ p.label }}</span> }</button> }</div> } }
                                  @if (r?.output) { <pre (contextmenu)="outMenu($event, { i: i, phase: ph.k === 'steps' ? '' : ph.k })">{{ r?.output }}</pre> } @else { <p class="mut">{{ r ? 'no output' : 'not run yet' }}</p> }
                                </div>
                              </div>
                            }
                          }
                        </div>
                      }
                    }
                  }
                } @else if (view() === 'history') {
                  @if (hist(); as h) {
                    <div class="kpis">
                      <div class="kpi"><b [class]="h.failed ? 'kbad' : 'kok'">{{ rate(h) }}%</b><span>passed ({{ h.passed }} of {{ h.passed + h.failed }})</span></div>
                      <div class="kpi"><b [class.kwarn]="h.flips >= 2">{{ h.flips }}</b><span>{{ h.flips >= 2 && h.passed && h.failed ? 'flips - flaky' : 'flips between passed and failed' }}</span></div>
                      <div class="kpi"><b>{{ dur(h.avgMs) }}</b><span>average time</span></div>
                      <div class="kpi"><b>{{ h.runs.length }}</b><span>runs kept</span></div></div>
                    <div class="dots" title="Oldest left, newest right">@for (r of h.runs.slice().reverse(); track $index) { <i [class]="r.skipped ? 'dw' : r.passed ? 'dp' : 'df'" [title]="r.at.slice(0, 16).replace('T', ' ') + ' ' + (r.skipped ? 'skipped' : r.passed ? 'passed' : 'failed: ' + (r.problem ?? ''))"></i> }</div>
                    @if (h.byVersion.length > 1 || (h.byVersion.length === 1 && h.byVersion[0].version !== 'unknown')) {
                      <div class="sec"><div class="sh">By software version</div>
                        @for (v of h.byVersion; track v.version) { <div class="vrow"><span>{{ v.version }}</span><span class="ok">{{ v.passed }} passed</span><span [class.bad]="v.failed">{{ v.failed }} failed</span><span class="bar2"><i class="okb" [style.flex]="v.passed"></i><i class="badb" [style.flex]="v.failed"></i></span></div> }</div>
                    }
                    <div class="sec"><div class="rhd"><span></span><span>When</span><span>Problem</span><span class="r">Time</span></div>
                      @for (r of h.runs; track $index) {
                        <div class="rrow" (click)="openRunReport(r.dir)" (keydown.enter)="openRunReport(r.dir)" tabindex="0" role="button"><span [class]="r.skipped ? 'warn' : r.passed ? 'ok' : 'bad'">{{ r.skipped ? '○' : r.passed ? '✓' : '✗' }}</span>
                          <span>{{ r.at.slice(0, 16).replace('T', ' ') }}@if (r.testbed) { <span class="mut"> · {{ base(r.testbed) }}</span> }@if (verText(r.versions)) { <span class="mut"> · {{ verText(r.versions) }}</span> }</span>
                          <span class="mut">{{ r.problem }}</span><span class="r mut">{{ dur(r.ms) }}</span></div>
                      }</div>
                  } @else { <p class="mut">No runs of this test yet.</p> }
                } @else {
                  <div class="codebar">
                    <div class="lang" role="tablist" aria-label="Language">@for (v of views.slice(1); track v.k) { <button type="button" role="tab" [class.on]="view() === v.k" (click)="yamlEdit.set(false); setView(v.k)">{{ v.l }}</button> }</div>
                    <span class="mut fl" [title]="runHint()">{{ view() === 'yaml' ? (yamlEdit() ? 'Editing - Save checks the test and writes its code again' : 'The test file - the code is written from it') : 'Written from the test - change the steps or the YAML, not this file. ' + runHint() }}</span>
                    <span class="sp"></span>
                    <button type="button" (click)="copyText(view() === 'yaml' ? yaml : goSrc())" title="Copy to the clipboard"><app-icon name="copy" [size]="13" /> Copy</button>
                    @if (view() === 'yaml') {
                      @if (yamlEdit()) { <button type="button" (click)="yamlEdit.set(false); setView('yaml')">Cancel</button><button type="button" class="pri" (click)="saveYaml()">Save</button> }
                      @else { <button type="button" (click)="yamlEdit.set(true)"><app-icon name="edit" [size]="13" /> Edit</button> }
                    } @else { <button type="button" (click)="backend.testsOpenInEditor(view() === 'go' ? goFile() : view() === 'py' ? pyFile() : tclFile())" title="Open the file in VS Code (or the file's app)">Open in VS Code</button> }
                  </div>
                  @if (view() === 'yaml' && yamlEdit()) { <textarea class="code" [(ngModel)]="yaml" spellcheck="false" aria-label="Test YAML"></textarea> }
                  @else { <pre class="code ro hl" [innerHTML]="codeHtml()"></pre> }
                }
              </div>
            } @else { <div class="empty mut">Select a test, or record one.</div> }
          }
        }
        <ng-template #fileView>
          @if (fileSel(); as fp) {
            <div class="head"><div class="crumb">{{ fp }}</div><div class="title"><h1>{{ base(fp) }}</h1>@if (fp.startsWith('reports/')) { <span class="pill">read only</span> }@if (lockOf(fp); as l) { <span class="pill s">🔒 in use by {{ l.user }} on {{ l.host }} since {{ l.since.slice(11, 16) }}</span><button type="button" (click)="releaseLock(fp)" title="Free the testbed when that run is over">Release</button> }<span class="sp"></span><button type="button" (click)="copyText(fileText)" title="Copy to the clipboard"><app-icon name="copy" [size]="13" /> Copy</button><button type="button" (click)="backend.testsOpenInEditor(fp)">Open in VS Code</button>@if (fp.startsWith('testbeds/') || fp.startsWith('suites/')) { <button type="button" class="badbtn" (click)="removeFile(fp)" title="Move it to .removed/ in the tests folder"><app-icon name="trash" [size]="13" /> Delete</button> }@if (!fp.startsWith('reports/') && !(fp.startsWith('testbeds/') && !tbYaml())) { <button type="button" class="savb" (click)="saveFile()">Save</button> }</div></div>
            @if (fp.startsWith('testbeds/') && !tbYaml()) {
              <div class="body">
                <div class="row tbvbar"><span class="mut">{{ tbDevs().length }} {{ tbDevs().length === 1 ? 'role' : 'roles' }} - tests use {{ '{{' }}role{{ '}}' }}</span><span class="sp"></span>
                  <button type="button" (click)="openTbPick(fp)">+ Add Devices</button><button type="button" (click)="tbYaml.set(true)">Edit YAML</button></div>
                @if (tbMeta(); as tm) {
                  <div class="tbmeta">
                    <div class="row"><span class="mut lbl">Description</span><input class="grow" [ngModel]="tm.description ?? ''" (change)="tm.description = $any($event.target).value; saveTbMeta(fp)" placeholder="what this testbed is, e.g. two EXOS switches, stack + standalone" /></div>
                    <div class="row"><span class="mut lbl">Tags</span>@for (t of tm.tags; track t) { <span class="tagc">{{ t }} <button type="button" class="x" (click)="tm.tags.splice($index, 1); saveTbMeta(fp)" title="Remove the tag">×</button></span> }
                      <input #tg class="tagin" placeholder="+ tag" (keydown.enter)="addTbTag(fp, tg)" (blur)="addTbTag(fp, tg)" /><span class="mut sm">what this testbed is (exos, stack, l3…) - tests can require tags</span></div>
                    <div class="lkh"><b>Links between devices</b> <span class="mut sm">which port of one device is cabled to which port of another</span></div>
                    @for (l of tbLinks(); track $index) {
                      <div class="lk"><select [(ngModel)]="l.a" (change)="saveLinks(fp)">@for (d of tbRoles(); track d.role) { <option [value]="d.role">{{ d.role }}</option> }</select>
                        <select [(ngModel)]="l.ap" (change)="saveLinks(fp)">@for (p of portsOfRole(l.a); track p.name) { <option [value]="p.name">{{ p.name }} ({{ p.value }})</option> }</select><span class="mut">↔</span>
                        <select [(ngModel)]="l.b" (change)="saveLinks(fp)">@for (d of tbRoles(); track d.role) { <option [value]="d.role">{{ d.role }}</option> }</select>
                        <select [(ngModel)]="l.bp" (change)="saveLinks(fp)">@for (p of portsOfRole(l.b); track p.name) { <option [value]="p.name">{{ p.name }} ({{ p.value }})</option> }</select>
                        <button type="button" class="ibtn" (click)="tbLinks().splice($index, 1); saveLinks(fp)" title="Remove the link">✕</button></div>
                    }
                    <div class="row"><button type="button" class="lnk" (click)="addLink(fp)" [disabled]="tbRoles().length < 2" [title]="tbRoles().length < 2 ? 'Links need two devices in the testbed' : 'Add a cable between two devices'">+ Link</button>
                      <span class="mut sm">Tests use {{ '{{' }}a.to.b{{ '}}' }} (a's port facing b) and {{ '{{' }}a.links.b{{ '}}' }} (all of them)</span></div>
                  </div>
                }
                <div class="tbv">
                  @for (g of tbDevGroups(); track g.folder) {
                    <div class="tbg"><span class="fi"></span>{{ g.folder || 'Other values' }}<span class="mut">{{ g.items.length }}</span></div>
                    @for (d of g.items; track d.role) {
                      @if (d.session) {
                        <div class="tcard">
                          <div class="tch"><b class="ell" [title]="d.session">{{ d.name }}</b><span class="mono mut">{{ d.host }}</span>@if (d.deviceType) { <span class="tg">{{ d.deviceType }}</span> }@if (d.model) { <span class="tg">{{ d.model }}</span> }@if (kindOf(d); as k) { <span class="tg">{{ k }}</span> }<span class="sp"></span>
                            <button type="button" class="ibtn" (click)="removeRole(fp, d.role)" title="Remove this device from the testbed (the saved session stays)"><app-icon name="trash" [size]="13" /></button></div>
                          <div class="trole">Tests use it as <code>{{ '{{' }}{{ d.role }}{{ '}}' }}</code><button type="button" class="lnk" (click)="renameRole(fp, d.role)" title="Give it the name the tests use, e.g. dut or core">Rename</button><span class="mut">also .device_type .host .model by themselves</span></div>
                          <div class="tsec">
                            <div class="th">Test ports <span class="mut" [title]="'Tests use {{' + d.role + '.port1}} … and {{' + d.role + '.ports}} for all of them (a snake test)'">port1 … port{{ d.ports?.length || 0 }}</span><span class="sp"></span><span class="mut">{{ portRule(d) }}</span>
                              @if (anyPortSet(d)) { <button type="button" class="lnk" (click)="resetPorts(fp, d)" title="Back to the defaults of the device type">Reset</button> }</div>
                            <div class="pg">
                              @for (p of d.ports ?? []; track p.name) { <label [title]="p.set ? 'Set in this testbed' : 'The default of the device type - type to change it'"><button type="button" class="plab" [class.has]="!!p.props" (click)="togglePortProps(d.role, p.name); $event.preventDefault()" [title]="'Properties of ' + p.name + ' (speed, what it is cabled to…) - tests use {{' + d.role + '.' + p.name + '.speed}}'">{{ p.name }}{{ p.props ? ' •' : '' }}</button><input class="mono" [class.set]="p.set" [value]="p.value" (change)="setPort(fp, d, p.name, $any($event.target).value)" /></label> }
                              @if (propsOpen()?.role === d.role) { @for (p of d.ports ?? []; track p.name) { @if (propsOpen()?.port === p.name) {
                                <div class="pprops"><b>{{ p.name }} · {{ p.value }}</b> <span class="mut sm">properties - tests use {{ '{{' }}{{ d.role }}.{{ p.name }}.name{{ '}}' }}</span>
                                  <div class="vals">@for (kv of propsOf(p); track kv.k) { <code>{{ kv.k }}</code><input class="mono" [value]="kv.v" (change)="setPortProp(fp, d.role, p.name, kv.k, $any($event.target).value)" /><button type="button" class="ibtn" (click)="setPortProp(fp, d.role, p.name, kv.k, '')" title="Remove">✕</button> }
                                    <input #pk class="mono" placeholder="name, e.g. speed" /><input #pv class="mono" placeholder="value, e.g. 10G" (keydown.enter)="addPortProp(fp, d.role, p.name, pk, pv)" /><button type="button" class="ibtn add" (click)="addPortProp(fp, d.role, p.name, pk, pv)" title="Add">＋</button></div></div>
                              } } }
                              <div class="padd"><button type="button" class="ibtn" (click)="portCount(fp, d, 1)" title="Add a port (e.g. for a snake test over all ports)">＋</button><button type="button" class="ibtn" (click)="portCount(fp, d, -1)" [disabled]="(d.ports?.length ?? 0) <= 1" title="Remove the last port">−</button></div>
                            </div>
                            <div class="th">Other values <span class="mut">{{ '{{' }}{{ d.role }}.name{{ '}}' }} - e.g. a slot, a VLAN or an uplink of this device</span></div>
                            <div class="vals">
                              @for (kv of valuesOf(d); track kv.k) { <code>{{ kv.k }}</code><input class="mono" [value]="kv.v" (change)="setVal(fp, d.role, kv.k, $any($event.target).value)" /><button type="button" class="ibtn" (click)="setVal(fp, d.role, kv.k, '')" title="Remove the value">✕</button> }
                              @for (k of d.wanted ?? []; track k) { <code title="The tests use it">{{ k }}</code><input class="mono" placeholder="used by the tests - not set (the test's default)" (change)="setVal(fp, d.role, k, $any($event.target).value)" /><span></span> }
                              <input #nk class="mono" placeholder="name, e.g. slot" /><input #nv class="mono" placeholder="value" (keydown.enter)="addVal(fp, d.role, nk, nv)" /><button type="button" class="ibtn add" (click)="addVal(fp, d.role, nk, nv)" title="Add the value">＋</button>
                            </div>
                          </div>
                        </div>
                      } @else {
                        <div class="tcard val"><div class="tch"><code>{{ d.role }}</code><span class="mono">{{ d.value || '(empty)' }}</span><span class="mut">{{ d.value?.includes('/') ? 'not a saved session' : 'a value tests use as ' }}{{ d.value?.includes('/') ? '' : '{{' + d.role + '}}' }}</span><span class="sp"></span><button type="button" class="ibtn" (click)="removeRole(fp, d.role)" title="Remove it from the testbed"><app-icon name="trash" [size]="13" /></button></div></div>
                      }
                    }
                  } @empty { <div class="empty mut">No roles yet: + Add Devices.</div> }
                </div>
              </div>
            } @else {
            <div class="body"><textarea class="code" [(ngModel)]="fileText" spellcheck="false" [attr.aria-label]="fp"></textarea>
              <div class="row"><span class="mut">{{ fp.startsWith('lib/') ? 'Termwright writes twtest and run_tests again when it is updated: keep your own helpers in other files.' : fp.startsWith('templates/') ? 'TextFSM templates; the index says which command each one parses (ntc-templates format).' : 'Roles and values: role: Folder/session, name: value.' }}</span>@if (fp.startsWith('testbeds/')) { <span class="sp"></span><button type="button" class="lnk" (click)="tbYaml.set(false); openFile(fp)">Back to the device list</button> }</div></div>
            }
          } @else { <div class="empty mut">Select a file.</div> }
        </ng-template>
      </section>
      @if (logView(); as lv) {
        <div class="scrim fixed" (click)="logView.set(null)" aria-hidden="true"></div>
        <div class="mdlg logdlg" role="dialog" aria-label="Run log">
          <div class="row"><h2 class="fl">Run log · {{ lv.title }}</h2><span class="sp"></span><button type="button" (click)="copyText(lv.text)">Copy</button><button type="button" (click)="backend.testsOpenInEditor(lv.file)">Open in Editor</button><button type="button" (click)="logView.set(null)">Close</button></div>
          <span class="mut">{{ lv.file }}</span>
          <pre class="logpre">@for (ln of lv.text.split('\n'); track $index) {<span [class]="logCls(ln)">{{ ln + '\n' }}</span>}</pre>
        </div>
      }
      @if (defDlg(); as dd) {
        <div class="scrim fixed" (click)="defDlg.set(null)" aria-hidden="true"></div>
        <div class="mdlg wide defdlg" role="dialog" aria-label="Defaults">
          <h3>Defaults - {{ dd.dir || 'All tests' }}</h3>
          <div class="order"><b>Who wins:</b>@for (o of orderOf(dd.dir); track o.l) { <span [class.on]="o.on">{{ o.l }}</span>@if (!$last) { <i>›</i> } }</div>
          <span class="mut">{{ defIntro(dd.dir) }}</span>
          <div class="dsec"><b>Values</b> <span class="mut">used as {{ '{{' }}name{{ '}}' }} in the steps</span></div>
          <div class="dvals">
            @for (kv of dd.vals; track $index) { <input class="mono" [(ngModel)]="kv.k" placeholder="name" /><input [(ngModel)]="kv.v" placeholder="value" /><button type="button" class="ibtn" (click)="dd.vals.splice($index, 1)" title="Remove">✕</button> }
            @for (kv of inheritedVals(dd); track kv.k) { <code class="grey">{{ kv.k }}</code><span class="grey">{{ kv.v }} <small>(from above - add it here to change it)</small></span><span></span> }
            <input class="mono" #dk placeholder="name, e.g. vlan" /><input #dv placeholder="value" (keydown.enter)="addDefVal(dd, dk, dv)" /><button type="button" class="ibtn add" (click)="addDefVal(dd, dk, dv)" title="Add">＋</button>
          </div>
          <div class="dsec"><b>Step options</b> <span class="mut">for the steps set to Default (empty: from above or none)</span></div>
          <div class="row opts dopts">
            <label>Timeout <input type="number" min="0" class="num2" [(ngModel)]="dd.o.timeout" [placeholder]="dd.inh.stepOptions?.timeout ?? '60'" /> s</label>
            <label>Wait <input type="number" min="0" class="num2" [(ngModel)]="dd.o.wait" [placeholder]="dd.inh.stepOptions?.wait ?? ''" /> s</label>
            <label><input type="checkbox" [ngModel]="!!dd.o.retry" (ngModelChange)="dd.o.retry = $event ? { timeout: 60, interval: 5 } : undefined" /> Retry until it passes</label>
            @if (dd.o.retry; as rt) { <label>for <input type="number" min="1" class="num2" [(ngModel)]="rt.timeout" /> s</label> }
            <label><input type="checkbox" [(ngModel)]="dd.o.continueOnFail" /> Go on when a step fails</label>
            <label><input type="checkbox" [(ngModel)]="dd.o.ignoreErrors" /> Ignore device errors</label>
          </div>
          @if (inhOptText(dd); as it) { <span class="mut sm">From above: {{ it }}</span> }
          <div class="row"><span class="mut sm">Saved as {{ (dd.dir ? dd.dir + '/' : '') + 'defaults.yaml' }}</span><span class="sp"></span><button type="button" (click)="defDlg.set(null)">Cancel</button><button type="button" class="pri" (click)="saveDefaults(dd)">Save</button></div>
        </div>
      }
      @if (bigPic()) { <div class="scrim fixed" (click)="bigPic.set('')" aria-hidden="true"></div><img class="bigpic" [src]="bigPic()" alt="" (click)="bigPic.set('')" /> }
      @if (webRecDlg(); as wr) {
        <div class="scrim fixed" (click)="webRecDlg.set(null)" aria-hidden="true"></div>
        <div class="mdlg wide" role="dialog" aria-label="Record a web test">
          <h3>{{ wr.proc ? 'Record a Web Procedure' : 'Record a Web Test' }} <span class="mut">· Playwright</span></h3>
          @if (testBrowsers.count()) { <div class="webst row opts">⚠ <span>{{ testBrowsers.count() === 1 ? '1 test browser from an earlier run is still open.' : testBrowsers.count() + ' test browsers from earlier runs are still open.' }}</span><span class="sp"></span><button type="button" (click)="testBrowsers.close()">Close Them</button></div> }
          @if (!wr.proc) {
            <div class="seg wrseg" role="radiogroup" aria-label="Record from">
              <button type="button" role="radio" [attr.aria-checked]="!wr.addr" [class.on]="!wr.addr" (click)="wr.addr = false">A saved web app</button>
              <button type="button" role="radio" [attr.aria-checked]="!!wr.addr" [class.on]="!!wr.addr" (click)="wr.addr = true">An address</button>
            </div>
          }
          @if (wr.addr && !wr.proc) {
            <label class="f2">Address <input [(ngModel)]="wr.start" placeholder="https://app.example.com/login" spellcheck="false" /></label>
            <span class="mut">No saved login: log in yourself while recording. Afterwards you can save the address as a web app session.</span>
          } @else {
          @if (!webApps().length) { <div class="webst row opts">🌐 <span>First save the web app as a session: <b>+ New</b> in the session list, protocol <b>Web App (HTTP/HTTPS)</b>, its address, user and password, then <b>● Record Login</b>.</span></div> }
          <label class="f2">Web app <select [(ngModel)]="wr.app">@for (p of webApps(); track p) { <option [value]="p">{{ p }}</option> }</select></label>
          <label class="f2">Start page <input [(ngModel)]="wr.start" placeholder="/ (the app's address) or /contacts" spellcheck="false" /></label>
          }
          @if (wr.proc) {
            <label class="f2">Procedure name <input [(ngModel)]="wr.name" placeholder="e.g. open-cp-gvi" spellcheck="false" /></label>
            <label class="f2">What it does <input [(ngModel)]="wr.description" placeholder="e.g. opens a configuration profile's Global Value Inheritance" /></label>
          } @else {
          <label class="f2">Test name <input [(ngModel)]="wr.name" placeholder="e.g. Create a contact" /></label>
          <label class="f2">Save in <input [(ngModel)]="wr.folder" list="twsuites" placeholder="cases/ folder, e.g. crm" spellcheck="false" /></label>
          }
          <datalist id="twsuites">@for (x of suiteDirs(); track x) { <option [value]="x"></option> }</datalist>
          <span class="mut">{{ wr.proc ? 'The browser opens logged in at the start page. Do the steps the tests share, then press Done in the bar at the bottom: they are saved as a procedure. In the procedure, replace values you typed (a profile, a device) with {{name}} - they become its values.' : 'Use the app as usual. The bar at the bottom of the browser pauses, checks what must be true (a text, that something is shown, a value) and undoes the last action. Press Done to finish - the test opens in the editor to look over and save.' }}</span>
          <div class="row"><span class="sp"></span><button type="button" (click)="webRecDlg.set(null)">Cancel</button><button type="button" class="pri" (click)="wr.proc ? startWebProc() : startWebRec()" [disabled]="!recFromOk(wr) || !wr.name.trim() || !webSt()?.ready">● Start Recording</button></div>
          @if (webSt(); as ws) { @if (!ws.ready) { <div class="row webst"><span>🌐 {{ ws.node ? 'Playwright is not set up on this computer yet.' : 'Node.js is not installed: install it from nodejs.org (or brew install node) first.' }}</span><span class="sp"></span>@if (ws.node) { <button type="button" class="pri" (click)="webSetup()" [disabled]="webBusy()">{{ webBusy() ? 'Setting up…' : 'Set Up Playwright' }}</button> }</div>@if (webBusy()) { <code class="mut">{{ webLine() }}</code> } } }
        </div>
      }
      @if (tbPick()) {
        <div class="scrim fixed" (click)="tbPick.set(false)" aria-hidden="true"></div>
        <div class="mdlg tbdlg" role="dialog" aria-label="Testbed devices">
          <h3>{{ tbAppend ? 'Add Devices to ' + base(tbAppend) : 'New Testbed' }}</h3>
          <span class="mut">Choose a session folder, then tick the devices the tests may use. Each device becomes a role: tests use <code>{{ '{{' }}role{{ '}}' }}</code>.</span>
          <div class="tbcols">
            <div class="tbcol">
              <input class="tbq" [(ngModel)]="tbQuery" placeholder="Search folders" aria-label="Search folders" />
              <div class="fpick">
                @for (fo of folderList(); track fo.path) {
                  <div class="fo" [class.on]="tbFolder === fo.path" [style.padding-left.px]="10 + fo.depth * 16" (click)="pickFolder(fo.path)" role="button" tabindex="0" [title]="fo.path">
                    <span class="fi"></span><span class="ell">{{ fo.name }}</span><span class="mut">{{ fo.n }}</span>
                  </div>
                } @empty { <p class="mut">No session folders{{ tbQuery ? ' match' : '' }}.</p> }
              </div>
            </div>
            <div class="tbcol">
              @if (tbFolder) {
                <div class="row tbbar"><input class="tbq grow" [(ngModel)]="tbDevQ" placeholder="Filter devices: name, address, type, model" aria-label="Filter devices" />
                  <button type="button" (click)="tbSelectAll(true)">All</button><button type="button" (click)="tbSelectAll(false)">None</button></div>
                <div class="fpick devs">
                  @for (g of devGroups(); track g.folder) {
                    <div class="dg" [title]="g.folder"><span class="fi"></span>{{ g.folder }}</div>
                    @for (d of g.items; track d.path) {
                      <label class="dv" [class.on]="tbPicked.has(d.path)" [class.had]="tbHas.has(d.path)">
                        <input type="checkbox" [checked]="tbPicked.has(d.path) || tbHas.has(d.path)" [disabled]="tbHas.has(d.path)" (change)="tbToggle(d.path)" />
                        <span class="dn ell">{{ d.name }}</span><span class="dh mut">{{ d.host }}</span><span class="dt">{{ d.deviceType || '-' }}</span><span class="dm mut ell">{{ tbModels()[d.path] || '' }}</span>
                        <code class="dr">{{ tbHas.has(d.path) ? 'in the testbed' : roleName(d.path) }}</code>
                      </label>
                    }
                  } @empty { <p class="mut">No devices{{ tbDevQ ? ' match' : '' }}.</p> }
                </div>
              } @else { <div class="empty mut">Choose a folder on the left.</div> }
            </div>
          </div>
          <div class="row">
            @if (!tbAppend) { <label class="fld grow tbname"><span>Testbed name</span><input [(ngModel)]="tbName" /></label> }
            <span class="mut">{{ tbPicked.size }} {{ tbPicked.size === 1 ? 'device' : 'devices' }} picked</span><span class="sp"></span>
            <button type="button" (click)="tbPick.set(false)">Cancel</button>
            <button type="button" class="savb" (click)="createTestbed()" [disabled]="!tbPicked.size || (!tbAppend && !tbName.trim())">{{ tbAppend ? 'Add' : 'Create' }}</button>
          </div>
        </div>
      }
      <datalist id="tw-api-sessions">@for (a of apiSessions(); track a) { <option [value]="a"></option> }</datalist>
      @if (exp(); as x) {
        <div class="scrim fixed" (click)="exp.set(null)" aria-hidden="true"></div>
        <div class="mdlg wide" role="dialog" aria-label="Export automation suite">
          <h3>Export Automation Suite</h3>
          <span class="mut">A zip with the tests and their code, the procedures, the library, the testbeds, a plan and a run script that writes JUnit XML and each test's run log.</span>
          <div class="fld"><span>How it runs</span>
            <label class="radio" [class.on]="x.standalone && x.lang !== 'runner' && x.lang !== 'playwright'"><input type="radio" name="twsa" [checked]="x.standalone && x.lang !== 'runner' && x.lang !== 'playwright'" (change)="expMode(x, 'code')" /><span>On its own, as code - Termwright is not needed<small>Python or Go tests with a library that logs in to the devices itself (SSH, Telnet, REST APIs, SNMP, NETCONF, web pages). Procedures become functions you can call.</small></span></label>
            <label class="radio" [class.on]="x.lang === 'runner'"><input type="radio" name="twsa" [checked]="x.lang === 'runner'" (change)="expMode(x, 'runner')" /><span>On its own, with the twrun program - no code<small>The YAML tests and twrun for the chosen system: the same runs, reports, procedures, reruns and timeouts as in Termwright.</small></span></label>
            <label class="radio" [class.on]="x.lang === 'playwright'"><input type="radio" name="twsa" [checked]="x.lang === 'playwright'" (change)="expMode(x, 'playwright')" /><span>Playwright tests (TypeScript) - the web steps<small>A Playwright Test project: each test's web steps as Playwright code you can read and change, the web apps' logins, test data rows. Run with <code>npx playwright test</code>; other steps are comments.</small></span></label>
            <label class="radio" [class.on]="!x.standalone"><input type="radio" name="twsa" [checked]="!x.standalone" (change)="expMode(x, 'termwright')" /><span>Through Termwright<small>Python, Go or TCL whose steps go through the app, or <code>termwright tests serve</code> on a server, which logs in with the saved sessions.</small></span></label>
            <small class="mut">Devices come from your sessions as inventory.yaml; passwords are never in the zip - environment variables (TW_PASSWORD…), the system keyring or a prompt.</small>
          </div>
          <label class="fld"><span>Tests</span><select [(ngModel)]="x.what">
            @if (picked().length) { <option value="picked">The {{ picked().length }} selected {{ picked().length === 1 ? 'test' : 'tests' }}</option> }
            @if (sel()) { <option value="sel">This test ({{ base(sel()) }})</option> }
            @for (p of tree()?.suites ?? []; track p.file) { <option [value]="p.file">Plan: {{ p.name }}</option> }
            <option value="all">Every test in the folder</option></select></label>
          @if (x.lang !== 'runner' && x.lang !== 'playwright') { <div class="segs"><span class="mut">Language</span>@for (l of (x.standalone ? ['python', 'go'] : ['python', 'go', 'tcl']); track l) { <button type="button" [class.on]="x.lang === l" (click)="x.lang = l">{{ l === 'python' ? 'Python' : l === 'go' ? 'Go' : 'TCL' }}</button> }</div> }
          <div class="segs"><span class="mut">Runs on</span>@for (o of ['linux', 'mac', 'windows']; track o) { <button type="button" [class.on]="x.os === o" (click)="x.os = o">{{ o === 'mac' ? 'macOS' : o === 'linux' ? 'Linux' : 'Windows' }}</button> }</div>
          @if (x.lang === 'playwright') {
            <div class="fld"><span>Pictures in the report - in the step they belong to</span>
              <label class="chk"><input type="checkbox" [(ngModel)]="x.failShots" /> A screenshot of a step that fails (a check not found, an action that fails)</label>
              <label class="chk"><input type="checkbox" [ngModel]="x.stepShots" (ngModelChange)="setStepShots(x, $event, $any($event.target))" /> A screenshot after every step - bigger reports</label>
              <small class="mut">Also when running: TW_FAIL_SHOTS=0 or TW_STEP_SHOTS=1.</small></div>
          }
          @if (x.lang !== 'playwright') { <label class="fld"><span>Testbed - the devices it runs on by default</span><select [(ngModel)]="x.testbed"><option value="">The tests' own sessions</option>@for (b of tree()?.testbeds ?? []; track b) { <option [value]="b">{{ base(b) }}</option> }</select></label> }
          <label class="fld"><span>Suite name</span><input [(ngModel)]="x.name" /></label>
          <div class="row"><span class="sp"></span><button type="button" (click)="exp.set(null)">Cancel</button><button type="button" class="savb" (click)="doExport()">Export…</button></div>
        </div>
      }
      @if (tm(); as m) {
        <div class="scrim fixed" (click)="tm.set(null)" aria-hidden="true"></div>
        <div class="mdlg tmk" role="dialog" aria-label="Make a table">
          <div class="row"><h3>Make a Table</h3><span class="mut">{{ m.command }}</span><span class="sp"></span>
            <div class="segs"><button type="button" [class.on]="m.kind === 'table'" (click)="m.kind = 'table'; previewTable()">Rows and columns</button><button type="button" [class.on]="m.kind === 'fields'" (click)="m.kind = 'fields'; detectFields(); previewTable()">Label: value fields</button></div></div>
          <span class="mut">{{ m.kind === 'table' ? 'Click the header line of the table. Its columns are found from the header; rename them and say which may have spaces in their values.' : 'Tick the lines whose value you want; each becomes a named value.' }}</span>
          <div class="tmgrid">
            <div class="tmout">
              @for (l of m.lines; track $index; let li = $index) {
                <div class="tl" [class.hd]="m.kind === 'table' && m.header === li" [class.pick]="m.kind === 'fields' && fieldOn(li)" (click)="m.kind === 'table' ? pickHeader(li) : toggleField(li)">@if (m.kind === 'fields' && isField(l)) { <input type="checkbox" [checked]="fieldOn(li)" tabindex="-1" /> }<span>{{ l || ' ' }}</span></div>
              }
            </div>
            <div class="tmside">
              @if (m.kind === 'table') {
                <h4>Columns</h4>
                @for (c of m.cols; track $index; let ci = $index) {
                  <div class="tc"><input [(ngModel)]="c.name" (ngModelChange)="previewTable()" aria-label="Column name" /><label title="Values in this column may contain spaces (the next column then starts after two spaces or more)"><input type="checkbox" [(ngModel)]="c.spaces" (ngModelChange)="previewTable()" /> spaces</label><button type="button" class="lnk" (click)="m.cols.splice(ci, 1); previewTable()" title="Leave this column out">✕</button></div>
                } @empty { <p class="mut">Click the header line on the left.</p> }
              } @else {
                <h4>Fields</h4>
                @for (fl of m.fields; track $index; let fi = $index) {
                  <div class="tc"><input [(ngModel)]="fl.name" (ngModelChange)="previewTable()" aria-label="Field name" /><span class="mut ell">{{ fl.label }}</span><button type="button" class="lnk" (click)="m.fields.splice(fi, 1); previewTable()">✕</button></div>
                } @empty { <p class="mut">Tick lines on the left.</p> }
              }
              <h4>Preview</h4>
              @if (m.err) { <p class="bad">{{ m.err }}</p> }
              @if (m.preview?.header?.length) {
                <div class="tprev"><table><tr>@for (h of m.preview!.header; track h) { <th>{{ h }}</th> }</tr>
                  @for (r of m.preview!.rows.slice(0, 50); track $index) { <tr>@for (c of r; track $index) { <td>{{ c }}</td> }</tr> }</table></div>
                <span class="mut">{{ m.preview!.rows.length }} rows</span>
              } @else if (!m.err) { <p class="mut">Nothing yet.</p> }
            </div>
          </div>
          <div class="row"><label class="fld nm"><span>Table name</span><input [(ngModel)]="m.name" /></label><span class="sp"></span>
            <span class="mut">Saved as a TextFSM template - it works in the runs, the generated code and anywhere TextFSM runs.</span>
            <button type="button" (click)="tm.set(null)">Cancel</button><button type="button" class="savb" (click)="saveTable()" [disabled]="!m.preview?.rows?.length || !m.name.trim()">Save Table</button></div>
        </div>
      }
      @if (manual(); as m) {
        <div class="scrim fixed" aria-hidden="true"></div>
        <div class="mdlg" role="dialog" aria-label="Manual step">
          <span class="mut">A test waits for you - manual step</span>
          <h2>{{ m.name }}</h2>
          <p class="inst">{{ m.instruction }}</p>
          @if (m.expected) { <p><span class="mut">Expected:</span> {{ m.expected }}</p> }
          <textarea [(ngModel)]="manualNote" rows="2" placeholder="Note (optional): what you saw"></textarea>
          <div class="row"><button type="button" (click)="answer('skipped')">Skip</button><span class="sp"></span><button type="button" class="badbtn" (click)="answer('failed')">✗ Failed</button><button type="button" class="okbtn" (click)="answer('passed')">✓ Passed</button></div>
        </div>
      }
      @if (runFiles(); as rf) {
        <div class="scrim" (click)="runFiles.set(null)" aria-hidden="true"></div>
        <aside class="drawer" role="dialog" aria-label="Run tests">
          <h2>Run {{ runLabel() }}</h2>
          <span class="mut">{{ rf.length ? rf.length + (rf.length === 1 ? ' test' : ' tests') : 'every test in the folder' }}</span>
          @if (!multi()) {
            <label class="fld"><span>Testbed - which devices</span><select [(ngModel)]="testbed" (ngModelChange)="loadRunMap()">@for (b of tree()?.testbeds ?? []; track b) { <option [value]="b">{{ base(b) }}{{ lockText(b) }}</option> }<option value="">The tests' own sessions</option></select>
              @if (testbed) { <small class="mut">{{ lockOf(testbed) && !lockOf(testbed)!.mine ? '🔒 in use by ' + lockOf(testbed)!.user + ' on ' + lockOf(testbed)!.host + ' - Run takes it over after asking' : '🔒 free - locked to you while the run lasts' }}</small> }</label>
            @if (willSkip().length) {
              <div class="skipw">⚠ {{ willSkip().length }} of {{ runCount() }} {{ runCount() === 1 ? 'test' : 'tests' }} will be skipped on {{ testbed ? base(testbed) : 'their own sessions' }}:
                @for (w of willSkip().slice(0, 6); track w.file) { <div>· <b>{{ w.case }}</b> - {{ w.why }}</div> }@if (willSkip().length > 6) { <div>· and {{ willSkip().length - 6 }} more</div> }</div>
            }
            @if (runRoles().length) {
              <div class="fld"><span>Devices of the test</span>
                @for (r of runRoles(); track r.role) {
                  @let ps = runPortsOf(r.role);
                  <div class="rdev">
                    <div class="row"><code class="rr">{{ '{{' }}{{ r.role }}{{ '}}' }}</code>@if (r.protocol === 'Web') { <span class="rk">web app</span> }</div>
                    <div class="row"><span class="mut">Runs on</span>
                      @if (testbed) {
                        <select [ngModel]="runMapOf(r.role)" (ngModelChange)="mapRole(r.role, $event)" [class.miss]="!runMapOf(r.role)" title="A device of the testbed; it is added to the testbed under the test's name">
                          <option value="">its own session: {{ r.session.split('/').pop() }}</option>
                          @for (d of runTbDevs(); track d.role) { @if (d.session && (d.protocol === 'Web') === (r.protocol === 'Web')) { <option [value]="d.session">{{ d.name }}{{ d.deviceType ? ' (' + d.deviceType + (kindOf(d) ? ' ' + kindOf(d) : '') + ')' : '' }}</option> } }
                        </select>
                      } @else { <span class="ell">{{ r.session }}</span> }
                    </div>
                    @if (ps.length) {
                      <div class="row pc"><span class="mut ell">Ports {{ portsLine(ps) }}</span><span class="sp"></span><button type="button" class="lnk" (click)="togglePorts(r.role)">{{ portsOpen().has(r.role) ? 'Done' : 'Change' }}</button></div>
                      @if (portsOpen().has(r.role)) {
                        <div class="pg4">@for (p of ps; track p.name) { <label><span>{{ p.name }}</span><input class="mono" [class.set]="p.set" [value]="p.value" (change)="setRunPort(r.role, p.name, $any($event.target).value)" /></label> }</div>
                        <small class="mut">{{ testbed && runMapOf(r.role) ? 'Changes are saved in the testbed (more ports: the testbed page).' : 'Changes are for this run.' }}</small>
                      }
                    }
                  </div>
                }
              </div>
            }
            @if ((tree()?.testbeds?.length ?? 0) > 1) { <button type="button" class="lnk sm" (click)="multi.set(true); multiPick = testbed ? [testbed] : []">Run on several testbeds at the same time…</button> }
          } @else {
            <div class="fld"><span>Testbeds - each runs the tests at the same time</span>
              @for (b of tree()?.testbeds ?? []; track b) { <label class="radio" [class.on]="multiPick.includes(b)"><input type="checkbox" [checked]="multiPick.includes(b)" (change)="toggleMulti(b)" /><span>{{ base(b) }}<small>{{ lockText(b) || 'free' }}</small></span></label> }
              <button type="button" class="lnk sm" (click)="multi.set(false)">One testbed</button></div>
          }
          <div class="fld"><span>After each test</span>
            @for (o of cleanupOpts; track o.k) {
              <label class="radio" [class.on]="cleanupMode === o.k"><input type="radio" name="twcl" [value]="o.k" [(ngModel)]="cleanupMode" (ngModelChange)="keepOcf()" /><span>{{ o.l }}<small>{{ o.d }}</small></span></label>
            }
          </div>
          @if (cleanupMode !== 'off') {
            <label class="fld"><span>If a cleanup fails</span><select [(ngModel)]="onCleanupFail" (ngModelChange)="keepOcf()"><option value="stop">Stop the run - skip the rest</option><option value="restore">Restore the configuration saved before the run</option><option value="factory">Factory default, then go on</option></select></label>
          }
          <div class="fld"><span>Run control</span>
            <div class="rc3">
              <label title="A failed test runs again up to this many times; if it then passes it is marked flaky"><small>Rerun a failed test</small><select [(ngModel)]="rerun" (ngModelChange)="keepRc()"><option [ngValue]="0">No</option><option [ngValue]="1">Once</option><option [ngValue]="2">Twice</option><option [ngValue]="3">3 times</option></select></label>
              <label title="A test without its own timeout is stopped after this (its cleanup still runs). 0 = no limit"><small>Test timeout, min</small><input type="number" min="0" [(ngModel)]="caseMin" (ngModelChange)="keepRc()" placeholder="no limit" /></label>
              <label title="Tests not started by then are skipped. 0 = no limit"><small>Run timeout, min</small><input type="number" min="0" [(ngModel)]="runMin" (ngModelChange)="keepRc()" placeholder="no limit" /></label>
            </div></div>
          @if (!multi()) { <label class="chk" title="Stop before every step: run the test one step at a time"><input type="checkbox" [(ngModel)]="stepMode" /> <span><b>Step through</b><small class="mut"> - stop before every step{{ breakCount() ? '; ' + breakCount() + ' breakpoints set' : '' }}</small></span></label> }
          @if (!multi()) { <label class="chk" title="When a step fails, the run waits with the devices' connections open: look at the device, then run the step again, go on or stop"><input type="checkbox" [(ngModel)]="pauseOnFail" /> <span><b>Pause when a step fails</b><small class="mut"> - debug: retry the step, go on or stop</small></span></label> }
          <span class="sp"></span>
          <div class="row"><button type="button" class="grow" (click)="runFiles.set(null)">Cancel</button><button type="button" class="go grow2" (click)="startRun()" [disabled]="multi() && multiPick.length < 1"><app-icon name="play" [size]="12" /> Run {{ rf.length ? rf.length + (rf.length === 1 ? ' test' : ' tests') : 'all' }}{{ multi() ? ' on ' + multiPick.length + ' testbeds' : '' }}</button></div>
        </aside>
      }
    </div>
`,
  styles: [`
    :host { display: block; height: 100%; min-height: 0; border-radius: 10px; overflow: hidden; background: var(--bg); font-family: var(--ui-font, inherit); font-size: 13px; color: var(--text); }
    :host button { font-size: 12.5px; } :host button:not(.lnk):not(.case):not(.tag) { height: 28px; padding: 0 12px; border-radius: 7px; display: inline-flex; align-items: center; gap: 6px; white-space: nowrap; }
    .tv { display: grid; grid-template-columns: 64px 360px minmax(0, 1fr); height: 100%; min-height: 0; position: relative; }
    button { font: inherit; } .sp { flex: 1; } .grow { flex: 1; } .grow2 { flex: 2; } .mut { color: var(--muted); font-size: 12px; } .ok { color: var(--ok, #3fbf75); } .bad { color: var(--danger, #ef6161); } .warn { color: var(--warn, #e0a030); }
    .row { display: flex; align-items: center; gap: 6px; } .r { text-align: right; } .fl { min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
    .pri { background: var(--accent) !important; color: #fff !important; border-color: transparent !important; } .pri:hover:not(:disabled) { filter: brightness(1.1); } .pri:disabled { opacity: .55; }
    .drawer .row button:not(.pri):not(.go) { background: var(--surface, var(--panel)) !important; color: var(--text) !important; border: 1px solid var(--border) !important; } .drawer .row button:not(.pri):not(.go):hover { background: var(--hover) !important; } .ghost { background: none; border-color: transparent; color: var(--muted); }
    .lnk { border: 0; background: none; color: var(--accent); cursor: pointer; padding: 0 4px; }
    .rail { display: flex; flex-direction: column; align-items: stretch; gap: 4px; padding: 8px 6px; background: var(--chrome); border-right: 1px solid var(--border); }
    .rail button { display: flex !important; flex-direction: column; align-items: center; justify-content: center; gap: 4px; height: auto !important; min-height: 52px; line-height: 1.15; padding: 6px 2px !important; font-size: 10.5px !important; border: 0; border-radius: 9px; background: none; color: var(--muted); font-size: 10.5px; cursor: pointer; }
    .rail button:hover { color: var(--text); } .rail button.on { background: color-mix(in srgb, var(--accent) 22%, transparent); color: var(--text); } .ri { font-size: 16px; line-height: 1.1; }
    .rail .gitb { font-size: 10px; word-break: break-all; } .rail i { font-style: normal; color: var(--accent); }
    .side { position: relative; display: flex; flex-direction: column; min-height: 0; min-width: 0; background: var(--panel); border-right: 1px solid var(--border); }
    .sgrip { position: absolute; right: -5px; top: 0; bottom: 0; width: 10px; cursor: col-resize; z-index: 20; display: flex; align-items: center; justify-content: center; }
    .sgrip::after { content: ''; width: 4px; height: 44px; border-radius: 3px; background: color-mix(in srgb, var(--text) 28%, transparent); }
    .sgrip:hover::after, .sgrip:active::after { background: var(--accent); height: 100%; border-radius: 0; width: 3px; }
    .stop { display: flex; flex-direction: column; gap: 8px; padding: 12px; border-bottom: 1px solid var(--border); }
    .search { height: 30px; padding: 0 10px; border-radius: 7px; }
    .tags { display: flex; flex-wrap: wrap; gap: 5px; } .tag { font-size: 11px; padding: 1px 8px; border-radius: 10px; border: 1px solid var(--border); background: color-mix(in srgb, var(--text) 5%, transparent); color: var(--muted); cursor: pointer; }
    .tag.on { border-color: var(--accent); color: var(--text); } .tag i { font-style: normal; opacity: .7; }
    .list { flex: 1; overflow: auto; padding: 6px; display: flex; flex-direction: column; gap: 1px; }
    .fold { display: flex; align-items: center; gap: 6px; padding: 7px 6px 3px; color: var(--muted); font-size: 12px; } .fold .lnk { color: var(--muted); padding: 0; text-align: left; } .fold .fl { flex: 1; } .cnt { font-size: 11px; }
    .case { display: flex; align-items: center; gap: 8px; padding: 6px 8px; border-radius: 7px; cursor: pointer; }
    .fold.suite { padding: 10px 6px 4px; } .fold.suite .lnk.fl { color: var(--text); font-size: 13.5px; font-weight: 600; } .fold.suite .cnt { font-size: 11.5px; }
    .case.in { margin-left: 16px; font-size: 12.5px; }
    .case:hover { background: var(--hover, rgba(255,255,255,.05)); } .case.sel { background: color-mix(in srgb, var(--accent) 18%, transparent); }
    .case input { margin: 0; } .cn { flex: 1; min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; } .n { color: var(--muted); font-size: 11px; }
    .dot { width: 8px; height: 8px; border-radius: 50%; background: color-mix(in srgb, var(--text) 25%, transparent); flex: none; } .dot.p { background: var(--ok, #3fbf75); } .dot.f { background: var(--danger, #ef6161); } .dot.e { background: var(--warn, #e0a030); }
    .sm { font-size: 12px; align-self: flex-start; } .drawer small { font-size: 11.5px; }
    .sui { color: var(--accent); font-size: 10px; width: 14px; text-align: center; }
    .empty-s { color: var(--muted); font-size: 12px; padding: 18px 8px; text-align: center; line-height: 1.6; }
    .recb { display: inline-flex; align-items: center; gap: 8px; height: 28px !important; padding: 0 12px !important; border-radius: 6px !important; background: #16171a !important; color: #f2f3f5 !important; border: 1px solid #000 !important; font-size: 12.5px; font-weight: 500; box-shadow: none !important; }
    .recb:hover { background: #24262b !important; } .rdot { width: 9px; height: 9px; border-radius: 50%; background: #ef4444; box-shadow: 0 0 0 3px rgba(239, 68, 68, .22); flex: none; }
    :host-context(.light) .recb { background: #ffffff !important; color: #1f2329 !important; border-color: #cfd2d8 !important; box-shadow: 0 1px 2px rgba(0,0,0,.06) !important; }
    :host-context(.light) .recb:hover { background: #f3f4f6 !important; border-color: #b9bdc5 !important; }
    .go { display: inline-flex; align-items: center; gap: 6px; justify-content: center; height: 28px !important; padding: 0 12px !important; border-radius: 6px !important; background: #1f9d55 !important; color: #fff !important; border: 1px solid #188a4a !important; font-size: 12.5px; font-weight: 600; box-shadow: none !important; }
    .go:hover:not(:disabled) { background: #188a4a !important; } .go:disabled { opacity: .55; }
    .pausebar { display: flex; flex-direction: column; gap: 8px; padding: 10px 20px; background: color-mix(in srgb, var(--warn, #e0a030) 14%, var(--panel)); border-bottom: 1px solid color-mix(in srgb, var(--warn, #e0a030) 45%, var(--border)); }
    .pausebar .pzi { color: var(--warn, #e0a030); font-weight: 700; } .pausebar .pzt { flex: 1; min-width: 0; } .pausebar .pzt > .mut { margin-left: 8px; } .pausebar .pzt .bad { font-size: 12px; margin-top: 2px; } .pausebar button { height: 28px; }
    .pzrow { display: flex; align-items: center; gap: 10px; } .pausebar.bp { background: color-mix(in srgb, var(--danger, #ef6161) 10%, var(--panel)); border-bottom-color: color-mix(in srgb, var(--danger, #ef6161) 40%, var(--border)); } .pausebar.bp .pzi { color: var(--danger, #ef6161); }
    .pzvars summary { cursor: pointer; color: var(--muted); font-size: 12px; } .pzvars .vars { margin-top: 6px; align-items: center; } .vin { height: 26px; font: 12px var(--mono, monospace); }
    .bpn { cursor: pointer; display: inline-flex; align-items: center; justify-content: center; width: 22px; height: 22px; border-radius: 50%; color: var(--muted); } .bpn:hover { background: color-mix(in srgb, var(--danger, #ef6161) 15%, transparent); } .bpn.on { background: var(--danger, #ef6161); color: #fff; font-weight: 600; }
    .oh { display: flex; align-items: center; gap: 8px; } .oh .sbtn { text-transform: none; letter-spacing: 0; }
    .tmk { width: min(1200px, 96vw) !important; height: min(86vh, 900px); max-height: none !important; } .tmk h3 { margin: 0; }
    .tmgrid { flex: 1; min-height: 0; display: grid; grid-template-columns: minmax(0, 1.4fr) minmax(280px, 1fr); gap: 14px; }
    .tmout { overflow: auto; border: 1px solid var(--border); border-radius: 8px; background: var(--cbg); padding: 6px 0; font: 12px/1.5 var(--mono, monospace); }
    .tmout .tl { display: flex; gap: 6px; align-items: center; padding: 0 10px; white-space: pre; cursor: pointer; min-height: 18px; } .tmout .tl:hover { background: color-mix(in srgb, var(--accent) 10%, transparent); }
    .tmout .tl.hd { background: var(--accent-soft); box-shadow: inset 3px 0 var(--accent); font-weight: 600; } .tmout .tl.pick { background: color-mix(in srgb, var(--ok, #3fbf75) 14%, transparent); } .tmout .tl input { margin: 0; }
    .tmside { overflow: auto; display: flex; flex-direction: column; gap: 6px; } .tmside h4 { margin: 6px 0 2px; font-size: 11.5px; text-transform: uppercase; letter-spacing: .05em; color: var(--muted); }
    .tc { display: flex; align-items: center; gap: 8px; } .tc input:not([type]) { flex: 1; height: 26px; font: 12px var(--mono, monospace); } .tc label { font-size: 12px; display: flex; gap: 4px; align-items: center; }
    .tprev { overflow: auto; max-height: 300px; border: 1px solid var(--border); border-radius: 8px; } .tprev table { border-collapse: collapse; font: 11.5px var(--mono, monospace); width: 100%; } .tprev th, .tprev td { border-bottom: 1px solid var(--border); padding: 3px 8px; text-align: left; white-space: nowrap; } .tprev th { background: var(--panel); position: sticky; top: 0; }
    .fld.nm { flex-direction: row; align-items: center; gap: 8px; } .fld.nm input { width: 240px; }
    .cal { border: 1px solid var(--border); border-radius: 8px; overflow: hidden; }
    .calh, .calr { display: grid; grid-template-columns: 150px 1fr; } .calh { grid-template-columns: 150px repeat(7, 1fr); background: var(--panel2, var(--hover)); font-size: 12px; font-weight: 600; }
    .calh span { padding: 7px 8px; border-left: 1px solid var(--border); } .calh span:first-child { border-left: 0; } .calh .today { color: var(--accent); }
    .calr { border-top: 1px solid var(--border); min-height: 44px; } .caln { padding: 12px 10px; font-weight: 500; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; font-size: 12.5px; }
    .calt { position: relative; display: grid; grid-template-columns: repeat(7, 1fr); cursor: copy; } .cald { border-left: 1px solid var(--border); } .cald.today { background: color-mix(in srgb, var(--accent) 6%, transparent); }
    .caln-now { position: absolute; top: 0; bottom: 0; width: 2px; background: var(--danger, #e5484d); }
    .calb { position: absolute; top: 7px; bottom: 7px; border-radius: 5px; padding: 3px 7px; font-size: 11.5px; overflow: hidden; white-space: nowrap; text-overflow: ellipsis; cursor: pointer;
      background: color-mix(in srgb, var(--warn, #e0a030) 22%, var(--bg)); border: 1px solid color-mix(in srgb, var(--warn, #e0a030) 55%, transparent); color: var(--text); }
    .calb.mine { background: color-mix(in srgb, var(--accent) 20%, var(--bg)); border-color: color-mix(in srgb, var(--accent) 55%, transparent); } .calb:hover { filter: brightness(1.08); }
    .calnote { margin: 10px 2px; font-size: 12px; } .bkl { margin-top: 12px; } b.accent { color: var(--accent); } .bkf { margin-top: 12px; } .bkf .form { padding: 12px; }
    .webst { background: color-mix(in srgb, var(--accent) 8%, transparent); border-radius: 6px; padding: 8px 10px; align-items: center; }
    button.rec { color: #f85149; border-color: color-mix(in srgb, #f85149 50%, transparent); font-weight: 600; }
    button.rec:disabled { opacity: .7; }
    input.pin.sm { width: 90px; }
    input.pin.lg { width: 220px; }
    .wn { min-width: 22px; text-align: right; }
    .mdlg.tbdlg { width: min(1180px, 96vw); height: min(84vh, 820px); } .tbcols { flex: 1; min-height: 0; display: grid; grid-template-columns: 300px 1fr; gap: 12px; } .tbcol { display: flex; flex-direction: column; gap: 8px; min-height: 0; }
    .tbcol .fpick { flex: 1; max-height: none; } .tbbar { gap: 6px; } .tbname { display: flex !important; flex-direction: row !important; align-items: center; gap: 8px; max-width: 380px; } .tbname span { white-space: nowrap; }
    .dg, .tbg { display: flex; align-items: center; gap: 8px; padding: 7px 10px 4px; font-weight: 600; font-size: 12px; color: var(--muted); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; } .tbg { color: var(--text); font-size: 12.5px; padding: 12px 0 2px; } .tbg .mut { font-weight: 400; }
    .dv, .tbd { display: grid; grid-template-columns: 18px minmax(120px, 1.4fr) 120px 90px minmax(80px, 1fr) minmax(110px, auto); align-items: center; gap: 10px; padding: 5px 10px 5px 28px; font-size: 12.5px; border-radius: 6px; cursor: pointer; }
    .pgrid { display: grid; grid-template-columns: repeat(4, 1fr); gap: 6px; } .pgrid label { display: flex; flex-direction: column; gap: 2px; font-size: 11px; color: var(--muted); } .pgrid input { width: 100%; font-family: var(--mono, monospace); } .pgrid label.set input { border-color: var(--accent); }
    .rdev { border: 1px solid var(--border); border-radius: 8px; padding: 8px 10px; margin: 6px 0; display: flex; flex-direction: column; gap: 6px; } .rdev .rr { font-size: 11.5px; color: var(--accent); word-break: break-all; } .rdev .rk { font-size: 10.5px; color: var(--muted); border: 1px solid var(--line); border-radius: 8px; padding: 0 6px; }
    .rdev .row { display: flex; gap: 8px; align-items: center; min-width: 0; } .rdev select { flex: 1; min-width: 0; } .rdev select.miss { border-color: #d29922; } .rdev .pc { font-size: 12px; }
    .pg4 { display: grid; grid-template-columns: repeat(4, 1fr); gap: 6px; } .pg4 label { display: flex; flex-direction: column; gap: 2px; font-size: 10.5px; color: var(--muted); min-width: 0; } .pg4 input { width: 100%; min-width: 0; font-family: var(--mono, monospace); color: var(--muted); } .pg4 input.set { color: inherit; border-color: var(--accent); }
    .rmap .row { display: flex; gap: 6px; align-items: center; } .rmap code { max-width: 45%; } .rmap select { flex: 1; min-width: 0; } .rmap select.miss { border-color: #d29922; }
    .ckedit { color: inherit; padding: 0; font: inherit; cursor: text; } .ckedit:hover { text-decoration: underline dotted; }
    .devsec .sh .norm, .ph .sh .norm { font-weight: 400; text-transform: none; letter-spacing: 0; font-size: 12px; } .devrow { gap: 8px; } .rolec { color: var(--accent); font-size: 12px; white-space: nowrap; } .sm { font-size: 11.5px; }
    .ph > .sh { white-space: nowrap; } .ph > .sh .norm { overflow: hidden; text-overflow: ellipsis; } .devrow .cmdi { flex: 1 1 auto !important; min-width: 200px; width: auto; } .devrow .sm { white-space: nowrap; }
    .addchip { background: none; border: 1px dashed var(--border); color: var(--muted); border-radius: 10px; padding: 1px 9px; font-size: 12px; cursor: pointer; }
    .ph { border-left: 3px solid var(--border); } .ph-steps { border-left-color: var(--accent); } .ph-cleanup { border-left-color: #d29922; } .ph-cleanup > .sh { background: color-mix(in srgb, #d29922 8%, transparent); }
    .ph .pht { font-size: 13.5px; font-weight: 600; text-transform: none; letter-spacing: 0; color: var(--text); } .ph .cnt { font-size: 11px; border: 1px solid var(--border); border-radius: 9px; padding: 0 7px; color: var(--muted); font-weight: 400; }
    .optbar { gap: 10px; align-items: center; } .optbar .seg { display: inline-flex; border: 1px solid var(--border); border-radius: 6px; overflow: hidden; } .optbar .seg button { border: 0; border-radius: 0; padding: 2px 10px; font-size: 11.5px; background: none; color: var(--muted); }
    .optbar .seg button.on { background: color-mix(in srgb, var(--accent) 22%, transparent); color: var(--text); } .optbar .lnk.on { color: var(--accent); } .optbar > .lnk { max-width: 45%; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; } .optbar { flex-wrap: nowrap !important; } .optpanel { border: 1px dashed var(--border); border-radius: 8px; padding: 6px 10px; margin-left: 24px; }
    input.exp, input.exp::placeholder { color: var(--muted); opacity: .75; }
    .lshot { display: block; margin: 4px 0 8px 18px; padding: 0; border: 1px solid #f8514966; border-radius: 6px; background: none; cursor: zoom-in; max-width: 480px; } .lshot img { display: block; width: 100%; border-radius: 6px; }
    .recs { display: grid; grid-template-columns: 1fr 1fr; gap: 6px; } .recb.rc { display: flex; align-items: flex-start; gap: 7px; text-align: left; padding: 6px 9px; min-width: 0; } .recb.rc .rdot { margin-top: 5px; flex: none; } .recb.rc span { display: flex; flex-direction: column; min-width: 0; } .recb.rc small { color: var(--muted); font-size: 11px; font-weight: 400; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
    .srch { display: flex; gap: 6px; align-items: center; } .srch .search { flex: 1; min-width: 0; }
    .pk { font-size: 10.5px; border: 1px solid color-mix(in srgb, var(--ok, #3fb950) 40%, transparent); color: var(--ok, #3fb950); border-radius: 8px; padding: 0 6px; margin-right: 6px; flex: none; } .pk.web { color: #a371f7; border-color: #a371f744; }
    .tbmeta { border: 1px solid var(--border); border-radius: 8px; padding: 8px 12px; margin-bottom: 10px; display: flex; flex-direction: column; gap: 6px; } .tbmeta .lbl { width: 80px; flex: none; } .tbmeta .grow { flex: 1; }
    .tagc { display: inline-flex; align-items: center; white-space: nowrap; border: 1px solid var(--border); border-radius: 10px; padding: 1px 8px; font-size: 12px; } .tagc .x { background: none; border: 0; color: var(--muted); padding: 0 0 0 2px; cursor: pointer; } .tagin { width: 90px; }
    .lkh { margin-top: 4px; font-size: 12.5px; } .lk { display: grid; grid-template-columns: minmax(0,1fr) 120px 18px minmax(0,1fr) 120px 28px; gap: 6px; align-items: center; }
    .plab { background: none; border: 0; padding: 0; color: var(--muted); font-size: 10.5px; text-align: left; cursor: pointer; } .plab:hover, .plab.has { color: var(--accent); }
    .pprops { grid-column: 1 / -1; border: 1px solid var(--accent); border-radius: 8px; padding: 8px; display: flex; flex-direction: column; gap: 6px; font-size: 12px; }
    .needs { gap: 8px; padding-left: 24px; font-size: 12px; } .needs select { font-size: 12px; }
    .skipw { background: color-mix(in srgb, #d29922 10%, transparent); border: 1px solid color-mix(in srgb, #d29922 40%, transparent); border-radius: 8px; padding: 8px 10px; font-size: 12px; margin: 6px 0; display: flex; flex-direction: column; gap: 2px; }
    .defdlg { width: min(760px, 94vw); } .order { display: flex; gap: 6px; align-items: center; flex-wrap: wrap; font-size: 12px; } .order span { border: 1px solid var(--border); border-radius: 6px; padding: 2px 8px; } .order span.on { border-color: var(--accent); color: var(--accent); } .order i { color: var(--muted); font-style: normal; }
    .dsec { margin-top: 6px; font-size: 13px; } .dsec b, .lkh b { margin-right: 6px; } .dvals { display: grid; grid-template-columns: 180px 1fr 28px; gap: 6px; align-items: center; } .dvals .grey { color: var(--muted); } .dopts { padding-left: 0 !important; }
    .tcard { border: 1px solid var(--border); border-radius: 8px; margin: 8px 0 12px; background: color-mix(in srgb, var(--bg) 60%, transparent); }
    .tch { display: flex; align-items: center; gap: 10px; padding: 9px 12px; border-bottom: 1px solid var(--border); font-size: 13px; } .tcard.val .tch { border: 0; }
    .tch .mono, .vals .mono, .pg .mono { font-family: var(--mono, monospace); font-size: 12px; } .tg { font-size: 11px; border: 1px solid var(--border); border-radius: 10px; padding: 1px 7px; color: var(--muted); white-space: nowrap; }
    .ibtn { width: 28px; height: 26px; padding: 0; display: inline-flex; align-items: center; justify-content: center; color: var(--muted); } .ibtn.add { color: var(--accent); }
    .trole { display: flex; align-items: center; gap: 8px; padding: 8px 12px 0; font-size: 12.5px; flex-wrap: wrap; } .trole code { background: color-mix(in srgb, var(--accent) 14%, transparent); color: var(--accent); border-radius: 4px; padding: 1px 6px; }
    .tsec { padding: 4px 12px 12px; } .th { display: flex; align-items: center; gap: 8px; font-size: 12px; margin: 10px 0 6px; } .th .mut { font-size: 11.5px; }
    .pg { display: grid; grid-template-columns: repeat(auto-fill, minmax(76px, 1fr)); gap: 6px; align-items: end; } .pg label { display: flex; flex-direction: column; gap: 2px; font-size: 10.5px; color: var(--muted); min-width: 0; }
    .pg input { width: 100%; min-width: 0; color: var(--muted); } .pg input.set { color: inherit; border-color: var(--accent); } .padd { display: flex; gap: 4px; }
    .vals { display: grid; grid-template-columns: minmax(120px, 200px) 1fr 28px; gap: 6px; align-items: center; } .vals code { font-size: 12px; } .vals input { min-width: 0; }
    .tbvals { display: flex; flex-wrap: wrap; gap: 6px; align-items: center; padding: 0 10px 8px 46px; font-size: 12px; } .tbvals .chip { border: 1px solid var(--border); background: none; border-radius: 10px; padding: 1px 8px; cursor: pointer; } .tbvals .chip.want { border-style: dashed; color: var(--muted); } button.dr { text-align: left; padding: 0; }
    .tbd { grid-template-columns: minmax(130px, auto) minmax(120px, 1.4fr) 120px 90px minmax(80px, 1fr) 24px; cursor: default; } .dv:hover, .tbd:hover { background: var(--hover); } .dv.on { background: color-mix(in srgb, var(--accent) 12%, transparent); } .dv.had { opacity: .6; }
    .dr { font: 11.5px var(--mono, monospace) !important; color: var(--muted) !important; background: none !important; padding: 0 !important; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; } .dt { font-size: 11.5px; } .tbd.val .dn { font-family: var(--mono, monospace); }
    .tbv { overflow: auto; } .tbvbar { margin-bottom: 8px; gap: 6px; }
    .rc3 { display: grid; grid-template-columns: repeat(3, 1fr); gap: 8px; } .rc3 label { display: flex; flex-direction: column; gap: 3px; } .rc3 small { color: var(--muted); font-size: 11px; } .rc3 input, .rc3 select { width: 100%; }
    .pill.w { background: color-mix(in srgb, var(--warn, #e0a030) 18%, transparent); color: var(--warn, #b07a10); }
    .trbars { display: flex; align-items: flex-end; gap: 4px; height: 120px; padding: 10px 12px; }
    .trb { flex: 1; max-width: 26px; height: 100%; display: flex; flex-direction: column; border-radius: 3px; overflow: hidden; cursor: pointer; background: color-mix(in srgb, var(--text) 6%, transparent); } .trb:hover { outline: 2px solid var(--accent); }
    .trb i { display: block; flex-basis: 0; } .trb .tp { background: var(--ok, #3fbf75); } .trb .tf { background: var(--danger, #ef6161); } .trb .ts { background: var(--faint, #9aa1ab); }
    .trgrid { display: grid; grid-template-columns: 1fr 1fr; gap: 14px; margin-top: 14px; } .trrow { display: flex; align-items: center; gap: 10px; padding: 7px 12px; border-top: 1px solid var(--border); cursor: default; font-size: 12.5px; } .trrow[role=button] { cursor: pointer; } .trrow:hover { background: var(--hover); }
    .trrow .cn { flex: none; max-width: 40%; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; font-weight: 500; } .trrow .ell { flex: 1; min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; } .trv { display: block; flex: 1; min-width: 60px; height: 8px; border-radius: 4px; overflow: hidden; background: color-mix(in srgb, var(--danger, #ef6161) 55%, transparent); } .trv i { display: block; height: 100%; background: var(--ok, #3fbf75); }
    .chip.n { background: color-mix(in srgb, var(--text) 7%, transparent); color: var(--muted); }
    .pill.run { display: inline-flex; align-items: center; gap: 6px; background: color-mix(in srgb, var(--accent) 16%, transparent); color: var(--accent); } .spin.sm { width: 10px; height: 10px; border-width: 2px; }
    .vars { display: grid; grid-template-columns: max-content 1fr; gap: 3px 12px; font: 12px/1.45 var(--mono, monospace); margin-bottom: 8px; } .vars .vk { color: var(--accent); } .vars .vv { word-break: break-all; }
    .dbg { display: flex; gap: 6px; margin-top: 10px; } .dbg button { height: 26px; display: inline-flex; align-items: center; gap: 5px; font-size: 12px; }
    .chk { display: flex; gap: 8px; align-items: flex-start; font-size: 12.5px; cursor: pointer; } .chk input { margin-top: 2px; }
    .pics { display: flex; flex-wrap: wrap; gap: 6px; margin: 6px 0; } .pic { padding: 0; border: 1px solid var(--border); border-radius: 4px; overflow: hidden; height: 64px; min-width: 64px; background: none; } .pic img { height: 62px; display: block; } .pic.fail { border: 2px solid #f85149; }
    .bigpic { position: fixed; z-index: 1001; left: 50%; top: 50%; transform: translate(-50%, -50%); max-width: 92vw; max-height: 88vh; border-radius: 8px; box-shadow: 0 12px 40px #000a; cursor: zoom-out; }
    .f2 { display: grid; grid-template-columns: 110px 1fr; align-items: center; gap: 8px; }
    .webdbg .ok { color: var(--ok, #3fb950); } .inner.cur { outline: 1px solid var(--accent); border-radius: 6px; }
    .inner .st { width: 14px; text-align: center; } .inner .st.ok { color: #3fb950; } .inner .st.hw { color: #d29922; } .opts.inner { flex-wrap: nowrap; gap: 6px; padding-left: 22px !important; } .inner .cmdi { flex: 1 1 200px; min-width: 100px; } .inner .pin { flex: 0 1 160px; min-width: 70px; } .inner select { flex: 0 0 auto; } .heal > span:nth-child(2) { flex: 1 1 300px; } .inner .st.bad { color: #f85149; }
    .wn.sel { background: color-mix(in srgb, var(--accent) 30%, transparent); border-radius: 4px; color: inherit; }
    .heal { background: color-mix(in srgb, #d29922 14%, transparent); border: 1px solid color-mix(in srgb, #d29922 45%, transparent); border-radius: 8px; padding: 6px 10px; align-items: center; }
    label.bp { display: inline-flex; gap: 4px; align-items: center; } button.pick { font-size: 15px; }
    .mdlg.wide { width: min(640px, 94vw); max-height: 86vh; } .mdlg h3 { margin: 0; font-size: 16px; }
    .tbq { height: 30px; } .fpick { max-height: 300px; overflow: auto; border: 1px solid var(--border); border-radius: 8px; padding: 4px; }
    .fpick .fo { display: flex; align-items: center; gap: 8px; padding: 6px 10px; border-radius: 6px; cursor: pointer; } .fpick .fo:hover { background: var(--hover); } .fpick .fo.on { background: var(--accent-soft); box-shadow: inset 2px 0 var(--accent); }
    .fpick .fi { width: 14px; height: 11px; border-radius: 2px; background: var(--folder, #d9b45a); flex: none; } .fpick .ell { flex: 1; min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
    .segs { display: flex; align-items: center; gap: 6px; } .segs .mut { width: 80px; } .segs button { height: 28px; padding: 0 12px; } .segs button.on { background: var(--accent-soft) !important; border-color: var(--accent) !important; color: var(--text) !important; }
    .sbtn { height: 28px; padding: 0 10px; font-size: 12.5px; }
    .savb { background: var(--accent) !important; color: #fff !important; border-color: transparent !important; font-weight: 600; } .savb:hover { filter: brightness(1.1); }
    .selbar > * { white-space: nowrap; } .selbar { display: flex; align-items: center; gap: 8px; margin: 8px; padding: 8px 10px; border-radius: 9px; border: 1px solid var(--border); background: color-mix(in srgb, var(--text) 4%, transparent); }
    .selbar.col { flex-direction: column; align-items: stretch; } .selbar .full { justify-content: center; width: 100%; } .selbar b { font-weight: 600; font-size: 12.5px; }
    .main { position: relative; background: var(--bg); display: flex; flex-direction: column; min-width: 0; min-height: 0; }
    .head { padding: 12px 20px 0; border-bottom: 1px solid var(--border); } .crumb { color: var(--muted); font-size: 12px; }
    .title { display: flex; align-items: center; gap: 8px; margin: 3px 0 10px; } .title h1 { font-size: 17px; margin: 0; font-weight: 600; letter-spacing: -.01em; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; flex: 0 1 auto; min-width: 120px; } .title button { white-space: nowrap; flex: none; } .title .mut.fl { flex: 1 1 0; }
    .pill { font-size: 11.5px; padding: 2px 9px; border-radius: 10px; white-space: nowrap; background: color-mix(in srgb, var(--text) 8%, transparent); color: var(--muted); }
    .pill.f { background: color-mix(in srgb, var(--danger, #ef6161) 18%, transparent); color: var(--danger, #ef6161); } .pill.p { background: color-mix(in srgb, var(--ok, #3fbf75) 18%, transparent); color: var(--ok, #3fbf75); } .pill.s { background: color-mix(in srgb, var(--warn, #e0a030) 18%, transparent); color: var(--warn, #e0a030); } .pill.sm { font-size: 10.5px; padding: 0 6px; margin-left: 6px; }
    .tabs { display: flex; align-items: center; gap: 18px; } .tabs > button { border: 0 !important; border-radius: 0 !important; box-shadow: none !important; min-height: 0 !important; height: auto !important; outline: none; background: none !important; color: var(--muted); padding: 8px 0; border-bottom: 2px solid transparent; cursor: pointer; } .tabs > button.on { color: var(--text); box-shadow: inset 0 -2px var(--accent) !important; font-weight: 600; }
    .wrseg { align-self: flex-start; margin-bottom: 4px; } .wrseg button { padding: 4px 14px !important; border: 0 !important; box-shadow: none !important; width: auto !important; min-width: max-content; flex: 0 0 auto; white-space: nowrap; } .wae button { white-space: nowrap; }
    .seg { display: flex; gap: 2px; background: color-mix(in srgb, var(--text) 6%, transparent); border-radius: 7px; padding: 2px; } .seg button { border: 0; background: none; padding: 2px 10px; border-radius: 5px; color: var(--muted); cursor: pointer; } .seg button.on { background: color-mix(in srgb, var(--accent) 28%, transparent); color: var(--text); }
    .body { flex: 1; overflow: auto; padding: 16px 20px; display: flex; flex-direction: column; gap: 14px; min-height: 0; } .body p { margin: 0; }
    .meta { display: flex; flex-wrap: wrap; gap: 6px 22px; color: var(--muted); font-size: 12px; } .meta b { color: var(--text); font-weight: 500; }
    .sec { border: 1px solid var(--border); border-radius: 10px; overflow: hidden; flex: none; }
    .sh { display: flex; align-items: center; gap: 8px; padding: 8px 12px; background: var(--panel); font-size: 11.5px; color: var(--muted); letter-spacing: .05em; text-transform: uppercase; } .sh .norm { text-transform: none; letter-spacing: 0; }
    .srow { display: grid; grid-template-columns: 26px minmax(80px, 140px) minmax(160px, 1.3fr) minmax(160px, 1fr) 78px 60px; gap: 10px; align-items: center; padding: 8px 12px; border-top: 1px solid var(--border); cursor: pointer; }
    .srow:hover { background: var(--hover, rgba(255,255,255,.03)); } .srow.f { background: color-mix(in srgb, var(--danger, #ef6161) 6%, transparent); } .srow.open { background: color-mix(in srgb, var(--accent) 7%, transparent); }
    .srow.hd { cursor: default; background: var(--panel); color: var(--muted); font-size: 11.5px; padding: 5px 12px; }
    .dev { color: var(--muted); font-size: 12px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; } .cmd { font-family: var(--mono, monospace); font-size: 12.5px; overflow-wrap: anywhere; }
    .chips { display: flex; flex-wrap: wrap; gap: 4px; } .chip { font-size: 11.5px; padding: 1px 8px; border-radius: 9px; background: color-mix(in srgb, var(--ok, #3fbf75) 14%, transparent); color: var(--ok, #3fbf75); }
    .chip.b { background: color-mix(in srgb, var(--danger, #ef6161) 14%, transparent); color: var(--danger, #ef6161); } .chip.w { background: color-mix(in srgb, var(--warn, #e0a030) 14%, transparent); color: var(--warn, #e0a030); }
    .x { border: 0; background: none; color: inherit; cursor: pointer; padding: 0 0 0 4px; }
    /* the web step card */
    .wapp { height: 28px; display: inline-flex; align-items: center; gap: 6px; max-width: 280px; border-radius: 14px; padding: 0 10px 0 8px; flex: none; }
    .wapp .wic { font-size: 12px; } .wapp code { font-size: 11px; color: var(--muted); } .wapp .cv { color: var(--muted); font-size: 10px; }
    .wurl { font-family: var(--mono); min-width: 120px; }
    .wpill { height: 26px; font-size: 11.5px; color: var(--muted); border-radius: 13px; padding: 0 10px; white-space: nowrap; flex: none; } .wpill.on { color: var(--accent); border-color: var(--accent); }
    .wlast { font-size: 11.5px; white-space: nowrap; color: var(--muted); flex: none; } .wlast.ok { color: var(--ok); } .wlast.bad { color: var(--danger); }
    .wdbg { display: flex; align-items: center; gap: 8px; padding: 6px 10px; margin: 4px 0 6px 30px; border-radius: 8px; background: color-mix(in srgb, var(--ok) 8%, transparent); border: 1px solid color-mix(in srgb, var(--ok) 25%, transparent); font-size: 12px; }
    .wdbg button { height: 24px; font-size: 12px; }
    .wset { display: flex; flex-wrap: wrap; gap: 8px 16px; align-items: center; margin: 4px 0 6px 30px; padding: 10px 12px; border: 1px solid var(--border); border-radius: 8px; font-size: 12px; color: var(--muted); }
    .wset label { display: inline-flex; align-items: center; gap: 6px; }
    .wacts { margin: 2px 0 0 30px; display: flex; flex-direction: column; gap: 1px; }
    .wa { border-radius: 7px; border: 1px solid transparent; } .wa.open { border-color: var(--border); background: color-mix(in srgb, var(--text) 3%, transparent); }
    .wa.cur { box-shadow: inset 3px 0 0 var(--accent); } .wa.drop { box-shadow: inset 0 2px 0 var(--accent); }
    .war { display: flex; align-items: center; gap: 8px; padding: 4px 8px; min-height: 34px; cursor: pointer; border-radius: 7px; } .war:hover { background: var(--hover); }
    .wh { color: var(--faint); cursor: grab; font-size: 11px; letter-spacing: -2px; opacity: 0; } .war:hover .wh { opacity: 1; }
    .wn { min-width: 22px; height: 20px; padding: 0 4px; font-size: 11.5px; color: var(--faint); background: none; border-color: transparent; } .wn.sel { background: var(--accent-soft); color: var(--accent); border-color: var(--accent); }
    .wth { width: 52px; height: 30px; object-fit: cover; object-position: top left; border-radius: 4px; border: 1px solid var(--border); flex: none; cursor: zoom-in; background: #fff; }
    .wsay { flex: 1; min-width: 0; font-size: 13px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; } .wa.chk .wsay { color: var(--text); }
    .wsay .ck { color: var(--ok); margin-right: 5px; } .wv { font: 11.5px var(--mono); color: var(--muted); margin-left: 8px; }
    .wsts { width: 16px; text-align: center; font-size: 12px; color: var(--faint); flex: none; } .wsts.ok { color: var(--ok); } .wsts.bad { color: var(--danger); } .wsts.hw { color: var(--warn); }
    .wtools { display: inline-flex; gap: 2px; opacity: 0; flex: none; } .war:hover .wtools, .wa.open .wtools { opacity: 1; } .wtools .lnk { padding: 0 5px; }
    .wae { display: flex; gap: 8px; align-items: center; padding: 2px 10px 6px 64px; } .wae select { width: 150px; flex: none; } .wae .pin { width: 190px; flex: none; }
    .wae > .wtg { flex: 1 1 auto !important; display: flex; align-items: center; gap: 6px; min-width: 0; } .wtg > input { flex: 1 1 auto !important; min-width: 0; width: auto; }
    .walt { padding: 0 10px 8px 64px; font-size: 11.5px; display: flex; flex-wrap: wrap; gap: 4px 10px; align-items: center; } .walt code { font-size: 11px; color: var(--muted); }
    .walt .bad { color: var(--danger); }
    .wempty { padding: 10px 12px; font-size: 12.5px; }
    .wfoot { display: flex; align-items: center; gap: 8px; margin: 8px 0 4px 30px; flex-wrap: wrap; }
    .wfoot2 { display: flex; margin: 6px 0 4px 30px; } .sbox > .lnk { align-self: flex-start; }
    .wai { flex: 1; min-width: 0; display: flex; align-items: center; gap: 6px; height: 30px; border: 1px solid color-mix(in srgb, #a371f7 40%, transparent); background: color-mix(in srgb, #a371f7 7%, transparent); border-radius: 8px; padding: 0 4px 0 10px; }
    .wai .aic { color: #c297ff; } .wai input { flex: 1; min-width: 0; border: 0; background: transparent; height: 26px; } .wai button { height: 24px; font-size: 12px; } .wai .mut { font-size: 11.5px; white-space: nowrap; }
    .wai.busy { border-color: #a371f7; }
    /* the editor: steps, with devices, values and settings beside them */
    .elay { display: grid; grid-template-columns: minmax(0, 1fr) 290px; gap: 14px; align-items: start; }
    @media (max-width: 1180px) { .elay { grid-template-columns: minmax(0, 1fr); } .eside { order: -1; } }
    .eside { position: sticky; top: 8px; display: flex; flex-direction: column; gap: 10px; }
    .sbox { border: 1px solid var(--border); border-radius: 10px; padding: 10px 12px; background: var(--panel); display: flex; flex-direction: column; gap: 5px; }
    .sbox h4 { margin: 0 0 2px; font-size: 10.5px; letter-spacing: .07em; text-transform: uppercase; color: var(--muted); font-weight: 600; }
    .srole { display: flex; align-items: center; gap: 6px; margin-top: 4px; } .srole .rolec { font-size: 12px; } .sk { font-size: 10.5px; color: var(--muted); border: 1px solid var(--line); border-radius: 8px; padding: 0 6px; }
    .srole .lnk.on { color: var(--accent); }
    .srin { height: 26px; font-size: 12px; width: 100%; }
    .sbox .needs { display: flex; flex-wrap: wrap; gap: 6px; font-size: 12px; } .sbox .needs select, .sbox .needs .pin { max-width: 100%; }
    .sval { display: grid; grid-template-columns: auto minmax(0, 1fr) auto; gap: 6px; align-items: center; } .sval code { font-size: 12px; color: var(--accent); } .sval .pin { width: 100%; height: 26px; }
    .sset { display: flex; justify-content: space-between; gap: 10px; font-size: 12px; color: var(--muted); } .sset b { color: var(--text); font-weight: 500; text-align: right; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
    .sfoot { margin-top: 4px; }
    .setsh { display: flex; align-items: center; gap: 10px; margin: 0 0 6px; }
    .wflow { display: inline; line-height: 1.7; } .wflow i { color: var(--faint); font-style: normal; margin: 0 6px; } .wpath { font-family: var(--mono); color: var(--muted); }
    .exp { display: grid; grid-template-columns: minmax(0, 1fr) minmax(0, 1.2fr); gap: 16px; padding: 12px 12px 14px 48px; border-top: 1px solid var(--border); background: color-mix(in srgb, var(--text) 3%, transparent); }
    .exp h4 { margin: 0 0 6px; font-size: 11px; color: var(--muted); font-weight: 500; text-transform: uppercase; letter-spacing: .05em; } .exp h4 + * { margin-bottom: 10px; }
    .ev { display: grid; grid-template-columns: auto minmax(0, 1fr) minmax(0, 1fr); gap: 4px 12px; font-size: 12px; } .ev span { overflow-wrap: anywhere; }
    .exp pre { margin: 0; font: 12px/1.5 var(--mono, monospace); background: var(--cbg); border-radius: 7px; padding: 8px 10px; max-height: 280px; overflow: auto; white-space: pre-wrap; }
    .tbw { max-height: 220px; overflow: auto; border: 1px solid var(--border); border-radius: 7px; } .ptab { border-collapse: collapse; font: 12px var(--mono, monospace); } .ptab th { position: sticky; top: 0; background: var(--panel); text-align: left; padding: 2px 8px; color: var(--muted); font-weight: 500; } .ptab td { padding: 2px 8px; border-top: 1px solid var(--border); white-space: nowrap; }
    .trow { display: flex; gap: 8px; font-size: 12px; } .none { color: var(--muted); font-size: 12px; padding: 10px 12px; border-top: 1px solid var(--border); }
    .ed { padding: 9px 12px; border-top: 1px solid var(--border); display: flex; flex-direction: column; gap: 5px; } .num { width: 18px; color: var(--muted); font-weight: 500; }
    .ses { width: 160px; height: 27px; } .cmdi { flex: 1; min-width: 0; height: 27px; font-family: var(--mono, monospace); } .num2 { width: 60px; height: 24px; } .pin { width: 100px; height: 24px; }
    .opts .wide { flex: 1; } .opts .wide .cmdi { height: 26px; }
    .dots { display: flex; gap: 3px; flex-wrap: wrap; } .dots i { width: 10px; height: 18px; border-radius: 3px; } .dp { background: var(--ok, #3fbf75); } .df { background: var(--danger, #ef6161); } .dw { background: var(--warn, #e0a030); }
    .vrow { display: grid; grid-template-columns: minmax(160px, 1fr) 90px 90px 1.2fr; gap: 10px; padding: 7px 12px; border-top: 1px solid var(--border); font-size: 12.5px; align-items: center; } .bar2 { display: flex; height: 6px; border-radius: 3px; overflow: hidden; background: color-mix(in srgb, var(--text) 8%, transparent); }
    .diff { margin: 0 0 10px; font: 12px/1.45 var(--mono, monospace); background: var(--cbg); border-radius: 7px; padding: 8px 10px; max-height: 220px; overflow: auto; white-space: pre-wrap; }
    .da { color: var(--ok, #3fbf75); } .dr { color: var(--danger, #ef6161); } .cmp { display: block; margin-bottom: 8px; }
    .inner { padding-left: 46px !important; } .body.cmdi, textarea.body { height: auto !important; min-height: 44px; resize: vertical; }
    .srow:focus-visible, .case:focus-visible, .rrow:focus-visible, button:focus-visible { outline: 2px solid var(--accent); outline-offset: -2px; }
    .console { position: relative; display: flex; flex-direction: column; border-bottom: 1px solid var(--border); background: #111317; flex: none; }
    .main.conmax > :not(.live):not(.console) { display: none !important; } .console.max { flex: 1; min-height: 0; border: 0; } .console.max pre { flex: 1; height: auto; }
    .console .ch .lnk { display: inline-flex; align-items: center; gap: 4px; }
    .cgrip { position: absolute; left: 0; right: 0; bottom: -5px; height: 10px; cursor: row-resize; z-index: 9; display: flex; justify-content: center; align-items: center; }
    .cgrip::after { content: ''; width: 44px; height: 4px; border-radius: 3px; background: color-mix(in srgb, var(--text) 30%, transparent); }
    .cgrip:hover::after, .cgrip:active::after { background: var(--accent); width: 100%; height: 3px; border-radius: 0; }
    .console .ch { display: flex; align-items: center; gap: 10px; padding: 5px 20px; font-size: 12px; border-bottom: 1px solid var(--border); background: var(--panel); }
    .console .con { min-height: 60px; overflow: auto; padding: 6px 20px 10px; font: 12px/1.45 var(--mono, monospace); color: #cfd3da; }
    .console.max .con { flex: 1; height: auto; }
    .lhd { margin: 12px 0 2px; color: #e8eaee; } .lhd .lar { color: #7aa7ff; } .lhd .lcase { color: #9aa1ab; } .lhd b { font-weight: 600; }
    .lout { margin: 0; white-space: pre-wrap; color: #b8bec8; font: inherit; }
    .lchk { border-collapse: collapse; margin: 6px 0 4px 14px; font: 11.5px/1.4 var(--mono, monospace); min-width: 50%; }
    .lchk th { text-align: left; color: #8b919b; font-weight: 500; padding: 3px 10px; border-bottom: 1px solid #2c2f36; } .lchk td { padding: 3px 10px; border-bottom: 1px solid #22252b; color: #d6dae1; vertical-align: top; }
    .lchk td.lm { color: #4ade80; width: 14px; } .lchk tr.bad td { color: #fca5a5; } .lchk tr.bad td.lm { color: #f87171; } .lchk tr:not(.bad) td:nth-child(3) { color: #86efac; }
    .lres { margin: 4px 0 0 14px; display: flex; align-items: baseline; gap: 10px; flex-wrap: wrap; font-weight: 600; } .lres.ok { color: #4ade80; } .lres.bad { color: #f87171; } .lres.skip { color: #9aa1ab; }
    .lres .lms { font-weight: 400; color: inherit; opacity: .85; } .lres .lerr { font-weight: 400; color: #fca5a5; } .lres .lnote { font-weight: 400; color: #8b919b; }
    .console pre.unused { margin: 0; min-height: 60px; overflow: auto; padding: 8px 20px; font: 12px/1.45 var(--mono, monospace); color: #cfd3da; white-space: pre-wrap; }
    .lg-ok { color: #4ade80; } .lg-bad { color: #f87171; } .lg-hd { color: #e8eaee; font-weight: 600; }
    .mdlg.logdlg { width: min(1400px, 96vw); height: min(88vh, 1100px); } .logpre { flex: 1; margin: 0; overflow: auto; padding: 10px 12px; border-radius: 8px; background: #111317; color: #cfd3da; font: 12px/1.5 var(--mono, monospace); white-space: pre; }
    .live { display: flex; align-items: center; gap: 10px; padding: 7px 20px; background: color-mix(in srgb, var(--accent) 12%, var(--panel)); border-bottom: 1px solid var(--border); font-size: 12.5px; }
    .spin { width: 12px; height: 12px; border-radius: 50%; border: 2px solid color-mix(in srgb, var(--accent) 30%, transparent); border-top-color: var(--accent); animation: sp 0.9s linear infinite; } @keyframes sp { to { transform: rotate(360deg); } }
    .scrim.fixed { position: fixed; z-index: 50; } .mdlg { position: fixed; z-index: 51; top: 50%; left: 50%; transform: translate(-50%, -50%); width: min(560px, 92vw); display: flex; flex-direction: column; gap: 10px; padding: 20px 22px; border-radius: 12px; background: var(--panel); border: 1px solid var(--border); box-shadow: 0 24px 60px rgba(0,0,0,.45); }
    .mdlg h2 { margin: 0; font-size: 16px; } .mdlg .inst { font-size: 15px; line-height: 1.5; margin: 0; } .mdlg p { margin: 0; } .mdlg textarea { resize: vertical; }
    .okbtn { background: var(--ok, #3fbf75) !important; color: #fff !important; border-color: transparent !important; } .badbtn { background: var(--danger, #ef6161) !important; color: #fff !important; border-color: transparent !important; }
    .expd { display: block; font: 11.5px var(--ui-font, inherit); color: var(--muted); margin-top: 2px; } .cmd.manual { font-family: var(--ui-font, inherit); font-style: italic; }
    .chip.m { background: color-mix(in srgb, var(--accent) 14%, transparent); color: var(--accent); }
    .onb { margin: 8px 4px; padding: 14px; border: 1px dashed var(--border); border-radius: 10px; display: flex; flex-direction: column; gap: 8px; font-size: 12.5px; }
    .onb ol { margin: 0; padding-left: 18px; display: flex; flex-direction: column; gap: 6px; color: var(--text); } .onb button { justify-content: center; }
    .logic { display: inline-block; margin-left: 8px; font: 11px var(--ui-font, inherit); color: var(--accent); background: color-mix(in srgb, var(--accent) 12%, transparent); padding: 0 6px; border-radius: 8px; }
    .opts { flex-wrap: wrap; font-size: 12px; color: var(--muted); gap: 16px; padding-left: 24px; } .opts label { display: flex; align-items: center; gap: 4px; } .ed .chips { padding-left: 24px; }
    .addck { display: inline-flex; gap: 4px; } .addck select, .addck input { height: 24px; font-size: 12px; } .addck input { width: 220px; }
    .codebar { display: flex; align-items: center; gap: 10px; flex-wrap: nowrap; } .codebar > .mut { flex: 1 1 0; min-width: 0; }
    .lang { display: inline-flex; padding: 3px; gap: 2px; border-radius: 8px; background: color-mix(in srgb, var(--text) 6%, transparent); border: 1px solid var(--border); }
    .lang button { height: 24px !important; padding: 0 12px !important; border: 0 !important; border-radius: 6px !important; background: none !important; color: var(--muted) !important; font-size: 12px !important; }
    .lang button.on { background: var(--panel) !important; color: var(--text) !important; box-shadow: 0 1px 2px rgba(0,0,0,.2) !important; } .lang button:focus, .tabs > button:focus { outline: none; box-shadow: none; }
    .hl { tab-size: 4; } :host { --cbg: color-mix(in srgb, var(--text) 4%, var(--bg)); --hc: #7f8b99; --hs: #a5d6a7; --hk: #82aaff; --hw: #c792ea; --hn: #f78c6c; --hv: #ffcb6b; --hf: #89ddff; }
    :host-context(.light) { --hc: #6a737d; --hs: #22863a; --hk: #005cc5; --hw: #8a3ffc; --hn: #b5520b; --hv: #a15c00; --hf: #0b7285; }
    :host ::ng-deep .hl-c { color: var(--hc); font-style: italic; } :host ::ng-deep .hl-s { color: var(--hs); } :host ::ng-deep .hl-k { color: var(--hk); } :host ::ng-deep .hl-w { color: var(--hw); } :host ::ng-deep .hl-n { color: var(--hn); } :host ::ng-deep .hl-v { color: var(--hv); } :host ::ng-deep .hl-f { color: var(--hf); }
    .code { flex: 1; min-height: 260px; font: 12.5px/1.5 var(--mono, monospace); resize: none; white-space: pre; overflow: auto; margin: 0; padding: 10px; border-radius: 8px; border: 1px solid var(--border); background: var(--cbg); color: var(--text); }
    .kpis { display: grid; grid-template-columns: repeat(4, 1fr); gap: 10px; } .kpi { border: 1px solid var(--border); border-radius: 10px; padding: 10px 12px; background: var(--panel); } .kpi b { display: block; font-size: 21px; font-weight: 600; } .kpi span { color: var(--muted); font-size: 12px; } .kok { color: var(--ok, #3fbf75); } .kbad { color: var(--danger, #ef6161); } .kwarn { color: var(--warn, #e0a030); }
    .bar { display: flex; height: 6px; border-radius: 3px; overflow: hidden; background: color-mix(in srgb, var(--text) 8%, transparent); flex: none; } .okb { background: var(--ok, #3fbf75); } .badb { background: var(--danger, #ef6161); } .warnb { background: var(--warn, #e0a030); }
    .rhd, .rrow { display: grid; grid-template-columns: 26px minmax(140px, 1fr) minmax(200px, 1.6fr) 64px; gap: 10px; padding: 8px 12px; align-items: center; } .rhd { background: var(--panel); color: var(--muted); font-size: 11.5px; padding: 5px 12px; } .rrow { border-top: 1px solid var(--border); cursor: pointer; } .rrow:hover { background: var(--hover, rgba(255,255,255,.03)); }
    .rq { grid-template-columns: 26px minmax(140px, 1fr) minmax(200px, 1.6fr) 130px 64px; }
    .form { display: flex; flex-direction: column; gap: 10px; max-width: 620px; } .form label { display: flex; align-items: center; gap: 10px; } .form label > span { width: 130px; color: var(--muted); font-size: 12px; flex: none; } .form input:not([type=checkbox]), .form select { height: 29px; flex: 1; } .form .ck { padding-left: 140px; }
    .empty { margin: auto; }
    .appr { display: flex; flex-direction: column; gap: 3px; padding: 7px 10px; margin: 2px 6px; border: 1px solid var(--border); border-radius: 8px; } .aact { display: flex; gap: 6px; margin-top: 3px; } .aact button { height: 24px; padding: 0 10px; }
    .scrim { position: absolute; inset: 0; background: rgba(0,0,0,.35); z-index: 5; }
    .drawer { position: absolute; top: 0; right: 0; bottom: 0; width: 380px; z-index: 6; display: flex; flex-direction: column; gap: 14px; padding: 18px; background: var(--panel); border-left: 1px solid var(--border); box-shadow: -20px 0 40px rgba(0,0,0,.35); }
    .drawer h2 { margin: 0; font-size: 16px; } .fld { display: flex; flex-direction: column; gap: 6px; } .fld > span { color: var(--muted); font-size: 12px; } .fld select { height: 30px; }
    .radio { display: flex; gap: 8px; align-items: flex-start; padding: 8px 10px; border: 1px solid var(--border); border-radius: 8px; cursor: pointer; } .radio.on { border-color: var(--accent); background: color-mix(in srgb, var(--accent) 10%, transparent); } .radio input { margin-top: 3px; } .radio small { display: block; color: var(--muted); font-size: 11.5px; }
  `],
})
export class TestsView implements OnInit, OnDestroy {
  readonly visible = input(true);
  readonly backend = inject(Backend);
  readonly testBrowsers = inject(TestBrowsers);
  private readonly ui = inject(Ui);
  private readonly tabsSvc = inject(Tabs);
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
  readonly section = signal<Section>('tests');
  /** The test list's width (drag its right edge; kept for next time). */
  readonly sideW = signal((() => { try { return Number(localStorage.getItem('tw.tests.side')) || 360; } catch { return 360; } })());
  keepSide() { try { localStorage.setItem('tw.tests.side', String(this.sideW())); } catch { /* not kept */ } }
  readonly conMax = signal(false);
  readonly conH = signal((() => { try { return Number(localStorage.getItem('tw.tests.conh')) || 240; } catch { return 240; } })());
  keepCon() { try { localStorage.setItem('tw.tests.conh', String(this.conH())); } catch { /* not kept */ } }
  dragCon(ev: MouseEvent) {
    ev.preventDefault();
    const y0 = ev.clientY, h0 = this.conH();
    const move = (e: MouseEvent) => this.conH.set(Math.max(80, Math.min(window.innerHeight - 260, h0 + e.clientY - y0)));
    const up = () => { document.removeEventListener('mousemove', move); document.removeEventListener('mouseup', up); this.keepCon(); };
    document.addEventListener('mousemove', move); document.addEventListener('mouseup', up);
  }
  dragSide(ev: MouseEvent) {
    ev.preventDefault();
    const x0 = ev.clientX, w0 = this.sideW();
    const move = (e: MouseEvent) => this.sideW.set(Math.max(220, Math.min(Math.max(320, window.innerWidth - 420), w0 + e.clientX - x0)));
    const up = () => { document.removeEventListener('mousemove', move); document.removeEventListener('mouseup', up); this.keepSide(); };
    document.addEventListener('mousemove', move); document.addEventListener('mouseup', up);
  }
  readonly rails: { k: Section; l: string; i: string; t: string }[] = [
    { k: 'tests', l: 'Tests', i: 'checklist', t: 'Tests: record, edit and run' }, { k: 'runs', l: 'Runs', i: 'play', t: 'Past runs and their results' },
    { k: 'schedules', l: 'Schedules', i: 'clock', t: 'Scheduled runs' }, { k: 'testbeds', l: 'Testbeds', i: 'hosts', t: 'Which devices a run uses' }, { k: 'procs', l: 'Procedures', i: 'layers', t: 'Shared steps with parameters that tests call' }, { k: 'library', l: 'Library', i: 'file', t: 'Templates and code helpers' }];
  readonly q = signal('');
  readonly tag = signal('all');
  readonly folded = signal<string[]>([]);
  readonly runSel = signal('');
  readonly runRep = signal<TestRunReport | null>(null);
  readonly fileSel = signal('');
  fileText = '';
  readonly runFiles = signal<string[] | null>(null);
  readonly runLabel = signal('');
  private readonly open = signal<string[]>([]);
  readonly cleanupOpts: { k: CleanupMode; l: string; d: string }[] = [
    { k: 'off', l: 'No cleanup', d: 'the next test starts from what this one configured' },
    { k: '', l: 'Run the cleanup steps', d: 'also when a step failed' },
    { k: 'auto', l: 'Cleanup steps, else undo the configuration', d: "from the device type's undo rules, the last change first" }];
  /** The test being edited in the Steps view (a copy; Save writes it). */
  readonly draft = signal<TestCase | null>(null);
  readonly phases: { k: Phase; l: string }[] = [{ k: 'setup', l: 'Setup' }, { k: 'steps', l: 'Steps' }, { k: 'cleanup', l: 'Cleanup' }];
  readonly methods = ['GET', 'POST', 'PUT', 'PATCH', 'DELETE'];
  private cks = new Map<string, { k: string; v: string }>();
  cleanupMode: CleanupMode = (() => { try { const v = localStorage.getItem('tw.tests.cleanup'); return (v === null ? 'off' : v) as CleanupMode; } catch { return 'off'; } })();
  onCleanupFail: OnCleanupFail = (() => { try { return (localStorage.getItem('tw.tests.ocf') as OnCleanupFail) || 'stop'; } catch { return 'stop'; } })();
  /** The last result of each test file, for the tree's dots. */
  readonly last = signal<Record<string, TestResult>>({});

  readonly groups = computed(() => {
    const m = new Map<string, TestCaseInfo[]>();
    for (const c of this.filtered()) {
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
    effect(() => { if (this.tabsSvc.testsAction() === 'web-rec') { this.tabsSvc.testsAction.set(''); queueMicrotask(() => this.openWebRec()); } });
  }

  ngOnInit() {
    void this.load();
    void this.backend.testLastRun().then(r => { if (r) this.took(r); }).catch(() => {});
    this.offs.push(this.backend.on('tests:schedule', () => void this.load()));
    this.offs.push(this.backend.on('tests:progress', (r: TestResult) => { if (r.file) this.last.update(m => ({ ...m, [r.file!]: r })); }));
    this.offs.push(this.backend.on('tests:step', (e: { case: string; step: TestStepResult }) => {
      this.liveStep.set(e); this.liveCount.update(n => n + 1);
      this.pushLive({ k: 'res', s: e.step });
    }));
    this.offs.push(this.backend.on('tests:paused', (e: { case: string; step: TestStepResult; breakpoint?: boolean }) => { this.paused.set(e); this.pauseVars.set(Object.entries(e.step.vars ?? {}).sort((a, b) => a[0].localeCompare(b[0])).map(([k, v]) => ({ k, v }))); this.showLive.set(true); }));
    this.offs.push(this.backend.on('tests:resumed', () => this.paused.set(null)));
    this.offs.push(this.backend.on('tests:step-start', (e: { case: string; step: string }) => this.pushLive({ k: 'head', c: e.case, t: e.step })));
    this.offs.push(this.backend.on('tests:out', (e: { session: string; text: string }) => this.addLive(e.text)));
    this.offs.push(this.backend.on('tests:manual', (m: { id: string; name: string; instruction: string; expected: string }) => { this.manualNote = ''; this.manual.set(m); }));
    this.offs.push(this.backend.on('tests:manual-done', (id: string) => { if (this.manual()?.id === id) this.manual.set(null); }));
    this.timer = setInterval(() => { if (this.visible()) void this.poll(); }, 4000);
  }
  ngOnDestroy() { clearInterval(this.timer); this.offs.forEach(f => f()); }

  private async load() {
    try {
      this.tree.set(await this.backend.testsTree());
      this.git.set(await this.backend.testsGit());
      this.schedules.set(await this.backend.testSchedules().catch(() => []) ?? []);
      void this.loadBookings(); void this.loadWebSt();
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
      this.sched.set(null); this.draft.set(null); this.open.set([]);
    }
    this.sel.set(file);
    try { this.test.set(await this.backend.testCase(file)); } catch (e) { this.test.set(null); this.ui.toast(errText(e), 'error'); }
    if (keepView && this.view() === 'yaml' && this.yamlEdit()) return; // unsaved edits stay
    await this.setView(keepView ? this.view() : 'steps');
  }

  readonly hist = signal<TestHistory | null>(null);
  readonly yamlEdit = signal(false);
  private yamlText = signal('');
  readonly codeHtml = computed(() => { const v = this.view(); return highlight(v === 'yaml' ? this.yamlText() : this.goSrc(), (v === 'yaml' ? 'yaml' : v) as Lang); });
  rate(h: TestHistory) { const n = h.passed + h.failed; return n ? Math.round(h.passed * 100 / n) : 0; }
  verText(v?: Record<string, string>) { const l = [...new Set(Object.values(v ?? {}))]; return l.join(', '); }
  async setView(v: View) {
    this.view.set(v);
    if (v === 'history') { const l = await this.backend.testHistory(this.sel(), 50).catch(() => []); this.hist.set(l?.[0] ?? null); return; }
    const f = this.sel();
    if (v === 'yaml') { this.yaml = await this.backend.testFile(f).catch(e => '# ' + errText(e)); this.yamlText.set(this.yaml); }
    if (v === 'go' || v === 'py' || v === 'tcl') this.goSrc.set(await this.backend.testFile(v === 'go' ? this.goFile() : v === 'py' ? this.pyFile() : this.tclFile()).catch(() => tr('No generated code yet: save the test (YAML → Save) to write it.')));
  }

  async saveYaml() {
    try {
      await this.backend.testSaveYAML(this.sel(), this.yaml);
      this.ui.toast(tr('Saved'), 'success');
      this.yamlEdit.set(false);
      await this.load();
      await this.setView('yaml');
    } catch (e) { this.ui.toast(errText(e), 'error'); }
  }

  async run(files: string[], confirmed = false, forceLock = false, extra: { from?: number; only?: number } = {}): Promise<void> {
    this.paused.set(null); this.runningFiles = files;
    this.liveStep.set(null); this.liveCount.set(0); this.liveLog.set([]); this.livePart = ''; this.showLive.set(true);
    if (!confirmed && this.cleanupMode !== 'off' && this.onCleanupFail === 'factory' && !(await this.ui.confirm(tr('Factory Default on a Failed Cleanup?'),
      tr('When a cleanup fails, the devices of that test are reset to factory default and rebooted, then the run goes on. A factory default may remove the management address: use console sessions. Production sessions are never reset.'), tr('Run'), true))) return;
    this.busy.set(true);
    try {
      const cleanup = extra.only ? 'off' : this.cleanupMode || undefined;
      this.took(await this.backend.runTests({ files, testbed: this.testbed || undefined, vars: Object.keys(this.runVars).length ? { ...this.runVars } : undefined, confirmed, forceLock, onCleanupFail: cleanup === 'off' ? undefined : this.onCleanupFail, cleanup, from: extra.from, only: extra.only, pauseOnFail: this.pauseOnFail, rerun: this.rerun || undefined, caseTimeout: this.caseMin && this.caseMin > 0 ? Math.round(this.caseMin * 60) : undefined, runTimeout: this.runMin && this.runMin > 0 ? Math.round(this.runMin * 60) : undefined, breaks: files.length === 1 ? this.breaksFor(files[0]) : undefined, stepMode: this.stepMode || undefined }));
      this.stepMode = false;
      await this.load();
    } catch (e) {
      const t = errText(e);
      if (!confirmed && /production sessions/i.test(t)) {
        this.busy.set(false);
        if (await this.ui.confirm(tr('Run on Production?'), t + '. ' + tr('Their commands and requests are sent to those devices.'), tr('Run'), true)) return this.run(files, true, forceLock, extra);
        return;
      }
      if (!forceLock && /is booked by/i.test(t)) {
        this.busy.set(false);
        if (await this.ui.confirm(tr('Testbed Booked'), t + '. ' + tr('Run anyway? Their work on it may be disturbed.'), tr('Run Anyway'), true)) return this.run(files, confirmed, true, extra);
        return;
      }
      if (/needs an approval/i.test(t)) { this.busy.set(false); this.showLive.set(false); return this.askApproval(files, this.testbed || '', t); }
      this.ui.toast(t, 'error');
    }
    this.busy.set(false);
  }

  private took(r: TestRunReport) {
    this.report.set(r);
    if (this.runSel() === r.dir || (r.results?.length ?? 0) > 1) { this.runRep.set(r); this.runSel.set(r.dir); }
    if ((r.results?.length ?? 0) > 1 && this.section() === 'tests') this.section.set('runs');
    const f = r.results?.find(x => x.file === this.sel() && !x.passed);
    if (f) { const i = f.steps.findIndex(s => !s.passed && !s.skipped); if (i >= 0) { const st = f.steps[i], ph = (st.phase || 'steps') as Phase, idx = f.steps.filter((x, j) => j < i && (x.phase || 'steps') === ph).length; this.open.set([ph + idx]); } }
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
  // ---- pictures of web steps (after each action, and of a failure) ----
  async setPictures(w: { pictures?: string }, v: string, el?: HTMLSelectElement) {
    if (v === 'all' && !(await this.ui.confirm(tr('A Picture After Every Action?'), tr('Every action of this step adds a picture to the run report: reports get bigger and runs use more memory and disk. Pictures of a failure are taken either way.'), tr('Take Them')))) {
      if (el) el.value = w.pictures === 'none' ? 'none' : '';
      return;
    }
    w.pictures = v || undefined; this.touch();
  }
  failPics(out?: string) { return this.picsOf(out).filter(p => p.fail); }
  private picCache = signal<Record<string, string>>({});
  private picAsked = new Set<string>();
  readonly bigPic = signal('');
  private picRoot() { const r = this.last()[this.sel()]; const m = /^(.*)\/logs\//.exec(r?.log ?? ''); return m ? m[1] + '/' : ''; }
  picsOf(out?: string): { path: string; label: string; fail: boolean }[] {
    if (!out) return [];
    const res: { path: string; label: string; fail: boolean }[] = []; let n = 0;
    for (const m of out.matchAll(/^(picture|screenshot): (\S+)/gm)) {
      const rel = m[2].startsWith('web/') ? this.picRoot() + m[2] : m[2];
      res.push({ path: rel, label: m[1] === 'picture' ? tr('after action {0}', ++n) : tr('when it failed'), fail: m[1] === 'screenshot' });
    }
    for (const p of res) if (!this.picAsked.has(p.path)) { this.picAsked.add(p.path); queueMicrotask(() => this.backend.testsImage(p.path).then(u => this.picCache.update(c => ({ ...c, [p.path]: u }))).catch(() => {})); }
    return res;
  }
  picUrl(p: string) { return this.picCache()[p] || ''; }
  showPic(p: string) { this.bigPic.set(this.picUrl(p)); }
  readonly webRecDlg = signal<{ app: string; start: string; name: string; folder: string; proc?: boolean; description?: string; addr?: boolean } | null>(null);
  recFromOk(wr: { app: string; start: string; proc?: boolean; addr?: boolean }) { return wr.addr && !wr.proc ? /^https?:\/\/\S+$/i.test(wr.start.trim()) : !!wr.app; }
  openWebRec() {
    void this.loadWebSt(); void this.testBrowsers.refresh();
    let folder = ''; try { folder = localStorage.getItem('tw.webrec.folder') ?? ''; } catch { /* none */ }
    this.webRecDlg.set({ app: this.webApps()[0] ?? '', start: '', name: '', folder, addr: !this.webApps().length });
  }
  suiteDirs() { const out = new Set<string>(); for (const c of this.tree()?.cases ?? []) { const m = /^cases\/(.+)\/[^/]+$/.exec(c.file); if (m) out.add(m[1]); } return [...out].sort(); }
  async startWebRec() {
    const wr = this.webRecDlg(); if (!wr) return;
    const slug = (x: string) => x.toLowerCase().replace(/[^a-z0-9]+/g, '-').replace(/^-+|-+$/g, '').slice(0, 50) || 'test';
    const dir = wr.folder.trim().replace(/^cases\//, '').split('/').filter(Boolean).map(slug).join('/');
    const file = 'cases/' + (dir ? dir + '/' : '') + slug(wr.name) + '.yaml';
    if (await this.backend.testFile(file).then(() => true).catch(() => false)
      && !(await this.ui.confirm(tr('Replace the Test?'), tr('{0} is already there.', file), tr('Replace'), true))) return;
    try { localStorage.setItem('tw.webrec.folder', wr.folder.trim()); } catch { /* not kept */ }
    const addr = !!wr.addr && !wr.proc;
    const c: TestCase = { name: wr.name.trim(), tags: ['web'], steps: [{ name: wr.name.trim(), web: { login: addr ? undefined : wr.app, url: wr.start.trim() || undefined, actions: [] } }] } as TestCase;
    try {
      const rel = await this.backend.testSave(file, c);
      this.webRecDlg.set(null);
      await this.load(); this.section.set('tests'); await this.select(rel); this.edit();
      const w = this.draft()?.steps?.[0]?.web;
      if (w) await this.webRecord(w);
      if (w && addr) await this.offerWebApp(w);
    } catch (e) { this.ui.toast(errText(e), 'error'); }
  }
  /** After recording from an address: offers to save the address as a web app session and use it in the step. */
  async offerWebApp(w: TestWeb) {
    let u: URL; try { u = new URL(w.url ?? ''); } catch { return; }
    if (!(await this.ui.confirm(tr('Save as a Web App?'), tr('Save {0} as a web app session? Steps then use it by name, and its login can be recorded once and reused.', u.origin), tr('Save Web App')))) return;
    const name = await this.ui.input(tr('Web App Name'), tr('Its name in the session list'), u.hostname);
    if (!name?.trim()) return;
    try {
      const s = await this.backend.newSessionDefaults();
      Object.assign(s, { name: name.trim(), folder: '', protocol: 'Web', hostname: u.hostname, webUrl: u.origin } as Partial<Session>);
      const saved = await this.backend.saveSession(s, '');
      await this.ui.refreshTree();
      w.login = saved.path; w.url = (u.pathname + u.search + u.hash) === '/' ? undefined : u.pathname + u.search + u.hash;
      this.touch();
      this.ui.toast(tr('Saved the web app {0}: record its login in the session editor', saved.path), 'success');
    } catch (e) { this.ui.toast(errText(e), 'error'); }
  }
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
    this.section.set('schedules');
    this.sched.set(s ? structuredClone({ ...s, state: undefined, next: undefined, when: undefined }) as TestSchedule : { id: '', name: '', repeat: 'daily', at: '06:00', day: 1, hours: 4, testbed: this.testbed });
    this.schedWhat = s?.files?.length ? 'keep' : 'all';
  }
  async saveSchedule(s: TestSchedule) {
    const w = this.schedWhat, files = w === 'sel' && this.sel() ? [this.sel()] : w === 'picked' ? this.picked() : w.startsWith('suite:') ? [w.slice(6)] : w === 'keep' ? s.files : undefined;
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
  readonly tbPick = signal(false);
  tbQuery = ''; tbFolder = ''; tbName = ''; tbDevQ = ''; tbAppend = '';
  tbPicked = new Set<string>(); tbHas = new Set<string>();
  readonly tbModels = signal<Record<string, string>>({});
  openTbPick(append = '') {
    this.tbQuery = ''; this.tbFolder = ''; this.tbName = ''; this.tbDevQ = ''; this.tbAppend = append; this.tbPicked = new Set(); this.tbHas = new Set();
    if (append) void this.backend.testbedDevices(append).then(l => { this.tbHas = new Set((l ?? []).map(d => d.session ?? '').filter(Boolean)); }).catch(() => {});
    this.tbPick.set(true);
    if (!this.ui.tree()) void this.backend.getSessionTree().then(t => this.ui.tree.set(t)).catch(() => {});
  }
  folderList() {
    const out: { path: string; name: string; depth: number; n: number }[] = [], q = this.tbQuery.trim().toLowerCase();
    const count = (n: SessionNode): number => (n.children ?? []).reduce((k, c) => k + (c.isFolder ? count(c) : 1), 0);
    const walk = (l: SessionNode[], d: number) => { for (const n of l) if (n.isFolder) { const k = count(n); if (k && (!q || n.path.toLowerCase().includes(q))) out.push({ path: n.path, name: n.name, depth: q ? 0 : d, n: k }); walk(n.children ?? [], d + 1); } };
    walk(this.ui.tree()?.children ?? [], 0);
    return out;
  }
  private folderNode(path: string) {
    const find = (l: SessionNode[]): SessionNode | null => { for (const n of l) { if (n.path === path) return n; const r = find(n.children ?? []); if (r) return r; } return null; };
    return find(this.ui.tree()?.children ?? []);
  }
  pickFolder(path: string) {
    this.tbFolder = path;
    if (!this.tbAppend && !this.tbName) this.tbName = path.split('/').slice(-2).join('-');
    const all = this.devicesOf(path).map(d => d.path);
    void this.backend.testsDeviceModels(all).then(m => this.tbModels.update(x => ({ ...x, ...(m ?? {}) }))).catch(() => {});
  }
  devicesOf(path: string) {
    const out: SessionNode[] = [];
    const all = (n: SessionNode | null) => { for (const c of n?.children ?? []) { if (c.isFolder) all(c); else out.push(c); } };
    all(this.folderNode(path));
    return out.map(c => ({ path: c.path, name: c.name, host: c.hostname ?? '', deviceType: c.deviceType || (c.protocol === 'Web' ? 'web app' : ''), folder: c.path.slice(0, c.path.lastIndexOf('/')) }));
  }
  devGroups() {
    const q = this.tbDevQ.trim().toLowerCase(), m = this.tbModels();
    const by = new Map<string, ReturnType<TestsView['devicesOf']>>();
    for (const d of this.devicesOf(this.tbFolder)) {
      if (q && ![d.name, d.host, d.deviceType, m[d.path] ?? ''].some(x => x.toLowerCase().includes(q))) continue;
      by.set(d.folder, [...(by.get(d.folder) ?? []), d]);
    }
    return [...by.entries()].sort((a, b) => a[0].localeCompare(b[0])).map(([folder, items]) => ({ folder, items: items.sort((a, b) => a.name.localeCompare(b.name, undefined, { numeric: true })) }));
  }
  tbToggle(p: string) { const s = new Set(this.tbPicked); if (s.has(p)) s.delete(p); else s.add(p); this.tbPicked = s; }
  tbSelectAll(on: boolean) { const s = new Set(this.tbPicked); for (const g of this.devGroups()) for (const d of g.items) { if (this.tbHas.has(d.path)) continue; if (on) s.add(d.path); else s.delete(d.path); } this.tbPicked = s; }
  roleName(path: string) { const n = path.split('/').pop()!.toLowerCase().replace(/[^a-z0-9]+/g, '_').replace(/^_+|_+$/g, ''); return !n || /^\d/.test(n) ? 'dev_' + n : n; }
  rolesOf(path: string) {
    const find = (l: SessionNode[]): SessionNode | null => { for (const n of l) { if (n.path === path) return n; const r = find(n.children ?? []); if (r) return r; } return null; };
    const all = (n: SessionNode | null): SessionNode[] => (n?.children ?? []).flatMap(c => c.isFolder ? all(c) : [c]);
    return all(find(this.ui.tree()?.children ?? [])).map(c => c.name.toLowerCase().replace(/[^a-z0-9]+/g, '_').replace(/^_|_$/g, ''));
  }
  fileMenu(ev: MouseEvent, rel: string) {
    ev.preventDefault();
    this.ui.showMenu(ev, [
      { label: 'Open', action: () => void this.openFile(rel) },
      { label: 'Open in VS Code', action: () => void this.backend.testsOpenInEditor(rel) },
      { separator: true },
      { label: rel.startsWith('suites/') ? 'Delete Plan' : 'Delete Testbed', icon: 'trash', action: () => void this.removeFile(rel) },
    ]);
  }
  async removeFile(rel: string) {
    if (!(await this.ui.confirm(tr('Delete {0}?', this.base(rel)), tr('It is moved to .removed/ in the tests folder (it can be brought back from there).'), tr('Delete'), true))) return;
    try { await this.backend.testRemove(rel); if (this.fileSel() === rel) this.fileSel.set(''); this.ui.toast(tr('Deleted {0}', this.base(rel)), 'success'); await this.load(); }
    catch (e) { this.ui.toast(errText(e), 'error'); }
  }
  async createTestbed() {
    if (!this.tbPicked.size) return;
    const picked = [...this.tbPicked], append = this.tbAppend;
    if (!append && !this.tbName.trim()) return;
    this.tbPick.set(false);
    try {
      if (append) { await this.backend.testbedChange(append, picked, []); this.ui.toast(tr('Added {0} devices', picked.length), 'success'); await this.openFile(append); return; }
      const rel = await this.backend.testsNewTestbed(this.tbName.trim(), this.tbFolder, picked);
      this.ui.toast(tr('Wrote {0}', rel), 'success'); await this.load(); this.section.set('testbeds'); void this.openFile(rel);
    } catch (e) { this.ui.toast(errText(e), 'error'); }
  }
  expMode(x: { lang: string; standalone: boolean }, m: 'code' | 'runner' | 'termwright' | 'playwright') {
    x.standalone = m !== 'termwright';
    if (m === 'runner' || m === 'playwright') x.lang = m;
    else if (x.lang === 'runner' || x.lang === 'playwright' || (m === 'code' && x.lang === 'tcl')) x.lang = 'python';
  }
  readonly exp = signal<{ what: string; lang: string; os: string; testbed: string; name: string; standalone: boolean; failShots?: boolean; stepShots?: boolean } | null>(null);
  openExport() {
    const plat = navigator.platform.toLowerCase(), os = plat.includes('win') ? 'windows' : plat.includes('mac') ? 'mac' : 'linux';
    const what = this.picked().length ? 'picked' : this.sel() ? 'sel' : 'all';
    this.exp.set({ what, lang: 'python', standalone: true, failShots: true, stepShots: false, os, testbed: this.testbed || '', name: (this.tree()?.root ?? 'termwright-suite').split(/[\\/]/).pop() || 'termwright-suite' });
  }
  async setStepShots(x: { stepShots?: boolean }, on: boolean, el?: HTMLInputElement) {
    if (on && !(await this.ui.confirm(tr('A Screenshot After Every Step?'), tr('Every step adds a full-page screenshot to the Playwright report: reports get bigger and runs use more memory and disk. Screenshots of a failure are taken either way.'), tr('Take Them')))) {
      if (el) el.checked = false;
      x.stepShots = false; return;
    }
    x.stepShots = on;
  }
  async doExport() {
    const x = this.exp(); if (!x) return;
    const files = x.what === 'picked' ? this.picked() : x.what === 'sel' ? [this.sel()] : x.what === 'all' ? [] : [x.what];
    try { const p = await this.backend.testsExport({ files, lang: x.lang, os: x.os, testbed: x.testbed, name: x.name, standalone: x.standalone || x.lang === 'runner', noFailShots: x.failShots === false, stepShots: !!x.stepShots }); if (p) { this.exp.set(null); this.ui.toast(tr('Exported {0}', p), 'success'); } }
    catch (e) { this.ui.toast(errText(e), 'error'); }
  }
  async newTestbed() {
    const folder = await this.ui.input(tr('New Testbed'), tr('Session folder whose sessions become roles (e.g. Lab)'), '');
    if (!folder) return;
    const name = await this.ui.input(tr('New Testbed'), tr('Testbed name'), folder.split('/').pop() ?? '');
    if (!name) return;
    try { const rel = await this.backend.testsNewTestbed(name, folder); this.ui.toast(tr('Wrote {0}', rel), 'success'); await this.load(); void this.backend.testsOpen(rel); }
    catch (e) { this.ui.toast(errText(e), 'error'); }
  }
  /** Right-click on selected text in a step's output: add it as a check of that step and save the test. */
  outMenu(ev: MouseEvent, row: { i: number; phase: string }) {
    const sel = (window.getSelection()?.toString() ?? '').replace(/\s*\r?\n\s*/g, ' ').trim();
    if (!sel) return;
    const out = this.res(row)?.output ?? '';
    const field = fieldPattern(out, sel);
    const short = sel.length > 30 ? sel.slice(0, 30) + '…' : sel;
    const add = (e: TestExpect) => void this.addRunCheck(row, e);
    this.ui.showMenu(ev, [
      { label: tr('Expect this: contains "{0}"', short), action: () => add({ contains: sel }) },
      ...(field ? [{ label: tr('Expect this: field = "{0}"', short), action: () => add({ field, equals: sel }) }] : []),
      { label: 'Expect this: matches a pattern', action: () => add({ matches: escapeRe(sel).replace(/\d+/g, '\\d+') }) },
      { label: 'Expect this: does not contain', action: () => add({ notContains: sel }) },
      { separator: true },
      { label: 'Copy', action: () => void navigator.clipboard?.writeText(sel).catch(() => {}) },
    ]);
  }
  private async addRunCheck(row: { i: number; phase: string }, e: TestExpect) {
    const t = this.test(); if (!t) return;
    const c = structuredClone(t);
    const list = row.phase === 'setup' ? c.setup : row.phase === 'cleanup' ? c.cleanup : c.steps;
    const st = list?.[row.i]; if (!st) return;
    (st.expect ??= []).push(e);
    try { await this.backend.testSave(this.sel(), c); this.ui.toast(tr('Check added to step {0}', row.i + 1), 'success'); await this.load(); }
    catch (err) { this.ui.toast(errText(err), 'error'); }
  }
  keepOcf() { try { localStorage.setItem('tw.tests.ocf', this.onCleanupFail); localStorage.setItem('tw.tests.cleanup', this.cleanupMode); } catch { /* not kept */ } }
  pick(f: string) { this.picked.update(l => l.includes(f) ? l.filter(x => x !== f) : [...l, f]); }
  filesOf(g: { cases: TestCaseInfo[] }) { return g.cases.map(c => c.file); }
  go(k: Section) {
    this.section.set(k);
    if (k === 'runs') void this.loadApprovals();
    if (k === 'procs') { void this.loadProcs(); this.proc.set(null); }
    if (k === 'runs' && !this.runRep()) { const r = this.report() ?? null; if (r) { this.runRep.set(r); this.runSel.set(r.dir); } else { const d = this.tree()?.reports?.[0]?.dir; if (d) void this.openRunReport(d); } }
  }
  /** The tests shown: search and tag filter. */
  readonly filtered = computed(() => {
    const q = this.q().trim().toLowerCase(), t = this.tag();
    return (this.tree()?.cases ?? []).filter(c => (!q || (c.name + ' ' + c.file + ' ' + (c.tags ?? []).join(' ')).toLowerCase().includes(q))
      && (t === 'all' || (t === '#failed' ? this.last()[c.file]?.passed === false : (c.tags ?? []).includes(t))));
  });
  readonly tagList = computed(() => {
    const cs = this.tree()?.cases ?? [], m = new Map<string, number>();
    for (const c of cs) for (const g of c.tags ?? []) m.set(g, (m.get(g) ?? 0) + 1);
    const failed = cs.filter(c => this.last()[c.file]?.passed === false).length;
    return [{ k: 'all', l: 'all', n: cs.length }, ...[...m.entries()].sort().map(([k, n]) => ({ k, l: k, n })), ...(failed ? [{ k: '#failed', l: 'failed', n: failed }] : [])];
  });
  toggleFold(d: string) { this.folded.update(l => l.includes(d) ? l.filter(x => x !== d) : [...l, d]); }
  readonly runList = computed(() => {
    let prev = '';
    return (this.tree()?.reports ?? []).map(r => {
      const m = r.dir.match(/(\d{4}-\d\d-\d\d)_(\d\d)(\d\d)/), day = m ? this.dayName(m[1]) : '', out = { ...r, day: day !== prev ? day : '', time: m ? m[2] + ':' + m[3] : r.dir };
      prev = day;
      return out;
    });
  });
  private dayName(d: string) {
    const t = new Date(), y = new Date(Date.now() - 864e5), f = (x: Date) => x.toISOString().slice(0, 10);
    return d === f(t) ? 'Today' : d === f(y) ? 'Yesterday' : d;
  }
  // ---- testbed calendar ----
  readonly calOn = signal(false);
  readonly bookings = signal<TestBooking[]>([]);
  readonly calStart = signal(this.monday(new Date()));
  readonly bk = signal<TestBooking | null>(null);
  bkFrom = ''; bkTo = '';
  private monday(d: Date) { const x = new Date(d); x.setHours(0, 0, 0, 0); x.setDate(x.getDate() - ((x.getDay() + 6) % 7)); return x; }
  calDays() { const s = this.calStart(); return Array.from({ length: 7 }, (_, i) => new Date(s.getFullYear(), s.getMonth(), s.getDate() + i)); }
  calMove(n: number) { const s = this.calStart(); this.calStart.set(new Date(s.getFullYear(), s.getMonth(), s.getDate() + n)); }
  calToday() { this.calStart.set(this.monday(new Date())); }
  isToday(d: Date) { return d.toDateString() === new Date().toDateString(); }
  calDay(d: Date) { return d.toLocaleDateString(undefined, { weekday: 'short', day: 'numeric' }); }
  hm(t: string) { return new Date(t).toLocaleTimeString(undefined, { hour: '2-digit', minute: '2-digit' }); }
  fmtWhen(t: string) { return new Date(t).toLocaleString(undefined, { weekday: 'short', day: 'numeric', month: 'short', hour: '2-digit', minute: '2-digit' }); }
  private weekMs = 7 * 864e5;
  nowPct() { const p = (Date.now() - this.calStart().getTime()) / this.weekMs; return p >= 0 && p < 1 ? p * 100 : -1; }
  weekBookings(tb: string) {
    const s = this.calStart().getTime(), e = s + this.weekMs;
    return this.bookings().filter(b => b.testbed === tb).map(b => ({ b, f: Math.max(s, new Date(b.from).getTime()), t: Math.min(e, new Date(b.to).getTime()) }))
      .filter(x => x.t > x.f).map(x => ({ id: x.b.id, b: x.b, l: 100 * (x.f - s) / this.weekMs, w: Math.max(10, 100 * (x.t - x.f) / this.weekMs) })).map(x => ({ ...x, l: Math.min(x.l, 100 - x.w) }));
  }
  bookedNow(tb: string) { const n = Date.now(); return this.bookings().find(b => b.testbed === tb && new Date(b.from).getTime() <= n && n < new Date(b.to).getTime()) ?? null; }
  async loadBookings() { try { this.bookings.set(await this.backend.testBookings() ?? []); } catch { /* no tests folder */ } }
  openCal() { this.calOn.set(true); this.section.set('testbeds'); void this.loadBookings(); }
  private localIso(d: Date) { const p = (n: number) => String(n).padStart(2, '0'); return d.getFullYear() + '-' + p(d.getMonth() + 1) + '-' + p(d.getDate()) + 'T' + p(d.getHours()) + ':' + p(d.getMinutes()); }
  newBooking(tb?: string, ev?: MouseEvent) {
    let from = new Date(); from.setMinutes(from.getMinutes() < 30 ? 30 : 60, 0, 0);
    if (ev) { const r = (ev.currentTarget as HTMLElement).getBoundingClientRect(); const t = this.calStart().getTime() + this.weekMs * (ev.clientX - r.left) / r.width; from = new Date(t); from.setMinutes(0, 0, 0); }
    this.bkFrom = this.localIso(from); this.bkTo = this.localIso(new Date(from.getTime() + 2 * 36e5));
    this.bk.set({ id: '', testbed: tb ?? this.testbed ?? this.tree()?.testbeds?.[0] ?? '', from: '', to: '', who: '', why: '' });
  }
  editBooking(b: TestBooking) { this.bkFrom = this.localIso(new Date(b.from)); this.bkTo = this.localIso(new Date(b.to)); this.bk.set({ ...b }); }
  async saveBooking(k: TestBooking) {
    try {
      await this.backend.testSaveBooking({ ...k, from: new Date(this.bkFrom).toISOString(), to: new Date(this.bkTo).toISOString() });
      this.bk.set(null); this.ui.toast(tr('Booked'), 'success'); await this.loadBookings();
    } catch (e) { this.ui.toast(errText(e), 'error'); }
  }
  async cancelBooking(k: TestBooking) {
    if (!k.mine && !(await this.ui.confirm(tr('Cancel Their Booking?'), tr('{0} booked this testbed. Cancel the booking anyway?', k.who), tr('Cancel Booking'), true))) return;
    try { await this.backend.testRemoveBooking(k.id); this.bk.set(null); await this.loadBookings(); } catch (e) { this.ui.toast(errText(e), 'error'); }
  }
  readonly trendsOn = signal(false);
  readonly trends = signal<TestTrends | null>(null);
  trWhat = ''; trTestbed = '';
  openTrends() { this.trendsOn.set(true); void this.loadTrends(); }
  async loadTrends() { try { this.trends.set(await this.backend.testTrends(this.trWhat ? [this.trWhat] : [], this.trTestbed, 40)); } catch (e) { this.ui.toast(errText(e), 'error'); } }
  trSum(t: TestTrends, k: 'failed' | 'flaky' | 'passed') { return t.runs.reduce((n, r) => n + r[k], 0); }
  trRate(t: TestTrends) { const p = this.trSum(t, 'passed'), all = p + this.trSum(t, 'failed'); return all ? Math.round(100 * p / all) : 0; }
  trAvgMin(t: TestTrends) { const ms = t.runs.reduce((n, r) => n + r.ms, 0) / Math.max(1, t.runs.length); return ms > 60000 ? (ms / 60000).toFixed(1) + ' min' : (ms / 1000).toFixed(0) + ' s'; }
  async openRunReport(dir: string) {
    this.trendsOn.set(false);
    this.section.set('runs'); this.runSel.set(dir);
    try { this.runRep.set(JSON.parse(await this.backend.testFile(dir + '/result.json')) as TestRunReport); }
    catch (e) { this.runRep.set(null); this.ui.toast(errText(e), 'error'); }
  }
  openTest(f: string) { this.section.set('tests'); void this.select(f); }
  base(p: string) { return (p.split('/').pop() ?? p).replace(/\.ya?ml$/, ''); }
  dur(ms: number) { return ms < 1000 ? ms + ' ms' : ms < 60000 ? (ms / 1000).toFixed(1) + ' s' : Math.floor(ms / 60000) + ' m ' + Math.round(ms % 60000 / 1000) + ' s'; }
  ago(iso: string) { const s = (Date.now() - new Date(iso).getTime()) / 1000; return s < 90 ? 'just now' : s < 3600 ? Math.round(s / 60) + ' min ago' : s < 86400 ? Math.round(s / 3600) + ' h ago' : iso.slice(0, 10); }
  failedFiles(r: TestRunReport) { return [...new Set(r.results.filter(x => !x.passed && x.file).map(x => x.file!))]; }
  unchecked(r: TestRunReport) { return r.results.reduce((n, x) => n + x.steps.filter(s => s.unchecked && !s.skipped).length, 0); }
  notChecked(r: TestResult) { const n = r.steps.filter(s => s.unchecked && !s.skipped).length; return n ? n + (n === 1 ? ' step' : ' steps') + ' not checked' : ''; }
  readonly approvals = signal<TestApproval[]>([]);
  async loadApprovals() { this.approvals.set((await this.backend.testApprovals().catch(() => [])) ?? []); }
  async decide(id: string, ok: boolean) {
    const note = ok ? '' : await this.ui.input(tr('Reject the Run'), tr('Why (optional)'), '');
    if (note === null) return;
    try { await this.backend.testDecide(id, ok, note ?? ''); this.ui.toast(ok ? tr('Approved - the run may start within 24 hours') : tr('Rejected'), 'success'); } catch (e) { this.ui.toast(errText(e), 'error'); }
    await this.loadApprovals();
  }
  private async askApproval(files: string[], testbed: string, why: string) {
    const reason = await this.ui.input(tr('Ask for an Approval'), why + '. ' + tr('Why should it run? An approver sees the request in Runs, approves it, and then you run it again.'), '');
    if (reason === null || reason === undefined) return;
    try { await this.backend.testRequestApproval(files, testbed, reason); this.ui.toast(tr('Approval requested'), 'success'); await this.loadApprovals(); } catch (e) { this.ui.toast(errText(e), 'error'); }
  }
  async openRoles() {
    await this.openFile('roles.yaml');
    if (this.fileText.startsWith('#') && !this.fileText.includes('approval')) this.fileText = '# Who may do what in this tests folder (user names as in "Run by"; an empty list lets everyone).\n# Shared through Git: it keeps an honest team in step, it is not a security boundary.\neditors: []\nrunners: []\napprovers: []\n# approval: production   # runs on production sessions need an approved request (or: always)\n';
  }
  async openFile(f: string) {
    this.fileSel.set(f); this.fileText = await this.backend.testFile(f).catch(e => '# ' + errText(e));
    if (f.startsWith('testbeds/')) { this.tbYaml.set(false); this.tbDevs.set(await this.backend.testbedDevices(f).catch(() => []) ?? []); await this.loadTbMeta(f); }
  }
  readonly tbYaml = signal(false);
  readonly tbDevs = signal<TestbedDevice[]>([]);
  tbDevGroups() {
    const by = new Map<string, TestbedDevice[]>();
    for (const d of this.tbDevs()) by.set(d.session ? d.folder ?? '' : '', [...(by.get(d.session ? d.folder ?? '' : '') ?? []), d]);
    return [...by.entries()].sort((a, b) => (a[0] === '' ? 1 : b[0] === '' ? -1 : a[0].localeCompare(b[0]))).map(([folder, items]) => ({ folder, items }));
  }
  phDesc(p: Phase) { return p === 'setup' ? 'before the steps: a known state' : p === 'steps' ? 'in order; a failed step stops the test' : 'after the steps, also when one failed - only what the test configured is undone'; }
  oneDevice(d: TestCase, s: TestStep) { const r = this.testRoles(d); return r.length === 1 && (s.run?.session ?? '').replace(/\s/g, '') === '{{' + r[0] + '}}'; }
  portsHint(r: string) { const p = this.rolePorts()[r] ?? []; return p.length ? 'ports ' + p[0] + ' … ' + p[p.length - 1] : ''; }
  async renameTestRole(d: TestCase, from: string) {
    const to = (await this.ui.input(tr('Rename the Device'), tr('Steps use {{name}} for it - e.g. dut or core. A testbed maps the same name to its own device.'), from))?.trim();
    if (!to || to === from || !/^[A-Za-z_][\w-]*$/.test(to)) return;
    const re = new RegExp('\\{\\{\\s*' + from.replace(/[-]/g, '\\-') + '(\\.[\\w-]+)?\\s*\\}\\}', 'g');
    const sub = (x?: string) => x?.replace(re, (_m, p) => '{{' + to + (p ?? '') + '}}');
    const walk = (o: unknown): unknown => typeof o === 'string' ? sub(o) : Array.isArray(o) ? o.map(walk) : o && typeof o === 'object' ? Object.fromEntries(Object.entries(o).map(([k, v]) => [k, walk(v)])) : o;
    for (const ph of ['setup', 'steps', 'cleanup'] as const) (d as unknown as Record<string, unknown>)[ph] = walk(this.listOf(d, ph));
    const vars: Record<string, string> = {};
    for (const [k, v] of Object.entries(d.vars ?? {})) vars[k === from ? to : k.startsWith(from + '.') ? to + k.slice(from.length) : k] = v;
    d.vars = vars; this.draft.set({ ...d }); this.touch(); void this.loadRolePorts(d);
  }
  optMode(s: TestStep): 'default' | 'custom' | 'none' {
    const o = (s as { options?: string }).options;
    if (o === 'none' || o === 'custom') return o;
    return s.timeout || s.wait || s.retry || s.continueOnFail || (s.run as { ignoreErrors?: boolean } | undefined)?.ignoreErrors ? 'custom' : 'default';
  }
  setOptMode(s: TestStep, m: 'default' | 'custom' | 'none') {
    const x = s as TestStep & { options?: string };
    if (m !== 'custom') { delete x.timeout; delete x.wait; delete x.retry; delete x.continueOnFail; if (x.run) delete (x.run as { ignoreErrors?: boolean }).ignoreErrors; }
    if (m === 'default') delete x.options; else x.options = m;
    this.touch();
  }
  defaultsText(d: TestCase) {
    const o = (d as { defaults?: { timeout?: number; wait?: number; retry?: { timeout?: number }; continueOnFail?: boolean; ignoreErrors?: boolean } }).defaults ?? {};
    const p = [o.timeout ? 'timeout ' + o.timeout + ' s' : '', o.wait ? 'wait ' + o.wait + ' s' : '', o.retry ? 'retry ' + (o.retry.timeout ?? 60) + ' s' : '', o.continueOnFail ? 'go on when it fails' : '', o.ignoreErrors ? 'ignore device errors' : ''].filter(Boolean);
    return p.length ? p.join(' · ') : 'the test\'s step options (none set)';
  }
  stepOptText(s: TestStep) {
    const p = [s.timeout ? 'timeout ' + s.timeout + ' s' : '', s.wait ? 'wait ' + s.wait + ' s' : '', s.retry ? 'retry ' + (s.retry.timeout ?? 60) + ' s' : '', s.continueOnFail ? 'go on when it fails' : '', (s.run as { ignoreErrors?: boolean } | undefined)?.ignoreErrors ? 'ignore device errors' : ''].filter(Boolean);
    return p.join(' · ');
  }
  setDef(d: TestCase, k: string, v: unknown) {
    const x = d as TestCase & { defaults?: Record<string, unknown> };
    const o = { ...(x.defaults ?? {}) };
    if (v === undefined || v === null || v === '' || v === 0) delete o[k]; else o[k] = v;
    x.defaults = Object.keys(o).length ? o : undefined; this.touch();
  }
  readonly ckOpen = signal(new Set<string>());
  openCk(p: Phase, i: number) { const x = new Set(this.ckOpen()); x.add(p + i); this.ckOpen.set(x); }
  private readonly moreOpen = signal(new Set<string>());
  isMore(p: Phase, i: number) { return this.moreOpen().has(p + i); }
  toggleMore(p: Phase, i: number) { const x = new Set(this.moreOpen()); if (x.has(p + i)) x.delete(p + i); else x.add(p + i); this.moreOpen.set(x); }
  moreSet(s: TestStep) { return !!(s.if?.trim() || s.sample || s.run?.parse || s.expected); }
  moreText(s: TestStep) { return [s.if?.trim() ? 'if ' + s.if : '', s.sample ? 'sampled' : '', s.run?.parse ? 'parsed' : '', s.expected ? 'expected' : ''].filter(Boolean).join(' · '); }
  /** The test's port numbers (1:2, 2, 1/1…) become its devices' port variables {{role.port1}}…. */
  async usePortVars() {
    const d = this.draft(), t = d ?? this.test(); if (!t) return;
    try {
      const r = await this.backend.testsUsePortVars(structuredClone(t));
      if (!r.changed) { this.ui.toast(tr('No port numbers of the devices\' port1…port8 found in the test'), 'info'); return; }
      if (d) { this.draft.set({ ...r.case, setup: r.case.setup ?? [], steps: r.case.steps ?? [], cleanup: r.case.cleanup ?? [] }); this.touch(); }
      else await this.saveCase(c => { c.setup = r.case.setup; c.steps = r.case.steps; c.cleanup = r.case.cleanup; });
      this.ui.toast(tr('{0} port numbers are now port variables ({{role.port1}}…) - each testbed sets its own', r.changed), 'success');
    } catch (e) { this.ui.toast(errText(e), 'error'); }
  }
  // ---- Make a Variable: a selected part of a command becomes {{name}} everywhere in the test ----
  varMenu(ev: MouseEvent, s: TestStep) {
    const el = ev.target as HTMLInputElement;
    const text = (el.value ?? '').slice(el.selectionStart ?? 0, el.selectionEnd ?? 0).trim();
    ev.preventDefault();
    const put = (name: string) => {
      let a = el.selectionStart ?? el.value.length, b = el.selectionEnd ?? a;
      const open = el.value.lastIndexOf('{{', a - 1), close = el.value.lastIndexOf('}}', a - 1);
      if (a === b && open > close) { const end = el.value.indexOf('}}', a); if (end >= 0) a = b = end + 2; } // not inside another {{…}}
      el.focus(); el.setRangeText('{{' + name + '}}', a, b, 'end');
      el.dispatchEvent(new Event('input', { bubbles: true })); this.touch();
    };
    const items: { label?: string; action?: () => void; separator?: boolean; disabled?: boolean; hint?: string; icon?: string }[] = [];
    if (text && !text.includes('{{')) items.push({ label: tr('Make a Variable from "{0}"…', text.length > 30 ? text.slice(0, 30) + '…' : text), icon: 'add', action: () => void this.makeVar(s, text) }, { separator: true });
    const vs = this.varChoices();
    if (vs.length) items.push({ label: tr('Insert a variable:'), disabled: true, icon: '' });
    for (const v of vs) items.push({ label: '{{' + v.name + '}}', hint: v.value, icon: '', action: () => put(v.name) });
    items.push({ separator: true }, { label: tr('New Variable…'), icon: 'add', action: () => void this.newVar(put) });
    this.ui.showMenu(ev, items);
  }
  /** The variables a step can use: the test's own, and each device's ports and facts. */
  varChoices(): { name: string; value: string }[] {
    const d = this.draft(); if (!d) return [];
    const roles = this.testRoles(d), out: { name: string; value: string }[] = [];
    for (const [k, v] of Object.entries(d.vars ?? {})) if (!roles.includes(k)) out.push({ name: k, value: v });
    for (const r of roles) {
      const pre = roles.length === 1 ? '' : r + '.', ps = this.rolePorts()[r] ?? [];
      ps.forEach((p, i) => out.push({ name: pre + 'port' + (i + 1), value: p }));
      if (ps.length) out.push({ name: pre + 'ports', value: tr('all ports') });
      out.push({ name: r + '.device_type', value: '' }, { name: r + '.host', value: '' });
    }
    return out;
  }
  /** The test's device roles: variables used as a step's session ({{role}}) that are saved sessions. */
  testRoles(d: TestCase) {
    const out: string[] = [];
    for (const st of [...(d.setup ?? []), ...(d.steps ?? []), ...(d.cleanup ?? [])]) {
      const m = /^\{\{\s*([\w-]+)\s*\}\}$/.exec(st.run?.session ?? st.snmp?.session ?? st.web?.login ?? st.http?.session ?? '');
      if (m && d.vars?.[m[1]] && !out.includes(m[1])) out.push(m[1]);
    }
    return out;
  }
  /** What a role is: a web app, an API, or the device's type. */
  roleKind(d: TestCase, r: string) {
    const all = [...(d.setup ?? []), ...(d.steps ?? []), ...(d.cleanup ?? [])], v = '{{' + r + '}}';
    if (all.some(s => s.web?.login === v)) return 'web app';
    if (all.some(s => s.http?.session === v)) return 'API';
    return this.sessionNode(d.vars?.[r] ?? '')?.deviceType || 'device';
  }
  /** The test's values: variables that are not devices. */
  testValues(d: TestCase) { const roles = this.testRoles(d); return Object.keys(d.vars ?? {}).filter(k => !roles.includes(k) && !k.includes('.')).sort(); }
  async addValue(d: TestCase) {
    const k = (await this.ui.input(tr('New Value'), tr('Its name, used as {{name}} in steps'), ''))?.trim().replace(/[^\w-]/g, '_');
    if (!k) return; (d.vars ??= {})[k] ??= ''; this.touch();
  }
  rmValue(d: TestCase, k: string) { delete d.vars![k]; this.touch(); }
  readonly setsOpen = signal(false);
  readonly needsOpen = signal(new Set<string>());
  toggleNeeds(r: string) { const x = new Set(this.needsOpen()); if (x.has(r)) x.delete(r); else x.add(r); this.needsOpen.set(x); }
  settingsSummary(d: TestCase): [string, string][] {
    const o = d.defaults ?? {};
    return [[tr('Timeout'), o.timeout ? o.timeout + ' s' : '60 s'], [tr('A step fails'), o.continueOnFail ? tr('go on') : tr('the test stops')],
      [tr('Retry'), o.retry ? tr('for {0} s', String(o.retry.timeout ?? 60)) : tr('no')], [tr('Test data'), d.data ? d.data.split('/').pop()! : tr('none')],
      [tr('Fails on'), this.watchText(d) || '-'], [tr('Configuration'), d.configDiff ? tr('compared') : '-']];
  }
  readonly rolePorts = signal<Record<string, string[]>>({});
  private async loadRolePorts(d: TestCase) {
    const m: Record<string, string[]> = {};
    for (const r of this.testRoles(d)) m[r] = await this.backend.testsPorts(d.vars![r]).catch(() => []) ?? [];
    this.rolePorts.set(m);
  }
  async newVar(put: (name: string) => void) {
    const d = this.draft(); if (!d) return;
    const kv = (await this.ui.input(tr('New Variable'), tr('name = value, e.g. vlan = 1000. The value is the default; a testbed or the Run panel can change it.'), ''))?.trim();
    if (!kv) return;
    const i = kv.indexOf('='), name = (i > 0 ? kv.slice(0, i) : kv).trim(), value = i > 0 ? kv.slice(i + 1).trim() : '';
    if (!/^[A-Za-z_][\w.-]*$/.test(name)) { this.ui.toast(tr('A name is letters, digits, _ and .'), 'error'); return; }
    d.vars = { ...(d.vars ?? {}), [name]: value };
    put(name);
  }
  /** Moves a check into the box where checks are added, to change it. */
  editCheck(s: TestStep, j: number, c: { k: string; v: string }) {
    const e = s.expect?.[j]; if (!e) return;
    const simple: [keyof TestExpect, string][] = [['contains', 'contains'], ['notContains', 'notContains'], ['matches', 'matches']];
    for (const [k, kind] of simple) {
      if (typeof e[k] === 'string' && Object.keys(e).length === 1) { c.k = kind; c.v = e[k] as string; s.expect!.splice(j, 1); this.touch(); return; }
    }
    if (e.status !== undefined && Object.keys(e).length === 1) { c.k = 'status'; c.v = String(e.status); s.expect!.splice(j, 1); this.touch(); return; }
    this.ui.toast(tr('This check is changed in the YAML (Code tab) - or remove it and add it again'), 'info');
  }
  async makeVar(s: TestStep, text: string) {
    const d = this.draft(); if (!d) return;
    const role = /^\{\{\s*([\w-]+)\s*\}\}$/.exec(s.run?.session ?? '')?.[1];
    const kind = /^\d+(:\d+)?([,-]\d+(:\d+)?)*$/.test(text) ? (text.includes(',') || text.includes('-') ? 'ports' : 'port') : /^\d{1,4}$/.test(text) ? 'vlan' : 'value';
    const sugg = role ? role + '.' + kind : kind;
    const name = (await this.ui.input(tr('Make a Variable'), tr('"{0}" becomes {{name}} in every step of the test, with "{0}" as its default. A name like {1} is a value of that device: each testbed can set its own (e.g. 1:2 on a stack, 2 on a standalone switch).', text, sugg), sugg))?.trim();
    if (!name || !/^[A-Za-z_][\w.-]*$/.test(name)) return;
    const esc = text.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
    const re = new RegExp('(?<![\\w:/.-])' + esc + '(?![\\w:/-])', 'g');
    let n = 0;
    const sub = (v?: string) => { if (!v) return v; const out = v.replace(re, () => { n++; return '{{' + name + '}}'; }); return out; };
    for (const st of [...(d.setup ?? []), ...(d.steps ?? []), ...(d.cleanup ?? [])]) {
      if (st.run) st.run.command = sub(st.run.command) ?? '';
      for (const e of st.expect ?? []) { e.contains = sub(e.contains); e.notContains = sub(e.notContains); e.equals = sub(e.equals); e.notEquals = sub(e.notEquals); }
    }
    d.vars = { ...(d.vars ?? {}), [name]: text };
    this.touch();
    this.ui.toast(tr('{{{0}}} in {1} places - default "{2}". Set it per device in the testbed.', name, n, text), 'success');
  }
  kindOf(d: TestbedDevice) { const p = d.ports?.[0]?.value ?? ''; return /^\d+:\d+$/.test(p) ? 'stack' : /^\d+$/.test(p) && /exos|switch engine/i.test(d.deviceType ?? '') ? 'standalone' : ''; }
  portRule(d: TestbedDevice) {
    const dt = (d.deviceType ?? '').toLowerCase(), k = this.kindOf(d);
    return /voss|fabric/.test(dt) ? 'VOSS: slot/1 … by default' : k === 'stack' ? 'EXOS stack: slot:2 … by default (port 1 left for the uplink)' : k === 'standalone' ? 'EXOS standalone: 2 … by default' : 'no defaults for this device type - type them';
  }
  anyPortSet(d: TestbedDevice) { return (d.ports ?? []).some(p => p.set) || !!d.values?.['port_count']; }
  async tbSet(fp: string, role: string, key: string, value: string) {
    try { await this.backend.testbedSetValue(fp, role, key, value); } catch (e) { this.ui.toast(errText(e), 'error'); }
    this.tbDevs.set(await this.backend.testbedDevices(fp).catch(() => []) ?? []);
  }
  async setPort(fp: string, d: TestbedDevice, name: string, value: string) {
    value = value.trim();
    const def = (await this.backend.testsPorts(d.session!).catch(() => []))[+name.slice(4) - 1] ?? '';
    await this.tbSet(fp, d.role, name, value === def && +name.slice(4) <= 8 ? '' : value);
  }
  async resetPorts(fp: string, d: TestbedDevice) { for (const p of d.ports ?? []) if (p.set) await this.backend.testbedSetValue(fp, d.role, p.name, '').catch(() => {}); await this.tbSet(fp, d.role, 'port_count', ''); }
  async portCount(fp: string, d: TestbedDevice, by: number) {
    const n = Math.max(1, (d.ports?.length ?? 8) + by);
    if (by < 0) await this.backend.testbedSetValue(fp, d.role, 'port' + (n + 1), '').catch(() => {});
    await this.tbSet(fp, d.role, 'port_count', n === 8 ? '' : String(n));
  }
  setVal(fp: string, role: string, k: string, v: string) { void this.tbSet(fp, role, k, v.trim()); }
  addVal(fp: string, role: string, nk: HTMLInputElement, nv: HTMLInputElement) {
    const k = nk.value.trim(), v = nv.value.trim();
    if (!k || !v) { this.ui.toast(tr('Type a name and a value'), 'error'); return; }
    nk.value = ''; nv.value = ''; void this.tbSet(fp, role, k, v);
  }
  // ---- testbed description, tags, links, port properties ----
  readonly tbMeta = signal<TestbedMeta | null>(null);
  readonly tbLinks = signal<{ a: string; ap: string; b: string; bp: string }[]>([]);
  readonly propsOpen = signal<{ role: string; port: string } | null>(null);
  async loadTbMeta(fp: string) {
    const m = await this.backend.testbedInfo(fp).catch(() => null);
    this.tbMeta.set(m);
    this.tbLinks.set((m?.links ?? []).map(l => { const p = l.replace(/<->|--/g, ' ').replace(/\s-\s/g, ' ').trim().split(/\s+/); const [a, ap] = (p[0] ?? '').split('.'); const [b, bp] = (p[1] ?? '').split('.'); return { a, ap, b, bp }; }));
  }
  async saveTbMeta(fp: string) {
    const m = this.tbMeta(); if (!m) return;
    m.links = this.tbLinks().filter(l => l.a && l.ap && l.b && l.bp).map(l => `${l.a}.${l.ap} - ${l.b}.${l.bp}`);
    try { await this.backend.testbedSetInfo(fp, m); } catch (e) { this.ui.toast(errText(e), 'error'); }
    this.tbMeta.set({ ...m });
  }
  saveLinks(fp: string) { this.tbLinks.set([...this.tbLinks()]); void this.saveTbMeta(fp); }
  addTbTag(fp: string, el: HTMLInputElement) {
    const t = el.value.trim().toLowerCase().replace(/[^\w-]+/g, '-'); el.value = '';
    const m = this.tbMeta(); if (!t || !m || m.tags.includes(t)) return;
    m.tags.push(t); void this.saveTbMeta(fp);
  }
  tbRoles() { return this.tbDevs().filter(d => d.session); }
  portsOfRole(role: string) { return this.tbDevs().find(d => d.role === role)?.ports ?? []; }
  addLink(fp: string) {
    const r = this.tbRoles(); if (r.length < 2) return;
    this.tbLinks.set([...this.tbLinks(), { a: r[0].role, ap: 'port1', b: r[1].role, bp: 'port1' }]); void this.saveTbMeta(fp);
  }
  togglePortProps(role: string, port: string) { const o = this.propsOpen(); this.propsOpen.set(o?.role === role && o.port === port ? null : { role, port }); }
  propsOf(p: { props?: Record<string, string> }) { return Object.entries(p.props ?? {}).sort().map(([k, v]) => ({ k, v })); }
  async setPortProp(fp: string, role: string, port: string, k: string, v: string) {
    try { await this.backend.testbedSetPortProp(fp, role, port, k, v.trim()); } catch (e) { this.ui.toast(errText(e), 'error'); }
    this.tbDevs.set(await this.backend.testbedDevices(fp).catch(() => []) ?? []);
  }
  addPortProp(fp: string, role: string, port: string, kEl: HTMLInputElement, vEl: HTMLInputElement) {
    const k = kEl.value.trim(), v = vEl.value.trim();
    if (!/^[A-Za-z_][\w-]*$/.test(k) || !v) { this.ui.toast(tr('Type a name and a value'), 'error'); return; }
    kEl.value = ''; vEl.value = ''; void this.setPortProp(fp, role, port, k, v);
  }
  // ---- what a test needs ----
  deviceTypeNames() { const s = new Set<string>(['EXOS', 'VOSS']); for (const d of this.ui.settings()?.deviceTypes ?? []) if (d.name) s.add(d.name); return [...s]; }
  setReq(d: TestCase, role: string, k: keyof TestRequirement, v: unknown) {
    const all = { ...(d.requires ?? {}) }, q: Record<string, unknown> = { ...(all[role] ?? {}) };
    if (v === undefined || v === '' || (Array.isArray(v) && !v.length)) delete q[k]; else q[k] = v;
    if (Object.keys(q).length) all[role] = q as TestRequirement; else delete all[role];
    d.requires = Object.keys(all).length ? all : undefined; this.touch();
  }
  // ---- defaults of a folder of tests / all tests ----
  readonly defDlg = signal<{ dir: string; vals: { k: string; v: string }[]; o: TestStepOptions; inh: TestSuiteDefaults } | null>(null);
  async openDefaults(dir: string) {
    try {
      const v = await this.backend.testsDefaultsView(dir);
      this.defDlg.set({ dir, vals: Object.entries(v.own.vars ?? {}).map(([k, x]) => ({ k, v: x })), o: { ...(v.own.stepOptions ?? {}) }, inh: v.inherited ?? {} });
    } catch (e) { this.ui.toast(errText(e), 'error'); }
  }
  defIntro(dir: string) { return dir ? tr('For every test in {0} and its folders. Its folders, the test itself, the testbed and the run can still change them.', dir) : tr('For every test. Folders, tests, testbeds and runs can still change them.'); }
  orderOf(dir: string) {
    const out = [{ l: 'All tests', on: !dir }];
    if (dir) { const parts = dir.split('/'); for (let i = 1; i <= parts.length; i++) out.push({ l: parts.slice(0, i).join('/'), on: i === parts.length }); }
    return [...out.filter(o => o.l !== 'cases' || o.on), { l: 'the test', on: false }, { l: 'the testbed', on: false }, { l: 'the run', on: false }];
  }
  inheritedVals(dd: { vals: { k: string }[]; inh: TestSuiteDefaults }) { return Object.entries(dd.inh.vars ?? {}).filter(([k]) => !dd.vals.some(x => x.k === k)).map(([k, v]) => ({ k, v })); }
  inhOptText(dd: { inh: TestSuiteDefaults }) {
    const o = dd.inh.stepOptions; if (!o) return '';
    return [o.timeout ? 'timeout ' + o.timeout + ' s' : '', o.wait ? 'wait ' + o.wait + ' s' : '', o.retry ? 'retry ' + (o.retry.timeout ?? 60) + ' s' : '', o.continueOnFail ? 'go on when a step fails' : '', o.ignoreErrors ? 'ignore device errors' : ''].filter(Boolean).join(' · ');
  }
  addDefVal(dd: { vals: { k: string; v: string }[] }, k: HTMLInputElement, v: HTMLInputElement) {
    if (!/^[A-Za-z_][\w.-]*$/.test(k.value.trim())) { this.ui.toast(tr('A name is letters, digits, _ and .'), 'error'); return; }
    dd.vals.push({ k: k.value.trim(), v: v.value }); k.value = ''; v.value = '';
  }
  async saveDefaults(dd: { dir: string; vals: { k: string; v: string }[]; o: TestStepOptions }) {
    const vars: Record<string, string> = {}; for (const x of dd.vals) if (x.k.trim()) vars[x.k.trim()] = x.v;
    const o: Record<string, unknown> = {}; for (const [k, v] of Object.entries(dd.o)) if (v !== undefined && v !== null && v !== '' && v !== 0 && v !== false) o[k] = v;
    try {
      await this.backend.testsSaveSuiteDefaults(dd.dir, { vars: Object.keys(vars).length ? vars : undefined, stepOptions: Object.keys(o).length ? o as TestStepOptions : undefined });
      this.defDlg.set(null); this.ui.toast(tr('Saved {0}', (dd.dir ? dd.dir + '/' : '') + 'defaults.yaml'), 'success');
    } catch (e) { this.ui.toast(errText(e), 'error'); }
  }
  valuesOf(d: TestbedDevice) { return Object.entries(d.values ?? {}).sort().map(([k, v]) => ({ k, v })); }
  async renameRole(fp: string, role: string) {
    const to = (await this.ui.input(tr('Rename the Role'), tr('Tests use {{role}}: give it the name the tests use, e.g. dut or core.'), role))?.trim();
    if (!to || to === role) return;
    try { await this.backend.testbedRenameRole(fp, role, to); await this.openFile(fp); } catch (e) { this.ui.toast(errText(e), 'error'); }
  }
  async editValue(fp: string, role: string, key: string, cur: string) {
    if (!key) {
      const kv = await this.ui.input(tr('A Value of {0}', role), tr('name = value, e.g. port = 1:2 (a stack) or port = 1 (a standalone switch). Tests use {{{0}.port}}.', role), '');
      if (!kv?.includes('=')) return;
      key = kv.split('=')[0].trim(); cur = kv.slice(kv.indexOf('=') + 1).trim();
      if (!key) return;
    } else {
      const v = await this.ui.input(tr('{0}.{1}', role, key), tr('The value for this device (empty: remove it).'), cur);
      if (v === null || v === undefined) return;
      cur = v.trim();
    }
    try { await this.backend.testbedSetValue(fp, role, key, cur); await this.openFile(fp); } catch (e) { this.ui.toast(errText(e), 'error'); }
  }
  async removeRole(fp: string, role: string) {
    try { await this.backend.testbedChange(fp, [], [role]); await this.openFile(fp); } catch (e) { this.ui.toast(errText(e), 'error'); }
  }
  async saveFile() { try { await this.backend.testSaveFile(this.fileSel(), this.fileText); this.ui.toast(tr('Saved'), 'success'); await this.load(); } catch (e) { this.ui.toast(errText(e), 'error'); } }
  moreMenu(ev: MouseEvent) {
    this.ui.showMenu(ev, [
      { label: 'Defaults for All Tests…', icon: 'options', action: () => void this.openDefaults('') },
      { label: 'Write a Test with AI…', action: () => void this.writeWithAI() },
      { label: 'Import Test Cases (Excel, CSV, Postman, OpenAPI)…', action: () => void this.importManual() },
      { label: 'Export Automation Suite (Go, Python, TCL)…', icon: 'export', action: () => this.openExport() },
      { separator: true },
      { label: 'Open a Tests Folder…', action: () => void this.openFolder() },
      { label: 'Clone a Git Repository…', action: () => void this.clone() },
      { label: 'Open the Folder in VS Code', action: () => void this.backend.testsOpenInEditor('') },
    ]);
  }
  gitMenu(ev: MouseEvent) {
    const g = this.git();
    this.ui.showMenu(ev, g?.repo ? [
      { label: tr('Pull') + (g.behind ? ' (' + g.behind + ')' : ''), action: () => void this.gitDo('pull'), disabled: !g.upstream },
      { label: tr('Commit…') + (g.changed ? ' (' + g.changed + ' changed)' : ''), action: () => void this.gitDo('commit'), disabled: !g.changed },
      { label: tr('Push') + (g.ahead ? ' (' + g.ahead + ')' : ''), action: () => void this.gitDo('push'), disabled: !!g.upstream && !g.ahead },
    ] : [{ label: 'Make it a Git Repository', action: () => void this.gitDo('init'), disabled: !g?.hasGit }, { label: 'Clone a Git Repository…', action: () => void this.clone() }]);
  }
  testMenu(ev: MouseEvent) {
    this.ui.showMenu(ev, [
      { label: 'Rename…', action: () => void this.rename(false) },
      { label: 'Duplicate…', action: () => void this.rename(true) },
      { label: 'Use Port Variables (port1…port8)', action: () => void this.usePortVars() },
      { label: 'Make a Procedure from This Test…', action: () => void this.procFromTest() },
      { label: 'Tags…', action: () => void this.editTags() },
      { label: 'Depends On…', action: () => void this.editDeps() },
      { label: 'qTest Test Case…', action: () => void this.editQTest() },
      { label: 'Open in VS Code', action: () => void this.backend.testsOpenInEditor(this.sel()) },
      { label: 'Record a New Test', action: () => this.recordMore() },
      { separator: true },
      { label: 'Delete…', action: () => void this.remove() },
    ]);
  }
  splitTags(v: string) { const l = v.split(',').map(x => x.trim()).filter(Boolean); return l.length ? l : undefined; }
  /** A test's name in the list; the file name joins it when the name is missing or used twice. */
  caseName(c: TestCaseInfo) {
    const n = (c.name || '').trim(), f = this.base(c.file);
    if (!n) return f;
    const dup = (this.tree()?.cases ?? []).filter(x => (x.name || '').trim() === n).length > 1;
    return dup || n === 'New test' ? n + ' · ' + f : n;
  }
  filtering() { return !!this.q().trim() || this.tag() !== 'all'; }
  shownFiles() { return this.filtered().map(c => c.file); }
  editSuite(f: string) { this.section.set('library'); void this.openFile(f); }
  async saveSuite() {
    const name = await this.ui.input(tr('Save as Plan'), tr('Plan name (suites/<name>.yaml)'), '');
    if (!name?.trim()) return;
    try { const rel = await this.backend.testSaveSuite(name.trim().toLowerCase().replace(/[^a-z0-9]+/g, '-'), { name: name.trim(), tests: this.picked() }); this.ui.toast(tr('Wrote {0}', rel), 'success'); await this.load(); }
    catch (e) { this.ui.toast(errText(e), 'error'); }
  }
  private async saveCase(mod: (c: TestCase) => void) {
    const t = this.test(); if (!t) return;
    const c = structuredClone(t); mod(c);
    try { await this.backend.testSave(this.sel(), c); await this.load(); } catch (e) { this.ui.toast(errText(e), 'error'); }
  }
  readonly sending = signal(false);
  async sendQTest(rep: TestRunReport) {
    this.sending.set(true);
    try { this.ui.toast(await this.backend.testSendQTest(rep.dir), 'success'); await this.openRunReport(rep.dir); }
    catch (e) { this.ui.toast(errText(e), 'error'); }
    this.sending.set(false);
  }
  async createDefect(dir: string, r: TestResult) {
    if (!(await this.ui.confirm(tr('Open a Jira Defect?'), tr('A defect for "{0}" with its steps, the failed output and the report, in the Jira project of Global Options → Integrations.', r.case), tr('Open Defect')))) return;
    try {
      const [key, link] = await this.backend.testCreateDefect(dir, r.file || r.case);
      this.ui.toast(tr('Opened {0}', key), 'success');
      r.defect = key; r.defectUrl = link; this.last.update(m => ({ ...m }));
      if (this.runSel() === dir) await this.openRunReport(dir);
    } catch (e) { this.ui.toast(errText(e), 'error'); }
  }
  async editQTest() {
    const v = await this.ui.input(tr('qTest Test Case'), tr('The id or PID of the qTest test case this test reports to, e.g. TC-12'), this.test()?.qtest?.testCase ?? '');
    if (v === null || v === undefined) return;
    await this.saveCase(c => { if (v.trim()) c.qtest = { testCase: v.trim() }; else delete c.qtest; });
  }
  async editTags() {
    const v = await this.ui.input(tr('Tags'), tr('Comma-separated, e.g. smoke, l2'), (this.test()?.tags ?? []).join(', '));
    if (v === null || v === undefined) return;
    const tags = v.split(',').map(x => x.trim()).filter(Boolean);
    await this.saveCase(c => { if (tags.length) c.tags = tags; else delete c.tags; });
  }
  async editDeps() {
    const v = await this.ui.input(tr('Depends On'), tr('Tests that must pass first (files or names, comma-separated); when one fails, this test is skipped'), (this.test()?.dependsOn ?? []).join(', '));
    if (v === null || v === undefined) return;
    const d = v.split(',').map(x => x.trim()).filter(Boolean);
    await this.saveCase(c => { if (d.length) c.dependsOn = d; else delete c.dependsOn; });
  }
  cleanupLabel() { return this.cleanupMode === 'off' ? 'no cleanup' : this.cleanupMode === 'auto' ? 'cleanup steps, else undo' : 'run the cleanup steps'; }
  /** A step's result in the last run. */
  resOf(p: Phase, i: number): TestStepResult | undefined {
    const l = this.resList(p)[i];
    if (!l?.length) return undefined;
    if (l.length === 1 && !this.listOf(this.test()!, p)[i]?.loop) return l[0];
    const bad = l.find(x => !x.passed && !x.skipped);
    return { ...(bad ?? l[0]), passed: !bad, skipped: l.every(x => x.skipped), ms: l.reduce((n, x) => n + x.ms, 0), checks: undefined, error: bad ? bad.name + ': ' + (bad.error || 'failed') : undefined };
  }
  /** The last run's results of each step of a section (a loop step has one per inner step and value). */
  resList(p: Phase): TestStepResult[][] {
    const t = this.test(), rs = this.last()[this.sel()]?.steps?.filter(x => (x.phase ?? '') === (p === 'steps' ? '' : p)) ?? [];
    if (!t) return [];
    const out: TestStepResult[][] = [];
    let k = 0;
    for (const st of this.listOf(t, p)) {
      if (st.loop) {
        const tag = '[' + st.loop.var + '=', mine: TestStepResult[] = [];
        while (k < rs.length && rs[k].name.includes(tag)) mine.push(rs[k++]);
        out.push(mine);
      } else out.push(k < rs.length ? [rs[k++]] : []);
    }
    return out;
  }
  private rc = (() => { try { return JSON.parse(localStorage.getItem('tw.tests.rc') ?? '{}') as { r?: number; c?: number; t?: number }; } catch { return {}; } })();
  rerun = this.rc.r ?? 0; caseMin: number | null = this.rc.c || null; runMin: number | null = this.rc.t || null;
  keepRc() { try { localStorage.setItem('tw.tests.rc', JSON.stringify({ r: this.rerun, c: this.caseMin, t: this.runMin })); } catch { /* not kept */ } }
  pauseOnFail = (() => { try { return localStorage.getItem('tw.tests.pause') === '1'; } catch { return false; } })();
  readonly paused = signal<{ case: string; step: TestStepResult; breakpoint?: boolean } | null>(null);
  async resume(act: 'retry' | 'continue' | 'stop' | 'step' | 'skip') {
    const orig = this.paused()?.step.vars ?? {}, set: Record<string, string> = {};
    for (const x of this.pauseVars()) if (orig[x.k] !== x.v) set[x.k] = x.v;
    this.paused.set(null);
    await this.backend.testResume(act, set).catch(e => this.ui.toast(errText(e), 'error'));
  }
  readonly pauseVars = signal<{ k: string; v: string }[]>([]);
  stepMode = false;
  private readonly breaks = signal<Record<string, number[]>>((() => { try { return JSON.parse(localStorage.getItem('tw.tests.breaks') || '{}'); } catch { return {}; } })());
  breaksFor(file: string) { return this.breaks()[file] ?? []; }
  hasBreak(i: number) { return this.breaksFor(this.sel()).includes(i + 1); }
  breakCount() { return this.breaksFor(this.sel()).length; }
  toggleBreak(i: number) {
    const f = this.sel(), l = this.breaksFor(f), n = i + 1;
    this.breaks.update(m => ({ ...m, [f]: l.includes(n) ? l.filter(x => x !== n) : [...l, n].sort((a, b) => a - b) }));
    try { localStorage.setItem('tw.tests.breaks', JSON.stringify(this.breaks())); } catch { /* not kept */ }
  }
  openDevice(path: string) { void this.tabsSvc.openSession(path); }
  runningNow(file: string) { const f = this.runningFiles; return !f.length || f.includes(file); }
  private runningFiles: string[] = [];
  isReadStep(s: TestStep) { return isRead(s); }
  varList(r?: TestStepResult | null): [string, string][] { return r?.vars ? Object.entries(r.vars).sort((a, b) => a[0].localeCompare(b[0])) : []; }
  stepMenu(ev: MouseEvent, p: Phase, i: number) {
    ev.preventDefault();
    if (p !== 'steps' || this.busy()) return;
    this.ui.showMenu(ev, [
      { label: 'Run This Step', icon: 'play', action: () => this.runStep(i, 'only') },
      { label: 'Run From Here', action: () => this.runStep(i, 'from') },
    ]);
  }
  runStep(i: number, how: 'only' | 'from') {
    const f = this.sel(); if (!f) return;
    void this.run([f], false, false, how === 'only' ? { only: i + 1 } : { from: i + 1 });
  }
  failedCheck(x: TestStepResult) { const c = x.checks?.find(y => !y.passed); return c ? c.what + ', got ' + (c.got ?? '') : ''; }
  logicText(s: TestStep) { return [s.if ? 'if ' + s.if : '', s.retry ? 'retry ' + (s.retry.timeout ?? 60) + ' s' : '', s.wait && (s.run || s.http || s.snmp) ? 'wait ' + s.wait + ' s first' : ''].filter(Boolean).join(' · '); }
  secSummary(p: Phase) {
    const t = this.test(); if (!t) return '';
    const l = this.listOf(t, p), rs = l.map((_, i) => this.resOf(p, i));
    const failed = rs.filter(r => r && !r.passed && !r.skipped).length, bare = l.filter(s => !s.expect?.length && !s.manual && this.isReadStep(s)).length;
    return [l.length + (l.length === 1 ? ' step' : ' steps'), failed ? failed + ' failed' : '', bare ? bare + ' not checked' : ''].filter(Boolean).join(' · ');
  }
  isOpen(p: Phase, i: number) { return this.open().includes(p + i); }
  toggleRow(p: Phase, i: number) { const k = p + i; this.open.update(l => l.includes(k) ? l.filter(x => x !== k) : [...l, k]); }
  target(s: TestStep) { return s.run?.session ?? s.http?.session ?? s.snmp?.session ?? s.web?.login ?? ''; }
  shortDev(t: string) { return t.replace(/^\{\{\s*|\s*\}\}$/g, '').split('/').pop() ?? t; }
  runHint() { const v = this.view(); return v === 'go' ? 'Run: go test ./cases/...' : v === 'py' ? 'Run: python lib/python/run_tests.py' : v === 'tcl' ? 'Run: tclsh lib/tcl/run_tests.tcl' : ''; }
  openRun(files: string[], label: string) {
    this.runFiles.set(files); this.runLabel.set(label);
    const tbs = this.tree()?.testbeds ?? [];
    let last = ''; try { last = localStorage.getItem('tw.tests.testbed') ?? ''; } catch { /* none */ }
    if (!tbs.includes(this.testbed)) this.testbed = tbs.includes(last) ? last : (tbs[0] ?? '');
    this.willSkip.set([]);
    this.runRoles.set([]);
    this.runVars = {}; this.runOwnPorts.set({});
    if (files.length) void this.backend.testsRoles(files).then(async r => {
      this.runRoles.set(r ?? []);
      const m: Record<string, string[]> = {};
      for (const x of r ?? []) m[x.role] = await this.backend.testsPorts(x.session).catch(() => []) ?? [];
      this.runOwnPorts.set(m);
    }).catch(() => {});
    void this.loadRunMap();
  }
  readonly runRoles = signal<{ role: string; session: string; deviceType?: string; protocol?: string }[]>([]);
  readonly runOwnPorts = signal<Record<string, string[]>>({});
  runVars: Record<string, string> = {};
  readonly portsOpen = signal(new Set<string>());
  togglePorts(role: string) { const x = new Set(this.portsOpen()); if (x.has(role)) x.delete(role); else x.add(role); this.portsOpen.set(x); }
  portsLine(ps: { value: string }[]) { const v = ps.map(p => p.value); return v.length > 4 ? v.slice(0, 3).join(', ') + ' … ' + v[v.length - 1] : v.join(', '); }
  runDevName(r: { role: string; session: string }) { const d = this.runTbDevs().find(x => x.role === r.role && x.session); return (d?.session ?? r.session).split('/').pop() ?? ''; }
  runPortsOf(role: string): { name: string; value: string; set?: boolean }[] {
    const d = this.testbed ? this.runTbDevs().find(x => x.role === role && x.session) : undefined;
    if (d?.ports?.length) return d.ports.filter(p => p.value || p.set);
    const def = this.runOwnPorts()[role] ?? [];
    return def.map((v, i) => { const n = 'port' + (i + 1), o = this.runVars[role + '.' + n]; return { name: n, value: o ?? v, set: o !== undefined }; });
  }
  async setRunPort(role: string, name: string, value: string) {
    value = value.trim();
    const d = this.testbed ? this.runTbDevs().find(x => x.role === role && x.session) : undefined;
    if (d) {
      const def = (await this.backend.testsPorts(d.session!).catch(() => []))[+name.slice(4) - 1] ?? '';
      try { await this.backend.testbedSetValue(this.testbed, role, name, value === def ? '' : value); } catch (e) { this.ui.toast(errText(e), 'error'); }
      await this.loadRunMap(); return;
    }
    const def = this.runOwnPorts()[role]?.[+name.slice(4) - 1];
    if (!value || value === def) delete this.runVars[role + '.' + name]; else this.runVars[role + '.' + name] = value;
    this.runOwnPorts.update(m => ({ ...m }));
  }
  readonly runTbDevs = signal<TestbedDevice[]>([]);
  readonly willSkip = signal<{ file: string; case: string; why: string }[]>([]);
  runCount() { return this.runFiles()?.length || this.tree()?.cases?.length || 0; }
  async loadRunMap() {
    try { localStorage.setItem('tw.tests.testbed', this.testbed); } catch { /* not kept */ }
    const files = (this.runFiles() ?? []).filter(x => x.startsWith('cases/'));
    void this.backend.testsWillSkip(files, this.testbed).then(x => this.willSkip.set(x ?? [])).catch(() => this.willSkip.set([]));
    this.runTbDevs.set(this.testbed ? (await this.backend.testbedDevices(this.testbed).catch(() => []) ?? []) : []);
  }
  runMapOf(role: string) { return this.runTbDevs().find(d => d.role === role)?.session ?? ''; }
  async mapRole(role: string, session: string) {
    if (!session) { await this.backend.testbedChange(this.testbed, [], [role]).catch(e => this.ui.toast(errText(e), 'error')); }
    else { try { await this.backend.testbedSetRole(this.testbed, role, session); this.ui.toast(tr('{0} is {1} in {2}', role, session.split('/').pop() ?? '', this.base(this.testbed)), 'success'); } catch (e) { this.ui.toast(errText(e), 'error'); } }
    await this.loadRunMap();
  }
  readonly multi = signal(false);
  multiPick: string[] = [];
  toggleMulti(b: string) { this.multiPick = this.multiPick.includes(b) ? this.multiPick.filter(x => x !== b) : [...this.multiPick, b]; }
  lockOf(tb: string) { return this.tree()?.locks?.find(l => l.testbed === tb); }
  lockText(tb: string) { const l = this.lockOf(tb); return l ? (l.mine ? '  · running' : '  · in use by ' + l.user) : ''; }
  async releaseLock(tb: string) { try { await this.backend.testReleaseLock(tb); await this.load(); } catch (e) { this.ui.toast(errText(e), 'error'); } }
  async startRun() {
    try { localStorage.setItem('tw.tests.pause', this.pauseOnFail ? '1' : '0'); } catch { /* not kept */ }
    const f = this.runFiles() ?? [];
    const tbs = this.multi() ? this.multiPick : this.testbed ? [this.testbed] : [];
    const held = tbs.map(t => this.lockOf(t)).filter(l => l && !l.mine);
    let force = false;
    if (held.length) {
      if (!(await this.ui.confirm(tr('Testbed in Use'), held.map(l => tr('{0} is in use by {1} on {2}', this.base(l!.testbed), l!.user, l!.host)).join('\n') + '\n' + tr('Take it over? Their run may still be sending commands.'), tr('Take Over'), true))) return;
      force = true;
    }
    this.runFiles.set(null);
    if (this.multi() && tbs.length > 1) void this.runMany(f, tbs, force); else void this.run(f, false, force);
  }
  private async runMany(files: string[], testbeds: string[], forceLock: boolean, confirmed = false): Promise<void> {
    this.busy.set(true);
    try {
      const reps = await this.backend.runTestsParallel({ files, testbeds, confirmed, forceLock, onCleanupFail: this.cleanupMode === 'off' ? undefined : this.onCleanupFail, cleanup: this.cleanupMode || undefined });
      for (const r of reps ?? []) if (r?.dir) this.took(r);
      const first = reps?.find(r => r?.dir);
      if (first) await this.openRunReport(first.dir);
      await this.load();
    } catch (e) {
      const t = errText(e);
      if (!confirmed && /production sessions/i.test(t)) {
        this.busy.set(false);
        if (await this.ui.confirm(tr('Run on Production?'), t + '. ' + tr('Their commands and requests are sent to those devices.'), tr('Run'), true)) return this.runMany(files, testbeds, forceLock, true);
        return;
      }
      if (/needs an approval/i.test(t)) { this.busy.set(false); for (const tb of testbeds) await this.askApproval(files, tb, t); return; }
      this.ui.toast(t, 'error'); await this.load();
    }
    this.busy.set(false);
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
  edit() { void this.loadBlocks(); const t = this.test(); if (t) { void this.loadRolePorts(t); this.cks.clear(); const d = structuredClone({ ...t, setup: t.setup ?? [], steps: t.steps ?? [], cleanup: t.cleanup ?? [] }); for (const x of [...d.setup, ...d.steps, ...d.cleanup]) if (x.http) x.http.method ||= 'GET'; this.draft.set(d); } }
  listOf(d: TestCase, p: Phase): TestStep[] { return (p === 'steps' ? d.steps : p === 'setup' ? d.setup : d.cleanup) ?? []; }
  touch() { const d = this.draft(); if (d) this.draft.set({ ...d }); this.cks.clear(); }
  addStep(p: Phase) {
    const d = this.draft(); if (!d) return;
    const all = [...(d.setup ?? []), ...d.steps, ...(d.cleanup ?? [])];
    const ses = [...all].reverse().find(s => s.run)?.run?.session ?? '';
    this.listOf(d, p).push({ run: { session: ses, command: '' } });
    this.touch();
  }
  lastSession() { const d = this.draft(); const all = d ? [...(d.setup ?? []), ...d.steps, ...(d.cleanup ?? [])] : []; return [...all].reverse().find(s => s.run)?.run?.session ?? ''; }
  loopSrc(l: NonNullable<TestStep['loop']>) { return l.values?.length ? l.values.join(', ') : l.range ?? l.from ?? ''; }
  setLoopSrc(l: NonNullable<TestStep['loop']>, v: string) {
    delete l.values; delete l.range; delete l.from; v = v.trim();
    if (/^\{\{.+\}\}$/.test(v)) l.from = v;
    else if (/^[\w:./]*\d+\s*-\s*[\w:./]*\d+$/.test(v) && !v.includes(',')) l.range = v.replace(/\s/g, '');
    else l.values = v.split(',').map(x => x.trim()).filter(Boolean);
  }
  addApi(p: Phase) { const d = this.draft(); if (!d) return; const last = [...(d.steps ?? [])].reverse().find(s => s.http)?.http?.session ?? ''; this.listOf(d, p).push({ http: { session: last, method: 'GET', url: '' }, expect: [{ status: 200 }] }); this.touch(); }
  async addCurl(p: Phase) {
    const d = this.draft(); if (!d) return;
    const c = await this.ui.input(tr('Paste a curl Command'), tr('It becomes a REST step (method, URL, headers, body). Passwords and tokens in it are not kept: use an API session for the login.'), '');
    if (!c?.trim()) return;
    const h = parseCurl(c);
    if (!h) return this.ui.toast(tr('That is not a curl command with a URL'), 'error');
    this.listOf(d, p).push({ http: h, expect: [{ status: h.method === 'POST' ? 201 : 200 }] }); this.touch();
  }
  private diffs = signal<Record<string, { when: string; lines: string[] }>>({});
  diffOf(p: Phase, i: number) { return this.diffs()[this.sel() + '|' + p + i]; }
  async compare(p: Phase, i: number) {
    const cur = this.resOf(p, i), file = this.sel();
    const h = (await this.backend.testHistory(file, 50).catch(() => []))?.[0];
    const lastDir = this.last()[file] ? this.report()?.dir : '';
    const prev = h?.runs.find(x => x.passed && !x.skipped && x.dir !== lastDir);
    if (!prev || !cur) return this.ui.toast(tr('No earlier passing run of this test to compare with'), 'info');
    try {
      const rep = JSON.parse(await this.backend.testFile(prev.dir + '/result.json')) as TestRunReport;
      const r = rep.results.find(x => x.file === file);
      const old = r?.steps.filter(x => (x.phase ?? '') === (p === 'steps' ? '' : p))[i];
      if (!old) return this.ui.toast(tr('That run has no such step'), 'info');
      this.diffs.update(m => ({ ...m, [file + '|' + p + i]: { when: prev.at.slice(0, 16).replace('T', ' '), lines: lineDiff(old.output ?? '', cur.output ?? '') } }));
    } catch (e) { this.ui.toast(errText(e), 'error'); }
  }
  readonly fetching = signal(false);
  async fetchTemplates() {
    this.fetching.set(true);
    try { this.ui.toast(await this.backend.testsFetchTemplates(), 'success'); await this.load(); } catch (e) { this.ui.toast(errText(e), 'error'); }
    this.fetching.set(false);
  }
  addMenu(ev: MouseEvent, p: Phase) {
    this.ui.showMenu(ev, [
      { label: 'Call a Procedure', action: () => this.addCall(p) },
      { label: 'NETCONF Request', action: () => this.addNetconf(p) },
      { label: 'Web Page (Playwright)', action: () => this.addWeb(p) },
      { label: 'Spirent / IxNetwork Traffic Run', action: () => this.addTGen(p) },
      { separator: true },
      { label: 'API Request', action: () => this.addApi(p) },
      { label: 'From a curl Command…', action: () => void this.addCurl(p) },
      { label: 'Manual Step (a person answers)', action: () => this.addManual(p) },
      { label: 'Wait', action: () => this.addWait(p) },
      { label: 'Loop over Values', action: () => this.addLoop(p) },
      { label: 'Software Upgrade', action: () => this.addUpgrade(p) },
      { label: 'Lab Traffic Test', action: () => this.addTraffic(p) },
    ]);
  }
  addCall(p: Phase) { const d = this.draft(); if (!d) return; void this.loadProcs(); this.listOf(d, p).push({ call: { proc: '', with: {} } }); this.touch(); }
  // the web actions: tg/v false = no target / value box
  readonly webGroups: { g: string; a: { k: string; l: string; t: string; tg?: boolean; v?: boolean }[] }[] = [
    { g: 'Do', a: [{ k: 'click', l: 'Click', t: 'Click it', v: false }, { k: 'fill', l: 'Type into', t: 'Replace the field\'s text' }, { k: 'select', l: 'Choose option', t: 'The option\'s text (or value)' },
      { k: 'check', l: 'Tick', t: 'Tick a checkbox or radio', v: false }, { k: 'uncheck', l: 'Untick', t: 'Untick a checkbox', v: false }, { k: 'press', l: 'Press key', t: 'Enter, Tab, Escape, Control+A… (no target: the page)' },
      { k: 'type', l: 'Type key by key', t: 'For fields that react to each key (search boxes)' }, { k: 'clear', l: 'Empty field', t: 'Empty a field', v: false },
      { k: 'dblclick', l: 'Double-click', t: '', v: false }, { k: 'rightclick', l: 'Right-click', t: '', v: false }, { k: 'hover', l: 'Hover', t: 'Move the mouse over it (menus)', v: false },
      { k: 'focus', l: 'Focus', t: '', v: false }, { k: 'scroll', l: 'Scroll to', t: 'No target: one screen down', v: false }] },
    { g: 'Files and windows', a: [{ k: 'upload', l: 'Upload files', t: 'Files of the tests folder, comma-separated' }, { k: 'download', l: 'Download', t: 'Click it and save the file in reports/web-shots (value: a file name)' },
      { k: 'dialog', l: 'Answer pop-ups', t: 'accept, dismiss or a text to type - for the alerts and confirms that come next', tg: false }, { k: 'tab', l: 'Switch tab', t: 'last, 1, 2… or a part of its address', tg: false },
      { k: 'close_tab', l: 'Close tab', t: '', tg: false, v: false }, { k: 'goto', l: 'Go to address', t: '', tg: false }, { k: 'reload', l: 'Reload', t: '', tg: false, v: false }, { k: 'back', l: 'Back', t: '', tg: false, v: false }] },
    { g: 'Wait', a: [{ k: 'wait', l: 'Wait for', t: 'A target to show, or seconds, or a part of the address' }, { k: 'wait_gone', l: 'Wait until gone', t: 'Spinners, "Loading…"', v: false }] },
    { g: 'Check', a: [{ k: 'expect', l: 'Check it shows', t: 'No target: the text anywhere. Target and text: it contains the text' }, { k: 'expect_not', l: 'Check it is gone', t: '' },
      { k: 'expect_value', l: 'Check field value', t: '' }, { k: 'expect_count', l: 'Check how many', t: 'e.g. table rows: tr' }, { k: 'expect_checked', l: 'Check ticked', t: '', v: false },
      { k: 'expect_enabled', l: 'Check enabled', t: '', v: false }, { k: 'expect_url', l: 'Check address', t: 'A part of the address', tg: false }, { k: 'expect_title', l: 'Check title', t: 'A part of the title', tg: false }] },
    { g: 'Keep', a: [{ k: 'block', l: 'Use procedure', t: 'The actions of a saved web procedure, with its values', v: false }, { k: 'read', l: 'Read into variable', t: 'The text becomes {{name}} for later steps' }, { k: 'screenshot', l: 'Screenshot', t: 'A target: only it. Value: the file name', }] },
  ];
  private readonly webActMap = new Map(this.webGroups.flatMap(g => g.a.map(a => [a.k, a] as const)));
  webAct(k: string) { return this.webActMap.get(k); }
  webTgtHint(d: string) {
    return ({ expect: 'text=Welcome  or  #status  (empty: the text anywhere)', expect_not: 'empty: the text anywhere', wait: 'what to wait for (empty: the value)', press: 'field (empty: the page)',
      scroll: 'empty: one screen down', screenshot: 'empty: the whole page', expect_count: 'tr  or  .alarm-row', upload: 'input[type=file]  or  label=Firmware' } as Record<string, string>)[d]
      ?? 'text=Sign in   label=Password   role=button[name="Save"]   #id';
  }
  rmWeb(w: TestWeb, k: number) { w.actions!.splice(k, 1); this.selActs.set(new Set()); this.touch(); }
  mvWeb(w: { actions?: unknown[] }, k: number, d: number) { const l = w.actions!; [l[k + d], l[k]] = [l[k], l[k + d]]; this.touch(); }
  allSessions() { const out: string[] = []; const w = (l: SessionNode[]) => { for (const n of l) { if (n.isFolder) w(n.children ?? []); else out.push(n.path); } }; w(this.ui.tree()?.children ?? []); return out; }
  readonly webGuide = 'https://github.com/nperiannan/termwright/blob/main/docs/WEB_TESTING_GUIDE.md';
  readonly webRec = signal(false);
  // ---- step by step in an open browser (Debug in Browser) ----
  readonly dbg = signal<{ w: TestWeb; at: number; res: Record<number, WebResult> } | null>(null);
  readonly dbgBusy = signal(false);
  readonly dbgMsg = signal('');
  private dbgOff?: () => void;
  dbgOn(w: TestWeb) { return this.dbg()?.w === w; }
  private dbgVars(): Record<string, string> { const d = this.draft(); return { ...(d?.vars ?? {}), ...(d?.params?.[0] ?? {}) }; }
  private dbgSet(f: (x: { w: TestWeb; at: number; res: Record<number, WebResult> }) => void) { const x = this.dbg(); if (!x) return; const y = { ...x, res: { ...x.res } }; f(y); this.dbg.set(y); }
  async dbgOpen(w: TestWeb): Promise<boolean> {
    if (this.dbgOn(w)) return true;
    if (!w.url && !this.isWebApp(w.login)) { this.ui.toast(tr('Type the page address first'), 'error'); return false; }
    this.dbgBusy.set(true); this.dbgMsg.set(tr('Opening the browser…'));
    if (!this.dbgOff) {
      this.dbgOff = this.backend.on('web:debug', (e: { open: boolean }) => { if (!e.open) { this.dbg.set(null); this.dbgMsg.set(''); this.webRec.set(false); } });
      this.offs.push(this.dbgOff);
    }
    try { await this.backend.webDebugOpen(w, this.dbgVars()); this.dbg.set({ w, at: 0, res: {} }); this.dbgMsg.set(''); return true; }
    catch (e) { this.ui.toast(errText(e), 'error'); this.dbgMsg.set(''); return false; }
    finally { this.dbgBusy.set(false); }
  }
  private async dbgRun(w: TestWeb, from: number, to: number) {
    const acts = (w.actions ?? []).slice(from, to);
    this.dbgBusy.set(true);
    try {
      const res = await this.backend.webDebugRun(acts, from, this.dbgVars());
      this.dbgSet(x => { for (const r of res) x.res[r.i] = r; const bad = res.find(r => !r.ok); x.at = bad ? bad.i : from + res.length; });
      const bad = res.find(r => !r.ok);
      if (bad) this.dbgMsg.set(tr('Action {0}: {1}', bad.i + 1, bad.error ?? '')); else this.dbgMsg.set('');
    } catch (e) { this.ui.toast(errText(e), 'error'); }
    finally { this.dbgBusy.set(false); }
  }
  async dbgRunOne(w: TestWeb, k: number) { if (await this.dbgOpen(w)) await this.dbgRun(w, k, k + 1); }
  /** From the step's page up to action k (opens the browser when it is not open). */
  async dbgRunTo(w: TestWeb, k: number) { if (!(await this.dbgOpen(w))) return; await this.dbgRestart(); this.dbgSet(x => { x.res = {}; }); await this.dbgRun(w, 0, k + 1); }
  // ---- the web step card ----
  private webSets = signal(new Set<string>());
  webSetOpen(p: string, i: number) { return this.webSets().has(p + i); }
  toggleWebSet(p: string, i: number) { const x = new Set(this.webSets()); if (x.has(p + i)) x.delete(p + i); else x.add(p + i); this.webSets.set(x); }
  webPill(w: TestWeb) {
    const b = w.browser === 'firefox' ? 'Firefox' : w.browser === 'webkit' ? 'WebKit' : 'Chromium';
    return [b, w.viewport ? w.viewport.replace('x', '×') : '', w.insecure ? 'self-signed OK' : '', w.show ? 'shown' : ''].filter(Boolean).join(' · ');
  }
  webAppName(login?: string) {
    if (!login) return tr('No web app');
    const m = /^\{\{\s*([^{}]+?)\s*\}\}$/.exec(login), p = m ? this.draft()?.vars?.[m[1]] ?? '' : login;
    return p ? p.split('/').pop()! : m![1];
  }
  webAppMenu(ev: MouseEvent, w: TestWeb) {
    const d = this.draft(), roles = Object.entries(d?.vars ?? {}).filter(([, v]) => this.isWebApp(v) && !v.includes('{{'));
    const items = [
      ...roles.map(([k, v]) => ({ label: (w.login === '{{' + k + '}}' ? '✓ ' : '') + v.split('/').pop() + '  {{' + k + '}}', icon: '', action: () => { w.login = '{{' + k + '}}'; this.touch(); } })),
      ...this.webApps().filter(p => !roles.some(([, v]) => v === p)).map(p => ({ label: (w.login === p ? '✓ ' : '') + p, icon: '', action: () => { this.useWebApp(w, p); } })),
      { label: (w.login ? '' : '✓ ') + tr('None (the address is a full URL)'), icon: '', action: () => { w.login = undefined; this.touch(); } },
    ];
    this.ui.showMenu(ev, items);
  }
  /** A saved web app chosen for a step: it becomes a role of the test (a testbed can bring its own). */
  private useWebApp(w: TestWeb, path: string) {
    const d = this.draft(); if (!d) return;
    d.vars ??= {};
    let k = Object.keys(d.vars).find(x => d.vars![x] === path);
    if (!k) { const base = path.split('/').pop()!.toLowerCase().replace(/[^a-z0-9]+/g, '_').replace(/^_+|_+$/g, '') || 'app'; k = /^\d/.test(base) ? 'app_' + base : base; let n = 2; while (d.vars[k]) k = base + '_' + n++; d.vars[k] = path; }
    w.login = '{{' + k + '}}'; this.touch();
  }
  webMore(ev: MouseEvent, w: TestWeb) {
    this.ui.showMenu(ev, [
      { label: tr('Debug in Browser (run actions one by one, pick targets)'), icon: 'play', action: () => void this.dbgOpen(w) },
      { label: tr('Save as Procedure…'), icon: '', action: () => void this.saveBlock(w) },
    ]);
  }
  /** The last run of a web step: each action's result and picture (when the run's lines match the actions). */
  webRun(p: Phase, i: number, w: TestWeb): { st: (string | undefined)[]; pic: string[] } {
    const out = this.resOf(p, i)?.output ?? '', st: (string | undefined)[] = [], pic: string[] = [];
    if (!out) return { st, pic };
    let k = -1, ok = true;
    for (const l of out.split('\n')) {
      if (/^[✓✗] /.test(l)) { if (k === -1 && /^✓ open /.test(l)) continue; k++; st[k] = l.startsWith('✓') ? 'ok' : 'bad'; }
      else if (k >= 0 && /^healed /.test(l)) st[k] = 'hw';
      else if (k >= 0 && /^picture: /.test(l)) { const pp = this.picsOf(l)[0]; if (pp) pic[k] = pp.path; }
      else if (/^--- page text ---$/.test(l)) break;
    }
    if (k + 1 > (w.actions?.length ?? 0)) ok = false; // procedures ran as several actions: no per-action results
    return ok ? { st, pic } : { st: [], pic: [] };
  }
  private actOpens = new WeakMap<TestWeb, Set<number>>();
  readonly actTick = signal(0);
  actOpen(w: TestWeb, k: number) { this.actTick(); return !!this.actOpens.get(w)?.has(k); }
  toggleAct(w: TestWeb, k: number) { const x = this.actOpens.get(w) ?? new Set<number>(); if (x.has(k)) x.delete(k); else x.add(k); this.actOpens.set(w, x); this.actTick.update(n => n + 1); }
  openAct(w: TestWeb, k: number) { this.actOpens.set(w, new Set([k])); this.actTick.update(n => n + 1); }
  dupWeb(w: TestWeb, k: number) { const a = w.actions![k]; w.actions!.splice(k + 1, 0, { ...a, alt: a.alt ? [...a.alt] : undefined }); this.openAct(w, k + 1); this.touch(); }
  readonly dropAt = signal(-1);
  private dragFrom: { w: TestWeb; k: number } | null = null;
  dragAct(w: TestWeb, k: number, ev: DragEvent) { this.dragFrom = { w, k }; ev.dataTransfer?.setData('text/plain', String(k)); if (ev.dataTransfer) ev.dataTransfer.effectAllowed = 'move'; }
  overAct(w: TestWeb, k: number, ev: DragEvent) { if (this.dragFrom?.w !== w) return; ev.preventDefault(); this.dropAt.set(k); }
  dropAct(w: TestWeb, k: number, ev: DragEvent) {
    ev.preventDefault(); this.dropAt.set(-1);
    const f = this.dragFrom; this.dragFrom = null;
    if (!f || f.w !== w || f.k === k) return;
    const [a] = w.actions!.splice(f.k, 1); w.actions!.splice(k, 0, a); this.actOpens.set(w, new Set()); this.actTick.update(n => n + 1); this.touch();
  }
  // ---- Ask AI: the AI drives a browser from where the step ends (the web tool); its actions are added here ----
  aiWebText = '';
  readonly aiWebBusy = signal<TestWeb | null>(null);
  readonly aiWebCount = signal(0);
  private aiWebOff?: () => void;
  async askAIWeb(w: TestWeb, p: Phase, i: number) {
    const what = this.aiWebText.trim();
    if (!what || this.aiWebBusy()) return;
    if (!this.ai.tools()['web']) { this.ui.toast(tr('Turn on the AI assistant\'s "Web browser (Playwright)" tool first (AI panel → tools).'), 'error'); return; }
    this.aiWebBusy.set(w); this.aiWebCount.set(0);
    try { await this.backend.webAIPrepare(w, this.dbgVars()); }
    catch (e) { this.aiWebBusy.set(null); this.ui.toast(errText(e), 'error'); return; }
    this.aiWebOff = this.backend.on('web:ai', (e: { count?: number }) => { if (e.count !== undefined) this.aiWebCount.set(e.count); });
    const app = this.webAppName(w.login), n = (w.actions ?? []).length;
    this.ai.show(true); this.ai.newChat();
    void this.ai.send(tr('A browser is already open on {0}, at the end of step {1} of the test {2} ({3} actions done). With the web tool (snapshot first, then act; checks as expect actions; values the test has as variables as {{name}}; passwords as {{password}}), do this: {4}. Do not open another browser and do not save: the actions are added to the step in the test editor. Stop when it is done and say what you did.', app, String(i + 1), this.draft()?.name ?? '', String(n), what));
    this.aiWebText = '';
  }
  async takeAIWeb(w: TestWeb) {
    const acts = await this.backend.webAITake().catch(() => [] as TestWebAction[]);
    this.aiWebOff?.(); this.aiWebBusy.set(null);
    if (!acts.length) { this.ui.toast(tr('The AI has not recorded anything yet'), 'error'); return; }
    (w.actions ??= []).push(...acts.map(a => ({ do: a.do, target: a.target, value: a.value, alt: a.alt?.length ? a.alt : undefined })));
    this.touch(); this.ui.toast(tr('Added {0} actions from the AI', acts.length), 'success');
  }
  async cancelAIWeb() { await this.backend.webAITake().catch(() => []); this.aiWebOff?.(); this.aiWebBusy.set(null); }
  async dbgRunAll(w: TestWeb) { await this.dbgRestart(); this.dbgSet(x => { x.res = {}; }); await this.dbgRun(w, 0, (w.actions ?? []).length); }
  async dbgRestart() { this.dbgBusy.set(true); try { await this.backend.webDebugRestart(); this.dbgSet(x => { x.at = 0; x.res = {}; }); } catch (e) { this.ui.toast(errText(e), 'error'); } finally { this.dbgBusy.set(false); } }
  async dbgClose() { await this.backend.webDebugClose().catch(() => {}); this.dbg.set(null); this.dbgMsg.set(''); }
  async dbgPick(w: TestWeb, k: number) {
    if (!(await this.dbgOpen(w))) return;
    this.dbgBusy.set(true); this.dbgMsg.set(tr('Click the element in the browser (Esc: cancel)'));
    try {
      const p = await this.backend.webDebugPick();
      if (p) { const a = w.actions![k]; a.target = p.target; a.alt = p.alt?.length ? p.alt : undefined; if (!a.value && a.do === 'expect') a.value = p.text; this.touch(); }
    } catch (e) { this.ui.toast(errText(e), 'error'); }
    finally { this.dbgBusy.set(false); this.dbgMsg.set(''); }
  }
  async dbgRecord(w: TestWeb) {
    if (this.webRec()) { await this.backend.webDebugRecord(false).catch(() => {}); this.webRec.set(false); return; }
    const acts = (w.actions ??= []);
    const sel = [...this.selActs()].sort((a, b) => a - b);
    let at = sel.length && this.selW === w ? sel[sel.length - 1] + 1 : acts.length;
    const from = at;
    const off = this.backend.on('web:rec', (a: TestWebAction) => {
      if (!this.webRec()) return;
      if (a.do === '__done') { off(); this.webRec.set(false); return; } // Done in the browser's toolbar
      if (a.do === '__undo') { if (at > from) { acts.splice(--at, 1); this.touch(); } return; }
      acts.splice(at++, 0, { do: a.do, target: a.target, value: a.value, alt: a.alt?.length ? a.alt : undefined }); this.touch();
    });
    this.offs.push(off);
    try { await this.backend.webDebugRecord(true); this.webRec.set(true); } catch (e) { off(); this.ui.toast(errText(e), 'error'); }
  }
  // ---- selected actions (a block, or where to record) ----
  readonly selActs = signal(new Set<number>());
  private selW?: TestWeb;
  private selLast = -1;
  isSelAct(w: TestWeb, k: number) { return this.selW === w && this.selActs().has(k); }
  selAct(w: TestWeb, k: number, e: MouseEvent) {
    const cur = this.selW === w ? new Set(this.selActs()) : new Set<number>();
    this.selW = w;
    if (e.shiftKey && this.selLast >= 0) { for (let j = Math.min(this.selLast, k); j <= Math.max(this.selLast, k); j++) cur.add(j); }
    else if (cur.has(k)) cur.delete(k); else cur.add(k);
    this.selLast = k; this.selActs.set(cur);
  }
  // ---- blocks ----
  readonly blocks = signal<TestProcInfo[]>([]);
  async loadBlocks() { try { this.blocks.set(await this.backend.testsWebBlocks()); } catch { /* none */ } }
  blockParams(name?: string) { const b = this.blocks().find(x => x.file === name); return (b?.params ?? []).map(p => p.split('=')[0].trim()); }
  setWith(a: TestWebAction, k: string, v: string) { a.with = { ...(a.with ?? {}), [k]: v }; this.touch(); }
  /** A greyed web button's tooltip: why it is greyed when Playwright is not set up. */
  needPw(t: string) { return this.webSt()?.ready ? t : tr('Set up Playwright first - the note above the actions'); }
  newProcMenu(ev: MouseEvent) {
    this.ui.showMenu(ev, [
      { label: '⌨ Device procedure - CLI, API, SNMP steps', action: () => void this.newProc() },
      { label: '🌐 Web procedure - record in the browser…', action: () => this.openWebProc() },
      { label: 'From the selected test…', action: () => void this.procFromTest(), disabled: !this.sel() },
    ]);
  }
  procMenu(ev: MouseEvent, w: TestWeb) {
    void this.loadBlocks();
    const items = this.blocks().map(b => ({ label: '🌐 ' + b.name + (b.params?.length ? ' (' + b.params.join(', ') + ')' : '') + ' · ' + (b.actions ?? 0) + ' actions', action: () => this.insertProc(w, b.file) }));
    this.ui.showMenu(ev, [...items, ...(items.length ? [{ separator: true }] : []), { label: 'Record a new procedure…', hint: 'opens the browser, saves what you do', action: () => this.openWebProc(w) }]);
  }
  /** Inserts a web procedure after the selected action (or at the end), its values as {{same names}}. */
  insertProc(w: TestWeb, name: string) {
    const acts = (w.actions ??= []), params = this.blockParams(name), withv: Record<string, string> = {};
    for (const p of params) withv[p] = '{{' + p + '}}';
    const at = this.selW === w && this.selActs().size ? Math.max(...this.selActs()) + 1 : acts.length;
    acts.splice(at, 0, { do: 'block', target: name, with: params.length ? withv : undefined }); this.touch();
  }
  /** Record a Web Procedure: the dialog of Record a Web Test, saving the actions as a procedure. */
  openWebProc(into?: TestWeb) {
    void this.loadWebSt();
    this.webProcInto = into ?? null;
    this.webRecDlg.set({ app: into?.login && this.isWebApp(into.login) ? into.login : this.webApps()[0] ?? '', start: into?.url ?? '', name: '', folder: '', proc: true, description: '' });
  }
  private webProcInto: TestWeb | null = null;
  async startWebProc() {
    const wr = this.webRecDlg(); if (!wr) return;
    const acts: TestWebAction[] = [];
    const off = this.backend.on('web:rec', (a: TestWebAction) => recInto(acts, a));
    this.webRecDlg.set(null); this.webRec.set(true);
    try {
      await this.backend.webRecord({ login: wr.app, url: wr.start.trim() || undefined, actions: [] }, {});
      if (!acts.length) { this.ui.toast(tr('Nothing was recorded'), 'error'); return; }
      const file = await this.backend.testsSaveWebBlock(wr.name.trim(), (wr.description ?? '').trim(), acts);
      await this.loadBlocks(); await this.loadProcs();
      const b = file.replace(/^procs\//, '').replace(/\.yaml$/, '');
      if (this.webProcInto) this.insertProc(this.webProcInto, b);
      else { this.section.set('procs'); this.proc.set(null); this.procSel.set(file); void this.openFile(file); }
      this.ui.toast(tr('Saved the procedure {0} ({1} actions) - replace typed values with {{names}} to make them its values', b, acts.length), 'success');
    } catch (e) { this.ui.toast(errText(e), 'error'); }
    finally { off(); this.webRec.set(false); this.webProcInto = null; }
  }
  async saveBlock(w: TestWeb) {
    const acts = w.actions ?? [];
    const sel = this.selW === w && this.selActs().size ? [...this.selActs()].sort((a, b) => a - b) : acts.map((_, k) => k);
    const name = await this.ui.input(tr('Save as Procedure'), tr('A name for these {0} actions, e.g. open-cp-gvi or add-contact. Other web steps use them with "Use procedure"; {{names}} in them become its values.', sel.length), '');
    if (!name?.trim()) return;
    try {
      const file = await this.backend.testsSaveWebBlock(name.trim(), '', sel.map(k => acts[k]));
      const b = file.replace(/^procs\//, '').replace(/\.yaml$/, '');
      await this.loadBlocks();
      const params = this.blockParams(b), withv: Record<string, string> = {};
      for (const p of params) withv[p] = '{{' + p + '}}';
      const first = sel[0];
      for (const k of [...sel].reverse()) acts.splice(k, 1);
      acts.splice(first, 0, { do: 'block', target: b, with: params.length ? withv : undefined });
      this.selActs.set(new Set()); this.touch();
      this.ui.toast(tr('Saved as block {0}', b), 'success');
    } catch (e) { this.ui.toast(errText(e), 'error'); }
  }
  // ---- healing: targets a run found through a fallback ----
  healedOf(p: Phase, i: number, w: TestWeb): { n: number; from: string; to: string }[] {
    const r = this.resOf(p, i), out: { n: number; from: string; to: string }[] = [];
    for (const m of (r?.output ?? '').matchAll(/^healed (\d+): (.*) → (.*)$/gm)) {
      const n = +m[1]; if (w.actions?.[n - 1]?.target === m[2]) out.push({ n, from: m[2], to: m[3] });
    }
    return out;
  }
  useHealed(w: TestWeb, hl: { n: number; from: string; to: string }[]) {
    for (const h of hl) { const a = w.actions![h.n - 1]; a.alt = [h.from, ...(a.alt ?? []).filter(x => x !== h.to && x !== h.from)].slice(0, 3); a.target = h.to; }
    this.touch(); this.ui.toast(tr('The new targets are in the step - save the test'), 'success');
  }
  // ---- web apps ----
  private sessionNode(path?: string): SessionNode | undefined {
    let found: SessionNode | undefined; const w = (l: SessionNode[]) => { for (const n of l) { if (found) return; if (n.isFolder) w(n.children ?? []); else if (n.path === path) found = n; } };
    w(this.ui.tree()?.children ?? []); return found;
  }
  /** A web action in plain words ("Type {{serial}} into “Serial number”"). */
  actWords(a: TestWebAction): string {
    const nm = (t?: string) => { if (!t) return 'the page'; const m = /name=("(?:[^"\\]|\\.)*")/.exec(t); if (m) { try { return JSON.parse(m[1]); } catch { /* as it is */ } }
      const p = /^(?:label|text|placeholder|testid|title|alt)=(.*)$/.exec(t); return p ? p[1] : t.length > 34 ? t.slice(0, 32) + '…' : t; };
    const q = (x: string) => '“' + x + '”', n = q(nm(a.target)), v = a.value ?? '';
    switch (a.do) {
      case 'click': return 'Click ' + n; case 'dblclick': return 'Double-click ' + n; case 'rightclick': return 'Right-click ' + n;
      case 'fill': case 'type': return 'Type ' + (a.secret || v === '{{password}}' ? 'the password' : q(v)) + ' into ' + n; case 'clear': return 'Clear ' + n;
      case 'select': return 'Choose ' + q(v) + ' in ' + n; case 'check': return 'Tick ' + n; case 'uncheck': return 'Untick ' + n; case 'hover': return 'Point at ' + n;
      case 'press': return 'Press ' + (v || 'Enter') + (a.target ? ' in ' + n : ''); case 'goto': return 'Go to ' + (v || a.target || '');
      case 'wait': return 'Wait ' + (/^\d+$/.test(v) ? v + ' s' : 'for ' + q(v)); case 'upload': return 'Upload ' + v + ' to ' + n; case 'download': return 'Download from ' + n;
      case 'read': return 'Read ' + n + ' into {{' + v + '}}'; case 'screenshot': return 'Take a picture'; case 'tab': return 'Switch to tab ' + v;
      case 'back': return 'Go back'; case 'reload': return 'Reload the page'; case 'dialog': return (v === 'dismiss' ? 'Dismiss' : 'Accept') + ' dialogs';
      case 'block': return 'Do “' + (a.target ?? '') + '”';
      case 'expect': return a.target ? (v ? n + ' shows ' + q(v) : n + ' is shown') : 'the page shows ' + q(v);
      case 'expect_not': return (a.target ? n : q(v)) + ' is not shown'; case 'expect_value': return n + ' is ' + q(v);
      case 'expect_count': return v + ' × ' + n; case 'expect_url': return 'the address has ' + q(v); case 'expect_title': return 'the title has ' + q(v);
      case 'expect_checked': return n + (v === 'false' ? ' is not ticked' : ' is ticked'); case 'expect_enabled': return n + (v === 'false' ? ' is disabled' : ' is enabled');
    }
    return a.do + ' ' + n;
  }
  /** A web step's actions in words, its checks apart. */
  webFlow(w: TestWeb): { words: string[]; checks: string[] } {
    const words: string[] = [], checks: string[] = [];
    for (const a of w.actions ?? []) (a.do.startsWith('expect') ? checks : words).push(this.actWords(a));
    return { words, checks };
  }
  isWebApp(path?: string) { return !!path && (this.sessionNode(path)?.protocol === 'Web' || path.includes('{{')); }
  webApps() { const out: string[] = []; const w = (l: SessionNode[]) => { for (const n of l) { if (n.isFolder) w(n.children ?? []); else if (n.protocol === 'Web') out.push(n.path); } }; w(this.ui.tree()?.children ?? []); return out; }
  webSessions() { const apps = this.webApps(); return [...apps, ...this.allSessions().filter(p => !apps.includes(p))]; }
  async webRecord(w: TestWeb) {
    if (!w.url && !this.isWebApp(w.login)) { this.ui.toast(tr('Type the page address first'), 'error'); return; }
    this.webRec.set(true);
    const acts = (w.actions ??= []);
    if (acts.length === 1 && acts[0].do === 'expect' && !acts[0].value && !acts[0].target) acts.pop();
    const off = this.backend.on('web:rec', (a: TestWebAction) => { if (recInto(acts, a)) this.touch(); });
    const role = /^\{\{\s*([^{}]+?)\s*\}\}$/.exec(w.login ?? ''), login = role ? this.draft()?.vars?.[role[1]] ?? w.login : w.login; // the web app's role: its session
    try { await this.backend.webRecord({ ...w, login, actions: [] }, this.draft()?.vars ?? {}); this.ui.toast(tr('Recording added'), 'success'); }
    catch (e) { this.ui.toast(errText(e), 'error'); }
    finally { off(); this.webRec.set(false); }
  }
  readonly webSt = signal<WebStatus | null>(null);
  readonly webBusy = signal(false);
  readonly webLine = signal('');
  private webOff?: () => void;
  webValHint(d: string) { return ({ fill: 'text, {{var}} or {{password}}', type: 'text', press: 'Enter', select: 'option text', goto: 'https://…', wait: 'seconds or address part', screenshot: 'file name', expect: 'text', expect_not: 'text', expect_value: 'value', expect_count: '3', expect_url: '/dashboard', expect_title: 'title', read: 'variable name', upload: 'files/fw.bin', download: 'file name (optional)', dialog: 'accept', tab: 'last' } as Record<string, string>)[d] ?? ''; }
  isSecretTarget(t?: string) { return /pass|secret|token/i.test(t ?? ''); }
  async loadWebSt() { try { this.webSt.set(await this.backend.webStatus()); } catch { /* not in this build */ } }
  async webSetup() {
    this.webBusy.set(true); this.webLine.set(tr('Starting…'));
    if (!this.webOff) { this.webOff = this.backend.on('web:setup', (l: string) => this.webLine.set(l)); this.offs.push(this.webOff); }
    try { await this.backend.webSetup(); this.ui.toast(tr('Playwright is ready'), 'success'); }
    catch (e) { this.ui.toast(errText(e), 'error'); }
    finally { this.webBusy.set(false); await this.loadWebSt(); }
  }
  splitPorts(v: string) { return v.split(/[,\s]+/).map(x => x.trim()).filter(Boolean); }
  addTGen(p: Phase) { const d = this.draft(); if (!d) return; this.listOf(d, p).push({ tgen: { kind: 'stc', server: '', duration: 30 }, expect: [{ json: 'loss', less: 0.01 }] }); this.touch(); }
  apiSessions() { const out: string[] = []; const w = (l: SessionNode[]) => { for (const n of l) { if (n.isFolder) w(n.children ?? []); else if ((n.protocol ?? '').toUpperCase() === 'API') out.push(n.path); } }; w(this.ui.tree()?.children ?? []); return out; }
  addWeb(p: Phase) { const d = this.draft(); if (!d) return; this.listOf(d, p).push({ web: { url: '', actions: [{ do: 'expect', value: '' }] } }); this.touch(); void this.loadWebSt(); }
  addNetconf(p: Phase) { const d = this.draft(); if (!d) return; this.listOf(d, p).push({ netconf: { session: this.lastSession(), op: 'get-config' } }); this.touch(); }
  setCallProc(s: TestStep, name: string) { if (!s.call) return; s.call.proc = name; const keep: Record<string, string> = {}; for (const p of this.procParams(name)) keep[p.k] = s.call.with?.[p.k] ?? ''; s.call.with = keep; this.touch(); }
  setCallArg(s: TestStep, k: string, v: string) { if (!s.call) return; s.call.with = { ...(s.call.with ?? {}), [k]: v }; this.touch(); }
  readonly varHint = 'value or {{var}}';
  procParams(name: string) { return (this.procs().find(p => p.name === name)?.params ?? []).map(x => ({ k: x.split('=')[0].trim(), d: x.includes('=') ? x.split('=').slice(1).join('=').trim() : '' })); }
  procParamsText(name: string) { const p = this.procs().find(x => x.name === name); return p ? `${p.steps} steps${p.description ? ' - ' + p.description : ''}` : ''; }
  async pickData(d: TestCase) { try { const rel = await this.backend.testPickData(); if (rel) { d.data = rel; this.touch(); } } catch (e) { this.ui.toast(errText(e), 'error'); } }
  // ---- procedures ----
  readonly procs = signal<TestProcInfo[]>([]);
  readonly procSel = signal('');
  readonly proc = signal<TestProc | null>(null);
  async loadProcs() { this.procs.set((await this.backend.testProcs().catch(() => [])) ?? []); }
  async openProc(file: string) {
    this.procSel.set(file); this.fileSel.set('');
    const name = this.procs().find(p => p.file === file)?.name ?? file;
    try { this.proc.set(await this.backend.testProc(file.replace(/^procs\//, '').replace(/\.yaml$/, ''))); } catch (e) { this.proc.set(null); this.ui.toast(errText(e), 'error'); void name; }
  }
  editProcYaml() { const f = this.procSel(); this.proc.set(null); void this.openFile(f); }
  callSnippet(p: TestProc) { return `- call:\n    proc: ${p.name}\n    with:\n` + (p.params ?? []).map(x => `      ${x.split('=')[0].trim()}: "${x.includes('=') ? x.split('=')[1].trim() : ''}"`).join('\n'); }
  procUsers(name: string) { return (this.tree()?.cases ?? []).filter(c => (c as TestCaseInfo & { calls?: string[] }).calls?.includes(name)); }
  async newProc() {
    const name = await this.ui.input(tr('New Procedure'), tr('Name, e.g. create-vlan'), ''); if (!name) return;
    try {
      const rel = await this.backend.testSaveProc(name, { name, description: '', params: ['switch', 'vlan'], steps: [{ run: { session: '{{switch}}', command: 'create vlan v{{vlan}} tag {{vlan}}' } }, { run: { session: '{{switch}}', command: 'show vlan v{{vlan}}' }, expect: [{ contains: 'v{{vlan}}' }] }] });
      await this.loadProcs(); this.section.set('procs'); this.proc.set(null); this.procSel.set(rel); void this.openFile(rel);
      this.ui.toast(tr('An example is in the file - change the steps and parameters, then Save'), 'info');
    } catch (e) { this.ui.toast(errText(e), 'error'); }
  }
  async procFromTest() {
    const t = this.test(); if (!t) return;
    const name = await this.ui.input(tr('Make a Procedure'), tr('Its steps become a procedure; the variables they use ({{x}}) become its parameters. Name:'), (t.name || 'procedure').toLowerCase().replace(/[^a-z0-9]+/g, '-'));
    if (!name) return;
    const steps = structuredClone(t.steps), used = new Set<string>();
    JSON.stringify(steps).replace(/\{\{\s*([\w.-]+)\s*\}\}/g, (_, v) => { used.add(v); return ''; });
    const params = [...used].map(v => t.vars?.[v] !== undefined ? `${v}=${t.vars[v]}` : v);
    try { const rel = await this.backend.testSaveProc(name, { name, description: t.description ?? '', params, steps }); await this.loadProcs(); this.section.set('procs'); await this.openProc(rel); this.ui.toast(tr('Procedure {0} made - call it from other tests with + More → Call a Procedure', name), 'success'); }
    catch (e) { this.ui.toast(errText(e), 'error'); }
  }
  // ---- table maker: a table (or fields) from output by clicking; saved as a TextFSM template ----
  readonly tm = signal<{ phase: Phase; i: number; session: string; command: string; lines: string[]; kind: 'table' | 'fields'; header: number; cols: { name: string; spaces: boolean }[]; fields: { line: number; name: string; label: string }[]; preview: TestTable | null; err: string; name: string } | null>(null);
  private tmTimer: ReturnType<typeof setTimeout> | undefined;
  openTable(p: Phase, i: number, s: TestStep, out: string) {
    const lines = out.replace(/\r/g, '').split('\n').slice(0, 400);
    const sess = s.run?.session ?? '', cmd = s.run?.command ?? '';
    const name = (this.shortDev(sess) + ' ' + this.fill(cmd)).toLowerCase().replace(/\{\{|\}\}/g, '').replace(/[^a-z0-9]+/g, '_').replace(/^_|_$/g, '').slice(0, 60);
    // the header: a line of three or more columns without "label:" whose next row (after a ---- line) has as many
    const segs = (l: string) => l.trim() ? l.trim().split(/\s{2,}/).length : 0, sepLine = (l: string) => /^[-=+|\s]{4,}$/.test(l);
    let header = -1;
    for (let k = 0; k < lines.length - 1 && header < 0; k++) {
      const n = segs(lines[k]);
      if (n < 3 || lines[k].includes(':') || sepLine(lines[k])) continue;
      let j = k + 1; while (j < lines.length && sepLine(lines[j])) j++;
      if (j < lines.length && segs(lines[j]) >= n - 1) header = k;
    }
    const labelled = lines.filter(l => /^\s*[A-Za-z][\w ./()-]{1,40}?:\s*\S/.test(l)).length;
    const kind: 'table' | 'fields' = header < 0 && labelled >= 1 ? 'fields' : 'table';
    this.tm.set({ phase: p, i, session: this.fill(sess), command: this.fill(cmd), lines, kind, header: -1, cols: [], fields: [], preview: null, err: '', name });
    if (kind === 'table' && header >= 0) this.pickHeader(header); else { this.detectFields(); this.previewTable(); }
  }
  private fill(s: string) { const v = this.test()?.vars ?? {}; return s.replace(/\{\{\s*([\w.-]+)\s*\}\}/g, (m, k) => v[k] ?? m); }
  pickHeader(li: number) {
    const m = this.tm(); if (!m) return;
    const segs = (m.lines[li].match(/\S+(?: \S+)*/g) ?? []);
    this.tm.set({ ...m, header: li, cols: segs.map(w => ({ name: w.toUpperCase().replace(/[^A-Z0-9]+/g, '_').replace(/^_|_$/g, '') || 'COL', spaces: w.includes(' ') })) });
    this.previewTable();
  }
  isField(l: string) { return /^\s*[A-Za-z][\w ./()-]{1,40}?(:\s*|\s{2,})\S/.test(l) && !/^[-=\s]+$/.test(l); }
  fieldOn(li: number) { return !!this.tm()?.fields.some(f => f.line === li); }
  detectFields() {
    const m = this.tm(); if (!m || m.fields.length) return;
    const fields: { line: number; name: string; label: string }[] = [];
    m.lines.forEach((l, li) => { if (fields.length < 8 && /^\s*[A-Za-z][\w ./()-]{1,40}?:\s*\S/.test(l)) { const lab = l.split(':')[0].trim(); fields.push({ line: li, name: lab.toUpperCase().replace(/[^A-Z0-9]+/g, '_').replace(/^_|_$/g, ''), label: lab }); } });
    m.fields = fields;
  }
  toggleField(li: number) {
    const m = this.tm(); if (!m || !this.isField(m.lines[li])) return;
    if (this.fieldOn(li)) m.fields = m.fields.filter(f => f.line !== li);
    else { const lab = m.lines[li].split(/:|\s{2,}/)[0].trim(); m.fields = [...m.fields, { line: li, name: lab.toUpperCase().replace(/[^A-Z0-9]+/g, '_').replace(/^_|_$/g, ''), label: lab }].sort((a, b) => a.line - b.line); }
    this.tm.set({ ...m }); this.previewTable();
  }
  private spec(): TemplateSpec | null {
    const m = this.tm(); if (!m) return null;
    return m.kind === 'table' ? { kind: 'table', header: m.header >= 0 ? m.lines[m.header] : '', columns: m.cols.map(c => ({ name: c.name, spaces: c.spaces })) } : { kind: 'fields', fields: m.fields.map(f => ({ name: f.name, label: f.label })) };
  }
  previewTable() {
    clearTimeout(this.tmTimer);
    this.tmTimer = setTimeout(async () => {
      const m = this.tm(), sp = this.spec(); if (!m || !sp) return;
      try { const t = await this.backend.testPreviewTemplate(sp, m.lines.join('\n')); this.tm.set({ ...m, preview: t, err: t?.rows?.length ? '' : 'No rows match yet - check the header line and the columns with spaces.' }); }
      catch (e) { this.tm.set({ ...m, preview: null, err: errText(e) }); }
    }, 250);
  }
  async saveTable() {
    const m = this.tm(), sp = this.spec(), t = this.test(); if (!m || !sp || !t) return;
    try {
      const file = await this.backend.testMakeTemplate(m.name, m.session, m.command, sp);
      const c = structuredClone(t), list = m.phase === 'setup' ? c.setup : m.phase === 'cleanup' ? c.cleanup : c.steps, st = list?.[m.i];
      if (st?.run) { st.run.parse = file; await this.backend.testSave(this.sel(), c); }
      this.tm.set(null); await this.load();
      this.ui.toast(tr('Table saved ({0}). Add checks on its values: Edit → + Check → table value, e.g. NAME=v200: VID = 200', file), 'success');
    } catch (e) { this.ui.toast(errText(e), 'error'); }
  }
  addManual(p: Phase) { const d = this.draft(); if (!d) return; this.listOf(d, p).push({ manual: { instruction: '' } }); this.touch(); }
  readonly manual = signal<{ id: string; name: string; instruction: string; expected: string } | null>(null);
  manualNote = '';
  async answer(r: 'passed' | 'failed' | 'skipped') {
    const m = this.manual(); if (!m) return;
    this.manual.set(null);
    try { await this.backend.testManualAnswer(m.id, r, this.manualNote); } catch (e) { this.ui.toast(errText(e), 'error'); }
    this.manualNote = '';
  }
  readonly liveStep = signal<{ case: string; step: TestStepResult } | null>(null);
  readonly liveCount = signal(0);
  async stopRun() { await this.backend.testStop().catch(() => {}); this.ui.toast(tr('Stopping after the step that runs now…'), 'info'); }
  readonly showLive = signal(true);
  readonly liveLog = signal<LiveItem[]>([]);
  private livePart = '';
  private pushLive(it: LiveItem) {
    this.liveLog.update(l => { const n = [...l, it]; return n.length > 3000 ? n.slice(-3000) : n; });
    this.scrollLive();
  }
  private scrollLive() { queueMicrotask(() => { const el = document.querySelector('app-tests-view .console .con'); if (el) el.scrollTop = el.scrollHeight; }); }
  private addLive(text: string) {
    const t = text.replace(/\r/g, '');
    if (!t) return;
    this.liveLog.update(l => {
      const last = l[l.length - 1];
      if (last?.k === 'out') return [...l.slice(0, -1), { k: 'out', t: ((last.t ?? '') + t).slice(-200000) }];
      return [...l, { k: 'out', t: t.replace(/^\n+/, '') }];
    });
    this.scrollLive();
  }
  logCls(ln: string) {
    if (/^\s*✓|\[PASSED|^Result:\s+PASSED/.test(ln)) return 'lg-ok';
    if (/^\s*✗|\[FAILED|^Result:\s+FAILED|^Error:/.test(ln)) return 'lg-bad';
    if (/^===|^Checks:|^\s+Result\s+Expected/.test(ln)) return 'lg-hd';
    return '';
  }
  found(c: { passed: boolean; got?: string }) { return c.got ? c.got : c.passed ? 'found' : 'not in the output'; }
  private liveText() {
    return this.liveLog().map(it => it.k === 'head' ? `\n▶ ${it.c} · ${it.t}` : it.k === 'out' ? (it.t ?? '').replace(/\n$/, '')
      : [...(it.s!.checks ?? []).map(c => `   ${c.passed ? '✓' : '✗'} ${c.what}  →  ${this.found(c)}`), `   ${it.s!.skipped ? '– skipped' : it.s!.passed ? '✓ passed' : '✗ failed'} (${this.dur(it.s!.ms)})${it.s!.error ? ' ' + it.s!.error : ''}`].join('\n')).join('\n');
  }
  copyLive() { this.copyText(this.liveText()); }
  copyText(t: string) { void navigator.clipboard?.writeText(t).then(() => this.ui.toast(tr('Copied'), 'success')).catch(() => {}); }
  readonly logView = signal<{ title: string; file: string; text: string } | null>(null);
  async openLog(file: string, title: string) {
    try { this.logView.set({ title, file, text: await this.backend.testFile(file) }); } catch (e) { this.ui.toast(errText(e), 'error'); }
  }
  async importManual() {
    const dev = await this.ui.input(tr('Import Test Cases'), tr('Excel or CSV: the session for steps without a Device column (empty: such steps become manual steps); a row with Type web is a web action on the web app in Device (click text=Log In, fill label=Email = {{email}}, expect Networks). Postman or OpenAPI: the API session the requests go to (empty: full URLs). Then pick the file.'), '');
    if (dev === null || dev === undefined) return;
    try {
      const l = await this.backend.testImportManual(dev.trim());
      if (l?.length) { this.ui.toast(tr('Imported {0} tests', l.length), 'success'); await this.load(); }
    } catch (e) { this.ui.toast(errText(e), 'error'); }
  }
  addUpgrade(p: Phase) {
    const d = this.draft(); if (!d) return;
    const all = [...(d.setup ?? []), ...d.steps, ...(d.cleanup ?? [])], ses = [...all].reverse().find(s => s.run)?.run?.session ?? '';
    this.listOf(d, p).push({ upgrade: { session: ses, image: '' } }); this.touch();
  }
  addTraffic(p: Phase) { const d = this.draft(); if (!d) return; this.listOf(d, p).push({ traffic: { test: '' }, expect: [{ json: 'rows[0].loss', less: 0.01 }] }); this.touch(); }
  setWatch(d: TestCase, k: 'syslog' | 'traps', v: string) {
    const l = v.split(/[,\n]/).map(x => x.trim()).filter(Boolean);
    const w = { ...(d.watch ?? {}) }; if (l.length) w[k] = l; else delete w[k];
    d.watch = w.syslog?.length || w.traps?.length ? w : undefined;
  }
  watchText(t: TestCase) { return [...(t.watch?.syslog ?? []).map(x => 'syslog ' + x), ...(t.watch?.traps ?? []).map(x => 'trap ' + x)].join(', '); }
  addWait(p: Phase) { const d = this.draft(); if (!d) return; this.listOf(d, p).push({ wait: 5 }); this.touch(); }
  addLoop(p: Phase) {
    const d = this.draft(); if (!d) return;
    const all = [...(d.setup ?? []), ...d.steps, ...(d.cleanup ?? [])], ses = [...all].reverse().find(s => s.run)?.run?.session ?? '';
    this.listOf(d, p).push({ loop: { var: 'port', range: '1-4', steps: [{ run: { session: ses, command: 'show ports {{port}} information' } }] } });
    this.touch();
  }
  moveStep(p: Phase, i: number, by: number) { const d = this.draft(); if (!d) return; const l = this.listOf(d, p); [l[i], l[i + by]] = [l[i + by], l[i]]; this.touch(); }
  removeStep(p: Phase, i: number) { const d = this.draft(); if (!d) return; this.listOf(d, p).splice(i, 1); this.touch(); }
  removeCheck(s: TestStep, j: number) { s.expect?.splice(j, 1); this.touch(); }
  ck(p: Phase, i: number) { const k = p + i; let c = this.cks.get(k); if (!c) { c = { k: 'contains', v: '' }; this.cks.set(k, c); } return c; }
  checkKinds(s: TestStep) {
    const k = [{ k: 'contains', l: 'contains' }, { k: 'notContains', l: 'does not contain' }, { k: 'matches', l: 'matches' }];
    const agg = s.sample ? [{ k: 'avg', l: 'average of' }, { k: 'max', l: 'maximum of' }, { k: 'min', l: 'minimum of' }, { k: 'delta', l: 'change of' }, { k: 'rate', l: 'rate/s of' }] : [];
    if (s.netconf) return [...agg, { k: 'json', l: 'XML path =' }, ...k];
    if (s.tgen) return [{ k: 'json', l: 'result' }, ...k];
    if (agg.length) return [...agg, ...(s.http ? [{ k: 'status', l: 'status' }, { k: 'json', l: 'JSON path =' }] : s.snmp ? [{ k: 'oid', l: 'OID =' }] : []), ...k];
    return s.http ? [{ k: 'status', l: 'status' }, { k: 'json', l: 'JSON path =' }, ...k, { k: 'maxMs', l: 'within ms' }] : s.snmp ? [{ k: 'oid', l: 'OID =' }, ...k] : [...k, { k: 'table', l: 'table value' }];
  }
  checkHint(k: string) { if (['avg', 'max', 'min', 'delta', 'rate'].includes(k)) return 'CPU:\\s+(\\d+) < 80   (a field pattern, JSON path or OID, then < > or =)'; return k === 'json' ? 'data.items[0].name = v42   or   loss < 0.01' : k === 'oid' ? 'ifOperStatus.4 = up' : k === 'status' ? '200' : k === 'maxMs' ? '500' : k === 'matches' ? 'v42\\s+42' : k === 'table' ? 'VLAN_ID=200: STATUS = active' : 'text'; }
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
    else if (['avg', 'max', 'min', 'delta', 'rate'].includes(c.k)) {
      const m = v.match(/^(.+?)\s*(<|>|=)\s*(-?[\d.]+)$/);
      if (!m) return this.ui.toast(tr('Write the value, then < > or = and a number, e.g. CPU:\\s+(\\d+) < 80'), 'error');
      const subj = m[1].trim(), n = Number(m[3]);
      e = { agg: c.k as TestExpect['agg'] };
      if (s.snmp) e.oid = subj; else if (s.http || (s.netconf && !subj.includes('('))) e.json = subj; else e.field = subj.includes('(') ? subj : escapeRe(subj) + '\\s*:?\\s*([\\d.]+)';
      if (m[2] === '<') e.less = n; else if (m[2] === '>') e.greater = n; else e.equals = String(n);
    }
    else if (c.k === 'json' || c.k === 'oid') {
      const m = v.match(/^(.+?)\s*(<|>)\s*(-?[\d.]+)$/);
      if (m) e = { [c.k]: m[1].trim(), ...(m[2] === '<' ? { less: Number(m[3]) } : { greater: Number(m[3]) }) };
      else { const i = v.indexOf('='); e = i > 0 ? { [c.k]: v.slice(0, i).trim(), equals: v.slice(i + 1).trim() } : { [c.k]: v, exists: true }; }
    }
    else e = { [c.k]: v };
    (s.expect ??= []).push(e); c.v = ''; this.touch();
  }
  async saveDraft() {
    const d = this.draft(); if (!d) return;
    const all = [...(d.setup ?? []), ...d.steps, ...(d.cleanup ?? [])];
    if (all.some(s => s.run && (!s.run.session.trim() || !s.run.command.trim()))) return this.ui.toast(tr('Every command step needs a session and a command.'), 'error');
    for (const s of all) { if (s.run && !s.run.parse) delete s.run.parse; if (!s.wait) delete s.wait; if (!s.if?.trim()) delete s.if; if (!s.retry) delete s.retry; if (!s.timeout) delete s.timeout; if (!s.continueOnFail) delete s.continueOnFail; if (!s.ignoreErrors) delete s.ignoreErrors; if (!s.expect?.length) delete s.expect; }
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

/** A curl command as a REST step: -X, -H (no Authorization/Cookie: those belong in an API session), -d / --data*. */
export function parseCurl(cmd: string): NonNullable<TestStep['http']> | null {
  const toks: string[] = [];
  const re = /'((?:[^'\\]|\\.)*)'|"((?:[^"\\]|\\.)*)"|(\S+)/g;
  let m: RegExpExecArray | null;
  const flat = cmd.replace(/\\\r?\n/g, ' ');
  while ((m = re.exec(flat))) toks.push(m[1] ?? m[2] ?? m[3]);
  if (!toks.length || !/curl$/.test(toks[0])) return null;
  let method = '', url = '', body = '';
  const headers: Record<string, string> = {};
  for (let i = 1; i < toks.length; i++) {
    const t = toks[i];
    if (t === '-X' || t === '--request') method = (toks[++i] ?? '').toUpperCase();
    else if (t === '-H' || t === '--header') { const [k, ...v] = (toks[++i] ?? '').split(':'); if (k && !/^(authorization|cookie|x-auth-token|x-api-key)$/i.test(k.trim())) headers[k.trim()] = v.join(':').trim(); }
    else if (/^(-d|--data|--data-raw|--data-binary|--json)$/.test(t)) body = toks[++i] ?? '';
    else if (/^-(u|-user)$/.test(t)) i++;
    else if (/^https?:\/\//.test(t)) url = t;
    else if (t.startsWith('-')) { if (/^-(o|-output|w|-write-out|m|-max-time|-connect-timeout)$/.test(t)) i++; }
  }
  if (!url) return null;
  if (!method) method = body ? 'POST' : 'GET';
  const h: NonNullable<TestStep['http']> = { method, url };
  if (Object.keys(headers).length) h.headers = headers;
  if (body) h.body = body;
  return h;
}

/** The lines removed (- …) and added (+ …) between two outputs, spacing ignored, at most 200. */
export function lineDiff(a: string, b: string): string[] {
  const norm = (s: string) => s.split('\n').map(l => l.replace(/\s+/g, ' ').trim()).filter(Boolean);
  const x = norm(a), y = norm(b), cy = new Map<string, number>(), cx = new Map<string, number>(), out: string[] = [];
  for (const l of y) cy.set(l, (cy.get(l) ?? 0) + 1);
  for (const l of x) { const n = cy.get(l) ?? 0; if (n) cy.set(l, n - 1); else out.push('- ' + l); }
  for (const l of x) cx.set(l, (cx.get(l) ?? 0) + 1);
  for (const l of y) { const n = cx.get(l) ?? 0; if (n) cx.set(l, n - 1); else out.push('+ ' + l); }
  return out.slice(0, 200);
}
