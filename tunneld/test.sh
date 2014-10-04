#!/bin/bash

# Test the tunneld binary
#
# This test starts a tunnel server and a mounttable server and then verifies
# that vsh can run commands through it and that all the expected names are
# in the mounttable.

source "${VEYRON_ROOT}/scripts/lib/shell_test.sh"

readonly WORKDIR=$(shell::tmp_dir)

# This imported library in scripts/lib/shell.sh runs set -e, which
# makes the shell exit immediately when any command fails. This can
# make troubleshooting problems much harder. Restore the default for
# this test.
set +e

build() {
  veyron go build veyron.io/examples/tunnel/tunneld || shell_test::fail "line ${LINENO}: failed to build tunneld"
  veyron go build veyron.io/examples/tunnel/vsh || shell_test::fail "line ${LINENO}: failed to build vsh"
  veyron go build veyron.io/veyron/veyron/services/mounttable/mounttabled || shell_test::fail "line ${LINENO}: failed to build mounttabled"
  veyron go build veyron.io/veyron/veyron/tools/mounttable || shell_test::fail "line ${LINENO}: failed to build mounttable"
  veyron go build veyron.io/veyron/veyron/tools/identity || shell_test::fail "line ${LINENO}: failed to build identity"
}

dumplogs() {
  for x in $*; do
    echo "-- $(basename "${x}") --"
    cat "${x}"
  done
}

main() {
  cd "${WORKDIR}"
  build

  # Start mounttabled and find its endpoint.
  local -r MTLOG="${WORKDIR}/mt.log"
  touch "${MTLOG}"
  ./mounttabled --veyron.tcp.address=127.0.0.1:0 > "${MTLOG}" 2>&1 &
  shell::wait_for "${MTLOG}" "Mount table service at:"
  local EP=$(grep "Mount table service at:" "${MTLOG}" | sed -e 's/^.*endpoint: //')
  [[ -z "${EP}" ]] && shell_test::fail "line ${LINENO}: no mounttable server"

  # Generate an identity for the tunnel server and client
  local -r ID="${WORKDIR}/id"
  VEYRON_IDENTITY="" ./identity generate test >"${ID}"

  export NAMESPACE_ROOT="${EP}"
  export VEYRON_IDENTITY="${ID}"

  # Start tunneld and find its endpoint.
  local -r TUNLOG="${WORKDIR}/tunnel.log"
  touch "${TUNLOG}"
  ./tunneld --address=127.0.0.1:0 -vmodule=publisher=2 > "${TUNLOG}" 2>&1 &
  shell::wait_for "${TUNLOG}" "ipc pub: mount"
  local EP=$(grep "Listening on endpoint" "${TUNLOG}" | sed -e 's/^.*endpoint //' | awk '{print $1}')
  if [[ -z "${EP}" ]]; then
    dumplogs "${TUNLOG}" "${MTLOG}"
    shell_test::fail "line ${LINENO}: no tunnel server"
  fi

  # Run remote command with the endpoint.
  local -r VSHLOG="${WORKDIR}/vsh.log"
  local GOT=$(./vsh --logtostderr --v=1 "/${EP}" echo HELLO ENDPOINT 2>"${VSHLOG}")
  local WANT="HELLO ENDPOINT"

  if [[ "${GOT}" != "${WANT}" ]]; then
    dumplogs "${VSHLOG}" "${TUNLOG}" "${MTLOG}"
    shell_test::fail "line ${LINENO}: unexpected output. Got ${GOT}, want ${WANT}"
  fi

  # Run remote command with the object name.
  GOT=$(./vsh --logtostderr --v=1 tunnel/id/test echo HELLO NAME 2>"${VSHLOG}")
  WANT="HELLO NAME"

  if [[ "${GOT}" != "${WANT}" ]]; then
    dumplogs "${VSHLOG}" "${TUNLOG}" "${MTLOG}"
    shell_test::fail "line ${LINENO}: unexpected output. Got ${GOT}, want ${WANT}"
  fi

  # Send input to remote command.
  echo "HELLO SERVER" | ./vsh --logtostderr --v=1 "/${EP}" "cat > ${WORKDIR}/hello.txt" > "${VSHLOG}" 2>&1
  GOT=$(cat "${WORKDIR}/hello.txt")
  WANT="HELLO SERVER"

  if [[ "${GOT}" != "${WANT}" ]]; then
    dumplogs "${VSHLOG}" "${TUNLOG}" "${MTLOG}"
    shell_test::fail "line ${LINENO}: unexpected output. Got ${GOT}, want ${WANT}"
  fi

  GOT=$(echo "ECHO" | ./vsh --logtostderr --v=1 "/${EP}" cat 2>"${VSHLOG}")
  WANT="ECHO"

  if [[ "${GOT}" != "${WANT}" ]]; then
    dumplogs "${VSHLOG}" "${TUNLOG}" "${MTLOG}"
    shell_test::fail "line ${LINENO}: unexpected output. Got ${GOT}, want ${WANT}"
  fi

  # Verify that all the published names are there.
  GOT=$(./mounttable glob "${NAMESPACE_ROOT}" 'tunnel/*/*' |    \
        sed -e 's/TTL .m..s/TTL XmXXs/'                     \
            -e 's!hwaddr/[^ ]*!hwaddr/XX:XX:XX:XX:XX:XX!' | \
        sort)
  WANT="[${NAMESPACE_ROOT}]
tunnel/hostname/$(hostname) /${EP}// (TTL XmXXs)
tunnel/hwaddr/XX:XX:XX:XX:XX:XX /${EP}// (TTL XmXXs)
tunnel/id/test /${EP}// (TTL XmXXs)"

  if [[ "${GOT}" != "${WANT}" ]]; then
    shell_test::fail "line ${LINENO}: unexpected output. Got ${GOT}, want ${WANT}"
  fi

  shell_test::pass
}

main "$@"
