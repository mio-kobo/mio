#!/usr/bin/env bash
CI_MODULE=go-reference
source "$(dirname "$0")/../scripts/ci/common.sh"
ci_module() {
  local result=0
  case "$2" in
    quick|unit) ci_check go core-unit go test -json -p 2 ./internal/lexer ./internal/parser ./internal/resolver ./internal/typecheck ./internal/compiler ./internal/code ./internal/vm ./internal/mochi ./internal/tester ./internal/bunko ./internal/bunkopm ./internal/playground ./cmd/mio -count=1 -timeout 10m || result=$? ;;
    full)
      # Existing Go bootstrap tests temporarily write this source-side fixture.
      # Restore it even when the check runner times out or cancels the child.
      cp selfhost/bootstrap/bootstrap.config "$MIO_CI_MODULE_DIR/bootstrap.config.original"
      trap 'cp "$MIO_CI_MODULE_DIR/bootstrap.config.original" "$CI_ROOT/selfhost/bootstrap/bootstrap.config"' EXIT
      trap 'exit 143' TERM HUP
      trap 'exit 130' INT
      ci_check go vm-regression go test -json -p 2 ./internal/vm/... ./internal/mochi/... -count=1 -timeout 30m || result=$?
      # The Go bootstrap self-compilation (Mio/selfhost/bootstrap) takes about an hour by itself. It has its own
      # long-running module, go-bootstrap-reference, which Full runs as a separate job, so that this check, and the
      # feedback of every other package, does not wait for it. Nothing else is excluded.
      # Plain read loop: macOS ships bash 3.2 (no bash 4 array builtins). A failing go list or an empty package list fails.
      local listing="" package="" packages=()
      if listing=$(go list ./... | grep -v '^Mio/selfhost/bootstrap$'); then
        while IFS= read -r package; do
          if [ -n "$package" ]; then packages+=("$package"); fi
        done <<< "$listing"
      fi
      if [ "${#packages[@]}" -eq 0 ]; then
        echo "go repository: no packages listed" >&2; result=1
      else
        MIO_CI_CHECK_TIMEOUT=6600 ci_check go repository go test -json -p 2 "${packages[@]}" -count=1 -timeout 60m || result=$?
      fi ;;
  esac
  ci_check command vm-independent "$PYTHON" scripts/ci/verify.py vm || result=$?
  return "$result"
}
ci_main "$@"
