#!/bin/bash

# Test the mounttabled binary
#
# This test starts a mounttable server with the neighborhood option enabled,
# and mounts 1) the mounttable on itself as 'myself', and 2) www.google.com:80
# as 'google'.
#
# Then it verifies that <mounttable>.Glob(*) and <neighborhood>.Glob(nhname)
# return the correct result.

source "${VEYRON_ROOT}/environment/scripts/lib/shell_test.sh"

build() {
  local -r GO="${repo_root}/scripts/build/go"
  "${GO}" build veyron/services/mounttable/mounttabled || shell_test::fail "line ${LINENO}: failed to build mounttabled"
  "${GO}" build veyron/tools/mounttable || shell_test::fail "line ${LINENO}: failed to build mounttable"
}

main() {
  cd "${TMPDIR}"
  build

  # Start mounttabled and find its endpoint.
  local NHNAME=test$$
  local MTLOG="${TMPDIR}/mt.log"
  ./mounttabled --address=localhost:0 --neighborhood_name="${NHNAME}" > "${MTLOG}" 2>&1 &

  for i in 1 2 3 4; do
    local EP=$(grep "Mount table service at:" "${MTLOG}" | sed -e 's/^.*endpoint: //')
    if [[ -n "${EP}" ]]; then
      break
    fi
    sleep 1
  done

  [[ -z "${EP}" ]] && shell_test::fail "line ${LINENO}: no server"

  # Get the neighborhood endpoint from the mounttable.
  local -r NHEP=$(./mounttable glob ${EP} nh | grep ^nh | cut -d' ' -f2)
  [[ -z "${NHEP}" ]] && shell_test::fail "line ${LINENO}: no neighborhood server"

  # Mount objects and verify the result.
  ./mounttable mount "${EP}/myself" "${EP}" 5m > /dev/null || shell_test::fail "line ${LINENO}: failed to mount the mounttable on itself"
  ./mounttable mount "${EP}/google" /www.google.com:80 5m > /dev/null || shell_test::fail "line ${LINENO}: failed to mount www.google.com"

  # <mounttable>.Glob('*')
  local GOT=$(./mounttable glob "${EP}" '*' | sed 's/TTL .m..s/TTL XmXXs/' | sort)
  local WANT="[${EP}]
google /www.google.com:80 (TTL XmXXs)
myself ${EP} (TTL XmXXs)
nh ${NHEP} (TTL XmXXs)"
  if [[ "${GOT}" != "${WANT}" ]]; then
    shell_test::fail "line ${LINENO}: unexpected output. Got ${GOT}, want ${WANT}"
  fi

  # <neighborhood>.Glob('NHNAME')
  GOT=$(./mounttable glob "${NHEP}" "${NHNAME}" | sed 's/TTL .m..s/TTL XmXXs/' | sort)
  WANT="[${NHEP}]
${NHNAME} ${EP} (TTL XmXXs)"
  if [[ "${GOT}" != "${WANT}" ]]; then
    shell_test::fail "line ${LINENO}: unexpected output. Got ${GOT}, want ${WANT}"
  fi

  shell_test::pass
}

main "$@"
