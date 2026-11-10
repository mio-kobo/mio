#!/usr/bin/env bash
CI_MODULE=kiso/aot/winarm64
source "$(dirname "$0")/../../../../scripts/ci/common.sh"
ci_module() {
  ci_native native bunko/kiso/aot/winarm64
}
ci_main "$@"
