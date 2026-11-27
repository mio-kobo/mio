#!/usr/bin/env bash
CI_MODULE=selfhost
source "$(dirname "$0")/../scripts/ci/common.sh"
ci_module() {
  ci_native_and_crosscheck selfhost-native selfhost/... native-lsp-protocol \
    "$PYTHON" tests/lsp/protocol.py --native "$MIO_CI_MIO"
}
ci_main "$@"
