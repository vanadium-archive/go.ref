#!/bin/bash

# Tests this example.
#
# Builds binaries, starts up services, waits a few seconds, then checks that the
# store browser responds with valid data.

set -e
set -u

readonly repo_root=$(git rev-parse --show-toplevel)
readonly thisscript="$0"
readonly workdir=$(mktemp -d "${repo_root}/go/tmp.XXXXXXXXXXX")

trap onexit INT TERM EXIT

onexit() {
  exec 2>/dev/null
  kill $(jobs -p)
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

main() {
  cd "${repo_root}/go/src/veyron/examples/mdb"
  make build || fail "line ${LINENO}: failed to build"
  ./run.sh >/dev/null 2>&1 &

  sleep 5  # Wait for services to warm up.

  local -r URL="http://localhost:5000"
  local -r FILE="${workdir}/index.html"

  curl 2>/dev/null "${URL}" -o "${FILE}" || fail "line ${LINENO}: failed to fetch ${URL}"

  if grep -q "moviesbox" "${FILE}"; then
    pass
  else
    cat ${FILE}
    fail "line ${LINENO}: fetched page does not meet expectations"
  fi
}

main "$@"
