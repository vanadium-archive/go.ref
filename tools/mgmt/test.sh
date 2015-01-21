#!/bin/bash

# Test the device manager and related services and tools.

source "$(go list -f {{.Dir}} v.io/core/shell/lib)/shell_test.sh"

# Run the test under the security agent.
shell_test::enable_agent "$@"

readonly WORKDIR="${shell_test_WORK_DIR}"

build() {
  BINARYD_BIN="$(shell_test::build_go_binary 'v.io/core/veyron/services/mgmt/binary/binaryd')"
  BINARY_BIN="$(shell_test::build_go_binary 'v.io/core/veyron/tools/binary')"
  APPLICATIOND_BIN="$(shell_test::build_go_binary 'v.io/core/veyron/services/mgmt/application/applicationd')"
  APPLICATION_BIN="$(shell_test::build_go_binary 'v.io/core/veyron/tools/application')"
  AGENTD_BIN="$(shell_test::build_go_binary 'v.io/core/veyron/security/agent/agentd')"
  SUIDHELPER_BIN="$(shell_test::build_go_binary 'v.io/core/veyron/services/mgmt/suidhelper')"
  INITHELPER_BIN="$(shell_test::build_go_binary 'v.io/core/veyron/services/mgmt/inithelper')"
  DEVICEMANAGER_BIN="$(shell_test::build_go_binary 'v.io/core/veyron/services/mgmt/device/deviced')"
  DEVICE_BIN="$(shell_test::build_go_binary 'v.io/core/veyron/tools/mgmt/device')"
  NAMESPACE_BIN="$(shell_test::build_go_binary 'v.io/core/veyron/tools/namespace')"
  PRINCIPAL_BIN="$(shell_test::build_go_binary 'v.io/core/veyron/tools/principal')"
  DEBUG_BIN="$(shell_test::build_go_binary 'v.io/core/veyron/tools/debug')"
  DEVICE_SCRIPT="$(go list -f {{.Dir}} v.io/core/veyron/tools/mgmt/device)/devicex"
}

# TODO(caprita): Move to shell_tesh.sh

###############################################################################
# Waits until the given name appears in the mounttable, within a set timeout.
# Arguments:
#   path to namespace command-line tool
#   timeout in seconds
#   name to look up
#   old mount entry value (if specified, waits until a different value appears)
# Returns:
#   0 if the name was successfully found, and 1 if the timeout expires before
#   the name appears.
#   Prints the new value of the mount entry.
###############################################################################
wait_for_mountentry() {
  local -r NAMESPACE_BIN="$1"
  local -r TIMEOUT="$2"
  local -r NAME="$3"
  local -r OLD_ENTRY="${4:+}"
  for i in $(seq 1 "${TIMEOUT}"); do
    local ENTRY=$("${NAMESPACE_BIN}" resolve "${NAME}" 2>/dev/null)
    if [[ -n "${ENTRY}" && "${ENTRY}" != "${OLD_ENTRY}" ]]; then
      echo ${ENTRY}
      return 0
    fi
    sleep 1
  done
  echo "Timed out waiting for ${NAME} to have a mounttable entry different from ${OLD_ENTRY}."
  return 1
}

###############################################################################
# Waits until the given name disappears from the mounttable, within a set
# timeout.
# Arguments:
#   path to namespace command-line tool
#   timeout in seconds
#   name to look up
# Returns:
#   0 if the name was gone from the mounttable, and 1 if the timeout expires
#   while the name is still in the mounttable.
###############################################################################
wait_for_no_mountentry() {
  local -r NAMESPACE_BIN="$1"
  local -r TIMEOUT="$2"
  local -r NAME="$3"
  for i in $(seq 1 "${TIMEOUT}"); do
    local ENTRY=$("${NAMESPACE_BIN}" resolve "${NAME}" 2>/dev/null)
    if [[ -z "${ENTRY}" ]]; then
      return 0
    fi
    sleep 1
  done
  echo "Timed out waiting for ${NAME} to disappear from the mounttable."
  return 1
}

