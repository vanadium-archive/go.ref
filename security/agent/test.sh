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

  # TODO(suharshs): After switching to new security model this shoudl be VEYRON_CREDENTIALS.
  export VEYRON_AGENT="$(shell::tmp_dir)"
  echo "-----BEGIN EC PRIVATE KEY-----
MHcCAQEEIGxO3M/KmUac35mffZAVZf0PcXj2qLj4AtTIxdSrAH1AoAoGCCqGSM49
AwEHoUQDQgAEoZ4cgHtkSzP/PeUBLIOoSJWjx7t2cAWKxj+dd3AgDct2nJujM2+c
dwwYdDyKjhBc2nacEDlvA3AqtCMU0c97hA==
-----END EC PRIVATE KEY-----" > "${VEYRON_AGENT}/privatekey.pem"

  # TODO(ashankar): Remove this block (and remove the compilation of the identity tool)
  # once the agent has been updated to comply with the new security model.
  local -r ID=$(shell::tmp_file)
  "${IDENTITY_BIN}" generate agenttest >"${ID}" || shell_test::fail "line ${LINENO}: failed to run identity"
  export VEYRON_IDENTITY="${ID}"

  shell_test::setup_server_test || shell_test::fail "line ${LINENO} failed to start server"
  unset VEYRON_CREDENTIALS

  # Test running a single app.
  shell_test::start_server "${PINGPONG_BIN}" --server
  "${AGENTD_BIN}" --v=4 "${PINGPONG_BIN}" || shell_test::fail "line ${LINENO}: failed to run pingpong"
  local -r OUTPUT=$(shell::tmp_file)
  RESULT=$(shell::check_result echo_identity "${OUTPUT}")
  shell_test::assert_eq "${RESULT}" "0" "${LINENO}"
  if [[ ! -s "${OUTPUT}" ]]; then
      shell_test::fail "line ${LINENO}: identity preserved"
  fi

  # Test running multiple apps connecting to the same agent.
  # Make sure the testchild.sh script get the same shell_test_BIN_DIR as the main script.
  export shell_test_BIN_DIR="${shell_test_BIN_DIR}"
  RESULT=$(shell::check_result "${AGENTD_BIN}" bash "${VEYRON_ROOT}/veyron/go/src/veyron.io/veyron/veyron/security/agent/testchild.sh")
  shell_test::assert_eq "${RESULT}" "0" "${LINENO}"

  shell_test::pass
}

main "$@"
