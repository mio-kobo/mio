#!/usr/bin/env bash
CI_MODULE=kiso/ffi/linux
source "$(dirname "$0")/../../../../scripts/ci/common.sh"
ci_module() {
  ci_native native bunko/kiso/ffi/linux
}
ci_main "$@"
