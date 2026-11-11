#!/usr/bin/env bash
CI_MODULE=kiso/net/tcp-async
source "$(dirname "$0")/../../../../scripts/ci/common.sh"
ci_module() {
  ci_native native bunko/kiso/net/tcp_async
}
ci_main "$@"
