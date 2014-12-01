#!/bin/bash

# Test the node manager and related services and tools.

source "${VEYRON_ROOT}/scripts/lib/shell_test.sh"

readonly WORKDIR="${shell_test_WORK_DIR}"

build() {
  BINARYD_BIN="$(shell_test::build_go_binary 'veyron.io/veyron/veyron/services/mgmt/binary/binaryd')"
  BINARY_BIN="$(shell_test::build_go_binary 'veyron.io/veyron/veyron/tools/binary')"
  AGENTD_BIN="$(shell_test::build_go_binary 'veyron.io/veyron/veyron/security/agent/agentd')"
  SUIDHELPER_BIN="$(shell_test::build_go_binary 'veyron.io/veyron/veyron/services/mgmt/suidhelper')"
  NODEMANAGER_BIN="$(shell_test::build_go_binary 'veyron.io/veyron/veyron/services/mgmt/node/noded')"
  NMINSTALL_SCRIPT="$(shell::go_package_dir 'veyron.io/veyron/veyron/tools/mgmt')/nminstall"
}

main() {
  cd "${WORKDIR}"
  build

  BIN_STAGING_DIR=$(shell::tmp_dir)
  cp "${AGENTD_BIN}" "${SUIDHELPER_BIN}" "${NODEMANAGER_BIN}" "${BIN_STAGING_DIR}"
  shell_test::setup_server_test
  # Unset VEYRON_CREDENTIALS set in setup_server_test.
  export VEYRON_CREDENTIALS=

  # TODO(caprita): Expose an option to turn --single_user off, so we can run
  # test.sh by hand and exercise the code that requires root privileges.

  # TODO(caprita): Uncomment when we're ready to use node manager.
  # "${NMINSTALL_SCRIPT}" --single_user $(shell::tmp_dir) "${BIN_STAGING_DIR}" &

  # TODO(caprita): Fill in the rest.
  shell_test::pass
}

main "$@"
