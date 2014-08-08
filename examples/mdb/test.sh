#!/bin/bash

# Tests the mdb example.
#
# Builds binaries, starts up services, waits a few seconds, then checks that the
# store browser responds with valid data.

set -e
set -u

readonly THIS_SCRIPT="$0"
readonly WORK_DIR=$(mktemp -d)

trap onexit INT TERM EXIT

onexit() {
  exec 2>/dev/null
  kill $(jobs -p)
  rm -rf "${WORK_DIR}"
}

fail() {
  [[ $# -gt 0 ]] && echo "${THIS_SCRIPT} $*"
  echo FAIL
  exit 1
}

pass() {
  echo PASS
  exit 0
}

main() {
  make build || fail "line ${LINENO}: failed to build"
  ./run.sh >/dev/null 2>&1 &

  sleep 5  # Wait for services to warm up.

  URL="http://localhost:5000"
  FILE="${WORK_DIR}/index.html"

  curl 2>/dev/null "${URL}" -o "${FILE}" || fail "line ${LINENO}: failed to fetch ${URL}"

  if grep -q moviesbox "${FILE}"; then
    pass
  else
    cat ${FILE}
    fail "line ${LINENO}: fetched page does not meet expectations"
  fi
}

main "$@"
