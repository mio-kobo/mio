#!/usr/bin/env bash
CI_MODULE=kiso/reactor
source "$(dirname "$0")/../../../scripts/ci/common.sh"
ci_module() {
  ci_native native bunko/kiso/reactor
}
ci_main "$@"
