import { Component, OnDestroy, OnInit, computed, effect, inject, input, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { NgTemplateOutlet } from '@angular/common';
import { Backend, errText } from '../core/backend';
import { Ui } from '../core/ui.service';
import { tr } from '../core/i18n';
import { Recorder, escapeRe, fieldPattern } from '../core/recorder.service';
import { CleanupMode, OnCleanupFail, TestCase, TestCaseInfo, TestExpect, TestStepResult, TestResult, TestRunReport, TestSchedule, TestScheduleView, TestStep, TestsGitInfo, TestsTree } from '../core/models';
import { expectLabel, parseTableCheck, stepText } from './recorder-panel';
import { AIChat } from '../core/ai-chat.service';

type View = 'steps' | 'yaml' | 'go' | 'py' | 'tcl';
type Phase = 'setup' | 'steps' | 'cleanup';
type Section = 'tests' | 'runs' | 'schedules' | 'testbeds' | 'library';

/** The Tests tab: the tests folder's tree, the selected test (steps, YAML, generated Go) and the last run. */
@Component({
  selector: 'app-tests-view',
  imports: [FormsModule, NgTemplateOutlet],
  template: `
    <div class="tv">
      <nav class="rail" aria-label="Tests sections">
        @for (r of rails; track r.k) {
          <button type="button" [class.on]="section() === r.k" (click)="go(r.k)" [title]="r.t"><span class="ri">{{ r.i }}</span>{{ r.l }}</button>
        }
        <span class="sp"></span>
        <button type="button" class="gitb" (click)="gitMenu($event)" [title]="git()?.repo ? 'Git: ' + git()?.changed + ' changed' : 'Git'"><span class="ri">⎇</span>{{ git()?.repo ? (git()?.branch || 'git') : 'Git' }}@if (git()?.ahead) { <i>↑{{ git()?.ahead }}</i> }@if (git()?.behind) { <i>↓{{ git()?.behind }}</i> }</button>
      </nav>
      <aside class="side">
        @switch (section()) {
          @case ('tests') {
            <div class="stop">
              <div class="row"><button type="button" class="pri grow" (click)="recordMore()">● Record a Test</button><button type="button" (click)="moreMenu($event)" title="Open, clone, write with AI, folder">⋯</button></div>
              <input class="search" type="search" placeholder="Search tests" [ngModel]="q()" (ngModelChange)="q.set($event)" aria-label="Search tests" />
              <div class="tags">@for (t of tagList(); track t.k) { <button type="button" class="tag" [class.on]="tag() === t.k" (click)="tag.set(t.k)">{{ t.l }} <i>{{ t.n }}</i></button> }</div>
            </div>
            <div class="list">
              @if (tree()?.suites?.length) {
                <div class="fold"><button type="button" class="lnk fl" (click)="toggleFold('#suites')">{{ folded().includes('#suites') ? '▸' : '▾' }} suites</button><span class="cnt">{{ tree()?.suites?.length }}</span></div>
                @if (!folded().includes('#suites')) {
                  @for (s of tree()?.suites ?? []; track s.file) {
                    <div class="case" (click)="openRun([s.file], 'suite ' + s.name)" (keydown.enter)="openRun([s.file], 'suite ' + s.name)" tabindex="0" role="button" [title]="s.error || 'Run the suite ' + s.name">
                      <span class="sui">▶</span><span class="cn">{{ s.name }}</span><span class="n">{{ s.tests }}</span>
                      <button type="button" class="lnk" (click)="$event.stopPropagation(); editSuite(s.file)" title="Open the suite file">✎</button></div>
                  }
                }
              }
              @for (g of groups(); track g.dir) {
                <div class="fold"><button type="button" class="lnk fl" (click)="toggleFold(g.dir)">{{ folded().includes(g.dir) ? '▸' : '▾' }} {{ g.dir || 'cases' }}</button><span class="cnt">{{ g.cases.length }}</span>
                  <button type="button" class="lnk" (click)="openRun(filesOf(g), g.dir || 'cases')" [title]="'Run the tests in ' + (g.dir || 'cases')">▶</button></div>
                @if (!folded().includes(g.dir)) {
                  @for (c of g.cases; track c.file) {
                    <div class="case" [class.sel]="c.file === sel() && section() === 'tests'" (click)="select(c.file)" (keydown.enter)="select(c.file)" tabindex="0" role="button" [title]="c.error || c.description || c.file">
                      <input type="checkbox" [checked]="picked().includes(c.file)" (click)="$event.stopPropagation()" (change)="pick(c.file)" [attr.aria-label]="'Select ' + c.name" />
                      <span class="dot" [class]="dot(c)"></span><span class="cn">{{ c.name }}</span><span class="n">{{ c.steps }}</span></div>
                  }
                }
              } @empty { <div class="empty-s">No tests yet.<br />Record one, or ⋯ → Write a Test with AI.</div> }
            </div>
            @if (picked().length) {
              <div class="selbar"><span>{{ picked().length }} selected</span><span class="sp"></span><button type="button" class="ghost" (click)="picked.set([])">Clear</button><button type="button" class="ghost" (click)="saveSuite()" title="Save the selected tests as a suite (suites/…) to run them together again">Save as suite…</button><button type="button" class="pri" (click)="openRun(picked(), picked().length + ' tests')">▶ Run…</button></div>
            } @else if (filtering()) {
              <div class="selbar"><span class="mut">{{ filtered().length }} of {{ tree()?.cases?.length ?? 0 }} shown</span><span class="sp"></span><button type="button" (click)="openRun(shownFiles(), filtered().length + ' shown tests')" [disabled]="!filtered().length">▶ Run shown…</button></div>
            } @else {
              <div class="selbar"><span class="mut">{{ tree()?.cases?.length ?? 0 }} tests</span><span class="sp"></span><button type="button" (click)="openRun([], 'all tests')" [disabled]="!tree()?.cases?.length">▶ Run all…</button></div>
            }
          }
          @case ('runs') {
            <div class="stop"><b>Runs</b><span class="mut">past runs of this tests folder, newest first</span></div>
            <div class="list">
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
              @for (b of tree()?.testbeds ?? []; track b) { <div class="case" [class.sel]="fileSel() === b" (click)="openFile(b)" (keydown.enter)="openFile(b)" tabindex="0" role="button"><span class="cn">{{ base(b) }}</span></div> } @empty { <div class="empty-s">No testbeds.</div> }
            </div>
          }
          @case ('library') {
            <div class="stop"><b>Library</b><span class="mut">helpers of the generated code, and output templates</span></div>
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
      <section class="main">
        @switch (section()) {
          @case ('runs') {
            @if (runRep(); as rep) {
              <div class="head"><div class="crumb">{{ rep.dir }} · {{ rep.testbed ? 'testbed ' + base(rep.testbed) : "the tests' own sessions" }} · {{ rep.cleanup === 'off' ? 'no cleanup' : rep.cleanup === 'auto' ? 'cleanup or undo' : 'cleanup steps' }} · {{ dur(rep.ms) }}</div>
                <div class="title"><h1>Run · {{ when(rep.dir) }}</h1>
                  <span class="pill" [class]="rep.failed ? 'pill f' : rep.skipped ? 'pill s' : 'pill p'">{{ rep.failed ? rep.failed + ' failed' : rep.skipped ? rep.skipped + ' skipped' : 'all passed' }}</span>
                  <span class="sp"></span>
                  <button type="button" (click)="backend.testsOpen(rep.dir)">HTML report</button><button type="button" (click)="backend.testsOpen(rep.dir + '/junit.xml')">JUnit</button>
                  @if (failedFiles(rep).length) { <button type="button" class="pri" (click)="openRun(failedFiles(rep), 'the failed tests')">▶ Run failed again</button> }
                </div></div>
              <div class="body">
                <div class="kpis">
                  <div class="kpi"><b class="kok">{{ rep.passed }}</b><span>passed</span></div><div class="kpi"><b class="kbad">{{ rep.failed }}</b><span>failed</span></div>
                  <div class="kpi"><b class="kwarn">{{ rep.skipped ?? 0 }}</b><span>skipped</span></div><div class="kpi"><b>{{ unchecked(rep) }}</b><span>steps not checked</span></div></div>
                <div class="bar"><i class="okb" [style.flex]="rep.passed"></i><i class="badb" [style.flex]="rep.failed"></i><i class="warnb" [style.flex]="rep.skipped ?? 0"></i></div>
                @if (rep.recovery?.length) { <div class="sec"><div class="sh">Bringing the devices back</div>@for (s of rep.recovery; track $index) { <div class="trow" [class.bad]="!s.passed"><span>{{ s.passed ? '✓' : '✗' }}</span><span>{{ s.name }}</span><span class="mut">{{ s.target }} {{ s.error }}</span></div> }</div> }
                <div class="sec"><div class="rhd"><span></span><span>Test</span><span>First problem</span><span class="r">Time</span></div>
                  @for (r of rep.results; track $index) {
                    <div class="rrow" (click)="r.file && openTest(r.file)" (keydown.enter)="r.file && openTest(r.file)" tabindex="0" role="button">
                      <span [class]="r.skipped ? 'warn' : r.passed ? 'ok' : 'bad'">{{ r.skipped ? '○' : r.passed ? '✓' : '✗' }}</span>
                      <span>{{ r.case }}@if (r.cleanupFailed) { <span class="pill f sm">cleanup</span> }</span>
                      <span class="mut">{{ r.passed ? notChecked(r) : failText(r) }}@if (!r.passed && !r.skipped) { · <button type="button" class="lnk" (click)="$event.stopPropagation(); explain(r)">✦ Explain</button> }</span>
                      <span class="r mut">{{ dur(r.ms) }}</span></div>
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
                  @if (!draft()) { <button type="button" (click)="edit()">✎ Edit</button> }
                  <button type="button" (click)="testMenu($event)" title="Rename, duplicate, delete, open">⋯</button>
                  <button type="button" class="pri" (click)="openRun([sel()], t.name || sel())" [disabled]="busy()">{{ busy() ? 'Running…' : '▶ Run…' }}</button>
                </div>
                <div class="tabs">
                  <button type="button" [class.on]="view() === 'steps'" (click)="setView('steps')">Steps</button>
                  <button type="button" [class.on]="view() !== 'steps'" (click)="setView(view() === 'steps' ? 'yaml' : view())">Code</button>
                  @if (view() !== 'steps') { <span class="seg">@for (v of views.slice(1); track v.k) { <button type="button" [class.on]="view() === v.k" (click)="setView(v.k)">{{ v.l }}</button> }</span> }
                </div>
              </div>
              <div class="body">
                @if (view() === 'steps') {
                  @if (t.description) { <p class="mut">{{ t.description }}</p> }
                  <div class="meta">
                    @if (varsText()) { <span>Defaults <b>{{ varsText() }}</b></span> }
                    @if (t.params?.length) { <span>Runs {{ t.params?.length }}× <b>{{ paramsText() }}</b></span> }
                    @if (t.restore?.length) { <span>Afterwards <b>{{ restoreText() }} put back</b></span> }
                    @if (t.dependsOn?.length) { <span>Depends on <b>{{ t.dependsOn.join(', ') }}</b></span> }
                    <span>After each test <b>{{ cleanupLabel() }}</b></span>
                  </div>
                  @for (r of restoreRes(); track $index) { <div class="trow" [class.bad]="!r.passed"><span>{{ r.passed ? '✓' : '✗' }}</span><span>{{ r.name }}</span><span class="mut">{{ r.target }} {{ r.error }}</span></div> }
                  @if (draft(); as d) {
                    @for (ph of phases; track ph.k) {
                      <div class="sec">
                        <div class="sh">{{ ph.l }}<span class="sp"></span><button type="button" class="lnk" (click)="addStep(ph.k)">+ Step</button><button type="button" class="lnk" (click)="addWait(ph.k)">+ Wait</button><button type="button" class="lnk" (click)="addLoop(ph.k)" title="Run steps once per value">+ Loop</button></div>
                        @for (s of listOf(d, ph.k); track $index; let i = $index; let lastI = $last) {
                          <div class="ed">
                            <div class="row"><b class="num">{{ i + 1 }}</b>
                              @if (s.run) { <input class="ses" [(ngModel)]="s.run.session" placeholder="session" aria-label="Session" /><input class="cmdi" [(ngModel)]="s.run.command" placeholder="command" aria-label="Command" /> }
                              @else if (s.http) { <select [(ngModel)]="s.http.method" aria-label="Method">@for (m of methods; track m) { <option [value]="m">{{ m }}</option> }</select><input class="cmdi" [(ngModel)]="s.http.url" placeholder="URL" aria-label="URL" /> }
                              @else { <span class="cmd fl">{{ text(s) }}</span><span class="sp"></span>@if (s.loop) { <span class="mut">edit loops in the YAML</span> } }
                              <button type="button" class="lnk" (click)="moveStep(ph.k, i, -1)" [disabled]="i === 0" title="Move up">↑</button><button type="button" class="lnk" (click)="moveStep(ph.k, i, 1)" [disabled]="lastI" title="Move down">↓</button><button type="button" class="lnk" (click)="removeStep(ph.k, i)" title="Remove the step">✕</button>
                            </div>
                            <div class="row opts"><label>Timeout <input type="number" min="0" class="num2" [(ngModel)]="s.timeout" /> s</label>
                              <label><input type="checkbox" [(ngModel)]="s.continueOnFail" /> Go on when it fails</label>
                              <label title="Seconds to wait before the step">Wait <input type="number" min="0" class="num2" [(ngModel)]="s.wait" /> s</label>
                              <label title="Run the step only when this holds, e.g. {{ '{{' }}vlan{{ '}}' }} != 1 (==, !=, contains, ~, >, <)">If <input class="pin" [(ngModel)]="s.if" placeholder="always" /></label>
                              <label title="Run the step again until its checks pass, for at most this many seconds"><input type="checkbox" [ngModel]="!!s.retry" (ngModelChange)="s.retry = $event ? { timeout: 60, interval: 5 } : undefined" /> Retry until it passes</label>
                              @if (s.retry) { <label>for <input type="number" min="1" class="num2" [(ngModel)]="s.retry.timeout" /> s</label> }
                              @if (s.run) { <label title="Do not fail the step when the device answers with an error message"><input type="checkbox" [(ngModel)]="s.ignoreErrors" /> Ignore device errors</label>
                                <label title="Turn the output into a table with a TextFSM template: auto (templates/index) or a file in templates/">Parse <input class="pin" [(ngModel)]="s.run.parse" placeholder="off" list="tw-parse" /></label> }</div>
                            <div class="chips">@for (e of s.expect ?? []; track $index; let j = $index) { <span class="chip">{{ label(e) }} <button type="button" class="x" (click)="removeCheck(s, j)" title="Remove the check">×</button></span> }
                              @if (ck(ph.k, i); as c) {
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
                            <div class="srow" [class.f]="r && !r.passed && !r.skipped" [class.open]="isOpen(ph.k, i)" (click)="toggleRow(ph.k, i)" (keydown.enter)="toggleRow(ph.k, i)" tabindex="0" role="button">
                              <span class="mut">{{ i + 1 }}</span><span class="dev" [title]="target(s)">{{ s.loop ? 'loop' : shortDev(target(s)) }}</span><span class="cmd">{{ text(s) }}@if (logicText(s)) { <span class="logic">{{ logicText(s) }}</span> }</span>
                              <span class="chips">@for (e of s.expect ?? []; track $index; let j = $index) { <span class="chip" [class.b]="r?.checks?.[j]?.passed === false">{{ r?.checks?.[j]?.passed === false ? '✗' : r ? '✓' : '' }} {{ label(e) }}</span> }
                                @if (!(s.expect?.length)) { <span class="chip w" title="Without checks the step passes when the device accepts the command">○ not checked</span> }</span>
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
                                  @if (r && !r.passed && !r.skipped && last()[sel()]; as lr) { <div class="row"><button type="button" (click)="explain(lr)">✦ Explain</button></div> }
                                </div>
                                <div>
                                  @if (r?.table; as tb) { <h4>Table · {{ tb.rows.length }} rows</h4><div class="tbw"><table class="ptab"><tr>@for (h of tb.header; track $index) { <th>{{ h }}</th> }</tr>@for (x of tb.rows.slice(0, 200); track $index) { <tr>@for (c of x; track $index) { <td>{{ c }}</td> }</tr> }</table></div> }
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
                } @else if (view() === 'yaml') {
                  <textarea class="code" [(ngModel)]="yaml" spellcheck="false" aria-label="Test YAML"></textarea>
                  <div class="row"><span class="mut">Saving checks the test and writes its Go, Python and TCL code again.</span><span class="sp"></span><button type="button" (click)="setView('yaml')">Revert</button><button type="button" class="pri" (click)="saveYaml()">Save</button></div>
                } @else {
                  <pre class="code ro">{{ goSrc() }}</pre>
                  <div class="row"><span class="mut">Written from the test - edit the steps or the YAML, not this file. {{ runHint() }}</span><span class="sp"></span><button type="button" (click)="backend.testsOpenInEditor(view() === 'go' ? goFile() : view() === 'py' ? pyFile() : tclFile())">Open in VS Code</button></div>
                }
              </div>
            } @else { <div class="empty mut">Select a test, or record one.</div> }
          }
        }
        <ng-template #fileView>
          @if (fileSel(); as fp) {
            <div class="head"><div class="crumb">{{ fp }}</div><div class="title"><h1>{{ base(fp) }}</h1><span class="sp"></span><button type="button" (click)="backend.testsOpenInEditor(fp)">Open in VS Code</button><button type="button" class="pri" (click)="saveFile()">Save</button></div></div>
            <div class="body"><textarea class="code" [(ngModel)]="fileText" spellcheck="false" [attr.aria-label]="fp"></textarea>
              <div class="row"><span class="mut">{{ fp.startsWith('lib/') ? 'Termwright writes twtest and run_tests again when it is updated: keep your own helpers in other files.' : fp.startsWith('templates/') ? 'TextFSM templates; the index says which command each one parses (ntc-templates format).' : 'Roles and values: role: Folder/session, name: value.' }}</span></div></div>
          } @else { <div class="empty mut">Select a file.</div> }
        </ng-template>
      </section>
      @if (runFiles(); as rf) {
        <div class="scrim" (click)="runFiles.set(null)" aria-hidden="true"></div>
        <aside class="drawer" role="dialog" aria-label="Run tests">
          <h2>Run {{ runLabel() }}</h2>
          <span class="mut">{{ rf.length ? rf.length + (rf.length === 1 ? ' test' : ' tests') : 'every test in the folder' }}</span>
          <label class="fld"><span>Testbed - which devices</span><select [(ngModel)]="testbed"><option value="">The tests' own sessions</option>@for (b of tree()?.testbeds ?? []; track b) { <option [value]="b">{{ base(b) }}</option> }</select></label>
          <div class="fld"><span>After each test</span>
            @for (o of cleanupOpts; track o.k) {
              <label class="radio" [class.on]="cleanupMode === o.k"><input type="radio" name="twcl" [value]="o.k" [(ngModel)]="cleanupMode" (ngModelChange)="keepOcf()" /><span>{{ o.l }}<small>{{ o.d }}</small></span></label>
            }
          </div>
          @if (cleanupMode !== 'off') {
            <label class="fld"><span>If a cleanup fails</span><select [(ngModel)]="onCleanupFail" (ngModelChange)="keepOcf()"><option value="stop">Stop the run - skip the rest</option><option value="restore">Restore the configuration saved before the run</option><option value="factory">Factory default, then go on</option></select></label>
          }
          <span class="sp"></span>
          <div class="row"><button type="button" class="grow" (click)="runFiles.set(null)">Cancel</button><button type="button" class="pri grow2" (click)="startRun()">▶ Run {{ rf.length ? rf.length + (rf.length === 1 ? ' test' : ' tests') : 'all' }}</button></div>
        </aside>
      }
    </div>
`,
  styles: [`
    :host { display: block; height: 100%; min-height: 0; }
    .tv { display: grid; grid-template-columns: 64px 272px 1fr; height: 100%; min-height: 0; position: relative; }
    button { font: inherit; } .sp { flex: 1; } .grow { flex: 1; } .grow2 { flex: 2; } .mut { color: var(--muted); font-size: 12px; } .ok { color: var(--ok, #3fbf75); } .bad { color: var(--danger, #ef6161); } .warn { color: var(--warn, #e0a030); }
    .row { display: flex; align-items: center; gap: 6px; } .r { text-align: right; } .fl { min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
    .pri { background: var(--accent); color: #fff; border-color: transparent; } .ghost { background: none; border-color: transparent; color: var(--muted); }
    .lnk { border: 0; background: none; color: var(--accent); cursor: pointer; padding: 0 4px; }
    .rail { display: flex; flex-direction: column; align-items: stretch; gap: 4px; padding: 8px 6px; background: color-mix(in srgb, var(--bg, #000) 70%, #000); border-right: 1px solid var(--border); }
    .rail button { display: flex; flex-direction: column; align-items: center; justify-content: center; gap: 3px; height: auto; min-height: 50px; line-height: 1.15; padding: 6px 2px; border: 0; border-radius: 9px; background: none; color: var(--muted); font-size: 10.5px; cursor: pointer; }
    .rail button:hover { color: var(--text); } .rail button.on { background: color-mix(in srgb, var(--accent) 22%, transparent); color: var(--text); } .ri { font-size: 16px; line-height: 1.1; }
    .rail .gitb { font-size: 10px; word-break: break-all; } .rail i { font-style: normal; color: var(--accent); }
    .side { display: flex; flex-direction: column; min-height: 0; background: var(--panel); border-right: 1px solid var(--border); }
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
    .sui { color: var(--accent); font-size: 10px; width: 14px; text-align: center; }
    .empty-s { color: var(--muted); font-size: 12px; padding: 18px 8px; text-align: center; line-height: 1.6; }
    .selbar { display: flex; align-items: center; gap: 6px; margin: 8px; padding: 8px 10px; border-radius: 9px; border: 1px solid var(--border); background: color-mix(in srgb, var(--text) 4%, transparent); }
    .main { display: flex; flex-direction: column; min-width: 0; min-height: 0; }
    .head { padding: 12px 20px 0; border-bottom: 1px solid var(--border); } .crumb { color: var(--muted); font-size: 12px; }
    .title { display: flex; align-items: center; gap: 8px; margin: 3px 0 10px; } .title h1 { font-size: 18px; margin: 0; font-weight: 600; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; flex: 0 1 auto; min-width: 120px; } .title button { white-space: nowrap; flex: none; } .title .mut.fl { flex: 1 1 0; }
    .pill { font-size: 11.5px; padding: 2px 9px; border-radius: 10px; white-space: nowrap; background: color-mix(in srgb, var(--text) 8%, transparent); color: var(--muted); }
    .pill.f { background: color-mix(in srgb, var(--danger, #ef6161) 18%, transparent); color: var(--danger, #ef6161); } .pill.p { background: color-mix(in srgb, var(--ok, #3fbf75) 18%, transparent); color: var(--ok, #3fbf75); } .pill.s { background: color-mix(in srgb, var(--warn, #e0a030) 18%, transparent); color: var(--warn, #e0a030); } .pill.sm { font-size: 10.5px; padding: 0 6px; margin-left: 6px; }
    .tabs { display: flex; align-items: center; gap: 18px; } .tabs > button { border: 0; background: none; color: var(--muted); padding: 8px 0; border-bottom: 2px solid transparent; cursor: pointer; } .tabs > button.on { color: var(--text); border-color: var(--accent); }
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
    .exp { display: grid; grid-template-columns: minmax(0, 1fr) minmax(0, 1.2fr); gap: 16px; padding: 12px 12px 14px 48px; border-top: 1px solid var(--border); background: color-mix(in srgb, var(--bg, #000) 60%, transparent); }
    .exp h4 { margin: 0 0 6px; font-size: 11px; color: var(--muted); font-weight: 500; text-transform: uppercase; letter-spacing: .05em; } .exp h4 + * { margin-bottom: 10px; }
    .ev { display: grid; grid-template-columns: auto minmax(0, 1fr) minmax(0, 1fr); gap: 4px 12px; font-size: 12px; } .ev span { overflow-wrap: anywhere; }
    .exp pre { margin: 0; font: 12px/1.5 var(--mono, monospace); background: var(--term-bg, rgba(0,0,0,.3)); border-radius: 7px; padding: 8px 10px; max-height: 280px; overflow: auto; white-space: pre-wrap; }
    .tbw { max-height: 220px; overflow: auto; border: 1px solid var(--border); border-radius: 7px; } .ptab { border-collapse: collapse; font: 12px var(--mono, monospace); } .ptab th { position: sticky; top: 0; background: var(--panel); text-align: left; padding: 2px 8px; color: var(--muted); font-weight: 500; } .ptab td { padding: 2px 8px; border-top: 1px solid var(--border); white-space: nowrap; }
    .trow { display: flex; gap: 8px; font-size: 12px; } .none { color: var(--muted); font-size: 12px; padding: 10px 12px; border-top: 1px solid var(--border); }
    .ed { padding: 9px 12px; border-top: 1px solid var(--border); display: flex; flex-direction: column; gap: 5px; } .num { width: 18px; color: var(--muted); font-weight: 500; }
    .ses { width: 160px; height: 27px; } .cmdi { flex: 1; min-width: 0; height: 27px; font-family: var(--mono, monospace); } .num2 { width: 60px; height: 24px; } .pin { width: 100px; height: 24px; }
    .logic { display: inline-block; margin-left: 8px; font: 11px var(--ui-font, inherit); color: var(--accent); background: color-mix(in srgb, var(--accent) 12%, transparent); padding: 0 6px; border-radius: 8px; }
    .opts { flex-wrap: wrap; font-size: 12px; color: var(--muted); gap: 16px; padding-left: 24px; } .opts label { display: flex; align-items: center; gap: 4px; } .ed .chips { padding-left: 24px; }
    .addck { display: inline-flex; gap: 4px; } .addck select, .addck input { height: 24px; font-size: 12px; } .addck input { width: 220px; }
    .code { flex: 1; min-height: 260px; font: 12.5px/1.5 var(--mono, monospace); resize: none; white-space: pre; overflow: auto; margin: 0; padding: 10px; border-radius: 8px; border: 1px solid var(--border); background: var(--term-bg, rgba(0,0,0,.25)); color: var(--text); }
    .kpis { display: grid; grid-template-columns: repeat(4, 1fr); gap: 10px; } .kpi { border: 1px solid var(--border); border-radius: 10px; padding: 10px 12px; background: var(--panel); } .kpi b { display: block; font-size: 21px; font-weight: 600; } .kpi span { color: var(--muted); font-size: 12px; } .kok { color: var(--ok, #3fbf75); } .kbad { color: var(--danger, #ef6161); } .kwarn { color: var(--warn, #e0a030); }
    .bar { display: flex; height: 6px; border-radius: 3px; overflow: hidden; background: color-mix(in srgb, var(--text) 8%, transparent); flex: none; } .okb { background: var(--ok, #3fbf75); } .badb { background: var(--danger, #ef6161); } .warnb { background: var(--warn, #e0a030); }
    .rhd, .rrow { display: grid; grid-template-columns: 26px minmax(140px, 1fr) minmax(200px, 1.6fr) 64px; gap: 10px; padding: 8px 12px; align-items: center; } .rhd { background: var(--panel); color: var(--muted); font-size: 11.5px; padding: 5px 12px; } .rrow { border-top: 1px solid var(--border); cursor: pointer; } .rrow:hover { background: var(--hover, rgba(255,255,255,.03)); }
    .form { display: flex; flex-direction: column; gap: 10px; max-width: 620px; } .form label { display: flex; align-items: center; gap: 10px; } .form label > span { width: 130px; color: var(--muted); font-size: 12px; flex: none; } .form input:not([type=checkbox]), .form select { height: 29px; flex: 1; } .form .ck { padding-left: 140px; }
    .empty { margin: auto; }
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
  readonly rails: { k: Section; l: string; i: string; t: string }[] = [
    { k: 'tests', l: 'Tests', i: '☰', t: 'Tests: record, edit and run' }, { k: 'runs', l: 'Runs', i: '▶', t: 'Past runs and their results' },
    { k: 'schedules', l: 'Sched.', i: '⏱', t: 'Scheduled runs' }, { k: 'testbeds', l: 'Testbeds', i: '⌗', t: 'Which devices a run uses' }, { k: 'library', l: 'Library', i: '{ }', t: 'Templates and code helpers' }];
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
    if (this.report()?.dir === dir) { this.runRep.set(this.report()); return; }
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
  async openFile(f: string) { this.fileSel.set(f); this.fileText = await this.backend.testFile(f).catch(e => '# ' + errText(e)); }
  async saveFile() { try { await this.backend.testSaveFile(this.fileSel(), this.fileText); this.ui.toast(tr('Saved'), 'success'); await this.load(); } catch (e) { this.ui.toast(errText(e), 'error'); } }
  moreMenu(ev: MouseEvent) {
    this.ui.showMenu(ev, [
      { label: 'Write a Test with AI…', action: () => void this.writeWithAI() },
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
      { label: 'Open in VS Code', action: () => void this.backend.testsOpenInEditor(this.sel()) },
      { label: 'Record a New Test', action: () => this.recordMore() },
      { separator: true },
      { label: 'Delete…', action: () => void this.remove() },
    ]);
  }
  splitTags(v: string) { const l = v.split(',').map(x => x.trim()).filter(Boolean); return l.length ? l : undefined; }
  filtering() { return !!this.q().trim() || this.tag() !== 'all'; }
  shownFiles() { return this.filtered().map(c => c.file); }
  editSuite(f: string) { this.section.set('library'); void this.openFile(f); }
  async saveSuite() {
    const name = await this.ui.input(tr('Save as Suite'), tr('Suite name (suites/<name>.yaml)'), '');
    if (!name?.trim()) return;
    try { const rel = await this.backend.testSaveSuite(name.trim().toLowerCase().replace(/[^a-z0-9]+/g, '-'), { name: name.trim(), tests: this.picked() }); this.ui.toast(tr('Wrote {0}', rel), 'success'); await this.load(); }
    catch (e) { this.ui.toast(errText(e), 'error'); }
  }
  private async saveCase(mod: (c: TestCase) => void) {
    const t = this.test(); if (!t) return;
    const c = structuredClone(t); mod(c);
    try { await this.backend.testSave(this.sel(), c); await this.load(); } catch (e) { this.ui.toast(errText(e), 'error'); }
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
  startRun() { const f = this.runFiles() ?? []; this.runFiles.set(null); void this.run(f); }
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
  addWait(p: Phase) { const d = this.draft(); if (!d) return; this.listOf(d, p).push({ wait: 5 }); this.touch(); }
  addLoop(p: Phase) {
    const d = this.draft(); if (!d) return;
    const all = [...(d.setup ?? []), ...d.steps, ...(d.cleanup ?? [])], ses = [...all].reverse().find(s => s.run)?.run?.session ?? '';
    this.listOf(d, p).push({ loop: { var: 'port', range: '1-4', steps: [{ run: { session: ses, command: 'show ports {{port}} information' } }] } });
    this.touch();
    this.ui.toast(tr('Loop added: change its values and steps in the YAML after saving'), 'info');
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
