#!/usr/bin/env bash
# Prints the release notes of a release tag (vX.Y.Z) or of the nightly build:
# its section of CHANGELOG.md (the "Unreleased" section for nightly builds and
# for a tag without its own section) followed by the commits since the
# previous release. Used by .github/workflows/build.yml and release-notes.yml.
set -euo pipefail
REF=${1:?usage: release-notes.sh vX.Y.Z | nightly}
REPO=${GH_REPO:-nperiannan/termwright}
cd "$(git rev-parse --show-toplevel)"

# section NAME: the lines under "## NAME …" up to the next "## " heading,
# without images and with links made absolute (release pages have no relative paths)
section() {
  awk -v v="$1" '/^## /{p=($2==v); next} p' CHANGELOG.md 2>/dev/null |
    sed -e '/^[[:space:]]*!\[/d' \
        -e "s#](docs/#](https://github.com/$REPO/blob/main/docs/#g" \
        -e "s#](CHANGELOG.md)#](https://github.com/$REPO/blob/main/CHANGELOG.md)#g" |
    awk 'NF{blank=0; started=1} !NF{blank++} started && blank<2'
}

# GitHub refuses release notes over 125000 characters: past LIMIT the commit list gives way to the compare
# link, then the changelog section is cut at a line with a link to the whole CHANGELOG.
LIMIT=120000
MORE="**…and more** - the full list is in [CHANGELOG.md](https://github.com/$REPO/blob/$([ "$REF" = nightly ] && echo main || echo "$REF")/CHANGELOG.md)."

# notes COMMITS: the release notes; COMMITS=1 lists the commits
notes() {
  if [ "$REF" = nightly ]; then
    TO=$(git rev-parse HEAD)
    FROM=$(git describe --tags --abbrev=0 --match 'v*' HEAD 2>/dev/null || true)
    echo "Automatic build of main ($(git rev-parse --short HEAD)). **Untested** - back up your settings first (the update window has *Back Up Now*)."
    echo
    body=$(section Unreleased)
    if [ -n "$body" ]; then echo "### Changes since ${FROM:-the first release}"; echo; echo "$body"; echo; fi
  else
    TO=$REF
    FROM=$(git describe --tags --abbrev=0 --match 'v*' "$REF^" 2>/dev/null || true)
    body=$(section "${REF#v}")
    [ -n "$body" ] || body=$(section Unreleased)
    echo "${body:-Maintenance release.}"
    echo
  fi

  if [ "$1" = 1 ]; then
    echo "### Commits"
    echo
    git log --no-merges --pretty='* %s (%h)' ${FROM:+"$FROM.."}"$TO"
  fi
  if [ -n "$FROM" ]; then
    echo
    echo "**Full changelog**: https://github.com/$REPO/compare/$FROM...$([ "$REF" = nightly ] && git rev-parse --short HEAD || echo "$REF")"
  fi
  echo
  echo "macOS: the app is ad-hoc signed, not notarized - after installing run \`xattr -dr com.apple.quarantine /Applications/Termwright.app\`."
}

out=$(notes 1)
[ ${#out} -le $LIMIT ] || out=$(notes 0)
if [ ${#out} -gt $LIMIT ]; then
  out=$(printf '%s\n' "$out" | awk -v max=$((LIMIT - 1000)) -v more="$MORE" '
    { n += length($0) + 1 }
    !cut && n > max { print ""; print more; cut = 1 }
    !cut { print; next }
    /^\*\*Full changelog|^macOS:/ { print ""; print }')
fi
printf '%s\n' "$out"
