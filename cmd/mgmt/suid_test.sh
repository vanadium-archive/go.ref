#!/bin/bash
# Copyright 2015 The Vanadium Authors. All rights reserved.
# Use of this source code is governed by a BSD-style
# license that can be found in the LICENSE file.

# Test the device manager and related services and tools.
#
#
# By default, this script tests the device manager in a fashion amenable
# to automatic testing: the --single_user is passed to the device
# manager so that all device manager components run as the same user and
# no user input (such as an agent pass phrase) is needed.
#
# When this script is invoked with the --with_suid <user1> <user2> flag, it
# installs the device manager in its more secure multi-account
# configuration where the device manager runs under the account of <user1>
# while test apps will be executed as <user2>. This mode will
# require root permissions to install and may require configuring an
# agent passphrase.
#
# For exanple:
#
#   ./suid_test.sh --with_suid devicemanager vana
#
# to test a device manager with multi-account support enabled for app
# account vana.
#

# When running --with_suid, TMPDIR must grant the invoking user rwx
# permissions and x permissions for all directories back to / for world.
# Otherwise, the with_suid user will not be able to use absolute paths.
# On Darwin, TMPDIR defaults to a directory hieararchy in /var that is
# 0700. This is unworkable so force TMPDIR to /tmp in this case.
WITH_SUID="${1:-no}"
# TODO(caprita,rjkroege): Add logic to the integration test that verifies
# installing and accessing packages from apps.  This would add coverage to the
# package-related code in suid mode.
if [[ "${WITH_SUID}" == "--with_suid" ]]; then
  DEVMGR_USER="${2:?--with_suid requires a devicemgr user}"
  SUID_USER="${3:?--with_suid requires a app user}"
  SUDO_USER="root"
  TMPDIR=/tmp
  umask 066
fi

 source "$(go list -f {{.Dir}} v.io/x/ref/cmd/mgmt)/shell_test.sh"

# Run the test under the security agent.
shell_test::enable_agent "$@"

readonly WORKDIR="${shell_test_WORK_DIR}"

