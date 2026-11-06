#!/usr/bin/env bash
CI_MODULE=kiso/fs
source "$(dirname "$0")/../../../scripts/ci/common.sh"
ci_module() {
  ci_native native bunko/kiso/fs
}
ci_main "$@"
