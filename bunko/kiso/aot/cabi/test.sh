#!/usr/bin/env bash
CI_MODULE=kiso/aot/cabi
source "$(dirname "$0")/../../../../scripts/ci/common.sh"
ci_module() {
  ci_native_and_crosscheck cabi-native bunko/kiso/aot/cabi cabi-clang \
    "$PYTHON" tests/ffi/darwin-arm64/run.py --mio "$MIO_CI_MIO" --output "$MIO_CI_MODULE_DIR/ffi"
}
ci_main "$@"
