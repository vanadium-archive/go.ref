#!/bin/bash

# Test the tunneld binary
#
# This test starts a tunnel server and a mounttable server and then verifies
# that vsh can run commands through it and that all the expected names are
# in the mounttable.

readonly repo_root=$(git rev-parse --show-toplevel)
readonly thisscript="$0"
readonly workdir=$(mktemp -d "${repo_root}/go/tmp.XXXXXXXXXXX")

export TMPDIR="${workdir}"
trap onexit EXIT

onexit() {
  cd /
  exec 2> /dev/null
  kill -9 $(jobs -p)
  rm -rf "${workdir}"
}

fail() {
  [[ $# -gt 0 ]] && echo "${thisscript} $*"
  echo FAIL
  exit 1
}

pass() {
  echo PASS
  exit 0
}

build() {
  local go="${repo_root}/scripts/build/go"
  "${go}" build veyron/examples/tunnel/tunneld || fail "line ${LINENO}: failed to build tunneld"
  "${go}" build veyron/examples/tunnel/vsh || fail "line ${LINENO}: failed to build vsh"
  "${go}" build veyron/services/mounttable/mounttabled || fail "line ${LINENO}: failed to build mounttabled"
  "${go}" build veyron/tools/mounttable || fail "line ${LINENO}: failed to build mounttable"
  "${go}" build veyron/tools/identity || fail "line ${LINENO}: failed to build identity"
}

dumplogs() {
  for x in $*; do
    echo "-- $(basename "${x}") --"
    cat "${x}"
  done
}

main() {
  cd "${workdir}"
  build

  # Start mounttabled and find its endpoint.
  local mtlog="${workdir}/mt.log"
  ./mounttabled --address=localhost:0 > "${mtlog}" 2>&1 &
  for i in 1 2 3 4; do
    ep=$(grep "Mount table service at:" "${mtlog}" | sed -e 's/^.*endpoint: //')
    if [[ -n "${ep}" ]]; then
      break
    fi
    sleep 1
  done
  [[ -z "${ep}" ]] && fail "line ${LINENO}: no mounttable server"

  # Generate an identity for the tunnel server and client
  readonly id="${workdir}/id"
  VEYRON_IDENTITY="" ./identity generate test >${id}

  export NAMESPACE_ROOT="${ep}"
  export VEYRON_IDENTITY="${id}"

  # Start tunneld and find its endpoint.
  local tunlog="${workdir}/tunnel.log"
  ./tunneld --address=localhost:0 > "${tunlog}" 2>&1 &
  for i in 1 2 3 4; do
    ep=$(grep "Listening on endpoint" "${tunlog}" | sed -e 's/^.*endpoint //' | awk '{print $1}')
    if [[ -n "${ep}" ]]; then
      break
    fi
    sleep 1
  done
  [[ -z "${ep}" ]] && fail "line ${LINENO}: no tunnel server"

  # Run remote command with the endpoint.
  local vshlog="${workdir}/vsh.log"
  local got=$(./vsh --logtostderr --v=1 "${ep}" echo HELLO ENDPOINT 2>"${vshlog}")
  local want="HELLO ENDPOINT"

  if [[ "${got}" != "${want}" ]]; then
    dumplogs "${vshlog}" "${tunlog}" "${mtlog}"
    fail "line ${LINENO}: unexpected output. Got ${got}, want ${want}"
  fi

  # Run remote command with the object name.
  got=$(./vsh --logtostderr --v=1 tunnel/id/test echo HELLO NAME 2>"${vshlog}")
  want="HELLO NAME"

  if [[ "${got}" != "${want}" ]]; then
    dumplogs "${vshlog}" "${tunlog}" "${mtlog}"
    fail "line ${LINENO}: unexpected output. Got ${got}, want ${want}"
  fi

  # Send input to remote command.
  echo "HELLO SERVER" | ./vsh --logtostderr --v=1 "${ep}" "cat > ${workdir}/hello.txt" > "${vshlog}" 2>&1
  got=$(cat "${workdir}/hello.txt")
  want="HELLO SERVER"

  if [[ "${got}" != "${want}" ]]; then
    dumplogs "${vshlog}" "${tunlog}" "${mtlog}"
    fail "line ${LINENO}: unexpected output. Got ${got}, want ${want}"
  fi

  got=$(echo "ECHO" | ./vsh --logtostderr --v=1 "${ep}" cat 2>"${vshlog}")
  want="ECHO"

  if [[ "${got}" != "${want}" ]]; then
    dumplogs "${vshlog}" "${tunlog}" "${mtlog}"
    fail "line ${LINENO}: unexpected output. Got ${got}, want ${want}"
  fi

  # Verify that all the published names are there.
  got=$(./mounttable glob "${NAMESPACE_ROOT}" 'tunnel/*/*' |    \
        sed -e 's/TTL .m..s/TTL XmXXs/'                     \
            -e 's!hwaddr/[^ ]*!hwaddr/XX:XX:XX:XX:XX:XX!' | \
        sort)
  want="[${NAMESPACE_ROOT}]
tunnel/hostname/$(hostname) ${ep}// (TTL XmXXs)
tunnel/hwaddr/XX:XX:XX:XX:XX:XX ${ep}// (TTL XmXXs)
tunnel/id/test ${ep}// (TTL XmXXs)"

  if [[ "${got}" != "${want}" ]]; then
    fail "line ${LINENO}: unexpected output. Got ${got}, want ${want}"
  fi

  pass
}

main "$@"
