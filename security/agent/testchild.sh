#!/bin/bash

# Helper script for testing two binaries under the same agent.

source "${VEYRON_ROOT}/scripts/lib/shell_test.sh"

main() {
  if [[ -n "${VEYRON_IDENTITY}" ]]; then
      shell_test::fail "line ${LINENO}: identity preserved"
  fi
  PINGPONG_BIN="$(shell_test::build_go_binary 'veyron.io/veyron/veyron/security/agent/test' 'pinpong')"
  shell_test::start_server "${PINGPONG_BIN}" --server
  "${PINGPONG_BIN}" || shell_test::fail "line ${LINENO}: ping"

  shell_test::pass
}

main "$@"
