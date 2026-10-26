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

echo "### Commits"
echo
git log --no-merges --pretty='* %s (%h)' ${FROM:+"$FROM.."}"$TO"
if [ -n "$FROM" ]; then
  echo
  echo "**Full changelog**: https://github.com/$REPO/compare/$FROM...$([ "$REF" = nightly ] && git rev-parse --short HEAD || echo "$REF")"
fi
echo
echo "macOS: the app is ad-hoc signed, not notarized - after installing run \`xattr -dr com.apple.quarantine /Applications/Termwright.app\`."
