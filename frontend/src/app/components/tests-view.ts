import { Component, OnDestroy, OnInit, computed, effect, inject, input, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { NgTemplateOutlet } from '@angular/common';
import { Backend, errText } from '../core/backend';
import { Ui } from '../core/ui.service';
import { tr } from '../core/i18n';
import { Recorder, escapeRe, fieldPattern } from '../core/recorder.service';
import { TestApproval, CleanupMode, OnCleanupFail, TestCase, TestCaseInfo, TestExpect, TestHistory, TestStepResult, TestResult, TestRunReport, TestSchedule, TestScheduleView, TestStep, TestsGitInfo, TestsTree } from '../core/models';
import { expectLabel, parseTableCheck, stepText } from './recorder-panel';
import { AIChat } from '../core/ai-chat.service';
import { Icon } from './icon';
import { highlight, Lang } from '../core/highlight';

type View = 'steps' | 'yaml' | 'go' | 'py' | 'tcl' | 'history';
type Phase = 'setup' | 'steps' | 'cleanup';
type Section = 'tests' | 'runs' | 'schedules' | 'testbeds' | 'library';

/** The Tests tab: the tests folder's tree, the selected test (steps, YAML, generated Go) and the last run. */
type LiveItem = { k: 'head' | 'out' | 'res'; t?: string; c?: string; s?: TestStepResult };

@Component({
  selector: 'app-tests-view',
  imports: [FormsModule, NgTemplateOutlet, Icon],
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
              <div class="row"><button type="button" class="recb" (click)="recordMore()" title="Record what you do in terminal tabs, the API Client and the SNMP Browser as a test"><i class="rdot"></i>Record a Test</button><span class="sp"></span><button type="button" (click)="moreMenu($event)" title="Open, clone, write with AI, folder">⋯</button></div>
              <input class="search" type="search" placeholder="Search tests" [ngModel]="q()" (ngModelChange)="q.set($event)" aria-label="Search tests" />
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
                <div class="fold"><button type="button" class="lnk fl" (click)="toggleFold(g.dir)">{{ folded().includes(g.dir) ? '▸' : '▾' }} {{ g.dir || 'No suite' }}</button><span class="cnt">{{ g.cases.length }}</span>
                  <button type="button" class="lnk" (click)="openRun(filesOf(g), g.dir || 'cases')" [title]="'Run the tests in ' + (g.dir || 'cases')">▶</button></div>
                @if (!folded().includes(g.dir)) {
                  @for (c of g.cases; track c.file) {
                    <div class="case" [class.sel]="c.file === sel() && section() === 'tests'" (click)="select(c.file)" (keydown.enter)="select(c.file)" tabindex="0" role="button" [title]="c.error || c.description || c.file">
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
                    <button type="button" class="recb" (click)="recordMore()"><i class="rdot"></i>Record a Test</button>
                    <button type="button" (click)="importManual()">Import Test Cases…</button>
                    <button type="button" (click)="writeWithAI()">✦ Write a Test with AI</button>
                    <span class="mut">Import reads your test case sheet (Excel or CSV): Suite, Test, Step, Device, Expected.</span>
                  </div>
                } @else { <div class="empty-s">No tests match.</div> }
              }
            </div>
            @if (picked().length) {
              <div class="selbar"><b>{{ picked().length }} selected</b><span class="sp"></span><button type="button" class="lnk" (click)="picked.set([])">Clear</button><button type="button" class="lnk" (click)="saveSuite()" title="Keep the selected tests as a plan (suites/…) to run them together again">Save as plan…</button><button type="button" class="go" (click)="openRun(picked(), (picked().length === 1 ? '1 test' : picked().length + ' tests'))"><app-icon name="play" [size]="12" /> Run {{ picked().length === 1 ? 'Test' : picked().length + ' Tests' }}</button></div>
            } @else if (filtering()) {
              <div class="selbar"><span class="mut">{{ filtered().length }} of {{ tree()?.cases?.length ?? 0 }} shown</span><span class="sp"></span><button type="button" (click)="openRun(shownFiles(), filtered().length + ' shown tests')" [disabled]="!filtered().length"><app-icon name="play" [size]="13" /> Run shown…</button></div>
            } @else {
              <div class="selbar"><span class="mut">{{ tree()?.cases?.length ?? 0 }} tests</span><span class="sp"></span><button type="button" (click)="openRun([], 'all tests')" [disabled]="!tree()?.cases?.length"><app-icon name="play" [size]="13" /> Run all…</button></div>
            }
          }
          @case ('runs') {
            <div class="stop"><b>Runs</b><span class="mut">past runs of this tests folder, newest first</span></div>
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
            <div class="stop"><div class="row"><b class="grow">Testbeds</b><button type="button" (click)="newTestbed()" title="A role for each session of a session folder">+ New</button></div><span class="mut">which devices a run uses</span></div>
            <div class="list">
              @for (b of tree()?.testbeds ?? []; track b) { <div class="case" [class.sel]="fileSel() === b" (click)="openFile(b)" (keydown.enter)="openFile(b)" tabindex="0" role="button"><span class="cn">{{ base(b) }}</span>@if (lockOf(b); as l) { <span class="n" [title]="'Locked by ' + l.user + ' on ' + l.host + ' since ' + l.since.slice(11, 16)">🔒 {{ l.mine ? 'running' : l.user }}</span> }</div> } @empty { <div class="empty-s">No testbeds.</div> }
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
                  }
                }
              }
            </div>
            @if (!conMax()) { <div class="cgrip" (mousedown)="dragCon($event)" (dblclick)="conH.set(240); keepCon()" title="Drag to change the height (double-click: default)" role="separator" aria-orientation="horizontal"></div> }
          </div>
        }
        @switch (section()) {
          @case ('runs') {
            @if (runRep(); as rep) {
              <div class="head"><div class="crumb">{{ rep.dir }} · {{ rep.testbed ? 'testbed ' + base(rep.testbed) : "the tests' own sessions" }} · {{ rep.cleanup === 'off' ? 'no cleanup' : rep.cleanup === 'auto' ? 'cleanup or undo' : 'cleanup steps' }} · {{ dur(rep.ms) }}@if (verText(rep.versions)) { · {{ verText(rep.versions) }} }@if (rep.runBy) { · run by {{ rep.runBy }} }</div>
                <div class="title"><h1>Run · {{ when(rep.dir) }}</h1>
                  <span class="pill" [class]="rep.failed ? 'pill f' : rep.skipped ? 'pill s' : 'pill p'">{{ rep.failed ? rep.failed + ' failed' : rep.skipped ? rep.skipped + ' skipped' : 'all passed' }}</span>
                  <span class="sp"></span>
                  <button type="button" (click)="backend.testsOpen(rep.dir)">HTML report</button><button type="button" (click)="backend.testsOpen(rep.dir + '/junit.xml')">JUnit</button>
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
                      <span>{{ r.case }}@if (r.cleanupFailed) { <span class="pill f sm">cleanup</span> }</span>
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
                <label class="ck"><input type="checkbox" [(ngModel)]="s.production" /> May use production sessions</label>
                <label class="ck"><input type="checkbox" [(ngModel)]="s.off" /> Off</label>
                @if (schedView(); as v) { @if (v.state.last) { <p class="mut">Last run {{ v.state.last.slice(0, 16).replace('T', ' ') }}: {{ v.state.error || (v.state.passed + ' passed, ' + v.state.failed + ' failed') }}@if (v.state.dir) { · <button type="button" class="lnk" (click)="openRunReport(v.state.dir!)">open</button> }</p> } }
                <div class="row"><span class="sp"></span>@if (s.id) { <button type="button" (click)="removeSchedule(s)">Remove</button><button type="button" (click)="runScheduleNow(s)">Run Now</button> }<button type="button" class="pri" (click)="saveSchedule(s)">Save</button></div>
              </div></div>
            } @else { <div class="empty mut">Select a schedule, or + New.</div> }
          }
          @case ('testbeds') { <ng-container *ngTemplateOutlet="fileView" /> }
          @case ('library') { <ng-container *ngTemplateOutlet="fileView" /> }
          @default {
            @if (sel() && test(); as t) {
              <div class="head">
                <div class="crumb">{{ sel() }}</div>
                <div class="title"><h1>{{ t.name || sel() }}</h1>
                  @if (last()[sel()]; as lr) { <span class="pill" [class]="lr.skipped ? 'pill s' : lr.passed ? 'pill p' : 'pill f'">{{ lr.skipped ? 'skipped' : lr.passed ? 'passed' : lr.cleanupFailed ? 'failed (cleanup)' : 'failed' }} · {{ ago(lr.start) }}</span> } @else { <span class="pill">not run yet</span> }
                  @for (g of t.tags ?? []; track g) { <span class="tag">{{ g }}</span> }
                  <span class="sp"></span>
                  @if (last()[sel()]?.log; as lg) { <button type="button" (click)="openLog(lg, t.name || sel())" title="The last run's log: every step, what was sent and what came back">Run Log</button> }
                  @if (!draft()) { <button type="button" (click)="edit()">✎ Edit</button> }
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
                    @if (t.restore?.length) { <span>Afterwards <b>{{ restoreText() }} put back</b></span> }
                    @if (t.qtest?.testCase) { <span>qTest <b>{{ t.qtest?.testCase }}</b></span> }
                    @if (t.dependsOn?.length) { <span>Depends on <b>{{ t.dependsOn.join(', ') }}</b></span> }
                    @if (t.watch?.syslog?.length || t.watch?.traps?.length) { <span>Fails on <b>{{ watchText(t) }}</b></span> }
                    @if (t.configDiff) { <span>Configuration <b>compared before and after</b></span> }
                    <span>After each test <b>{{ cleanupLabel() }}</b></span>
                  </div>
                  @for (r of restoreRes(); track $index) { <div class="trow" [class.bad]="!r.passed"><span>{{ r.passed ? '✓' : '✗' }}</span><span>{{ r.name }}</span><span class="mut">{{ r.target }} {{ r.error }}</span></div> }
                  @if (draft(); as d) {
                    <div class="sec"><div class="sh">While the test runs</div>
                      <div class="ed">
                        <div class="row opts"><label class="wide" title="Words, or /regex/; one per line or comma-separated. The syslog server must be on.">Fail on syslog <input class="cmdi" [ngModel]="(d.watch?.syslog ?? []).join(', ')" (ngModelChange)="setWatch(d, 'syslog', $event)" placeholder="e.g. LINK-3-UPDOWN, /%.*-[0-2]-/" /></label></div>
                        <div class="row opts"><label class="wide" title="Notification names, OIDs or text. The trap receiver must be on.">Fail on SNMP notifications <input class="cmdi" [ngModel]="(d.watch?.traps ?? []).join(', ')" (ngModelChange)="setWatch(d, 'traps', $event)" placeholder="e.g. linkDown, coldStart" /></label></div>
                        <div class="row opts"><label title="Reads each device's configuration (its device type's backup commands) before the test and after the cleanup; a difference fails the test"><input type="checkbox" [(ngModel)]="d.configDiff" /> Compare the configuration before and after (needs the cleanup on)</label>
                          @if (d.configDiff) { <label class="wide">ignore lines <input class="cmdi" [ngModel]="(d.configIgnore ?? []).join(', ')" (ngModelChange)="d.configIgnore = splitTags($event)" placeholder="regular expressions, e.g. ^! Last configuration change" /></label> }</div>
                      </div></div>
                    @for (ph of phases; track ph.k) {
                      <div class="sec">
                        <div class="sh">{{ ph.l }}<span class="sp"></span><button type="button" class="lnk" (click)="addStep(ph.k)">+ Command</button><button type="button" class="lnk" (click)="addMenu($event, ph.k)" title="An API request, a curl command, a manual step, a wait, a loop, an upgrade or a traffic test">+ More ▾</button></div>
                        @for (s of listOf(d, ph.k); track $index; let i = $index; let lastI = $last) {
                          <div class="ed">
                            <div class="row"><b class="num">{{ i + 1 }}</b>
                              @if (s.run) { <input class="ses" [(ngModel)]="s.run.session" placeholder="session" aria-label="Session" /><input class="cmdi" [(ngModel)]="s.run.command" placeholder="command" aria-label="Command" /> }
                              @else if (s.http) { <input class="ses" [(ngModel)]="s.http.session" placeholder="API session (or a full URL)" aria-label="API session" /><select [(ngModel)]="s.http.method" aria-label="Method">@for (m of methods; track m) { <option [value]="m">{{ m }}</option> }</select><input class="cmdi" [(ngModel)]="s.http.url" placeholder="URL or /path" aria-label="URL" /> }
                              @else if (s.manual) { <span class="mut">manual</span><input class="cmdi" [(ngModel)]="s.manual.instruction" placeholder="what the person does or checks" aria-label="Instruction" /> }
                              @else if (s.upgrade) { <span class="mut">upgrade</span><input class="ses" [(ngModel)]="s.upgrade.session" placeholder="session" aria-label="Session" /><input class="cmdi" [(ngModel)]="s.upgrade.image" placeholder="image file or URL" aria-label="Image" /> }
                              @else if (s.traffic) { <span class="mut">traffic test</span><input class="cmdi" [(ngModel)]="s.traffic.test" placeholder="saved lab traffic test" aria-label="Traffic test" /> }
                              @else { <span class="cmd fl">{{ s.loop ? 'loop' : text(s) }}</span><span class="sp"></span> }
                              <button type="button" class="lnk" (click)="moveStep(ph.k, i, -1)" [disabled]="i === 0" title="Move up">↑</button><button type="button" class="lnk" (click)="moveStep(ph.k, i, 1)" [disabled]="lastI" title="Move down">↓</button><button type="button" class="lnk" (click)="removeStep(ph.k, i)" title="Remove the step">✕</button>
                            </div>
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
                            <div class="row opts"><label class="wide" title="What should happen, in words - shown with the step and in the reports">Expected <input class="cmdi" [(ngModel)]="s.expected" placeholder="e.g. VLAN 200 lists port 1:2 tagged" /></label></div>
                            <div class="row opts"><label>Timeout <input type="number" min="0" class="num2" [(ngModel)]="s.timeout" /> s</label>
                              <label><input type="checkbox" [(ngModel)]="s.continueOnFail" /> Go on when it fails</label>
                              <label title="Seconds to wait before the step">Wait <input type="number" min="0" class="num2" [(ngModel)]="s.wait" /> s</label>
                              <label title="Run the step only when this holds, e.g. {{ '{{' }}vlan{{ '}}' }} != 1 (==, !=, contains, ~, >, <)">If <input class="pin" [(ngModel)]="s.if" placeholder="always" /></label>
                              <label title="Run the step again until its checks pass, for at most this many seconds"><input type="checkbox" [ngModel]="!!s.retry" (ngModelChange)="s.retry = $event ? { timeout: 60, interval: 5 } : undefined" /> Retry until it passes</label>
                              @if (s.retry) { <label>for <input type="number" min="1" class="num2" [(ngModel)]="s.retry.timeout" /> s</label> }
                              @if (s.run) { <label title="Do not fail the step when the device answers with an error message"><input type="checkbox" [(ngModel)]="s.ignoreErrors" /> Ignore device errors</label>
                                <label title="Turn the output into a table with a TextFSM template: auto (templates/index) or a file in templates/">Parse <input class="pin" [(ngModel)]="s.run.parse" placeholder="off" list="tw-parse" /></label> }</div>
                            <div class="chips">@for (e of s.expect ?? []; track $index; let j = $index) { <span class="chip">{{ label(e) }} <button type="button" class="x" (click)="removeCheck(s, j)" title="Remove the check">×</button></span> }
                              @if (!s.loop && !s.manual && ck(ph.k, i); as c) {
                                <span class="addck"><select [(ngModel)]="c.k" aria-label="Check">@for (k of checkKinds(s); track k.k) { <option [value]="k.k">{{ k.l }}</option> }</select>
                                  <input [(ngModel)]="c.v" [placeholder]="checkHint(c.k)" aria-label="Value" (keydown.enter)="addCheck(s, c)" /><button type="button" (click)="addCheck(s, c)" [disabled]="!c.v.trim()">+ Check</button></span>
                              }</div>
                          </div>
                        } @empty { <div class="none">no {{ ph.l.toLowerCase() }} steps</div> }
                      </div>
                    }
                    <datalist id="tw-parse"><option value="auto"></option></datalist>
                    <div class="row"><span class="mut">Saving writes the test's YAML and its Go, Python and TCL code again.</span><span class="sp"></span><button type="button" (click)="draft.set(null)">Cancel</button><button type="button" class="pri" (click)="saveDraft()">Save</button></div>
                  } @else {
                    @for (ph of phases; track ph.k) {
                      @if (ph.k === 'steps' || listOf(t, ph.k).length) {
                        <div class="sec">
                          <div class="sh">{{ ph.k === 'cleanup' ? 'Cleanup' : ph.l }}<span class="sp"></span><span class="norm">{{ secSummary(ph.k) }}</span></div>
                          <div class="srow hd"><span>#</span><span>Device</span><span>Command</span><span>Checks</span><span>Result</span><span class="r">Time</span></div>
                          @for (s of listOf(t, ph.k); track $index; let i = $index) {
                            @let r = resOf(ph.k, i);
                            <div class="srow" [attr.aria-expanded]="isOpen(ph.k, i)" [class.f]="r && !r.passed && !r.skipped" [class.open]="isOpen(ph.k, i)" (click)="toggleRow(ph.k, i)" (keydown.enter)="toggleRow(ph.k, i)" tabindex="0" role="button">
                              <span class="mut">{{ i + 1 }}</span><span class="dev" [title]="target(s)">{{ s.loop ? 'loop' : s.manual ? 'you' : shortDev(target(s)) }}</span><span class="cmd" [class.manual]="!!s.manual">{{ text(s) }}@if (logicText(s)) { <span class="logic">{{ logicText(s) }}</span> }@if (s.expected) { <span class="expd">Expected: {{ s.expected }}</span> }</span>
                              <span class="chips">@for (e of s.expect ?? []; track $index; let j = $index) { <span class="chip" [class.b]="r?.checks?.[j]?.passed === false">{{ r?.checks?.[j]?.passed === false ? '✗' : r ? '✓' : '' }} {{ label(e) }}</span> }
                                @if (!(s.expect?.length) && !s.manual) { <span class="chip w" title="Without checks the step passes when the device accepts the command">○ not checked</span> }@if (s.manual) { <span class="chip m">you decide</span> }</span>
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
                                  <h4>Output · select text, right-click to check it</h4>
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
            <div class="head"><div class="crumb">{{ fp }}</div><div class="title"><h1>{{ base(fp) }}</h1>@if (fp.startsWith('reports/')) { <span class="pill">read only</span> }@if (lockOf(fp); as l) { <span class="pill s">🔒 in use by {{ l.user }} on {{ l.host }} since {{ l.since.slice(11, 16) }}</span><button type="button" (click)="releaseLock(fp)" title="Free the testbed when that run is over">Release</button> }<span class="sp"></span><button type="button" (click)="copyText(fileText)" title="Copy to the clipboard"><app-icon name="copy" [size]="13" /> Copy</button><button type="button" (click)="backend.testsOpenInEditor(fp)">Open in VS Code</button>@if (!fp.startsWith('reports/')) { <button type="button" class="pri" (click)="saveFile()">Save</button> }</div></div>
            <div class="body"><textarea class="code" [(ngModel)]="fileText" spellcheck="false" [attr.aria-label]="fp"></textarea>
              <div class="row"><span class="mut">{{ fp.startsWith('lib/') ? 'Termwright writes twtest and run_tests again when it is updated: keep your own helpers in other files.' : fp.startsWith('templates/') ? 'TextFSM templates; the index says which command each one parses (ntc-templates format).' : 'Roles and values: role: Folder/session, name: value.' }}</span></div></div>
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
            <label class="fld"><span>Testbed - which devices</span><select [(ngModel)]="testbed"><option value="">The tests' own sessions</option>@for (b of tree()?.testbeds ?? []; track b) { <option [value]="b">{{ base(b) }}{{ lockText(b) }}</option> }</select>
              @if (testbed) { <small class="mut">{{ lockOf(testbed) && !lockOf(testbed)!.mine ? '🔒 in use by ' + lockOf(testbed)!.user + ' on ' + lockOf(testbed)!.host + ' - Run takes it over after asking' : '🔒 free - locked to you while the run lasts' }}</small> }</label>
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
    .selbar > * { white-space: nowrap; } .selbar { display: flex; align-items: center; gap: 8px; margin: 8px; padding: 8px 10px; border-radius: 9px; border: 1px solid var(--border); background: color-mix(in srgb, var(--text) 4%, transparent); }
    .selbar.col { flex-direction: column; align-items: stretch; } .selbar .full { justify-content: center; width: 100%; } .selbar b { font-weight: 600; font-size: 12.5px; }
    .main { position: relative; background: var(--bg); display: flex; flex-direction: column; min-width: 0; min-height: 0; }
    .head { padding: 12px 20px 0; border-bottom: 1px solid var(--border); } .crumb { color: var(--muted); font-size: 12px; }
    .title { display: flex; align-items: center; gap: 8px; margin: 3px 0 10px; } .title h1 { font-size: 17px; margin: 0; font-weight: 600; letter-spacing: -.01em; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; flex: 0 1 auto; min-width: 120px; } .title button { white-space: nowrap; flex: none; } .title .mut.fl { flex: 1 1 0; }
    .pill { font-size: 11.5px; padding: 2px 9px; border-radius: 10px; white-space: nowrap; background: color-mix(in srgb, var(--text) 8%, transparent); color: var(--muted); }
    .pill.f { background: color-mix(in srgb, var(--danger, #ef6161) 18%, transparent); color: var(--danger, #ef6161); } .pill.p { background: color-mix(in srgb, var(--ok, #3fbf75) 18%, transparent); color: var(--ok, #3fbf75); } .pill.s { background: color-mix(in srgb, var(--warn, #e0a030) 18%, transparent); color: var(--warn, #e0a030); } .pill.sm { font-size: 10.5px; padding: 0 6px; margin-left: 6px; }
    .tabs { display: flex; align-items: center; gap: 18px; } .tabs > button { border: 0 !important; border-radius: 0 !important; box-shadow: none !important; min-height: 0 !important; height: auto !important; outline: none; background: none !important; color: var(--muted); padding: 8px 0; border-bottom: 2px solid transparent; cursor: pointer; } .tabs > button.on { color: var(--text); box-shadow: inset 0 -2px var(--accent) !important; font-weight: 600; }
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
    .logdlg { width: min(1000px, 94vw); height: min(80vh, 900px); } .logpre { flex: 1; margin: 0; overflow: auto; padding: 10px 12px; border-radius: 8px; background: #111317; color: #cfd3da; font: 12px/1.5 var(--mono, monospace); white-space: pre-wrap; }
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
    { k: 'schedules', l: 'Schedules', i: 'clock', t: 'Scheduled runs' }, { k: 'testbeds', l: 'Testbeds', i: 'hosts', t: 'Which devices a run uses' }, { k: 'library', l: 'Library', i: 'file', t: 'Templates and code helpers' }];
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
  readonly phases: { k: Phase; l: string }[] = [{ k: 'setup', l: 'Setup' }, { k: 'steps', l: 'Steps' }, { k: 'cleanup', l: 'Cleanup - runs when the run cleans up' }];
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

  async run(files: string[], confirmed = false, forceLock = false): Promise<void> {
    this.liveStep.set(null); this.liveCount.set(0); this.liveLog.set([]); this.livePart = ''; this.showLive.set(true);
    if (!confirmed && this.cleanupMode !== 'off' && this.onCleanupFail === 'factory' && !(await this.ui.confirm(tr('Factory Default on a Failed Cleanup?'),
      tr('When a cleanup fails, the devices of that test are reset to factory default and rebooted, then the run goes on. A factory default may remove the management address: use console sessions. Production sessions are never reset.'), tr('Run'), true))) return;
    this.busy.set(true);
    try {
      this.took(await this.backend.runTests({ files, testbed: this.testbed || undefined, confirmed, forceLock, onCleanupFail: this.cleanupMode === 'off' ? undefined : this.onCleanupFail, cleanup: this.cleanupMode || undefined }));
      await this.load();
    } catch (e) {
      const t = errText(e);
      if (!confirmed && /production sessions/i.test(t)) {
        this.busy.set(false);
        if (await this.ui.confirm(tr('Run on Production?'), t + '. ' + tr('Their commands and requests are sent to those devices.'), tr('Run'), true)) return this.run(files, true, forceLock);
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
  async openRunReport(dir: string) {
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
  async openFile(f: string) { this.fileSel.set(f); this.fileText = await this.backend.testFile(f).catch(e => '# ' + errText(e)); }
  async saveFile() { try { await this.backend.testSaveFile(this.fileSel(), this.fileText); this.ui.toast(tr('Saved'), 'success'); await this.load(); } catch (e) { this.ui.toast(errText(e), 'error'); } }
  moreMenu(ev: MouseEvent) {
    this.ui.showMenu(ev, [
      { label: 'Write a Test with AI…', action: () => void this.writeWithAI() },
      { label: 'Import Test Cases (Excel, CSV, Postman, OpenAPI)…', action: () => void this.importManual() },
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
  failedCheck(x: TestStepResult) { const c = x.checks?.find(y => !y.passed); return c ? c.what + ', got ' + (c.got ?? '') : ''; }
  logicText(s: TestStep) { return [s.if ? 'if ' + s.if : '', s.retry ? 'retry ' + (s.retry.timeout ?? 60) + ' s' : '', s.wait && (s.run || s.http || s.snmp) ? 'wait ' + s.wait + ' s first' : ''].filter(Boolean).join(' · '); }
  secSummary(p: Phase) {
    const t = this.test(); if (!t) return '';
    const l = this.listOf(t, p), rs = l.map((_, i) => this.resOf(p, i));
    const failed = rs.filter(r => r && !r.passed && !r.skipped).length, bare = l.filter(s => !s.expect?.length).length;
    return [l.length + (l.length === 1 ? ' step' : ' steps'), failed ? failed + ' failed' : '', bare ? bare + ' not checked' : ''].filter(Boolean).join(' · ');
  }
  isOpen(p: Phase, i: number) { return this.open().includes(p + i); }
  toggleRow(p: Phase, i: number) { const k = p + i; this.open.update(l => l.includes(k) ? l.filter(x => x !== k) : [...l, k]); }
  target(s: TestStep) { return s.run?.session ?? s.http?.session ?? s.snmp?.session ?? ''; }
  shortDev(t: string) { return t.replace(/^\{\{\s*|\s*\}\}$/g, '').split('/').pop() ?? t; }
  runHint() { const v = this.view(); return v === 'go' ? 'Run: go test ./cases/...' : v === 'py' ? 'Run: python lib/python/run_tests.py' : v === 'tcl' ? 'Run: tclsh lib/tcl/run_tests.tcl' : ''; }
  openRun(files: string[], label: string) { this.runFiles.set(files); this.runLabel.set(label); }
  readonly multi = signal(false);
  multiPick: string[] = [];
  toggleMulti(b: string) { this.multiPick = this.multiPick.includes(b) ? this.multiPick.filter(x => x !== b) : [...this.multiPick, b]; }
  lockOf(tb: string) { return this.tree()?.locks?.find(l => l.testbed === tb); }
  lockText(tb: string) { const l = this.lockOf(tb); return l ? (l.mine ? '  · running' : '  · in use by ' + l.user) : ''; }
  async releaseLock(tb: string) { try { await this.backend.testReleaseLock(tb); await this.load(); } catch (e) { this.ui.toast(errText(e), 'error'); } }
  async startRun() {
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
  edit() { const t = this.test(); if (t) { this.cks.clear(); const d = structuredClone({ ...t, setup: t.setup ?? [], steps: t.steps ?? [], cleanup: t.cleanup ?? [] }); for (const x of [...d.setup, ...d.steps, ...d.cleanup]) if (x.http) x.http.method ||= 'GET'; this.draft.set(d); } }
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
      { label: 'API Request', action: () => this.addApi(p) },
      { label: 'From a curl Command…', action: () => void this.addCurl(p) },
      { label: 'Manual Step (a person answers)', action: () => this.addManual(p) },
      { label: 'Wait', action: () => this.addWait(p) },
      { label: 'Loop over Values', action: () => this.addLoop(p) },
      { label: 'Software Upgrade', action: () => this.addUpgrade(p) },
      { label: 'Lab Traffic Test', action: () => this.addTraffic(p) },
    ]);
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
    const dev = await this.ui.input(tr('Import Test Cases'), tr('Excel or CSV: the session for steps without a Device column (empty: such steps become manual steps). Postman or OpenAPI: the API session the requests go to (empty: full URLs). Then pick the file.'), '');
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
