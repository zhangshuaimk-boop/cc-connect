#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'USAGE'
Usage:
  tools/worktree-setup.sh [cc-connect-worktree-path]

Initializes a cc-connect git worktree after it is created.

This script does not start, stop, restart, or replace any cc-connect daemon.
USAGE
}

if [[ "${1:-}" == "-h" || "${1:-}" == "--help" ]]; then
  usage
  exit 0
fi

worktree="${1:-$PWD}"
root="$(git -C "$worktree" rev-parse --show-toplevel 2>/dev/null)"

if [[ ! -f "$root/go.mod" ]]; then
  echo "ERROR: go.mod not found under $root" >&2
  exit 1
fi

module="$(sed -n 's/^module //p' "$root/go.mod" | head -1)"
if [[ "$module" != "github.com/chenhg5/cc-connect" ]]; then
  echo "ERROR: $root is not cc-connect (module=$module)" >&2
  exit 1
fi

if [[ "$root" == "/Users/bytedance/code/cc-connect" && "${CC_ALLOW_MAIN_CHECKOUT:-}" != "1" ]]; then
  echo "ERROR: refusing to initialize the main checkout as a worktree: $root" >&2
  echo "Set CC_ALLOW_MAIN_CHECKOUT=1 only for an intentional main-checkout run." >&2
  exit 1
fi

echo "cc-connect worktree: $root"
echo "branch: $(git -C "$root" branch --show-current || true)"

mkdir -p "$root/.tmp/go-cache"

echo "Downloading Go modules..."
GOCACHE=/private/tmp/cc-connect-go-cache go -C "$root" mod download

if [[ -f "$root/web/package.json" ]]; then
  echo "Installing web dependencies..."
  if [[ -f "$root/web/package-lock.json" ]]; then
    npm --prefix "$root/web" ci
  else
    npm --prefix "$root/web" install
  fi

  echo "Building web/dist for Go embed tests and full build..."
  npm --prefix "$root/web" run build
fi

cat <<EOF

Worktree setup complete.

Recommended validation:
  cd "$root"
  GOCACHE=/private/tmp/cc-connect-go-cache go test ./...
  npm --prefix web test
  GOCACHE=/private/tmp/cc-connect-go-cache make build

Worktree-local gate:
  make test-worktree-local
EOF
