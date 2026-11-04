#!/usr/bin/env bash
CI_MODULE=kiso/aot/x86-64
source "$(dirname "$0")/../../../../scripts/ci/common.sh"
ci_module() {
  local native_result=0 independent_result=0
  ci_native native bunko/kiso/aot/x86_64 || native_result=$?
  # The independent layer decodes the golden bytes with GNU objdump (Homebrew's gobjdump is found too).
  # Without a GNU objdump the layer is not run: report NOT_RUN (exit 2), never PASS.
  if "$PYTHON" bunko/kiso/aot/x86_64/check_with_objdump.py --probe; then
    ci_check command independent "$PYTHON" bunko/kiso/aot/x86_64/check_with_objdump.py || independent_result=$?
  else
    echo "NOT_RUN: independent (GNU objdump is not available)" >&2
    independent_result=2
  fi
  if (( native_result > independent_result )); then return "$native_result"; fi
  return "$independent_result"
}
ci_main "$@"
