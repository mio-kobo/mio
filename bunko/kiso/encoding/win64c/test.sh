#!/usr/bin/env bash
CI_MODULE=kiso/encoding/win64c
source "$(dirname "$0")/../../../../scripts/ci/common.sh"
ci_module() {
  ci_native native bunko/kiso/encoding/win64c
}
ci_main "$@"
