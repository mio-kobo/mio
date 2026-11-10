#!/usr/bin/env bash
CI_MODULE=kiso/aot/machox64
source "$(dirname "$0")/../../../../scripts/ci/common.sh"
ci_module() {
  ci_native_and_crosscheck native bunko/kiso/aot/machox64 independent "$PYTHON" bunko/kiso/aot/machox64/check_machox64.py
}
ci_main "$@"
