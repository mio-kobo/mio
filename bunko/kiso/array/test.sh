#!/usr/bin/env bash
CI_MODULE=kiso/array
source "$(dirname "$0")/../../../scripts/ci/common.sh"
ci_module() {
  ci_native native bunko/kiso/array
}
ci_main "$@"
