# termwright_run.py - starts a Python script that uses the crt object inside Termwright.
# Usage: python3 termwright_run.py <script.py> [args...]
import builtins
import os
import runpy
import sys

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import termwright_crt as _t  # noqa: E402

script = os.path.abspath(sys.argv[1])
args = sys.argv[2:]
builtins.crt = _t.Crt(os.environ.get("TERMWRIGHT_TAB", ""), args, script)
for _k, _v in _t.CONSTANTS.items():
    setattr(builtins, _k, _v)
sys.argv = [script] + args
sys.path.insert(0, os.path.dirname(script))
try:
    runpy.run_path(script, run_name="__main__")
except _t.QuitScript:
    pass
