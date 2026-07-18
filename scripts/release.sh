#!/bin/bash
# Create a release tag only after the exact main commit has passed GitHub CI.
# Usage: scripts/release.sh vX.Y.Z
set -euo pipefail

VERSION="${1:-}"
if [[ ! "$VERSION" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
  echo "usage: $0 vX.Y.Z" >&2
  exit 2
fi
if [[ "$(git branch --show-current)" != "main" ]]; then
  echo "release must run from the main branch" >&2
  exit 1
fi
if [[ -n "$(git status --porcelain)" ]]; then
  echo "release requires a clean worktree" >&2
  exit 1
fi

MANIFEST_VERSION="$(awk -F '"' '/^version = / { print $2; exit }' herdr-plugin.toml)"
if [[ "$MANIFEST_VERSION" != "${VERSION#v}" ]]; then
  echo "herdr-plugin.toml version ($MANIFEST_VERSION) must match $VERSION" >&2
  exit 1
fi

git fetch --quiet origin main
HEAD_SHA="$(git rev-parse HEAD)"
if [[ "$HEAD_SHA" != "$(git rev-parse origin/main)" ]]; then
  echo "local main must match origin/main before releasing" >&2
  exit 1
fi
if git show-ref --tags --verify --quiet "refs/tags/$VERSION"; then
  echo "tag already exists locally: $VERSION" >&2
  exit 1
fi
if git ls-remote --exit-code --tags origin "refs/tags/$VERSION" >/dev/null 2>&1; then
  echo "tag already exists on origin: $VERSION" >&2
  exit 1
fi

RUN_ID="$(gh run list --repo senna-lang/herdr-agent-usage --workflow CI --branch main --commit "$HEAD_SHA" --limit 1 --json databaseId --jq '.[0].databaseId')"
if [[ -z "$RUN_ID" || "$RUN_ID" == "null" ]]; then
  echo "no CI run found for $HEAD_SHA; push main and wait for CI to start" >&2
  exit 1
fi

echo "waiting for CI run $RUN_ID on $HEAD_SHA..." >&2
gh run watch "$RUN_ID" --repo senna-lang/herdr-agent-usage --exit-status

git tag -a "$VERSION" -m "$VERSION" "$HEAD_SHA"
git push origin "$VERSION"
