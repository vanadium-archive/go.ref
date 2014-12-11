#!/bin/bash

# Test running an application using vrun under the agent.

source "${VEYRON_ROOT}/scripts/lib/shell_test.sh"

readonly WORKDIR="${shell_test_WORK_DIR}"

build() {
  AGENTD_BIN="$(shell_test::build_go_binary 'veyron.io/veyron/veyron/security/agent/agentd')"
}

main() {
  cd "${WORKDIR}"
  build

  export VEYRON_CREDENTIALS="$(shell::tmp_dir)"

  # Make sure the testchild.sh script gets the same shell_test_BIN_DIR as the main script.
  export shell_test_BIN_DIR
  "${AGENTD_BIN}" --no_passphrase --additional_principals="$(shell::tmp_dir)" bash "${VEYRON_ROOT}/veyron/go/src/veyron.io/veyron/veyron/tools/vrun/testchild.sh" || shell_test::fail "${LINENO}: testchild.sh failed"

  shell_test::pass
}

main "$@"
