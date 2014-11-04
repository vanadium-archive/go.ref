#!/bin/bash

# Test the binary repository daemon.
#
# This test starts a binary repository daemon and uses the binary
# repository client to verify that <binary>.Upload(),
# <binary>.Download(), and <binary>.Delete() work as expected.

source "${VEYRON_ROOT}/scripts/lib/shell_test.sh"

readonly WORKDIR="${shell_test_WORK_DIR}"

build() {
  BINARYD_BIN="$(shell_test::build_go_binary 'veyron.io/veyron/veyron/services/mgmt/binary/binaryd')"
  BINARY_BIN="$(shell_test::build_go_binary 'veyron.io/veyron/veyron/tools/binary')"
}

main() {
  cd "${WORKDIR}"
  build

  shell_test::setup_server_test

  # Start the binary repository daemon.
  local -r REPO="binaryd-test-repo"
  shell_test::start_server "${BINARYD_BIN}" --name="${REPO}" --veyron.tcp.address=127.0.0.1:0 \
    || shell_test::fail "line ${LINENO} failed to start binaryd"

  # Create a binary file.
  local -r BINARY_SUFFIX="test-binary"
  local -r BINARY="${REPO}/${BINARY_SUFFIX}"
  local -r BINARY_FILE=$(shell::tmp_file)
  dd if=/dev/urandom of="${BINARY_FILE}" bs=1000000 count=16 \
    || shell_test::fail "line ${LINENO}: faile to create a random binary file"
  "${BINARY_BIN}" upload "${BINARY}" "${BINARY_FILE}" || shell_test::fail "line ${LINENO}: 'upload' failed"

  # Download the binary file.
  local -r BINARY_FILE2=$(shell::tmp_file)
  "${BINARY_BIN}" download "${BINARY}" "${BINARY_FILE2}" || shell_test::fail "line ${LINENO}: 'RPC download' failed"
  if [[ $(cmp "${BINARY_FILE}" "${BINARY_FILE2}" &> /dev/null) ]]; then
    shell_test::fail "mismatching binary file downloaded via RPC"
  fi

  local -r BINARY_FILE3=$(shell::tmp_file)
  curl -f -o "${BINARY_FILE3}" http://localhost:8080/"${BINARY_SUFFIX}" || shell_test::fail "line ${LINENO}: 'HTTP download' failed"
  if [[ $(cmp "${BINARY_FILE}" "${BINARY_FILE3}" &> /dev/null) ]]; then
    shell_test::fail "mismatching binary file downloaded via HTTP"
  fi

  # Remove the binary file.
  "${BINARY_BIN}" delete "${BINARY}" || shell_test::fail "line ${LINENO}: 'delete' failed"

  # Check the binary no longer exists.
  local -r RESULT=$(shell::check_result "${BINARY_BIN}" download "${BINARY}" "${BINARY_FILE2}")
  shell_test::assert_ne "${RESULT}" "0" "${LINENO}"

  shell_test::pass
}

main "$@"
