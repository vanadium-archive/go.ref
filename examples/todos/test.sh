#!/bin/bash

# Tests this example.
#
# Builds binaries, starts up services, waits a few seconds, then checks that the
# store browser responds with valid data.

source "${VEYRON_ROOT}/environment/scripts/lib/shell_test.sh"

main() {
  cd "${REPO_ROOT}/go/src/veyron/examples/todos"
  make buildgo &>/dev/null || shell_test::fail "line ${LINENO}: failed to build"
  local -r VIEWER_PORT_FILE="${TMPDIR}/viewer_port.txt"
  ./run.sh "${VIEWER_PORT_FILE}" &>/dev/null &

  sleep 5  # Wait for services to warm up.

  if [ ! -f "${VIEWER_PORT_FILE}" ]; then
    shell_test::fail "line ${LINENO}: failed to get viewer url"
  fi
  local -r VIEWER_PORT=$(cat "${VIEWER_PORT_FILE}")

  local -r HTML_FILE="${TMPDIR}/index.html"
  local -r URL="http://127.0.0.1:${VIEWER_PORT}"
  curl 2>/dev/null "${URL}" -o "${HTML_FILE}" || shell_test::fail "line ${LINENO}: failed to fetch ${URL}"

  if grep -q "/lists" "${HTML_FILE}"; then
    shell_test::pass
  else
    cat "${HTML_FILE}"
    shell_test::fail "line ${LINENO}: fetched page does not meet expectations"
  fi
}

main "$@"
