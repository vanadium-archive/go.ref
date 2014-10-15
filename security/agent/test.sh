#!/bin/bash

# Test running an application using the agent.

source "${VEYRON_ROOT}/scripts/lib/shell_test.sh"

readonly  WORKDIR="$(shell::tmp_dir)"

build() {
  veyron go build veyron.io/veyron/veyron/security/agent/agentd || shell_test::fail "line ${LINENO}: failed to build agentd"
  veyron go build -o pingpong veyron.io/veyron/veyron/security/agent/test || shell_test::fail "line ${LINENO}: failed to build pingpong"
  veyron go build veyron.io/veyron/veyron/tools/identity || shell_test::fail "line ${LINENO}: failed to build identity"
}

echo_identity() {
  local -r OUTPUT="$1"
  ./agentd bash -c 'echo ${VEYRON_IDENTITY}' > "${OUTPUT}"
}

main() {
  local RESULT

  cd "${WORKDIR}"
  build

  # TODO(ashankar): Remove this block (and remove the compilation of the identity tool)
  # once the agent has been updated to comply with the new security model.
  local -r ID=$(shell::tmp_file)
  ./identity generate agenttest >"${ID}" || shell_test::fail "line ${LINENO}: failed to run identity"
  export VEYRON_IDENTITY="${ID}"

  shell_test::setup_server_test || shell_test::fail "line ${LINENO} failed to start server"
  unset VEYRON_CREDENTIALS

  # Test running a single app.
  shell_test::start_server ./pingpong --server
  export VEYRON_PUBLICID_STORE="$(shell::tmp_dir)"
  ./agentd --v=4 ./pingpong || shell_test::fail "line ${LINENO}: failed to run pingpong"
  local -r OUTPUT=$(shell::tmp_file)
  RESULT=$(shell::check_result echo_identity "${OUTPUT}")
  shell_test::assert_eq "${RESULT}" "0" "${LINENO}"
  if [[ ! -s "${OUTPUT}" ]]; then
      shell_test::fail "line ${LINENO}: identity preserved"
  fi

  # Test running multiple apps connecting to the same agent.
  RESULT=$(shell::check_result ./agentd bash "${VEYRON_ROOT}/veyron/go/src/veyron.io/veyron/veyron/security/agent/testchild.sh")
  shell_test::assert_eq "${RESULT}" "0" "${LINENO}"

  shell_test::pass
}

main "$@"
