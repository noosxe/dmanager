#!/usr/bin/env bash
# Refresh the fixed-output dependency hashes in nix/hashes.json.
#
# Run after any change to frontend/pnpm-lock.yaml, go.mod or go.sum (for
# example a Dependabot bump). Each hash is first set to a placeholder, the
# dependency derivation is built, and the hash Nix reports is written back.
set -euo pipefail

cd "$(dirname "$0")/.."

file=nix/hashes.json
fake="sha256-AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="

set_hash() { # key value
  local tmp
  tmp=$(mktemp)
  jq --arg k "$1" --arg v "$2" '.[$k] = $v' "$file" >"$tmp"
  mv "$tmp" "$file"
}

refresh() { # key attribute
  local key=$1 attr=$2 out got
  set_hash "$key" "$fake"
  # Flakes only see tracked files, so make sure the edited file is visible.
  git add --intent-to-add "$file" 2>/dev/null || true
  if out=$(nix build --no-link ".#$attr" 2>&1); then
    echo "error: build of $attr succeeded with a placeholder hash" >&2
    exit 1
  fi
  got=$(sed -n 's/^ *got: *\(sha256-[A-Za-z0-9+\/=]*\).*/\1/p' <<<"$out" | head -n1)
  if [ -z "$got" ]; then
    echo "$out" >&2
    echo "error: could not read the new $key hash from the build output" >&2
    exit 1
  fi
  set_hash "$key" "$got"
  echo "$key: $got"
}

refresh pnpm default.frontend.pnpmDeps
refresh go default.goModules
