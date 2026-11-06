#!/usr/bin/env bash
CI_MODULE=kiso/testing
source "$(dirname "$0")/../../../scripts/ci/common.sh"
ci_module() {
  ci_native native bunko/kiso/testing
}
ci_main "$@"
