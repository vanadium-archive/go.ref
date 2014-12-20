#!/bin/bash

# Test the device manager and related services and tools.

source "$(go list -f {{.Dir}} veyron.io/veyron/shell/lib)/shell_test.sh"

readonly WORKDIR="${shell_test_WORK_DIR}"

build() {
  BINARYD_BIN="$(shell_test::build_go_binary 'veyron.io/veyron/veyron/services/mgmt/binary/binaryd')"
  BINARY_BIN="$(shell_test::build_go_binary 'veyron.io/veyron/veyron/tools/binary')"
  APPLICATIOND_BIN="$(shell_test::build_go_binary 'veyron.io/veyron/veyron/services/mgmt/application/applicationd')"
  APPLICATION_BIN="$(shell_test::build_go_binary 'veyron.io/veyron/veyron/tools/application')"
  AGENTD_BIN="$(shell_test::build_go_binary 'veyron.io/veyron/veyron/security/agent/agentd')"
  SUIDHELPER_BIN="$(shell_test::build_go_binary 'veyron.io/veyron/veyron/services/mgmt/suidhelper')"
  DEVICEMANAGER_BIN="$(shell_test::build_go_binary 'veyron.io/veyron/veyron/services/mgmt/device/deviced')"
  DEVICE_BIN="$(shell_test::build_go_binary 'veyron.io/veyron/veyron/tools/mgmt/device')"
  NAMESPACE_BIN="$(shell_test::build_go_binary 'veyron.io/veyron/veyron/tools/namespace')"
  PRINCIPAL_BIN="$(shell_test::build_go_binary 'veyron.io/veyron/veyron/tools/principal')"
  DEBUG_BIN="$(shell_test::build_go_binary 'veyron.io/veyron/veyron/tools/debug')"
  DMINSTALL_SCRIPT="$(go list -f {{.Dir}} veyron.io/veyron/veyron/tools/mgmt/device)/dminstall"
  DMUNINSTALL_SCRIPT="$(go list -f {{.Dir}} veyron.io/veyron/veyron/tools/mgmt/device)/dmuninstall"
}

# TODO(caprita): Move to shell_tesh.sh

###############################################################################
# Waits until the given name appears in the mounttable, within a set timeout.
# Arguments:
#   path to namespace command-line tool
#   timeout in seconds
#   name to look up
# Returns:
#   0 if the name was successfully found, and 1 if the timeout expires before
#   the name appears.
###############################################################################
wait_for_mountentry() {
  local -r NAMESPACE_BIN="$1"
  local -r TIMEOUT="$2"
  local -r NAME="$3"
  for i in $(seq 1 "${TIMEOUT}"); do
    local ENTRY=$("${NAMESPACE_BIN}" glob "${NAME}")
    if [[ ! -z "${ENTRY}" ]]; then
      return 0
    fi
    sleep 1
  done
  echo "Timed out waiting for ${NAME} to appear in the mounttable."
  return 1
}

###############################################################################
# Waits until the given process is gone, within a set timeout.
# Arguments:
#   pid of process
#   timeout in seconds
# Returns:
#   0 if the pid is gone, and 1 if the timeout expires before that happens.
###############################################################################
wait_for_process_exit() {
  local -r PID="$1"
  local -r TIMEOUT="$2"
  for i in $(seq 1 "${TIMEOUT}"); do
    local RESULT=$(shell::check_result kill -0 "${PID}")
    if [[ "${RESULT}" != "0" ]]; then
      # Process is gone, can return early.
      return 0
    fi
    sleep 1
  done
  echo "Timed out waiting for PID ${PID} to disappear."
  return 1
}

