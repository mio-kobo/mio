#!/usr/bin/env bash
CI_MODULE=kiso/regexp/unicode-data
source "$(dirname "$0")/../../../../scripts/ci/common.sh"
ci_module() {
  ci_native native bunko/kiso/regexp/unicode_data
}
ci_main "$@"
