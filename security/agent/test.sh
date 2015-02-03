#!/bin/bash

# Test running an application using the agent.

source "$(go list -f {{.Dir}} v.io/core/shell/lib)/shell_test.sh"

readonly WORKDIR="${shell_test_WORK_DIR}"

build() {
  AGENTD_BIN="$(shell_test::build_go_binary 'v.io/core/veyron/security/agent/agentd')"
  PINGPONG_BIN="$(shell_test::build_go_binary 'v.io/core/veyron/security/agent/pingpong')"
  TESTPRINCIPAL_BIN="$(shell_test::build_go_binary 'v.io/core/veyron/security/agent/test_principal')"
  VRUN="$(shell_test::build_go_binary 'v.io/core/veyron/tools/vrun')"
}

main() {
  local RESULT

  cd "${WORKDIR}"
  build


  shell_test::setup_server_test || shell_test::fail "line ${LINENO} failed to setup server test"

  # Test passphrase use
  export VEYRON_CREDENTIALS="$(shell::tmp_dir)"
  local -r LOG_DIR="$(shell::tmp_dir)"
  echo -n "agentd: passphrase..."
  # Create the passphrase
  echo "PASSWORD" | "${AGENTD_BIN}" echo "Hello" &>"${LOG_DIR}/1" || shell_test::fail "line ${LINENO}: agent failed to create passphrase protected principal: $(cat "${LOG_DIR}/1")"
  # Use it successfully
  echo "PASSWORD" | "${AGENTD_BIN}" echo "Hello" &>"${LOG_DIR}/2"  || shell_test::fail "line ${LINENO}: agent failed to use passphrase protected principal: $(cat "${LOG_DIR}/2")"
  # Wrong passphrase should fail
  echo "BADPASSWORD" | "${AGENTD_BIN}" echo "Hello" &>"${LOG_DIR}/3" && shell_test::fail "line ${LINENO}: agent should have failed with wrong passphrase"
  local -r NHELLOS="$(grep "Hello" "${LOG_DIR}"/* | wc -l)"
  if [[ "${NHELLOS}" -ne 2 ]]; then
	  shell_test::fail "line ${LINENO}: expected 2 'Hello's, got "${NHELLOS}" in $(cat "${LOG_DIR}"/*)"
  fi
  echo "OK"



  # Test all methods of the principal interface.
  # (Errors are printed to STDERR)
  export VEYRON_CREDENTIALS="$(shell::tmp_dir)"
  echo -n "agentd: test_principal..."
  "${AGENTD_BIN}" "${TESTPRINCIPAL_BIN}" >/dev/null || shell_test::fail "line ${LINENO}"
  echo "OK"

  # Use a different credentials directory for future tests
  # since test_principal "pollutes" it.
  export VEYRON_CREDENTIALS="$(shell::tmp_dir)"

  # Test running a single app.
  echo -n "agentd: single app..."
  shell_test::start_server "${PINGPONG_BIN}" --server
  "${AGENTD_BIN}" --v=4 "${PINGPONG_BIN}" || shell_test::fail "line ${LINENO}: failed to run pingpong"
  local -r CREDENTIALS_UNDER_AGENT=$("${AGENTD_BIN}" bash -c 'echo ${VEYRON_CREDENTIALS}')
  if [[ "${CREDENTIALS_UNDER_AGENT}" != "" ]]; then
    shell_test::fail "line ${LINENO}: VEYRON_CREDENTIALS should not be set when running under the agent(${CREDENTIALS_UNDER_AGENT})"
  fi
  echo "OK"

  # Test running multiple apps connecting to the same agent.
  # Make sure the testchild.sh script get the same shell_test_BIN_DIR as the main script.
  echo -n "agentd: multiple apps..."
  export shell_test_BIN_DIR="${shell_test_BIN_DIR}"
  RESULT=$(shell::check_result "${AGENTD_BIN}" bash "$(go list -f {{.Dir}} v.io/core/veyron/security/agent)/testchild.sh")
  shell_test::assert_eq "${RESULT}" "0" "${LINENO}"
  echo "OK"

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
  echo -n "agentd: restart child while its exit code is 0..."
  echo "0" > "${COUNTER_FILE}"
  RESULT=$(shell::check_result "${AGENTD_BIN}" --additional_principals="$(shell::tmp_dir)" --restart_exit_code="0" bash -c "${SCRIPT}")
  shell_test::assert_eq "${RESULT}" "1" "${LINENO}"
  [[ $(<"${COUNTER_FILE}") -eq 5 ]] || shell_test::fail "Failed"
  echo "OK"

  # Have the agent restart the child while its exit code is !0.
  echo -n "agentd: restart child while its exit code != 0..."
  echo "0" > "${COUNTER_FILE}"
  RESULT=$(shell::check_result "${AGENTD_BIN}" --additional_principals="$(shell::tmp_dir)" --restart_exit_code="!0" bash -c "${SCRIPT}")
  shell_test::assert_eq "${RESULT}" "0" "${LINENO}"
  [[ $(<"${COUNTER_FILE}") -eq 1 ]] || shell_test::fail "Failed"
  echo "OK"

  # Have the agent restart the child while its exit code is != 1.
  echo -n "agentd: restart child while its exit code != 1..."
  echo "0" > "${COUNTER_FILE}"
  RESULT=$(shell::check_result "${AGENTD_BIN}" --additional_principals="$(shell::tmp_dir)" --restart_exit_code="!1" bash -c "${SCRIPT}")
  shell_test::assert_eq "${RESULT}" "1" "${LINENO}"
  [[ $(<"${COUNTER_FILE}") -eq 5 ]] || shell_test::fail "Failed"
  echo "OK"

  shell_test::pass
}

main "$@"
