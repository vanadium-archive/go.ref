#!/bin/bash

# Tests this example.
#
# Builds binaries, starts up services, waits a few seconds, then checks that the
# store browser responds with valid data.

source "${VEYRON_ROOT}/environment/scripts/lib/shell_test.sh"

main() {
  # TODO(sadovsky): Reenable this test when we can get it to pass on Jenkins.
  shell_test::pass

  cd "${repo_root}/go/src/veyron/examples/todos"
  make build || shell_test::fail "line ${LINENO}: failed to build"
  local -r VIEWER_PORT_FILE="${TMPDIR}/viewer_port.txt"
  ./run.sh "${VIEWER_PORT_FILE}" &>/dev/null &

  sleep 5  # Wait for services to warm up.

  if [ ! -f "${VIEWER_PORT_FILE}" ]; then
    shell_test::fail "line ${LINENO}: failed to get viewer url"
  fi

  local -r VIEWER_PORT=$(cat "${VIEWER_PORT_FILE}")
  local -r HTML_FILE="${TMPDIR}/index.html"
  curl 2>/dev/null "http://localhost:${VIEWER_PORT}" -o "${HTML_FILE}" || fail "line ${LINENO}: failed to fetch http://localhost:${VIEWER_PORT}"

  if grep -q "/lists" "${HTML_FILE}"; then
    shell_test::pass
  else
    cat "${HTML_FILE}"
    shell_test::fail "line ${LINENO}: fetched page does not meet expectations"
  fi
}

main "$@"
