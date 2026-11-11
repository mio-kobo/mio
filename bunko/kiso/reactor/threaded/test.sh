#!/usr/bin/env bash
CI_MODULE=kiso/reactor/threaded
source "$(dirname "$0")/../../../../scripts/ci/common.sh"
ci_module() {
  ci_native native bunko/kiso/reactor/threaded
}
ci_main "$@"
