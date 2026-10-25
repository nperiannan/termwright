# termwright_crt.py - the SecureCRT-style "crt" object for Termwright Python scripts.
#
# Termwright runs .py scripts with your own Python 3 and makes `crt` (plus the
# usual BUTTON_*/ICON_*/ID* constants) available as globals, so most SecureCRT
# Python scripts run unchanged. Calls are forwarded to Termwright over a local
# socket.
import json
import os
import socket
import sys


class ScriptError(Exception):
    pass


class QuitScript(BaseException):
    pass


class _Bridge:
    def __init__(self):
        host, port = os.environ["TERMWRIGHT_RPC"].rsplit(":", 1)
        self._sock = socket.create_connection((host, int(port)))
        self._r = self._sock.makefile("r", encoding="utf-8", newline="\n")
        self._n = 0
        self._send({"token": os.environ.get("TERMWRIGHT_TOKEN", "")})

    def _send(self, obj):
        self._sock.sendall((json.dumps(obj) + "\n").encode("utf-8"))

    def call(self, op, **args):
        self._n += 1
        self._send({"id": self._n, "op": op, "args": args})
        line = self._r.readline()
        if not line:
            raise QuitScript()  # Termwright cancelled the script
        reply = json.loads(line)
        if reply.get("error"):
            raise ScriptError(reply["error"])
        return reply.get("result")


_bridge = _Bridge()


def _split(args, kwargs):
    """SecureCRT's WaitForStrings/ReadString accept a list or several strings,
    then an optional timeout (seconds) and an optional case-insensitive flag."""
    args = list(args)
    if args and isinstance(args[0], (list, tuple)):
        strs, rest = [str(s) for s in args[0]], args[1:]
    else:
        strs = [a for a in args if isinstance(a, str)]
        rest = [a for a in args if not isinstance(a, str)]
    timeout = kwargs.get("timeout", rest[0] if len(rest) > 0 and not isinstance(rest[0], bool) else 0)
    ci = kwargs.get("bCaseInsensitive", rest[1] if len(rest) > 1 else (rest[0] if rest and isinstance(rest[0], bool) else False))
    return strs, float(timeout or 0), bool(ci)


class Screen:
    def __init__(self, tab):
        self._tab = tab
        self.Synchronous = False
        self.IgnoreEscape = False
        self.IgnoreCase = False
        self.MatchIndex = 0

    def Send(self, text, *_):
        _bridge.call("send", tab=self._tab.Id, text=str(text))

    SendKeys = Send

    def SendSpecial(self, name):
        keys = {"VT_ENTER": "\r", "VK_RETURN": "\r", "VK_TAB": "\t", "VK_ESCAPE": "\x1b", "VK_BACK": "\x7f"}
        if name in ("TN_BREAK", "SSH_BREAK", "SERIAL_BREAK", "MENU_SEND_BREAK"):
            _bridge.call("break", tab=self._tab.Id)
        elif name in keys:
            self.Send(keys[name])

    def _wait(self, strs, timeout, ci):
        r = _bridge.call("wait", tab=self._tab.Id, patterns=strs, timeout=timeout, ci=ci or self.IgnoreCase)
        self.MatchIndex = r["index"] + 1 if r["index"] >= 0 else 0
        return r

    def WaitForString(self, s, timeout=0, bCaseInsensitive=False):
        return self._wait([str(s)], float(timeout or 0), bCaseInsensitive)["index"] == 0

    def WaitForStrings(self, *args, **kwargs):
        strs, timeout, ci = _split(args, kwargs)
        return self._wait(strs, timeout, ci)["index"] + 1

    def ReadString(self, *args, **kwargs):
        strs, timeout, ci = _split(args, kwargs)
        r = self._wait(strs, timeout, ci)
        return r["text"] if r["index"] >= 0 else ""

    def WaitForCursor(self, timeout=0):
        return _bridge.call("waitany", tab=self._tab.Id, timeout=float(timeout or 0))

    WaitForKey = WaitForCursor

    def _screen(self):
        return _bridge.call("screen", tab=self._tab.Id)

    def _lines(self):
        s = self._screen()
        rows = s["rows"] or 24
        if s.get("lines") is not None:  # the rendered screen
            return s["lines"], rows, s["cols"] or 80
        lines = s["text"].replace("\r", "").split("\n")[-rows:]  # no terminal view: recent output
        return lines, rows, s["cols"] or 80

    @property
    def Rows(self):
        return self._lines()[1]

    @property
    def Columns(self):
        return self._lines()[2]

    @property
    def CurrentRow(self):
        s = self._screen()
        if s.get("row"):
            return s["row"]
        return len(self._lines()[0])

    @property
    def CurrentColumn(self):
        s = self._screen()
        if s.get("col"):
            return s["col"]
        return len(self._lines()[0][-1]) + 1

    def Get(self, row1, col1, row2, col2):
        lines, rows, cols = self._lines()
        lines = lines + [""] * (rows - len(lines))
        out = []
        for r in range(int(row1), int(row2) + 1):
            line = lines[r - 1] if 0 < r <= len(lines) else ""
            out.append(line.ljust(cols)[int(col1) - 1:int(col2)])
        return "".join(out)

    def Get2(self, row1, col1, row2, col2):
        lines, rows, cols = self._lines()
        lines = lines + [""] * (rows - len(lines))
        return "\r\n".join((lines[r - 1] if 0 < r <= len(lines) else "").ljust(cols)[int(col1) - 1:int(col2)]
                           for r in range(int(row1), int(row2) + 1))

    def Clear(self):
        pass


