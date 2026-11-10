#!/usr/bin/env bash
CI_MODULE=kiso/aot/lowerlinux
source "$(dirname "$0")/../../../../scripts/ci/common.sh"
ci_module() {
  ci_native native bunko/kiso/aot/lowerlinux
}
ci_main "$@"
