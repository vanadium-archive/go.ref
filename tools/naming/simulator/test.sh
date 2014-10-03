#!/bin/bash

# Test the simulator command-line tool.
#

source "${VEYRON_ROOT}/scripts/lib/shell_test.sh"

main() {
  # Build binaries.
  cd "${TMPDIR}"
  local -r PKG=veyron.io/veyron/veyron/tools/naming/simulator
  veyron go build "${PKG}" || shell_test::fail "line ${LINENO}: failed to build simulator"

  local -r DIR=$(shell::go_package_dir "${PKG}")
  local file
  for file in ${DIR}/*.scr; do
    echo $file
    ./simulator < $file > /dev/null 2>&1 || shell_test::fail "failed for $file"
  done
  shell_test::pass
}

main "$@"
