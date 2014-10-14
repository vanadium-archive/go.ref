#!/bin/bash

# Test running an application using the agent.

source "${VEYRON_ROOT}/scripts/lib/shell_test.sh"

build() {
  veyron go build veyron.io/veyron/veyron/security/agent/agentd || shell_test::fail "line ${LINENO}: failed to build agentd"
  veyron go build -o pingpong veyron.io/veyron/veyron/security/agent/test || shell_test::fail "line ${LINENO}: failed to build pingpong"
  veyron go build veyron.io/veyron/veyron/tools/identity || shell_test::fail "line ${LINENO}: failed to build identity"
}

main() {
  local workdir="$(shell::tmp_dir)"
  cd "${workdir}"
  build

  # TODO(ashankar): Remove this block (and remove the compilation of the identity tool)
  # once the agent has been updated to comply with the new security model.
  local -r ID=$(shell::tmp_file)
  ./identity generate agenttest >"${ID}"
  export VEYRON_IDENTITY="${ID}"

  shell_test::setup_server_test || shell_test::fail "line ${LINENO} failed to start server"
  unset VEYRON_CREDENTIALS

  # Test running a single app.
  shell_test::start_server ./pingpong --server
  export VEYRON_PUBLICID_STORE="$(shell::tmp_dir)"
  echo VEYRON_PUBLICID_STORE=$VEYRON_PUBLICID_STORE
  ls $VEYRON_PUBLICID_STORE
  ./agentd --v=4 ./pingpong || shell_test::fail "line ${LINENO}: ping"
  local identity=$(./agentd bash -c 'echo $VEYRON_IDENTITY')
  if [[ -n "${identity}" ]]; then
      shell_test::fail "line ${LINENO}: identity preserved"
  fi

  # Test running multiple apps connecting to the same agent.
  exec ./agentd bash ${VEYRON_ROOT}/veyron/go/src/veyron.io/veyron/veyron/security/agent/testchild.sh
}

main "$@"
