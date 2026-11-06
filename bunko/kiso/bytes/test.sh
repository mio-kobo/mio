#!/usr/bin/env bash
CI_MODULE=kiso/bytes
source "$(dirname "$0")/../../../scripts/ci/common.sh"
ci_module() {
  ci_native native bunko/kiso/bytes
}
ci_main "$@"
