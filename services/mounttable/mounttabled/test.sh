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
  veyron go build veyron.io/veyron/veyron/services/mounttable/mounttabled || shell_test::fail "line ${LINENO}: failed to build mounttabled"
  veyron go build veyron.io/veyron/veyron/tools/mounttable || shell_test::fail "line ${LINENO}: failed to build mounttable"
}

main() {
  local EP GOT NHEP WANT

  cd "${TMPDIR}"
  build

  # Start mounttabled and find its endpoint.
  local -r NHNAME=test-$(hostname)-$$
  local -r MTLOG="${TMPDIR}/mt.log"

  shell::run_server "${shell_test_DEFAULT_SERVER_TIMEOUT}" "${MTLOG}" "${MTLOG}" \
    ./mounttabled --veyron.tcp.address=127.0.0.1:0 -vmodule=publisher=2 --neighborhood_name="${NHNAME}" &> /dev/null \
    || shell_test::fail "line ${LINENO}: failed to start mounttabled"
  shell::timed_wait_for "${shell_test_DEFAULT_MESSAGE_TIMEOUT}" "${MTLOG}" "ipc pub: mount" \
    || shell_test::fail "line ${LINENO}: failed to mount mounttabled"
  EP=$(grep "Mount table service at:" "${MTLOG}" | sed -e 's/^.*endpoint: //') \
    || (cat "${MTLOG}"; shell_test::fail "line ${LINENO}: failed to identify endpoint")

  # Get the neighborhood endpoint from the mounttable.
  NHEP=$(./mounttable glob "${EP}" nh | grep ^nh | cut -d' ' -f2) \
    || (cat "${MTLOG}"; shell_test::fail "line ${LINENO}: failed to identify neighborhood endpoint")

  # Mount objects and verify the result.
  ./mounttable mount "${EP}/myself" "${EP}" 5m > /dev/null \
    || shell_test::fail "line ${LINENO}: failed to mount the mounttable on itself"
  ./mounttable mount "${EP}/google" /www.google.com:80 5m > /dev/null \
    || shell_test::fail "line ${LINENO}: failed to mount www.google.com"

  # <mounttable>.Glob('*')
  GOT=$(./mounttable glob "${EP}" '*' | sed 's/TTL [^)]*/TTL XmXXs/' | sort) \
    || shell_test::fail "line ${LINENO}: failed to run mounttable"
  WANT="[${EP}]
google /www.google.com:80 (TTL XmXXs)
myself ${EP} (TTL XmXXs)
nh ${NHEP} (TTL XmXXs)"
  shell_test::assert_eq "${GOT}" "${WANT}" "${LINENO}"

  # <neighborhood>.Glob('NHNAME')
  GOT=$(./mounttable glob "${NHEP}" "${NHNAME}" | sed 's/TTL [^)]*/TTL XmXXs/' | sort) \
    || shell_test::fail "line ${LINENO}: failed to run mounttable"
  WANT="[${NHEP}]
${NHNAME} ${EP} (TTL XmXXs)"
  shell_test::assert_eq "${GOT}" "${WANT}" "${LINENO}"

  shell_test::pass
}

main "$@"