main() {
  cd "${WORKDIR}"
  build

  BIN_STAGING_DIR=$(shell::tmp_dir)
  cp "${AGENTD_BIN}" "${SUIDHELPER_BIN}" "${DEVICEMANAGER_BIN}" "${BIN_STAGING_DIR}"
  shell_test::setup_server_test
  # Unset VEYRON_CREDENTIALS set in setup_server_test.
  export VEYRON_CREDENTIALS=

  # TODO(caprita): Expose an option to turn --single_user off, so we can run
  # test.sh by hand and exercise the code that requires root privileges.

  # Install and start device manager.
  DM_INSTALL_DIR=$(shell::tmp_dir)
  shell_test::start_server "${DMINSTALL_SCRIPT}" --single_user "${DM_INSTALL_DIR}" \
    "${BIN_STAGING_DIR}" -- --veyron.tcp.address=127.0.0.1:0 || shell_test::fail "line ${LINENO} failed to start device manager"
  # Dump dminstall's log, just to provide visibility into its steps.
  local -r DM_PID="${START_SERVER_PID}"
  cat "${START_SERVER_LOG_FILE}"

  local -r DM_NAME=$(hostname)
  # Verify that device manager is published under the expected name (hostname).
  shell_test::assert_ne "$("${NAMESPACE_BIN}" glob "${DM_NAME}")" "" "${LINENO}"

  # Create the client principal, "alice".
  "${PRINCIPAL_BIN}" create --overwrite=true ./alice alice >/dev/null || \
    shell_test::fail "line ${LINENO}: create alice failed"

  # All the commands executed henceforth will run as alice.
  export VEYRON_CREDENTIALS=./alice

  # Claim the device as "alice/myworkstation".
  "${DEVICE_BIN}" claim "${DM_NAME}/device" myworkstation

  # Verify the device's default blessing is as expected.
  shell_test::assert_eq "$("${DEBUG_BIN}" stats read "${DM_NAME}/__debug/stats/security/principal/blessingstore" | head -1 | sed -e 's/^.*Default blessings: '//)" \
    "alice/myworkstation" "${LINENO}"

  # Start a binary server.
  local -r BINARYD_NAME="binaryd"
  shell_test::start_server "${BINARYD_BIN}" --name="${BINARYD_NAME}" \
    --root_dir="$(shell::tmp_dir)/binstore" --veyron.tcp.address=127.0.0.1:0 --http=127.0.0.1:0 \
    || shell_test::fail "line ${LINENO} failed to start binaryd"

  # Upload a binary to the binary server.  The binary we upload is binaryd
  # itself.
  local -r SAMPLE_APP_BIN_NAME="${BINARYD_NAME}/testapp"
  "${BINARY_BIN}" upload "${SAMPLE_APP_BIN_NAME}" "${BINARYD_BIN}"

  # Verify that the binary we uploaded is shown by glob.
  shell_test::assert_eq "$("${NAMESPACE_BIN}" glob "${SAMPLE_APP_BIN_NAME}")" \
    "${SAMPLE_APP_BIN_NAME}" "${LINENO}"

  # Start an application server.
  local -r APPLICATIOND_NAME="applicationd"
  shell_test::start_server "${APPLICATIOND_BIN}" --name="${APPLICATIOND_NAME}" \
    --store="$(shell::tmp_dir)" --veyron.tcp.address=127.0.0.1:0 \
    || shell_test::fail "line ${LINENO} failed to start applicationd"

  # Upload an envelope for our test app.
  local -r SAMPLE_APP_NAME="${APPLICATIOND_NAME}/testapp/v0"
  local -r APP_PUBLISH_NAME="testbinaryd"
  echo "{\"Title\":\"BINARYD\", \"Args\":[\"--name=${APP_PUBLISH_NAME}\", \"--root_dir=./binstore\", \"--veyron.tcp.address=127.0.0.1:0\"], \"Binary\":\"${SAMPLE_APP_BIN_NAME}\", \"Env\":[]}" > ./app.envelope && \
    "${APPLICATION_BIN}" put "${SAMPLE_APP_NAME}" test ./app.envelope && rm ./app.envelope

  # Verify that the envelope we uploaded shows up with glob.
  shell_test::assert_eq "$("${APPLICATION_BIN}" match "${SAMPLE_APP_NAME}" test | grep Title | sed -e 's/^.*"Title": "'// | sed -e 's/",//')" \
    "BINARYD" "${LINENO}"

  # Install the app on the device.
  local -r INSTALLATION_NAME=$("${DEVICE_BIN}" install "${DM_NAME}/apps" "${SAMPLE_APP_NAME}" | sed -e 's/Successfully installed: "//' | sed -e 's/"//')

  # Verify that the installation shows up when globbing the device manager.
  shell_test::assert_eq "$("${NAMESPACE_BIN}" glob "${DM_NAME}/apps/BINARYD/*")" \
    "${INSTALLATION_NAME}" "${LINENO}"

  # Start an instance of the app, granting it blessing extension myapp.
  local -r INSTANCE_NAME=$("${DEVICE_BIN}" start "${INSTALLATION_NAME}" myapp | sed -e 's/Successfully started: "//' | sed -e 's/"//')
  wait_for_mountentry "${NAMESPACE_BIN}" "5" "${APP_PUBLISH_NAME}"

  # Verify that the instance shows up when globbing the device manager.
  shell_test::assert_eq "$("${NAMESPACE_BIN}" glob "${DM_NAME}/apps/BINARYD/*/*")" "${INSTANCE_NAME}" "${LINENO}"

  # Verify the app's default blessing.
  shell_test::assert_eq "$("${DEBUG_BIN}" stats read "${INSTANCE_NAME}/stats/security/principal/blessingstore" | head -1 | sed -e 's/^.*Default blessings: '//)" \
    "alice/myapp/BINARYD" "${LINENO}"

  # Stop the instance.
  "${DEVICE_BIN}" stop "${INSTANCE_NAME}"

  # Verify that logs, but not stats, show up when globbing the stopped instance.
  shell_test::assert_eq "$("${NAMESPACE_BIN}" glob "${INSTANCE_NAME}/stats/...")" "" "${LINENO}"
  shell_test::assert_ne "$("${NAMESPACE_BIN}" glob "${INSTANCE_NAME}/logs/...")" "" "${LINENO}"

  kill "${DM_PID}"
  wait_for_process_exit "${DM_PID}" 5

  "${DMUNINSTALL_SCRIPT}" --single_user "${DM_INSTALL_DIR}" \
    || shell_test::fail "line ${LINENO} failed to uninstall device manager"

  if [[ -n "$(ls -A "${DM_INSTALL_DIR}")" ]]; then
    shell_test::fail "${DM_INSTALL_DIR} is not empty"
  fi
  shell_test::pass
}

main "$@"
