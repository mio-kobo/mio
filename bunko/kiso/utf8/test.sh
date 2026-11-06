#!/usr/bin/env bash
CI_MODULE=kiso/utf8
source "$(dirname "$0")/../../../scripts/ci/common.sh"
ci_module() {
  ci_native native bunko/kiso/utf8
}
ci_main "$@"
