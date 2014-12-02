#!/bin/bash

# Test the simulator command-line tool.
#

source "${VEYRON_ROOT}/scripts/lib/shell_test.sh"

readonly WORKDIR="${shell_test_WORK_DIR}"

main() {
  # Build binaries.
  cd "${WORKDIR}"
  PKG="veyron.io/veyron/veyron/tools/naming/simulator"
  SIMULATOR_BIN="$(shell_test::build_go_binary ${PKG})"

  local -r DIR=$(shell::go_package_dir "${PKG}")
  local file
  for file in "${DIR}"/*.scr; do
    echo "${file}"
    "${SIMULATOR_BIN}" --interactive=false < "${file}" &> /dev/null || shell_test::fail "line ${LINENO}: failed for ${file}"
  done
  shell_test::pass
}

main "$@"
