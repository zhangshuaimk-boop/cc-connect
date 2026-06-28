#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'USAGE'
Usage:
  tools/coverage/local_stats.sh line-ratio
  tools/coverage/local_stats.sh package-coverage [go test args...]
  tools/coverage/local_stats.sh low-functions [threshold] [coverprofile]
  tools/coverage/local_stats.sh all [threshold]

Examples:
  tools/coverage/local_stats.sh line-ratio
  GOCACHE=/private/tmp/cc-connect-go-cache tools/coverage/local_stats.sh package-coverage
  GOCACHE=/private/tmp/cc-connect-go-cache tools/coverage/local_stats.sh low-functions 50
USAGE
}

repo_root() {
  git rev-parse --show-toplevel
}

go_cache() {
  printf '%s\n' "${GOCACHE:-/private/tmp/cc-connect-go-cache}"
}

count_files() {
  local file_list
  file_list=$(mktemp)
  rg --files "$@" >"$file_list"

  if [ ! -s "$file_list" ]; then
    rm -f "$file_list"
    printf '0\n'
    return
  fi

  xargs wc -l <"$file_list" | awk '$2 != "total" { total += $1 } END { print total + 0 }'
  rm -f "$file_list"
}

line_ratio() {
  cd "$(repo_root)"

  test_lines=$(count_files -g '*_test.go')
  business_lines=$(count_files -g '*.go' -g '!*_test.go' -g '!tests/**' -g '!web/**')
  awk -v t="$test_lines" -v b="$business_lines" 'BEGIN {
    if (b == 0) {
      print "test_lines=" t
      print "business_lines=0"
      print "test:business=undefined"
      exit 1
    }
    printf "test_lines=%d\nbusiness_lines=%d\ntest:business=%.2f:1\n", t, b, t / b
  }'
}

package_coverage() {
  cd "$(repo_root)"

  if [ "$#" -eq 0 ]; then
    set -- ./...
  fi

  GOCACHE="$(go_cache)" go test -cover "$@"
}

make_coverprofile() {
  local profile=$1
  cd "$(repo_root)"
  GOCACHE="$(go_cache)" go test ./... -coverprofile="$profile"
}

low_functions() {
  cd "$(repo_root)"

  local threshold=${1:-50}
  local profile=${2:-/private/tmp/cc-connect-coverage.out}

  if ! awk -v n="$threshold" 'BEGIN { exit !(n >= 0 && n <= 100) }'; then
    printf 'threshold must be a number from 0 to 100: %s\n' "$threshold" >&2
    exit 2
  fi

  make_coverprofile "$profile" >/dev/null

  go tool cover -func="$profile" |
    awk -v threshold="$threshold" '
      $NF ~ /%$/ && $1 != "total:" {
        pct = $NF
        sub(/%$/, "", pct)
        file = $1
        sub(/:.*/, "", file)
        if (pct + 0 < threshold && file !~ /_test\.go$/ && file !~ /\/tests\// && file !~ /\/web\//) {
          print
        }
      }
    '
}

main() {
  if [ "$#" -lt 1 ]; then
    usage >&2
    exit 2
  fi

  local command=$1
  shift

  case "$command" in
    line-ratio)
      line_ratio "$@"
      ;;
    package-coverage)
      package_coverage "$@"
      ;;
    low-functions)
      low_functions "$@"
      ;;
    all)
      threshold=${1:-50}
      line_ratio
      printf '\n'
      package_coverage ./...
      printf '\nlow coverage functions below %s%%:\n' "$threshold"
      low_functions "$threshold"
      ;;
    -h|--help|help)
      usage
      ;;
    *)
      usage >&2
      exit 2
      ;;
  esac
}

main "$@"
