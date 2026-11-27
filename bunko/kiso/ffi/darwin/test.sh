#!/usr/bin/env bash
CI_MODULE=kiso/ffi/darwin
source "$(dirname "$0")/../../../../scripts/ci/common.sh"
ci_module() {
  ci_native_and_crosscheck dylib-native tests/ffi/dylib/unit dylib-integration \
    "$PYTHON" tests/ffi/dylib/run.py --mio "$MIO_CI_MIO" --output "$MIO_CI_MODULE_DIR/dylib"
}
ci_main "$@"
