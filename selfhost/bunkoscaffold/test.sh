#!/usr/bin/env bash
CI_MODULE=bunko-scaffold
source "$(dirname "$0")/../../scripts/ci/common.sh"
ci_module() {
  local result=0
  ci_native scaffold-native selfhost/bunkoscaffold || result=$?
  ci_check go scaffold-reference go test -json ./internal/bunkoscaffold -count=1 || result=$?
  ci_check command scaffold-assets "$PYTHON" scripts/bunko/generate.py --check || result=$?
  return "$result"
}
ci_main "$@"
