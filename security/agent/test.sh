#!/bin/bash

# Test running an application using the agent.

source "$(go list -f {{.Dir}} veyron.io/veyron/shell/lib)/shell_test.sh"

readonly WORKDIR="${shell_test_WORK_DIR}"

build() {
  AGENTD_BIN="$(shell_test::build_go_binary 'veyron.io/veyron/veyron/security/agent/agentd')"
  PINGPONG_BIN="$(shell_test::build_go_binary 'veyron.io/veyron/veyron/security/agent/pingpong')"
}

main() {
  local RESULT

  cd "${WORKDIR}"
  build


  shell_test::setup_server_test || shell_test::fail "line ${LINENO} failed to setup server test"
  export VEYRON_CREDENTIALS="$(shell::tmp_dir)"

  # Test running a single app.
  shell_test::start_server "${PINGPONG_BIN}" --server
  "${AGENTD_BIN}" --v=4 "${PINGPONG_BIN}" || shell_test::fail "line ${LINENO}: failed to run pingpong"
  local -r CREDENTIALS_UNDER_AGENT=$("${AGENTD_BIN}" bash -c 'echo ${VEYRON_CREDENTIALS}')
  if [[ "${CREDENTIALS_UNDER_AGENT}" != "" ]]; then
      shell_test::fail "line ${LINENO}: VEYRON_CREDENTIALS should not be set when running under the agent(${CREDENTIALS_UNDER_AGENT})"
  fi

  # Test running multiple apps connecting to the same agent.
  # Make sure the testchild.sh script get the same shell_test_BIN_DIR as the main script.
  export shell_test_BIN_DIR="${shell_test_BIN_DIR}"
  RESULT=$(shell::check_result "${AGENTD_BIN}" bash "$(go list -f {{.Dir}} veyron.io/veyron/veyron/security/agent)/testchild.sh")
  shell_test::assert_eq "${RESULT}" "0" "${LINENO}"

  shell_test::pass
}

main "$@"
