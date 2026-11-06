#!/usr/bin/env bash
CI_MODULE=kiso/convert
source "$(dirname "$0")/../../../scripts/ci/common.sh"
ci_module() {
  ci_native native bunko/kiso/convert
}
ci_main "$@"
