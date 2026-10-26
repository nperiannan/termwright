#!/usr/bin/env bash
# Follows the GitHub Actions build of a tag or branch until it ends and shows
# every job with its current step.
#   scripts/watch-build.sh v0.12.3     # a release tag
#   scripts/watch-build.sh             # the current branch (main)
# Uses the GitHub CLI when it is installed and signed in (live view), and the
# public GitHub API otherwise (no sign-in; refreshed every 90 s).
set -uo pipefail
REPO=${GH_REPO:-nperiannan/termwright}
REF=${1:-$(git rev-parse --abbrev-ref HEAD 2>/dev/null || echo main)}

if command -v gh >/dev/null 2>&1 && gh auth status >/dev/null 2>&1; then
  id=""
  for _ in $(seq 1 24); do # the run appears a few seconds after the push
    id=$(gh run list -R "$REPO" --workflow build --branch "$REF" --limit 1 --json databaseId -q '.[0].databaseId' 2>/dev/null)
    [ -n "$id" ] && break
    echo "waiting for the build of $REF to start…"; sleep 5
  done
  [ -n "$id" ] || { echo "no build found for $REF"; exit 1; }
  gh run watch -R "$REPO" "$id" --interval 10 --exit-status
  exit $?
fi

command -v python3 >/dev/null 2>&1 || { echo "Install the GitHub CLI (brew install gh; gh auth login) or python3."; exit 1; }
API="https://api.github.com/repos/$REPO/actions"
while :; do
  run=$(curl -fsS "$API/workflows/build.yml/runs?branch=$REF&per_page=1" 2>/dev/null) || { echo "GitHub not reachable, retrying…"; sleep 30; continue; }
  id=$(printf '%s' "$run" | python3 -c 'import json,sys; r=json.load(sys.stdin)["workflow_runs"]; print(r[0]["id"] if r else "")')
  if [ -z "$id" ]; then echo "waiting for the build of $REF to start…"; sleep 15; continue; fi
  jobs=$(curl -fsS "$API/runs/$id/jobs?per_page=50" 2>/dev/null) || { sleep 30; continue; }
  clear 2>/dev/null || true
  RUN="$run" JOBS="$jobs" python3 - <<'PY'
import datetime, json, os
run = json.loads(os.environ["RUN"])["workflow_runs"][0]
jobs = json.loads(os.environ["JOBS"])["jobs"]
icon = {"success": "OK  ", "failure": "FAIL", "cancelled": "STOP", "skipped": "skip", None: "..  "}
now = datetime.datetime.now(datetime.timezone.utc)
def when(t):
    return datetime.datetime.fromisoformat(t.replace("Z", "+00:00")) if t else now
print("Build #%s  %s  %s  %s %s" % (run["run_number"], run["head_branch"], run["head_sha"][:7], run["status"], run["conclusion"] or ""))
print(run["html_url"])
print()
for j in jobs:
    steps = j.get("steps") or []
    step = next((s["name"] for s in steps if s["status"] == "in_progress"), "")
    if j["conclusion"] == "failure":
        step = "failed in: " + next((s["name"] for s in steps if s["conclusion"] == "failure"), "?")
    took = ""
    if j["started_at"]:
        sec = int((when(j["completed_at"]) - when(j["started_at"])).total_seconds())
        took = "%dm%02ds" % (sec // 60, sec % 60)
    print("  %s  %-58s %7s  %s" % (icon.get(j["conclusion"], j["conclusion"]), j["name"][:58], took, step))
PY
  status=$(printf '%s' "$run" | python3 -c 'import json,sys; r=json.load(sys.stdin)["workflow_runs"][0]; print(r["status"], r["conclusion"] or "")')
  case "$status" in
    "completed success") echo; echo "Build finished successfully."; exit 0 ;;
    completed*) echo; echo "Build ended: ${status#completed }"; exit 1 ;;
  esac
  echo; echo "(refreshes every 90 s - Ctrl+C to stop)"; sleep 90
done
