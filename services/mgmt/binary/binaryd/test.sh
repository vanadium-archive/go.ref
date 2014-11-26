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
  shell_test::start_server "${BINARYD_BIN}" --name="${REPO}" --veyron.tcp.address=127.0.0.1:0 --http=127.0.0.1:0 \
    || shell_test::fail "line ${LINENO} failed to start binaryd"
  local -r HTTP_ADDR=$(grep 'HTTP server at: "' "${START_SERVER_LOG_FILE}" | sed -e 's/^.*HTTP server at: "//' | sed -e 's/"$//')

  # Create a binary file.
  local -r BINARY_SUFFIX="test-binary"
  local -r BINARY="${REPO}/${BINARY_SUFFIX}"
  local -r BINARY_FILE="${WORKDIR}/bin1"
  dd if=/dev/urandom of="${BINARY_FILE}" bs=1000000 count=16 \
    || shell_test::fail "line ${LINENO}: faile to create a random binary file"
  "${BINARY_BIN}" upload "${BINARY}" "${BINARY_FILE}" || shell_test::fail "line ${LINENO}: 'upload' failed"

  # Create TAR file.
  local -r TAR="${REPO}/tarobj"
  local -r TAR_FILE="${WORKDIR}/bin1.tar.gz"
  tar zcvf "${TAR_FILE}" "${BINARY_FILE}"
  "${BINARY_BIN}" upload "${TAR}" "${TAR_FILE}" || shell_test::fail "line ${LINENO}: 'upload' failed"

  # Download the binary file.
  local -r BINARY_FILE2="${WORKDIR}/bin2"
  "${BINARY_BIN}" download "${BINARY}" "${BINARY_FILE2}" || shell_test::fail "line ${LINENO}: 'RPC download' failed"
  if [[ $(cmp "${BINARY_FILE}" "${BINARY_FILE2}" &> /dev/null) ]]; then
    shell_test::fail "mismatching binary file downloaded via RPC"
  fi
  local -r BINARY_FILE2_INFO=$(cat "${BINARY_FILE2}.info")
  shell_test::assert_eq "${BINARY_FILE2_INFO}" '{"type":"application/octet-stream"}' "${LINENO}"

  # Download the tar file.
  local -r TAR_FILE2="${WORKDIR}/downloadedtar"
  "${BINARY_BIN}" download "${TAR}" "${TAR_FILE2}" || shell_test::fail "line ${LINENO}: 'RPC download' failed"
  if [[ $(cmp "${TAR_FILE}" "${TAR_FILE2}" &> /dev/null) ]]; then
    shell_test::fail "mismatching tar file downloaded via RPC"
  fi
  local -r TAR_FILE2_INFO=$(cat "${TAR_FILE2}.info")
  shell_test::assert_eq "${TAR_FILE2_INFO}" '{"encoding":"gzip","type":"application/x-tar"}' "${LINENO}"

  local -r BINARY_FILE3="${WORKDIR}/bin3"
  curl -f -o "${BINARY_FILE3}" "http://${HTTP_ADDR}/${BINARY_SUFFIX}" || shell_test::fail "line ${LINENO}: 'HTTP download' failed"
  if [[ $(cmp "${BINARY_FILE}" "${BINARY_FILE3}" &> /dev/null) ]]; then
    shell_test::fail "mismatching binary file downloaded via HTTP"
  fi

  # Remove the files.
  "${BINARY_BIN}" delete "${BINARY}" || shell_test::fail "line ${LINENO}: 'delete' failed"
  "${BINARY_BIN}" delete "${TAR}" || shell_test::fail "line ${LINENO}: 'delete' failed"

  # Check the files no longer exist.
  local RESULT=$(shell::check_result "${BINARY_BIN}" download "${BINARY}" "${BINARY_FILE2}")
  shell_test::assert_ne "${RESULT}" "0" "${LINENO}"
  RESULT=$(shell::check_result "${BINARY_BIN}" download "${TAR}" "${TAR_FILE2}")
  shell_test::assert_ne "${RESULT}" "0" "${LINENO}"

  shell_test::pass
}

main "$@"
