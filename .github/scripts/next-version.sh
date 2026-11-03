#!/usr/bin/env bash
# Computes the next semantic version for one exporter, based only on the
# commits that touched that exporter's folder since its last release tag.
#
#   usage: next-version.sh <exporter> [major|minor|patch|auto]
#
# Tags look like  <exporter>/v<MAJOR>.<MINOR>.<PATCH>   e.g. mikrotik/v0.1.3
#
# Bump rules (Conventional Commits, scoped to the folder):
#   feat!: / fix!: / "BREAKING CHANGE"  -> major (minor while still on 0.x)
#   feat:                               -> minor
#   anything else                       -> patch
#   no previous tag                     -> 0.1.0
#
# Prints key=value lines (for $GITHUB_OUTPUT):
#   changed=true|false   whether there is anything to release
#   previous=<tag>       last release tag ("" if none)
#   version=X.Y.Z        next version (only when changed=true)
#   tag=<exporter>/vX.Y.Z
set -euo pipefail

name="${1:?exporter name required}"
forced="${2:-auto}"

last_tag="$(git tag --list "${name}/v*" --sort=-v:refname | head -n1)"
range="${last_tag:+${last_tag}..}HEAD"

# Subjects + bodies of commits that touched this exporter.
log="$(git log "$range" --format='%s%n%b' -- "${name}/")"

if [[ -z "$log" && "$forced" == "auto" ]]; then
  echo "changed=false"
  echo "previous=${last_tag}"
  exit 0
fi

current="${last_tag#"${name}/v"}"
current="${current:-0.0.0}"
IFS=. read -r major minor patch <<<"$current"

bump="$forced"
if [[ "$bump" == "auto" ]]; then
  if grep -qE '^[a-z]+(\([^)]*\))?!:|BREAKING[ -]CHANGE' <<<"$log"; then
    bump=major
  elif grep -qE '^feat(\([^)]*\))?:' <<<"$log"; then
    bump=minor
  else
    bump=patch
  fi
fi

# Pre-1.0: a breaking change only bumps minor (semver "anything goes" zone).
if [[ "$bump" == "major" && "$major" == "0" && "$forced" == "auto" ]]; then
  bump=minor
fi

if [[ -z "$last_tag" ]]; then
  # First release from this repo: start every exporter at 0.1.0.
  major=0; minor=1; patch=0
else
  case "$bump" in
  major) major=$((major + 1)); minor=0; patch=0 ;;
  minor) minor=$((minor + 1)); patch=0 ;;
  patch) patch=$((patch + 1)) ;;
  *) echo "unknown bump: $bump" >&2; exit 1 ;;
  esac
fi

version="${major}.${minor}.${patch}"
echo "changed=true"
echo "previous=${last_tag}"
echo "version=${version}"
echo "tag=${name}/v${version}"
