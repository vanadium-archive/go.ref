#!/bin/bash

# Test running an application using the agent.

source "$(go list -f {{.Dir}} v.io/core/shell/lib)/shell_test.sh"

readonly WORKDIR="${shell_test_WORK_DIR}"

build() {
  AGENTD_BIN="$(shell_test::build_go_binary 'v.io/core/veyron/security/agent/agentd')"
  PINGPONG_BIN="$(shell_test::build_go_binary 'v.io/core/veyron/security/agent/pingpong')"
  VRUN="$(shell_test::build_go_binary 'v.io/core/veyron/tools/vrun')"
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
  RESULT=$(shell::check_result "${AGENTD_BIN}" bash "$(go list -f {{.Dir}} v.io/core/veyron/security/agent)/testchild.sh")
  shell_test::assert_eq "${RESULT}" "0" "${LINENO}"

  # Verify the restart feature.
  local -r COUNTER_FILE="${WORKDIR}/counter"
  # This script increments a counter in $COUNTER_FILE and exits with exit code 0
  # while the counter is < 5, and 1 otherwise.
  local -r SCRIPT=$(cat <<EOF
"${PINGPONG_BIN}" || exit 101
"${VRUN}" "${PINGPONG_BIN}" || exit 102
readonly COUNT=\$(expr \$(<"${COUNTER_FILE}") + 1)
echo \$COUNT > "${COUNTER_FILE}"
[[ \$COUNT -lt 5 ]]; exit \$?
EOF
  )
  # Have the agent restart the child while its exit code is 0.
  echo "0" > "${COUNTER_FILE}"
  RESULT=$(shell::check_result "${AGENTD_BIN}" --additional_principals="$(shell::tmp_dir)" --restart_exit_code="0" bash -c "${SCRIPT}")
  shell_test::assert_eq "${RESULT}" "1" "${LINENO}"
  [[ $(<"${COUNTER_FILE}") -eq 5 ]] || shell_test::fail "Failed"

  # Have the agent restart the child while its exit code is !0.
  echo "0" > "${COUNTER_FILE}"
  RESULT=$(shell::check_result "${AGENTD_BIN}" --additional_principals="$(shell::tmp_dir)" --restart_exit_code="!0" bash -c "${SCRIPT}")
  shell_test::assert_eq "${RESULT}" "0" "${LINENO}"
  [[ $(<"${COUNTER_FILE}") -eq 1 ]] || shell_test::fail "Failed"

  # Have the agent restart the child while its exit code is != 1.
  echo "0" > "${COUNTER_FILE}"
  RESULT=$(shell::check_result "${AGENTD_BIN}" --additional_principals="$(shell::tmp_dir)" --restart_exit_code="!1" bash -c "${SCRIPT}")
  shell_test::assert_eq "${RESULT}" "1" "${LINENO}"
  [[ $(<"${COUNTER_FILE}") -eq 5 ]] || shell_test::fail "Failed"

  shell_test::pass
}

main "$@"
