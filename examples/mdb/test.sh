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

  local -r VIEWER_PORT_FILE="${workdir}/viewer_port.txt"
  ./run.sh "${VIEWER_PORT_FILE}" >/dev/null 2>&1 &

  sleep 5  # Wait for services to warm up.

  if [ ! -f "${VIEWER_PORT_FILE}" ]; then
    fail "line ${LINENO}: failed to get viewer url"
  fi
  local VIEWER_PORT
  VIEWER_PORT=$(cat "${VIEWER_PORT_FILE}")

  local -r HTML_FILE="${workdir}/index.html"
  curl 2>/dev/null "http://localhost:${VIEWER_PORT}" -o "${HTML_FILE}" || fail "line ${LINENO}: failed to fetch ${URL}"

  if grep -q "moviesbox" "${HTML_FILE}"; then
    pass
  else
    cat "${HTML_FILE}"
    fail "line ${LINENO}: fetched page does not meet expectations"
  fi
}

main "$@"