build() {
  echo ">> Building binaries"
  BINARYD_BIN="$(shell_test::build_go_binary 'v.io/x/ref/services/mgmt/binary/binaryd')"
  BINARY_BIN="$(shell_test::build_go_binary 'v.io/x/ref/cmd/binary')"
  APPLICATIOND_BIN="$(shell_test::build_go_binary 'v.io/x/ref/services/mgmt/application/applicationd')"
  APPLICATION_BIN="$(shell_test::build_go_binary 'v.io/x/ref/cmd/application')"
  AGENTD_BIN="$(shell_test::build_go_binary 'v.io/x/ref/security/agent/agentd')"
  SUIDHELPER_BIN="$(shell_test::build_go_binary 'v.io/x/ref/services/mgmt/suidhelper')"
  INITHELPER_BIN="$(shell_test::build_go_binary 'v.io/x/ref/services/mgmt/inithelper')"
  DEVICEMANAGER_BIN="$(shell_test::build_go_binary 'v.io/x/ref/services/mgmt/device/deviced')"
  DEVICE_BIN="$(shell_test::build_go_binary 'v.io/x/ref/cmd/mgmt/device')"
  NAMESPACE_BIN="$(shell_test::build_go_binary 'v.io/x/ref/cmd/namespace')"
  PRINCIPAL_BIN="$(shell_test::build_go_binary 'v.io/x/ref/cmd/principal')"
  DEBUG_BIN="$(shell_test::build_go_binary 'v.io/x/ref/cmd/debug')"
  DEVICE_SCRIPT="$(go list -f {{.Dir}} v.io/x/ref/cmd/mgmt/device)/devicex"
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

  local -r APPLICATIOND_NAME="applicationd"
  local -r DEVICED_APP_NAME="${APPLICATIOND_NAME}/deviced/test"

  BIN_STAGING_DIR="${WORKDIR}/bin"
  mkdir -p "${BIN_STAGING_DIR}"
  cp "${AGENTD_BIN}" "${SUIDHELPER_BIN}" "${INITHELPER_BIN}" "${DEVICEMANAGER_BIN}" "${BIN_STAGING_DIR}"
  shell_test::setup_server_test

  if [[ "${WITH_SUID}" == "--with_suid" ]]; then
    chmod go+x "${WORKDIR}"
  fi

  echo ">> Installing and starting the device manager"
  DM_INSTALL_DIR="${WORKDIR}/dm"

  export V23_DEVICE_DIR="${DM_INSTALL_DIR}"

  if [[ "${WITH_SUID}" != "--with_suid" ]]; then
    local -r extra_arg="--single_user"
  else
    local -r extra_arg="--devuser=${DEVMGR_USER}"
  fi

  local -r NEIGHBORHOODNAME="$(hostname)-$$-${RANDOM}"
  "${DEVICE_SCRIPT}" install "${BIN_STAGING_DIR}" \
    ${extra_arg} \
    --origin="${DEVICED_APP_NAME}" \
    -- \
    --v23.tcp.address=127.0.0.1:0 \
    --neighborhood-name="${NEIGHBORHOODNAME}"

  "${VRUN}" "${DEVICE_SCRIPT}" start
  local -r MT_NAME=devices/$(hostname)
  MT_EP=$(wait_for_mountentry "${NAMESPACE_BIN}" 5 "${MT_NAME}")

  # Verify that device manager's mounttable is published under the expected name
  # (hostname).
  shell_test::assert_ne "$("${NAMESPACE_BIN}" glob "${MT_NAME}")" "" "${LINENO}"

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
  echo ">> Claiming the device manager"
  "${DEVICE_BIN}" claim "${MT_NAME}/devmgr/device" myworkstation
  # Wait for the device manager to re-mount after being claimed
  MT_EP=$(wait_for_mountentry "${NAMESPACE_BIN}" 5 "${MT_NAME}" "${MT_EP}")

  if [[ "${WITH_SUID}" == "--with_suid" ]]; then
    echo ">> Verify that devicemanager has valid association for alice"
    "${DEVICE_BIN}" associate add "${MT_NAME}/devmgr/device" "${SUID_USER}"  "alice"
     shell_test::assert_eq   "$("${DEVICE_BIN}" associate list "${MT_NAME}/devmgr/device")" \
       "alice ${SUID_USER}" "${LINENO}"
     echo ">> Verify that devicemanager runs as ${DEVMGR_USER}"
     local -r DPID=$("${DEBUG_BIN}" stats read \
         "${MT_NAME}/devmgr/__debug/stats/system/pid" \
         | awk '{print $2}')
    # ps flags need to be different on linux
    case "$(uname)" in
    "Darwin")
      local -r COMPUTED_DEVMGR_USER=$(ps -ej | \
          awk '$2 ~'"${DPID}"' { print $1 }')
      ;;
    "Linux")
      local -r COMPUTED_DEVMGR_USER=$(awk '/^Uid:/{print $2}' /proc/${DPID}/status | \
          xargs getent passwd | awk -F: '{print $1}')
      ;;
     esac
     shell_test::assert_eq "${COMPUTED_DEVMGR_USER}" \
          "${DEVMGR_USER}" \
          "${LINENO}"
  fi

  # Verify the device's default blessing is as expected.
  shell_test::assert_contains "$("${DEBUG_BIN}" stats read "${MT_NAME}/devmgr/__debug/stats/security/principal/*/blessingstore" | head -1)" \
    "Default blessings: alice/myworkstation" "${LINENO}"

  # Get the device's profile.
  local -r DEVICE_PROFILE=$("${DEVICE_BIN}" describe "${MT_NAME}/devmgr/device" | sed -e 's/{Profiles:map\[\(.*\):{}]}/\1/')

  # Start a binary server under the blessing "alice/myworkstation/binaryd" so that
  # the device ("alice/myworkstation") can talk to it.
  local -r BINARYD_NAME="binaryd"
  shell_test::start_server "${VRUN}" --name=myworkstation/binaryd "${BINARYD_BIN}" --name="${BINARYD_NAME}" \
    --root-dir="${WORKDIR}/binstore" --v23.tcp.address=127.0.0.1:0 --http=127.0.0.1:0 \
    || shell_test::fail "line ${LINENO} failed to start binaryd"

  # Upload a binary to the binary server.  The binary we upload is binaryd
  # itself.
  local -r SAMPLE_APP_BIN_NAME="${BINARYD_NAME}/testapp"
  echo ">> Uploading ${SAMPLE_APP_BIN_NAME}"
  "${BINARY_BIN}" upload "${SAMPLE_APP_BIN_NAME}" "${BINARYD_BIN}"

  # Verify that the binary we uploaded is shown by glob.
  shell_test::assert_eq "$("${NAMESPACE_BIN}" glob "${SAMPLE_APP_BIN_NAME}")" \
    "${SAMPLE_APP_BIN_NAME}" "${LINENO}"

  # Start an application server under the blessing "alice/myworkstation/applicationd" so that
  # the device ("alice/myworkstation") can talk to it.
  mkdir -p "${WORKDIR}/appstore"
  shell_test::start_server "${VRUN}" --name=myworkstation/applicationd "${APPLICATIOND_BIN}" --name="${APPLICATIOND_NAME}" \
    --store="${WORKDIR}/appstore" --v23.tcp.address=127.0.0.1:0 \
    || shell_test::fail "line ${LINENO} failed to start applicationd"

  # Upload an envelope for our test app.
  local -r SAMPLE_APP_NAME="${APPLICATIOND_NAME}/testapp/v0"
  local -r APP_PUBLISH_NAME="testbinaryd"
  echo ">> Uploading ${SAMPLE_APP_NAME}"
  echo "{\"Title\":\"BINARYD\", \"Args\":[\"--name=${APP_PUBLISH_NAME}\", \"--root-dir=./binstore\", \"--v23.tcp.address=127.0.0.1:0\"], \"Binary\":{\"File\":\"${SAMPLE_APP_BIN_NAME}\"}, \"Env\":[]}" > ./app.envelope
  "${APPLICATION_BIN}" put "${SAMPLE_APP_NAME}" "${DEVICE_PROFILE}" ./app.envelope
  rm ./app.envelope

  # Verify that the envelope we uploaded shows up with glob.
  shell_test::assert_eq "$("${APPLICATION_BIN}" match "${SAMPLE_APP_NAME}" "${DEVICE_PROFILE}" | grep Title | sed -e 's/^.*"Title": "'// | sed -e 's/",//')" \
    "BINARYD" "${LINENO}"

  # Install the app on the device.
  echo ">> Installing ${SAMPLE_APP_NAME}"
  local -r INSTALLATION_NAME=$("${DEVICE_BIN}" install "${MT_NAME}/devmgr/apps" "${SAMPLE_APP_NAME}" | sed -e 's/Successfully installed: "//' | sed -e 's/"//')

  # Verify that the installation shows up when globbing the device manager.
  shell_test::assert_eq "$("${NAMESPACE_BIN}" glob "${MT_NAME}/devmgr/apps/BINARYD/*")" \
    "${INSTALLATION_NAME}" "${LINENO}"

  # Start an instance of the app, granting it blessing extension myapp.
  echo ">> Starting ${INSTALLATION_NAME}"
  local -r INSTANCE_NAME=$("${DEVICE_BIN}" start "${INSTALLATION_NAME}" myapp | sed -e 's/Successfully started: "//' | sed -e 's/"//')
  wait_for_mountentry "${NAMESPACE_BIN}" "5" "${MT_NAME}/${APP_PUBLISH_NAME}"

  # Verify that the instance shows up when globbing the device manager.
  shell_test::assert_eq "$("${NAMESPACE_BIN}" glob "${MT_NAME}/devmgr/apps/BINARYD/*/*")" "${INSTANCE_NAME}" "${LINENO}"

  if [[ "${WITH_SUID}" == "--with_suid" ]]; then
    echo ">> Verifying that the app is actually running as the associated user"
     local -r PID=$("${DEBUG_BIN}" stats read "${MT_NAME}/devmgr/apps/BINARYD/*/*/stats/system/pid"  | awk '{print $2}')
    # ps flags need to be different on linux
    case "$(uname)" in
    "Darwin")
      local -r COMPUTED_SUID_USER=$(ps -ej | awk '$2 ~'"${PID}"' { print $1 }')
      ;;
    "Linux")
        local -r COMPUTED_SUID_USER=$(awk '/^Uid:/{print $2}' /proc/${PID}/status | \
          xargs getent passwd | awk -F: '{print $1}')
      ;;
     esac
     shell_test::assert_eq "${COMPUTED_SUID_USER}" "${SUID_USER}" "${LINENO}"
 fi

  # Verify the app's default blessing.
  shell_test::assert_contains "$("${DEBUG_BIN}" stats read "${INSTANCE_NAME}/stats/security/principal/*/blessingstore" | head -1)" \
    "Default blessings: alice/myapp/BINARYD" "${LINENO}"

  # Stop the instance.
  echo ">> Stopping ${INSTANCE_NAME}"
  "${DEVICE_BIN}" stop "${INSTANCE_NAME}"

  # Verify that logs, but not stats, show up when globbing the stopped instance.
  shell_test::assert_eq "$("${NAMESPACE_BIN}" glob "${INSTANCE_NAME}/stats/...")" "" "${LINENO}"
  shell_test::assert_ne "$("${NAMESPACE_BIN}" glob "${INSTANCE_NAME}/logs/...")" "" "${LINENO}"

  # Upload a deviced binary.
  local -r DEVICED_APP_BIN_NAME="${BINARYD_NAME}/deviced"
  echo ">> Uploading ${DEVICEMANAGER_BIN}"
  "${BINARY_BIN}" upload "${DEVICED_APP_BIN_NAME}" "${DEVICEMANAGER_BIN}"

  # Upload a device manager envelope.
  echo ">> Uploading ${DEVICED_APP_NAME}"
  echo "{\"Title\":\"device manager\", \"Binary\":{\"File\":\"${DEVICED_APP_BIN_NAME}\"}}" > ./deviced.envelope
  "${APPLICATION_BIN}" put "${DEVICED_APP_NAME}" "${DEVICE_PROFILE}" ./deviced.envelope
  rm ./deviced.envelope
  # Update the device manager.
  echo ">> Updating device manager"
  "${DEVICE_BIN}" update "${MT_NAME}/devmgr/device"
  MT_EP=$(wait_for_mountentry "${NAMESPACE_BIN}" 5 "${MT_NAME}" "${MT_EP}")

  # Verify that device manager's mounttable is still published under the
  # expected name (hostname).
  shell_test::assert_ne "$("${NAMESPACE_BIN}" glob "${MT_NAME}")" "" "${LINENO}"

  # Revert the device manager.
  echo ">> Reverting device manager"
  "${DEVICE_BIN}" revert "${MT_NAME}/devmgr/device"
  MT_EP=$(wait_for_mountentry "${NAMESPACE_BIN}" 5 "${MT_NAME}" "${MT_EP}")

  # Verify that device manager's mounttable is still published under the
  # expected name (hostname).
  shell_test::assert_ne "$("${NAMESPACE_BIN}" glob "${MT_NAME}")" "" "${LINENO}"

  # Verify that the local mounttable exists, and that the device manager, the
  # global namespace, and the neighborhood are mounted on it.
  shell_test::assert_ne $("${NAMESPACE_BIN}" resolve "${MT_EP}/devmgr") "" "${LINENO}"
  shell_test::assert_eq $("${NAMESPACE_BIN}" resolve "${MT_EP}/global") "[alice/myworkstation]${V23_NAMESPACE}" "${LINENO}"
  shell_test::assert_ne $("${NAMESPACE_BIN}" resolve "${MT_EP}/nh") "" "${LINENO}"

  # Suspend the device manager.
  "${DEVICE_BIN}" suspend "${MT_NAME}/devmgr/device"
  wait_for_mountentry "${NAMESPACE_BIN}" "5" "${MT_NAME}" "${MT_EP}"

  # Stop the device manager.
  "${DEVICE_SCRIPT}" stop
  wait_for_no_mountentry "${NAMESPACE_BIN}" "5" "${MT_NAME}"

  "${DEVICE_SCRIPT}" uninstall
  if [[ -n "$(ls -A "${V23_DEVICE_DIR}" 2>/dev/null)" ]]; then
    shell_test::fail "${V23_DEVICE_DIR} is not empty"
  fi
  shell_test::pass
}

main "$@"
