#!/usr/bin/env bash
CI_MODULE=kiso/aot/linuxarm64
source "$(dirname "$0")/../../../../scripts/ci/common.sh"
ci_module() {
  ci_native_and_crosscheck native bunko/kiso/aot/linuxarm64 independent "$PYTHON" bunko/kiso/aot/linuxarm64/check_linuxarm64.py
}
ci_main "$@"
