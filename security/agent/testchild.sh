#!/bin/bash

# Helper script for testing two binaries under the same agent.

source "${VEYRON_ROOT}/environment/scripts/lib/shell_test.sh"


main() {
  if [[ -n "${VEYRON_IDENTITY}" ]]; then
      shell_test::fail "line ${LINENO}: identity preserved"
  fi
  shell_test::start_server ./pingpong --server
  ./pingpong || shell_test::fail "line ${LINENO}: ping"

  shell_test::pass
}

main "$@"
