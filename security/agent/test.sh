#!/bin/bash

# Test running an application using the agent.

source "${VEYRON_ROOT}/environment/scripts/lib/shell_test.sh"

build() {
  local GO="${REPO_ROOT}/scripts/build/go"
  "${GO}" build veyron/security/agent/agentd || shell_test::fail "line ${LINENO}: failed to build agentd"
  "${GO}" build -o pingpong veyron/security/agent/test || shell_test::fail "line ${LINENO}: failed to build pingpong"
}

main() {
  local workdir="$(shell::tmp_dir)"
  cd "${workdir}"
  build

  shell_test::setup_server_test
  shell_test::start_server ./pingpong --server
  ./agentd ./pingpong || shell_test::fail "line ${LINENO}: ping"
  local identity=$(./agentd bash -c 'echo $VEYRON_IDENTITY')
  if [[ -n "${identity}" ]]; then
      shel_test::fail "line ${LINENO}: identity preserved"
  fi

  shell_test::pass
}

main "$@"
