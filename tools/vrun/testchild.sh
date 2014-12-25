#!/bin/bash

# Helper script for testing vrun.

source "$(go list -f {{.Dir}} veyron.io/veyron/shell/lib)/shell_test.sh"

main() {
  shell_test::setup_server_test
  local -r PINGPONG="$(shell_test::build_go_binary 'veyron.io/veyron/veyron/security/agent/pingpong')"
  local -r VRUN="$(shell_test::build_go_binary 'veyron.io/veyron/veyron/tools/vrun')"
  local -r PRINCIPAL="$(shell_test::build_go_binary 'veyron.io/veyron/veyron/tools/principal')"

  local blessing=$("${PRINCIPAL}" dump | grep Default|cut -d: -f2)
  shell_test::assert_eq "${blessing}" " agent_principal" $LINENO
  blessing=$("${VRUN}" "${PRINCIPAL}" dump | grep Default|cut -d: -f2)
  shell_test::assert_eq "${blessing}" " agent_principal/principal" $LINENO
  blessing=$("${VRUN}" --name=foo "${PRINCIPAL}" dump | grep Default|cut -d: -f2)
  shell_test::assert_eq "${blessing}" " agent_principal/foo" $LINENO

  # TODO(ribrdb): test --duration

  shell_test::start_server "${VRUN}" "${PINGPONG}" --server
  "${VRUN}" "${PINGPONG}" || shell_test::fail "line ${LINENO}: ping"
  shell_test::pass
}

main "$@"
