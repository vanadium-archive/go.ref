#!/bin/bash

# Test the tunneld binary
#
# This test starts a tunnel server and a mounttable server and then verifies
# that vsh can run commands through it and that all the expected names are
# in the mounttable.

source "${VEYRON_ROOT}/environment/scripts/lib/shell_test.sh"

build() {
  local -r GO="${repo_root}/scripts/build/go"
  "${GO}" build veyron/examples/tunnel/tunneld || shell_test::fail "line ${LINENO}: failed to build tunneld"
  "${GO}" build veyron/examples/tunnel/vsh || shell_test::fail "line ${LINENO}: failed to build vsh"
  "${GO}" build veyron/services/mounttable/mounttabled || shell_test::fail "line ${LINENO}: failed to build mounttabled"
  "${GO}" build veyron/tools/mounttable || shell_test::fail "line ${LINENO}: failed to build mounttable"
  "${GO}" build veyron/tools/identity || shell_test::fail "line ${LINENO}: failed to build identity"
}

dumplogs() {
  for x in $*; do
    echo "-- $(basename "${x}") --"
    cat "${x}"
  done
}

main() {
  cd "${TMPDIR}"
  build

  # Start mounttabled and find its endpoint.
  local -r MTLOG="${TMPDIR}/mt.log"
  ./mounttabled --address=localhost:0 > "${MTLOG}" 2>&1 &
  for i in 1 2 3 4; do
    local EP=$(grep "Mount table service at:" "${MTLOG}" | sed -e 's/^.*endpoint: //')
    if [[ -n "${EP}" ]]; then
      break
    fi
    sleep 1
  done
  [[ -z "${EP}" ]] && shell_test::fail "line ${LINENO}: no mounttable server"

  # Generate an identity for the tunnel server and client
  local -r ID="${TMPDIR}/id"
  VEYRON_IDENTITY="" ./identity generate test >"${ID}"

  export NAMESPACE_ROOT="${EP}"
  export VEYRON_IDENTITY="${ID}"

  # Start tunneld and find its endpoint.
  local -r TUNLOG="${TMPDIR}/tunnel.log"
  ./tunneld --address=localhost:0 > "${TUNLOG}" 2>&1 &
  for i in 1 2 3 4; do
    local EP=$(grep "Listening on endpoint" "${TUNLOG}" | sed -e 's/^.*endpoint //' | awk '{print $1}')
    if [[ -n "${EP}" ]]; then
      break
    fi
    sleep 1
  done
  [[ -z "${EP}" ]] && shell_test::fail "line ${LINENO}: no tunnel server"

  # Run remote command with the endpoint.
  local -r VSHLOG="${TMPDIR}/vsh.log"
  local GOT=$(./vsh --logtostderr --v=1 "${EP}" echo HELLO ENDPOINT 2>"${VSHLOG}")
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
  echo "HELLO SERVER" | ./vsh --logtostderr --v=1 "${EP}" "cat > ${TMPDIR}/hello.txt" > "${VSHLOG}" 2>&1
  GOT=$(cat "${TMPDIR}/hello.txt")
  WANT="HELLO SERVER"

  if [[ "${GOT}" != "${WANT}" ]]; then
    dumplogs "${VSHLOG}" "${TUNLOG}" "${MTLOG}"
    shell_test::fail "line ${LINENO}: unexpected output. Got ${GOT}, want ${WANT}"
  fi

  GOT=$(echo "ECHO" | ./vsh --logtostderr --v=1 "${EP}" cat 2>"${VSHLOG}")
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
tunnel/hostname/$(hostname) ${EP}// (TTL XmXXs)
tunnel/hwaddr/XX:XX:XX:XX:XX:XX ${EP}// (TTL XmXXs)
tunnel/id/test ${EP}// (TTL XmXXs)"

  if [[ "${GOT}" != "${WANT}" ]]; then
    shell_test::fail "line ${LINENO}: unexpected output. Got ${GOT}, want ${WANT}"
  fi

  shell_test::pass
}

main "$@"
