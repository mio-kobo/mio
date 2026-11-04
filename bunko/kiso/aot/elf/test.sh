#!/usr/bin/env bash
CI_MODULE=kiso/aot/elf
source "$(dirname "$0")/../../../../scripts/ci/common.sh"
ci_module() {
  ci_native_and_crosscheck native bunko/kiso/aot/elf independent "$PYTHON" bunko/kiso/aot/elf/check_elf.py
}
ci_main "$@"
