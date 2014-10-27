#!/bin/bash

# Test running an application using the agent.

source "${VEYRON_ROOT}/scripts/lib/shell_test.sh"

readonly WORKDIR="${shell_test_WORK_DIR}"

build() {
  AGENTD_BIN="$(shell_test::build_go_binary 'veyron.io/veyron/veyron/security/agent/agentd')"
  PINGPONG_BIN="$(shell_test::build_go_binary 'veyron.io/veyron/veyron/security/agent/test' 'pingpong')"
  IDENTITY_BIN="$(shell_test::build_go_binary 'veyron.io/veyron/veyron/tools/identity')"
}

echo_identity() {
  local -r OUTPUT="$1"
  "${AGENTD_BIN}" bash -c 'echo ${VEYRON_IDENTITY}' > "${OUTPUT}"
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
  local -r OUTPUT=$(shell::tmp_file)
  RESULT=$(shell::check_result echo_identity "${OUTPUT}")
  shell_test::assert_eq "${RESULT}" "0" "${LINENO}"
  if [[ ! -s "${OUTPUT}" ]]; then
      shell_test::fail "line ${LINENO}: credentials preserved"
  fi

  # Test running multiple apps connecting to the same agent.
  # Make sure the testchild.sh script get the same shell_test_BIN_DIR as the main script.
  export shell_test_BIN_DIR="${shell_test_BIN_DIR}"
  RESULT=$(shell::check_result "${AGENTD_BIN}" bash "${VEYRON_ROOT}/veyron/go/src/veyron.io/veyron/veyron/security/agent/testchild.sh")
  shell_test::assert_eq "${RESULT}" "0" "${LINENO}"

  shell_test::pass
}

main "$@"
