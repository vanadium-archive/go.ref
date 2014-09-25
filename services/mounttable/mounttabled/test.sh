#!/bin/bash

# Test the mounttabled binary
#
# This test starts a mounttable server with the neighborhood option enabled,
# and mounts 1) the mounttable on itself as 'myself', and 2) www.google.com:80
# as 'google'.
#
# Then it verifies that <mounttable>.Glob(*) and <neighborhood>.Glob(nhname)
# return the correct result.

source "${VEYRON_ROOT}/scripts/lib/shell_test.sh"

build() {
  local -r GO="${VEYRON_ROOT}/scripts/build/go"
  "${GO}" build veyron.io/veyron/veyron/services/mounttable/mounttabled || shell_test::fail "line ${LINENO}: failed to build mounttabled"
  "${GO}" build veyron.io/veyron/veyron/tools/mounttable || shell_test::fail "line ${LINENO}: failed to build mounttable"
}

main() {
  cd "${TMPDIR}"
  build

  # Start mounttabled and find its endpoint.
  local -r NHNAME=test-$(hostname)-$$
  local -r MTLOG="${TMPDIR}/mt.log"
  ./mounttabled --address=127.0.0.1:0 -vmodule=publisher=2 --neighborhood_name="${NHNAME}" > "${MTLOG}" 2>&1 &
  shell::wait_for "${MTLOG}" "ipc pub: mount"

  local -r EP=$(grep "Mount table service at:" "${MTLOG}" | sed -e 's/^.*endpoint: //')
  [[ -z "${EP}" ]] && shell_test::fail "line ${LINENO}: no server"

  # Get the neighborhood endpoint from the mounttable.
  local -r NHEP=$(./mounttable glob ${EP} nh | grep ^nh | cut -d' ' -f2)
  [[ -z "${NHEP}" ]] && shell_test::fail "line ${LINENO}: no neighborhood server"

  # Mount objects and verify the result.
  ./mounttable mount "${EP}/myself" "${EP}" 5m > /dev/null || shell_test::fail "line ${LINENO}: failed to mount the mounttable on itself"
  ./mounttable mount "${EP}/google" /www.google.com:80 5m > /dev/null || shell_test::fail "line ${LINENO}: failed to mount www.google.com"

  # <mounttable>.Glob('*')
  local GOT=$(./mounttable glob "${EP}" '*' | sed 's/TTL [^)]*/TTL XmXXs/' | sort)
  local WANT="[${EP}]
google /www.google.com:80 (TTL XmXXs)
myself ${EP} (TTL XmXXs)
nh ${NHEP} (TTL XmXXs)"
  if [[ "${GOT}" != "${WANT}" ]]; then
    shell_test::fail "line ${LINENO}: unexpected output. Got ${GOT}, want ${WANT}"
  fi

  # <neighborhood>.Glob('NHNAME')
  GOT=$(./mounttable glob "${NHEP}" "${NHNAME}" | sed 's/TTL [^)]*/TTL XmXXs/' | sort)
  WANT="[${NHEP}]
${NHNAME} ${EP} (TTL XmXXs)"
  if [[ "${GOT}" != "${WANT}" ]]; then
    shell_test::fail "line ${LINENO}: unexpected output. Got ${GOT}, want ${WANT}"
  fi

  shell_test::pass
}

main "$@"
