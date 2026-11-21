# Testing with Termwright: record, run, report, schedule

This guide takes you from a manual check on a switch to a test that runs by itself every morning: record the check, clean up after it, look at it, run it on a testbed, read the report, schedule it and let the AI assistant explain a failure. Every step has a picture. The reference for the test format and all options is the User Guide, chapter [9f. Tests](USER_GUIDE.md#9f-tests-record-run-and-generate-code).

**What you need:** saved sessions for the devices, with their logins saved (Termwright logs in with them; tests never hold passwords). For REST steps an API session; for SNMP steps the sessions' SNMP settings.

---

## 1. Start recording

Open **Tests → Record a Test**. The **Steps** panel opens on the right. Give the test a name, then connect to the device as usual - here `access-sw7`.

![The Steps panel after Record a Test, with a session open](screenshots/tests-guide/01-recording-started.png)

Everything you now do becomes a step:

- **commands** you type in terminal tabs (or send from the Send bar),
- **requests** you send in the **API Client** (its status code is added as a first check),
- **gets, walks and sets** in the **SNMP Browser** (a get's values are added as checks; a query on several devices gives one step per device).

Commands that may hold a password, secret, community or key are not recorded. ⏸ pauses recording.

## 2. Type the command and mark what must be true

Type `show vlan 42`. To check a value, select it in the output (a double-click selects a word), right-click and choose **Expect this**:

![Right-click on selected output: Expect this](screenshots/tests-guide/02-expect-this.png)

| Choice | What the test checks |
|---|---|
| *contains "…"* | the output contains the text |
| *field = "…"* | the value after its label - Termwright builds `Admin\s+State:\s*(\S+)` from the label, so it still works when spacing or other lines change |
| *matches a pattern* | the text as a regular expression, numbers as `\d+` |
| *does not contain* | the text must not be there |
| *Save as Test Variable…* | keeps the value as `{{name}}` for later steps (for example an address the REST step must return) |

## 3. Add a check by hand

**+ Check** under a step adds a check without selecting anything - contains, does not contain, matches, a field, and for REST steps the status, a JSON path (`data.items[0].name`, `length`) or a time limit; for SNMP steps a value by name or OID.

![The + Check form](screenshots/tests-guide/03-add-check.png)

## 4. Record the rest

Type the next command (`show ports 4 information`), send a request in the API Client or read a value in the SNMP Browser. Each becomes a step with its checks. ↑ moves a step up, ✕ removes it, × on a check removes the check.

**A step without checks only proves that the command ran** - see *How a step passes or fails* below. Under a read step (`show …`) the panel suggests checks from the configuration commands you recorded before it on the same device: after `create vlan v200 tag 200`, a `show vlan` step offers **+ contains "v200"**. Click one to add it. Saving a test whose read steps check nothing asks first.

![Two steps with their checks](screenshots/tests-guide/04-steps-recorded.png)

### How a step passes or fails

Termwright does not know what a command is meant to do; it only knows the checks you give it. A step passes when:

1. the command was sent and the device answered (no timeout, the session could log in), **and**
2. the answer has no line that matches the device type's **error patterns** - `% Invalid input`, `Error:`, `%% Unrecognized command` … (built in for EXOS, VOSS, Cisco IOS, NX-OS, Huawei VRP, Eltex MES and Ruckus ICX; your own in Global Options → Device Types), **and**
3. every check of the step is true.

So a step **without checks** passes whenever the device accepts the command - also a `show vlan` that shows that `v200` is missing. The run marks such steps **○ ran, nothing checked**, in the Tests tab and in the report, with their output under **Output**. **The same holds for cleanup steps**: `delete vlan v200` without a check passes as long as the device does not answer with an error.

To make a test really test something, give the read steps checks:

| You did | Then check |
|---|---|
| `create vlan v200 tag 200` | `show vlan` - **contains** `v200` (or a field: the VLAN's state = `Enabled`) |
| `delete vlan v200` in the cleanup | `show vlan` in the cleanup - **does not contain** `v200` |
| POST a VLAN | GET it - status 200 and a JSON path, e.g. `data.name = v200` |

In the YAML the same test reads:

```yaml
steps:
  - run: {session: "{{sw}}", command: create vlan v200 tag 200}
  - run: {session: "{{sw}}", command: show vlan}
    expect: [{contains: v200}]
cleanup:
  - run: {session: "{{sw}}", command: delete vlan v200}
  - run: {session: "{{sw}}", command: show vlan}
    expect: [{notContains: v200}]
```

A command that is allowed to answer with an error (for example deleting something that may not be there in the setup) gets `ignoreErrors: true` - or *Ignore device errors* in the step editor. The report shows each check as expected vs. got, so a failure says what was there instead.

**Sessions as roles** (on by default) saves the session as a role - `access-sw7` becomes `{{access_sw7}}` with this session as the default - so the same test can later run on other devices (section 10).

## 5. Clean up after the test

A test that changes a device must undo the change, or the next test starts from a dirty device. The Steps panel records into three sections - **Setup**, **Steps** and **Cleanup** - and **the cleanup always runs**, also when a step failed or the run was stopped.

For every change you record, the panel suggests its undo (**↩ undo**):

| Recorded | Suggested cleanup |
|---|---|
| a configuration command | from the device type's **Undo rules**: `create vlan v42 tag 42` → `delete vlan v42`, `configure vlan v42 add ports 4` → `configure vlan v42 delete ports 4`, `vlan 42` → `no vlan 42`, Huawei `vlan 42` → `undo vlan 42` |
| a REST **POST** | the **DELETE** of what it created, with the id taken from its answer |
| an SNMP **set** | setting the value back to what it was (read just before the set) |

![Two configuration commands with their suggested undos](screenshots/tests-guide/15-undo-suggested.png)

**+ Cleanup** adds one undo; **↩ Undo All in Cleanup** adds them all, the last change first. Switch to **Cleanup** to record more - for example `show vlan` with a *does not contain v42* check, so the test confirms the device is clean. **✦ Cleanup with AI** asks the assistant to write the cleanup in the right order and configuration mode for each device type.

![The cleanup, with a lab snapshot to go back to](screenshots/tests-guide/16-cleanup-recorded.png)

**Setup** steps run first and put the device in a known state (for example remove a VLAN a crashed earlier run left behind); when a setup step fails, the test's steps are skipped but the cleanup still runs.

**Afterwards put back** is the safety net for changes the cleanup may miss:

- **configuration**: Termwright saves the configuration before the test and restores it after the cleanup with the device type's **Test save / restore** commands (Global Options → Device Types; built in for NX-OS: `checkpoint` and `rollback`).
- **lab snapshot**: for emulated lab switches - a checkpoint before the test, and back to it afterwards. The cleanest option when you test on the lab.

The undo rules are in Global Options → **Device Types** too: one rule per line, `regular expression => undo` (`$1` is the first group). Built-in rules cover EXOS, VOSS, Cisco IOS, NX-OS, Huawei VRP, Eltex MES and Ruckus ICX; add your own for other devices.

## 6. Save

**Save Test…** asks for the file. Tests live in the tests folder (`cases/…`); sub-folders group them.

![Save Test](screenshots/tests-guide/05-save-test.png)

Saving writes the test as YAML and also as Go, Python and TCL code (section 7).

## 7. The Tests tab

**Tests → Tests** opens the tests folder: the tests by folder (a dot shows the last result), testbeds, schedules and past reports. Select a test to see its steps, the roles' defaults and the checks.

![The Tests tab with the recorded test](screenshots/tests-guide/06-tests-tab.png)

**✎ Edit Steps** changes the test without YAML: the session and command of each step (method and URL for REST), its timeout, *Go on when it fails* and *Ignore device errors*; ↑ ↓ move a step, ✕ removes it, **+ Step** adds one to the setup, steps or cleanup, × removes a check and **+ Check** adds one (contains, does not contain, matches; for REST the status, a JSON path `path = value` or a time limit; for SNMP `OID = value`). **Save** writes the YAML and the Go, Python and TCL code again.

![Editing the steps of a test](screenshots/tests-guide/19-edit-steps.png)

**Rename…** moves the test to another name or folder, **Duplicate…** copies it, **Delete** moves it and its code to `.removed/` in the tests folder - nothing is erased, so you can bring it back from there.

**YAML** shows the test as a file you can edit and **Save** (it is checked first). This is the part to edit - the code is written from it.

![The test as YAML](screenshots/tests-guide/07-yaml.png)

**Go**, **Python** and **TCL** show the same test as code, for CI systems and people who want to read it as a script. They send each step to Termwright, which logs in with the saved logins - no passwords in the code.

![The test as Python](screenshots/tests-guide/08-python.png)

![The test as TCL](screenshots/tests-guide/08b-tcl.png)

The library is kept apart from the tests: **LIB** in the tree lists the files in `lib/` (the helpers `twtest` and `run_tests` that the generated code uses, and your own); click one to read it, **Open in VS Code** to change it. Keep your own helpers in their own files - Termwright writes its helpers again when it is updated. The tests keep their values as variables (`{{vlan}}`, `vars:`, `params:` and the testbeds), so you change a value in one place.

Run them in the tests folder while Termwright is open:

```sh
go test ./cases/...
python lib/python/run_tests.py --junit junit.xml     # or: pytest cases
tclsh lib/tcl/run_tests.tcl -junit junit.xml
```

## 8. Run it on a testbed

Pick a **testbed** next to **▶ Run** (or none to use the recorded devices) and press **▶ Run** - **▶▶** at the top runs every test, **▶** on a folder runs the tests in it, and the check boxes in front of the tests with **▶ Run Selected (N)** run only those. The steps show the result of each check and what came back; the commands of a test run over one login per device. The **Last run** list shows each test with its first failure.

![A run: three steps passed, the SNMP step failed](screenshots/tests-guide/09-run-results.png)

![Two tests selected; each step's output under Output](screenshots/tests-guide/18-selected-unchecked.png)

A test with setup and cleanup shows its sections; the restore lines say whether the lab snapshot (or configuration) was saved and put back, and a failed cleanup is shown as *the device may not be clean*:

![A run with setup, steps, cleanup and a lab snapshot](screenshots/tests-guide/17-cleanup-run.png)

A run that uses production sessions is asked first.

### When a cleanup fails

A test whose cleanup fails is reported as **failed (cleanup)** - also when its steps passed - because the devices may not be clean for the next test. What happens next is chosen with **On a failed cleanup** above the tree, before the run:

| Choice | What Termwright does |
|---|---|
| **Stop the run** (default) | stops; the remaining tests are reported as **skipped**, with the reason |
| **Restore the saved configuration** | saves each device's configuration before the run (the device type's *Test save / restore* commands), puts it back after a failed cleanup, then goes on |
| **Factory default** | resets the devices of that test with the device type's *Factory reset* commands (built in: EXOS `unconfigure switch all`, Cisco IOS and NX-OS `write erase` + `reload`), then goes on |

Both may reboot the device; Termwright answers the confirmations and waits up to 15 minutes until it can log in again. If bringing the devices back fails too, the run stops and the remaining tests are skipped. A factory default is never run on production sessions, and it may remove the management address - use console sessions for tests that may need it. The report lists these recovery steps at the top.

## 9. Read the report

**Open Report** opens the run's HTML report: every test and step, the checks with what came back, and the output of failed steps. **JUnit XML** is the same run for CI systems. Both are kept in `reports/` (one folder per run).

![The HTML report](screenshots/tests-guide/10-report.png)

## 10. Testbeds: the same test on other devices

A testbed maps the roles to sessions. **+** next to **Testbeds** writes one from a session folder - every saved session in it becomes a role (`access-sw7` → `access_sw7: Datacenter/Access/access-sw7`). Point it at the emulated lab's folder to run the tests there first, or at another site's folder.

![New testbed from a session folder](screenshots/tests-guide/11-new-testbed.png)

You can also write testbeds by hand (`testbeds/site-b.yaml`):

```yaml
access_sw7: Site B/access-sw2
lab_rest: APIs/Site B REST
vlan: "142"
```

To run a test with several values, add `params` to its YAML: `params: [{vlan: "42"}, {vlan: "100"}]` runs it once per entry.

## 11. Schedule it

**+** next to **Schedules** runs tests by themselves while Termwright is open: all tests or the selected one, with a testbed, daily, on weekdays, weekly or every N hours. *Failed cleanup* sets the same policy for the schedule's runs. A failed run shows a notification; the list shows each schedule's last result (with its report) and next run. **Run Now** starts it at once.

![A schedule: weekdays at 07:30 on lab-a](screenshots/tests-guide/12-schedule.png)

Tests may use production sessions in a schedule only when *May use production sessions* is ticked. For a server that tests around the clock without the window, use `termwright tests run` from cron or Task Scheduler (section 14).

## 12. Ask the AI assistant why it failed

**✦ Explain** next to a failed test (or **Explain the Failure** under its steps) sends the test and the run - each step, the checks with what came back, the failed output - to the AI assistant. It says whether the device, the network or the test is wrong and shows a corrected test when the test needs one.

![The AI assistant explains the failed SNMP step](screenshots/tests-guide/13-explain-ai.png)

## 13. Let the AI assistant write a test

**✦ Write a Test with AI** asks what the test should check. The assistant finds the saved sessions, writes the YAML with roles and checks, saves it with the `tests` tool and offers to run it. Saving and running are asked like other changes.

![Write a Test with AI](screenshots/tests-guide/14-write-with-ai.png)

## 14. Share and run in CI

- **Git**: when the tests folder is a Git repository (**Clone…**, or *Make it a Git repository*), **Pull**, **Commit…** and **Push** keep it in step with your team; reports stay out of Git.
- **Without the window**, on a CI machine with Termwright installed:

  ```sh
  termwright tests run --testbed lab-a.yaml --junit junit.xml   # exits 1 when a test failed or was skipped
  termwright tests run --on-cleanup-failure restore             # stop (default) | restore | factory
  termwright tests serve                                         # only the local API, for the Go/Python/TCL code
  termwright tests secrets                                       # which TERMWRIGHT_SECRET_… variables the tests may need
  ```

  Where there is no credential store, passwords and communities can come from `TERMWRIGHT_SECRET_<KEY>` environment variables (`termwright tests secrets` lists the names; values are never printed).

## Tips

- **Mode-dependent commands**: the commands of a test share one login per device, like when you recorded them - a test that goes into configure mode stays there for its next steps. Make the cleanup leave that mode where the device needs it.
- **Always clean up**: record the undo of every change (or tick *lab snapshot* on the emulated lab); add a check in the cleanup that the change is gone - without it, a cleanup step passes whenever the device accepts the command.
- **○ ran, nothing checked** in a run means the step has no checks: add one, or the test cannot fail there.
- **Values that change** (uptime, counters): check them with a field and `greater` / `less` in the YAML instead of an exact text.
- **A value that must change**: save it in one step and check `notEquals: "{{name}}"` in a later one.
- **Slow devices**: give a step `timeout: 120` (seconds) in the YAML.
- **One test failed, the rest should go on**: add `continueOnFail: true` to the step.
