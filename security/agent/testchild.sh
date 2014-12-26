#!/bin/bash

# Helper script for testing two binaries under the same agent.

source "$(go list -f {{.Dir}} v.io/veyron/shell/lib)/shell_test.sh"

main() {
  if [[ -n "${VEYRON_CREDENTIALS}" ]]; then
      shell_test::fail "line ${LINENO}: identity preserved"
  fi
  PINGPONG_BIN="$(shell_test::build_go_binary 'v.io/core/veyron/security/agent/pingpong')"
  shell_test::start_server "${PINGPONG_BIN}" --server
  "${PINGPONG_BIN}" || shell_test::fail "line ${LINENO}: ping"

  shell_test::pass
}

main "$@"
