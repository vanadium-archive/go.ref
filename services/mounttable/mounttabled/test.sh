#!/bin/bash

# Test the mounttabled binary
#
# This test starts a mounttable server with the neighborhood option enabled,
# and mounts 1) the mounttable on itself as 'myself', and 2) www.google.com:80
# as 'google'.
#
# Then it verifies that <mounttable>.Glob(*) and <neighborhood>.Glob(nhname)
# return the correct result.

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
  "$go" build veyron/services/mounttable/mounttabled || fail "line ${LINENO}: failed to build mounttabled"
  "$go" build veyron/tools/mounttable || fail "line ${LINENO}: failed to build mounttable"
}

main() {
  cd "${workdir}"
  build

  # Start mounttabled and find its endpoint.
  local nhname=test$$
  local mtlog="${workdir}/mt.log"
  ./mounttabled --address=localhost:0 --neighborhood_name="${nhname}" > "${mtlog}" 2>&1 &

  for i in 1 2 3 4; do
    ep=$(grep "Mount table service at:" "${mtlog}" | sed -e 's/^.*endpoint: //')
    if [[ -n "${ep}" ]]; then
      break
    fi
    sleep 1
  done

  [[ -z "${ep}" ]] && fail "line ${LINENO}: no server"

  # Get the neighborhood endpoint from the mounttable.
  local nhep=$(./mounttable glob ${ep} nh | grep ^nh | cut -d' ' -f2)
  [[ -z "${nhep}" ]] && fail "line ${LINENO}: no neighborhood server"

  # Mount objects and verify the result.
  ./mounttable mount "${ep}/myself" "${ep}" 5m > /dev/null || fail "line ${LINENO}: failed to mount the mounttable on itself"
  ./mounttable mount "${ep}/google" /www.google.com:80 5m > /dev/null || fail "line ${LINENO}: failed to mount www.google.com"

  # <mounttable>.Glob('*')
  got=$(./mounttable glob ${ep} '*' | sed 's/TTL .m..s/TTL XmXXs/' | sort)
  want="[${ep}]
google /www.google.com:80 (TTL XmXXs)
myself ${ep} (TTL XmXXs)
nh ${nhep} (TTL XmXXs)"

  if [[ "${got}" != "${want}" ]]; then
    fail "line ${LINENO}: unexpected output. Got ${got}, want ${want}"
  fi

  # <neighborhood>.Glob('nhname')
  got=$(./mounttable glob "${nhep}" "${nhname}" | sed 's/TTL .m..s/TTL XmXXs/' | sort)
  want="[${nhep}]
${nhname} ${ep} (TTL XmXXs)"

  if [[ "${got}" != "${want}" ]]; then
    fail "line ${LINENO}: unexpected output. Got ${got}, want ${want}"
  fi

  pass
}

main "$@"