main() {
  cd "${WORKDIR}"
  build

  BIN_STAGING_DIR=$(shell::tmp_dir)
  cp "${AGENTD_BIN}" "${SUIDHELPER_BIN}" "${INITHELPER_BIN}" "${DEVICEMANAGER_BIN}" "${BIN_STAGING_DIR}"
  shell_test::setup_server_test

  # TODO(caprita): Expose an option to turn --single_user off, so we can run
  # test.sh by hand and exercise the code that requires root privileges.

  # Install and start device manager.
  DM_INSTALL_DIR=$(shell::tmp_dir)

  export VANADIUM_DEVICE_DIR="${DM_INSTALL_DIR}/dm"
  "${DEVICE_SCRIPT}" install "${BIN_STAGING_DIR}" --single_user -- --veyron.tcp.address=127.0.0.1:0
  "${VRUN}" "${DEVICE_SCRIPT}" start
  local -r DM_NAME=$(hostname)
  DM_EP=$(wait_for_mountentry "${NAMESPACE_BIN}" 5 "${DM_NAME}")

  # Verify that device manager is published under the expected name (hostname).
  shell_test::assert_ne "$("${NAMESPACE_BIN}" glob "${DM_NAME}")" "" "${LINENO}"

  # Create a self-signed blessing with name "alice" and set it as default and
  # shareable with all peers on the principal that this process is running
  # as. This blessing will be used by all commands except those running under
  # "vrun" which gets a principal forked from the process principal.
  "${PRINCIPAL_BIN}" blessself alice > alice.bless || \
    shell_test::fail "line ${LINENO}: blessself alice failed"
  "${PRINCIPAL_BIN}" store setdefault alice.bless || \
    shell_test::fail "line ${LINENO}: store setdefault failed"
  "${PRINCIPAL_BIN}" store set alice.bless ... || \
    shell_test::fail "line ${LINENO}: store set failed"

  # Claim the device as "alice/myworkstation".
  "${DEVICE_BIN}" claim "${DM_NAME}/device" myworkstation

  # Verify the device's default blessing is as expected.
  shell_test::assert_eq "$("${DEBUG_BIN}" stats read "${DM_NAME}/__debug/stats/security/principal/blessingstore" | head -1 | sed -e 's/^.*Default blessings: '//)" \
    "alice/myworkstation" "${LINENO}"

  # Get the device's profile.
  local -r DEVICE_PROFILE=$("${DEVICE_BIN}" describe "${DM_NAME}/device" | sed -r 's/\{Profiles:map\[(.+):.*/\1/g')

  # Start a binary server under the blessing "alice/myworkstation/binaryd" so that
  # the device ("alice/myworkstation") can talk to it.
  local -r BINARYD_NAME="binaryd"
  shell_test::start_server "${VRUN}" --name=myworkstation/binaryd "${BINARYD_BIN}" --name="${BINARYD_NAME}" \
    --root_dir="$(shell::tmp_dir)/binstore" --veyron.tcp.address=127.0.0.1:0 --http=127.0.0.1:0 \
    || shell_test::fail "line ${LINENO} failed to start binaryd"

  # Upload a binary to the binary server.  The binary we upload is binaryd
  # itself.
  local -r SAMPLE_APP_BIN_NAME="${BINARYD_NAME}/testapp"
  "${BINARY_BIN}" upload "${SAMPLE_APP_BIN_NAME}" "${BINARYD_BIN}"

  # Verify that the binary we uploaded is shown by glob.
  shell_test::assert_eq "$("${NAMESPACE_BIN}" glob "${SAMPLE_APP_BIN_NAME}")" \
    "${SAMPLE_APP_BIN_NAME}" "${LINENO}"

  # Start an application server under the blessing "alice/myworkstation/applicationd" so that
  # the device ("alice/myworkstation") can talk to it.
  local -r APPLICATIOND_NAME="applicationd"
  shell_test::start_server "${VRUN}" --name=myworkstation/applicationd "${APPLICATIOND_BIN}" --name="${APPLICATIOND_NAME}" \
    --store="$(shell::tmp_dir)" --veyron.tcp.address=127.0.0.1:0 \
    || shell_test::fail "line ${LINENO} failed to start applicationd"

  # Upload an envelope for our test app.
  local -r SAMPLE_APP_NAME="${APPLICATIOND_NAME}/testapp/v0"
  local -r APP_PUBLISH_NAME="testbinaryd"
  echo "{\"Title\":\"BINARYD\", \"Args\":[\"--name=${APP_PUBLISH_NAME}\", \"--root_dir=./binstore\", \"--veyron.tcp.address=127.0.0.1:0\"], \"Binary\":\"${SAMPLE_APP_BIN_NAME}\", \"Env\":[]}" > ./app.envelope && \
    "${APPLICATION_BIN}" put "${SAMPLE_APP_NAME}" "${DEVICE_PROFILE}" ./app.envelope && rm ./app.envelope

  # Verify that the envelope we uploaded shows up with glob.
  shell_test::assert_eq "$("${APPLICATION_BIN}" match "${SAMPLE_APP_NAME}" "${DEVICE_PROFILE}" | grep Title | sed -e 's/^.*"Title": "'// | sed -e 's/",//')" \
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

  # Restart the device manager.
  "${DEVICE_BIN}" suspend "${DM_NAME}/device"
  wait_for_mountentry "${NAMESPACE_BIN}" "5" "${DM_NAME}" "{DM_EP}"

  # Stop the device manager.
  "${DEVICE_SCRIPT}" stop
  wait_for_no_mountentry "${NAMESPACE_BIN}" "5" "${DM_NAME}"

  "${DEVICE_SCRIPT}" uninstall
  if [[ -n "$(ls -A "${VANADIUM_DEVICE_DIR}" 2>/dev/null)" ]]; then
    shell_test::fail "${VANADIUM_DEVICE_DIR} is not empty"
  fi
  shell_test::pass
}

main "$@"