class Session:
    def __init__(self, tab):
        self._tab = tab
        self.LogFileName = ""

    def _info(self):
        return _bridge.call("session", tab=self._tab.Id)

    @property
    def Connected(self):
        return bool(_bridge.call("connected", tab=self._tab.Id))

    @property
    def Logging(self):
        return self._info()["logging"]

    @property
    def Path(self):
        return self._info()["path"]

    @property
    def RemoteAddress(self):
        return self._info()["host"]

    @property
    def RemotePort(self):
        return self._info()["port"]

    def Connect(self, cmd="", *_):
        """Connect with SecureCRT command-line arguments, e.g. "/S Lab/sw1" or
        "/SSH2 /L admin /P 22 10.0.0.1". The script then works with the new tab."""
        tid = _bridge.call("connect", cmd=str(cmd))
        self._tab._rebind(tid)
        return self

    def ConnectInTab(self, cmd="", *_):
        return Tab(_bridge.call("connect", cmd=str(cmd)))

    def Disconnect(self):
        _bridge.call("disconnect", tab=self._tab.Id)

    def Log(self, start, append=False, raw=False):
        f = _bridge.call("log", tab=self._tab.Id, start=bool(start), append=bool(append), raw=bool(raw), file=self.LogFileName)
        if f:
            self.LogFileName = f


class Tab:
    def __init__(self, tid):
        self.Id = tid
        self.Screen = Screen(self)
        self.Session = Session(self)

    def _rebind(self, tid):
        self.Id = tid

    @property
    def Caption(self):
        return _bridge.call("session", tab=self.Id)["caption"]

    @property
    def Index(self):
        return _bridge.call("tabs").index(self.Id) + 1

    def Activate(self):
        _bridge.call("activate", tab=self.Id)

    def Close(self):
        _bridge.call("close", tab=self.Id)


_IDS = {"OK": 1, "Cancel": 2, "Abort": 3, "Retry": 4, "Ignore": 5, "Yes": 6, "No": 7}


class Dialog:
    def Prompt(self, message, title="", default="", isPassword=False):
        r = _bridge.call("prompt", message=str(message), title=str(title), default=str(default), secret=bool(isPassword))
        return "" if r is None else r

    def MessageBox(self, message, title="", buttons=0):
        return _bridge.call("msgbox", message=str(message), title=str(title), buttons=int(buttons))

    def FileOpenDialog(self, title="Open", buttonLabel="Open", defaultFilename="", filter=""):
        return self.Prompt("File path:", title, defaultFilename)

    def FileSaveDialog(self, title="Save", buttonLabel="Save", defaultFilename="", filter=""):
        return self.Prompt("File path:", title, defaultFilename)


class Arguments:
    def __init__(self, args):
        self._a = list(args)

    @property
    def Count(self):
        return len(self._a)

    def GetArg(self, i):
        return self._a[i]

    def __len__(self):
        return len(self._a)

    def __getitem__(self, i):
        return self._a[i]

    def __iter__(self):
        return iter(self._a)


class Window:
    Caption = "Termwright"
    State = 0

    def Activate(self):
        pass

    def Show(self, *_):
        pass


class Crt:
    def __init__(self, tab, args, script):
        self._tab = Tab(tab)
        self.Dialog = Dialog()
        self.Window = Window()
        self.Arguments = Arguments(args)
        self.ScriptFullName = script
        self.Version = _bridge.call("version")

    @property
    def Screen(self):
        return self._tab.Screen

    @property
    def Session(self):
        return self._tab.Session

    def Sleep(self, ms):
        _bridge.call("sleep", ms=int(ms))

    def GetScriptTab(self):
        return self._tab

    def GetActiveTab(self):
        return self._tab

    def GetTabCount(self):
        return len(_bridge.call("tabs"))

    def GetTab(self, i):
        return Tab(_bridge.call("tabs")[int(i) - 1])

    def Quit(self):
        raise QuitScript()


CONSTANTS = {
    "BUTTON_OK": 0, "BUTTON_OKCANCEL": 1, "BUTTON_CANCEL": 1, "BUTTON_ABORTRETRYIGNORE": 2, "BUTTON_YESNOCANCEL": 3,
    "BUTTON_YESNO": 4, "BUTTON_RETRYCANCEL": 5, "ICON_STOP": 16, "ICON_QUESTION": 32, "ICON_WARN": 48, "ICON_INFO": 64,
    "DEFBUTTON1": 0, "DEFBUTTON2": 256, "DEFBUTTON3": 512,
    "IDOK": 1, "IDCANCEL": 2, "IDABORT": 3, "IDRETRY": 4, "IDIGNORE": 5, "IDYES": 6, "IDNO": 7,
}
