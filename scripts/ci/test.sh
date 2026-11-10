#!/usr/bin/env bash
CI_MODULE=quality
source "$(dirname "$0")/common.sh"
ci_module() {
  local result=0
  ci_check command quality bash scripts/ci/quality.sh || result=$?
  ci_check command infrastructure "$PYTHON" scripts/ci/selftest.py || result=$?
  ci_check command examples-driver "$PYTHON" scripts/ci/test_examples_native.py || result=$?
  ci_check command release-isolation "$PYTHON" scripts/ci/release_isolation.py || result=$?
  ci_check command bunko-coverage "$PYTHON" scripts/ci/bunko_matrix.py --check || result=$?
  ci_check command i18n-catalog "$PYTHON" scripts/i18n/catalog.py check --check-generated || result=$?
  ci_check command i18n-source "$PYTHON" scripts/i18n/test_catalog.py || result=$?
  return "$result"
}
ci_main "$@"
