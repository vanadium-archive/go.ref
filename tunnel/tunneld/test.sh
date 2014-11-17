#!/bin/bash

# Test the tunneld binary
#
# This test starts a tunnel server and a mounttable server and then verifies
# that vsh can run commands through it and that all the expected names are
# in the mounttable.

source "${VEYRON_ROOT}/scripts/lib/shell_test.sh"

readonly WORKDIR="${shell_test_WORK_DIR}"

# This imported library in scripts/lib/shell.sh runs set -e, which
# makes the shell exit immediately when any command fails. This can
# make troubleshooting problems much harder. Restore the default for
# this test.
set +e

build() {
  TUNNELD_BIN="$(shell_test::build_go_binary 'veyron.io/apps/tunnel/tunneld')"
  VSH_BIN="$(shell_test::build_go_binary 'veyron.io/apps/tunnel/vsh')"
  MOUNTTABLED_BIN="$(shell_test::build_go_binary 'veyron.io/veyron/veyron/services/mounttable/mounttabled')"
  MOUNTTABLE_BIN="$(shell_test::build_go_binary 'veyron.io/veyron/veyron/tools/mounttable')"
}

dumplogs() {
  for x in $*; do
    echo "-- $(basename "${x}") --"
    cat "${x}"
  done
}

main() {
  local EP
  local GOT
  local WANT

  cd "${WORKDIR}"
  build
  # Directory where all credentials will be stored (private key etc.)
  # All binaries will use the same credentials in this test.
  export VEYRON_CREDENTIALS="${WORKDIR}/credentials"

  # Start mounttabled and find its endpoint.
  local -r MTLOG="${WORKDIR}/mt.log"
  touch "${MTLOG}"

  shell::run_server "${shell_test_DEFAULT_SERVER_TIMEOUT}" "${MTLOG}" "${MTLOG}" \
    "${MOUNTTABLED_BIN}" --veyron.tcp.address=127.0.0.1:0 -vmodule=publisher=2 &> /dev/null \
    || (dumplogs "${MTLOG}"; shell_test::fail "line ${LINENO}: failed to start mounttabled")
  shell::timed_wait_for "${shell_test_DEFAULT_MESSAGE_TIMEOUT}" "${MTLOG}" "Mount table service at:" \
    || (dumplogs "${MTLOG}"; shell_test::fail "line ${LINENO}: failed to start mount table service")
  EP=$(grep "Mount table service at:" "${MTLOG}" | sed -e 's/^.*endpoint: //') \
    || (dumplogs "${MTLOG}"; shell_test::fail "line ${LINENO}: failed to identify endpoint")

  # Start tunneld and find its endpoint.
  export NAMESPACE_ROOT="${EP}"
  local -r TUNLOG="${WORKDIR}/tunnel.log"
  touch "${TUNLOG}"
  shell::run_server "${shell_test_DEFAULT_SERVER_TIMEOUT}" "${TUNLOG}" "${TUNLOG}" \
    "${TUNNELD_BIN}" --veyron.tcp.address=127.0.0.1:0 -vmodule=publisher=2 &> /dev/null \
    || (dumplogs "${TUNLOG}"; shell_test::fail "line ${LINENO}: failed to start tunneld")
  shell::timed_wait_for "${shell_test_DEFAULT_MESSAGE_TIMEOUT}" "${TUNLOG}" "ipc pub: mount" \
    || (dumplogs "${TUNLOG}"; shell_test::fail "line ${LINENO}: failed to mount tunneld")
  EP=$(grep "Listening on endpoint" "${TUNLOG}" | sed -e 's/^.*endpoint //' | awk '{print $1}') \
    || (dumplogs "${TUNLOG}"; shell_test::fail "line ${LINENO}: failed to identify endpoint")

  # Run remote command with the endpoint.
  local -r VSHLOG="${WORKDIR}/vsh.log"
  GOT=$("${VSH_BIN}" --logtostderr --v=1 "/${EP}" echo HELLO ENDPOINT 2>"${VSHLOG}") \
    || (dumplogs "${VSHLOG}"; shell_test::fail "line ${LINENO}: failed to run vsh")
  WANT="HELLO ENDPOINT"

  if [[ "${GOT}" != "${WANT}" ]]; then
    shell_test::fail "line ${LINENO}: unexpected output. Got ${GOT}, want ${WANT}"
  fi

  # Run remote command with the object name.
  GOT=$("${VSH_BIN}" --logtostderr --v=1 tunnel/hostname/$(hostname) echo HELLO NAME 2>"${VSHLOG}") \
    || (dumplogs "${VSHLOG}"; shell_test::fail "line ${LINENO}: failed to run vsh")
  WANT="HELLO NAME"

  if [[ "${GOT}" != "${WANT}" ]]; then
    shell_test::fail "line ${LINENO}: unexpected output. Got ${GOT}, want ${WANT}"
  fi

  # Send input to remote command.
  echo "HELLO SERVER" | "${VSH_BIN}" --logtostderr --v=1 "/${EP}" "cat > ${WORKDIR}/hello.txt" 2>"${VSHLOG}" \
    || (dumplogs "${VSHLOG}"; shell_test::fail "line ${LINENO}: failed to run vsh")
  GOT=$(cat "${WORKDIR}/hello.txt")
  WANT="HELLO SERVER"

  if [[ "${GOT}" != "${WANT}" ]]; then
    shell_test::fail "line ${LINENO}: unexpected output. Got ${GOT}, want ${WANT}"
  fi

  GOT=$(echo "ECHO" | "${VSH_BIN}" --logtostderr --v=1 "/${EP}" cat 2>"${VSHLOG}") \
    || (dumplogs "${VSHLOG}"; shell_test::fail "line ${LINENO}: failed to run vsh")
  WANT="ECHO"

  if [[ "${GOT}" != "${WANT}" ]]; then
    shell_test::fail "line ${LINENO}: unexpected output. Got ${GOT}, want ${WANT}"
  fi

  # Verify that all the published names are there.
  GOT=$("${MOUNTTABLE_BIN}" glob "${NAMESPACE_ROOT}" 'tunnel/*/*' | \
	  sed -e 's/ \/@.@ws@[^ ]* (TTL .m..s)//' -e 's/TTL .m..s/TTL XmXXs/' -e 's!hwaddr/[^ ]*!hwaddr/XX:XX:XX:XX:XX:XX!' | \
        sort) \
    || shell_test::fail "line ${LINENO}: failed to run mounttable"
  WANT="[${NAMESPACE_ROOT}]
tunnel/hostname/$(hostname) /${EP} (TTL XmXXs)
tunnel/hwaddr/XX:XX:XX:XX:XX:XX /${EP} (TTL XmXXs)"

  if [[ "${GOT}" != "${WANT}" ]]; then
    shell_test::fail "line ${LINENO}: unexpected output. Got ${GOT}, want ${WANT}"
  fi

  shell_test::pass
}

main "$@"
