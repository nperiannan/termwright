# Scripting

Termwright runs **JavaScript** (ES5.1+ with most ES6 syntax, via [goja](https://github.com/dop251/goja)) and **Python** with the `crt` object ([see below](#python-scripts-with-the-crt-object)) against a terminal tab.
Scripts run synchronously: `waitFor` blocks until the text arrives, so there's no need for callbacks or `await`.

Run a script from **Script → Run Script…**, from a **key binding** (Options → Key Map), or after connecting with a session's
**Logon script** (Session Options → Logon Actions & Triggers). **Script → Start / Stop Script Recording** turns typed commands into a script.

## API

| Call | Description |
|---|---|
| `term.send(text)` | Send text as-is (`"\r"` is Enter, `"\u0003"` is Ctrl+C) |
| `term.sendLine(text)` | Send text followed by Enter |
| `term.waitFor(pattern, [timeoutMs])` | Wait for a string or `RegExp` in the output, return `true`, or `false` on timeout (no timeout = wait forever) |
| `term.waitForAny([p1, p2, …], [timeoutMs])` | Index of the first pattern that appears, `-1` on timeout |
| `term.readUntil(pattern, [timeoutMs])` | Text received before the pattern (use it to capture command output), `null` on timeout |
| `term.screen()` | Recent output as plain text (last 16 KB) |
| `term.clear()` | Forget output buffered so far (the next wait only sees new output) |
| `term.connected()` / `term.waitConnected([ms])` | Connection state |
| `openSession(path)` | Open a saved session in a new tab, return its `term` object |
| `tabs()` | `[{id, title, state}]` of open tabs |
| `sleep(ms)`, `log(...)` | Pause / show a message |
| `prompt(message, [default], [secret])` | Ask the user, return the string or `null` |
| `confirm(message)` | OK/Cancel question → `true` / `false` |
| `scriptPath` | Path of the running script |

Output is buffered from the moment the script starts, so text that arrives between `send` and `waitFor` is never missed.
Each successful `waitFor` / `readUntil` consumes the output up to the end of the match.

## Example: back up configs from several switches

```js
var switches = ["Lab/sw1", "Lab/sw2"];
for (var i = 0; i < switches.length; i++) {
  var t = openSession(switches[i]);
  if (!t.waitConnected(30000)) { log("could not connect to " + switches[i]); continue; }
  t.waitFor(/[#>] ?$/m, 20000);
  t.sendLine("disable clipaging");
  t.waitFor(/# ?$/m, 10000);
  t.sendLine("show configuration");
  var cfg = t.readUntil(/\n\S+\.\d+ # $/, 60000);
  log(switches[i] + ": " + (cfg ? cfg.length + " characters" : "timeout"));
}
```

See `docs/examples/` for more.

## Python scripts with the crt object

Files ending in `.py` run with your installed **Python 3** (found automatically as `python3`, or `py -3` on Windows; set another
interpreter in Global Options → General → *Python interpreter*). Termwright provides the `crt` script object and the
`BUTTON_*`, `ICON_*` and `ID*` constants as globals, so most existing Python scripts written for the `crt` object run unchanged — including logon scripts
(Session Options → Logon Actions → *Logon script*), Script → Run Script…, buttons and key bindings. Any Python module can be imported.

```python
# $language = "Python3"
# $interface = "1.0"
def main():
    crt.Screen.Synchronous = True
    crt.Screen.WaitForString("login:", 10)
    crt.Screen.Send("admin\r")
    crt.Screen.WaitForString("Password:")
    crt.Screen.Send(crt.Dialog.Prompt("Password for " + crt.Session.RemoteAddress, "Login", "", True) + "\r")
    for dut in crt.Arguments:                       # Arguments field, e.g. "DUT1 DUT2"
        crt.Screen.Send("ping " + dut + "\r")
        idx = crt.Screen.WaitForStrings(["#", "unreachable"], 30)
        if idx == 2:
            crt.Dialog.MessageBox(dut + " is unreachable", "Ping", ICON_WARN)

main()
```

| `crt` member | Supported |
|---|---|
| `crt.Screen` | `Send`, `SendKeys`, `SendSpecial` (Enter/Tab/Esc/Break), `WaitForString(s, timeout, caseInsensitive)`, `WaitForStrings(list or strings, timeout, caseInsensitive)` → 1-based index or 0, `ReadString(...)`, `MatchIndex`, `WaitForCursor`, `WaitForKey`, `Get`, `Get2`, `CurrentRow`, `CurrentColumn`, `Rows`, `Columns`, `Synchronous`, `IgnoreCase` |
| `crt.Session` | `Connected`, `Connect("/S Folder/session")` or `Connect("/SSH2 /L user /P 22 host")` (also `/TELNET`, `/SERIAL COM1 /BAUD 9600`), `ConnectInTab`, `Disconnect`, `Log(start, append)`, `Logging`, `LogFileName`, `Path`, `RemoteAddress`, `RemotePort` |
| `crt.Dialog` | `Prompt(message, title, default, isPassword)`, `MessageBox(message, title, buttons)` → `IDOK`/`IDYES`/`IDNO`/`IDCANCEL`…, `FileOpenDialog`, `FileSaveDialog` (ask for a path) |
| `crt.Arguments` | `Count`, `GetArg(i)`, iteration, `len()` |
| `crt` | `Sleep(ms)`, `GetScriptTab`, `GetActiveTab`, `GetTab(i)`, `GetTabCount`, `Quit`, `Version`, `ScriptFullName`, `Window.Caption` |

Notes: timeouts are in seconds (0 = wait forever); `print()` output appears as a notification; a Python error stops the script and
shows the last line of the traceback; Script → Cancel Running Scripts stops the Python process.

**Reading the screen.** `crt.Screen.Get(row1, col1, row2, col2)`, `Get2`, `CurrentRow`, `CurrentColumn`, `Rows` and `Columns` read the
screen **exactly as it is rendered** in the tab (the same rows and columns you see, after colors, cursor movement and full-screen
redraws are applied), so the classic "read the prompt" idiom works:

```python
row = crt.Screen.CurrentRow
prompt = crt.Screen.Get(row, 1, row, crt.Screen.CurrentColumn - 1).strip()   # e.g. "core-sw1:1#"
```

(When a tab has no terminal view - for example a hidden script-only tab - the last received text is used instead.)

## VBScript scripts (Windows)

Files ending in `.vbs` run with **Windows Script Host** (`cscript`, part of Windows) - as logon scripts (Session Options → Logon
Actions → *Logon script*), with Script → Run Script…, from buttons and key bindings. The `# $language = "VBScript"` header is
accepted, `Sub Main` is called automatically, and the same `crt` object as for Python is available (`crt.Screen`, `crt.Session`,
`crt.Dialog`, `crt.Arguments`, `crt.Sleep`, …, plus the `BUTTON_*`, `ICON_*`, `ID*` constants), including optional arguments:

```vbscript
# $language = "VBScript"
# $interface = "1.0"
Sub Main
  crt.Screen.Synchronous = True
  crt.Screen.WaitForString "login:"
  crt.Screen.Send "admin" & vbCr
  crt.Screen.WaitForString "Password:", 10
  crt.Screen.Send crt.Dialog.Prompt("Password", "Login", "", True) & vbCr
  If crt.Screen.WaitForStrings(Array("#", ">"), 15) = 2 Then crt.Screen.Send "enable" & vbCr
  row = crt.Screen.CurrentRow
  MsgPrompt = crt.Screen.Get(row, 1, row, crt.Screen.CurrentColumn - 1)
  If crt.Arguments.Count > 0 Then crt.Screen.Send "ping " & crt.Arguments(0) & vbCr
End Sub
```

How it works: the script is wrapped in a Windows Script Host job whose JScript part provides `crt` (JScript accepts the
optional arguments) and talks to Termwright over HTTP on a random local port with a one-time token. Property-style values
(`CurrentRow`, `CurrentColumn`, `Rows`, `Columns`, `Session.Connected`, `Session.Path`) are refreshed with every call, so read them
after a `Send`, `WaitFor…` or `Sleep`. `WScript.Echo` output appears as a notification; a script error shows Windows Script Host's
message (line numbers count from the start of the wrapper). On macOS and Linux, which have no Windows Script Host, use Python instead.
