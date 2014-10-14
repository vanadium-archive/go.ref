#!/bin/bash

# Test the build server daemon.
#
# This test starts a build server daemon and uses the build client to
# verify that <build>.Build() works as expected.

source "${VEYRON_ROOT}/scripts/lib/shell_test.sh"

build() {
  veyron go build veyron.io/veyron/veyron/services/mgmt/build/buildd || shell_test::fail "line ${LINENO}: failed to build 'buildd'"
  veyron go build veyron.io/veyron/veyron/tools/build || shell_test::fail "line ${LINENO}: failed to build 'build'"
}

main() {
  cd "${TMPDIR}"
  build

  shell_test::setup_server_test

  # Start the binary repository daemon.
  local -r SERVER="buildd-test-server"
  local GO_BIN=$(which go)
  local -r GO_ROOT=$("${GO_BIN}" env GOROOT)
  shell_test::start_server ./buildd --name="${SERVER}" --gobin="${GO_BIN}" --goroot="${GO_ROOT}" --veyron.tcp.address=127.0.0.1:0 || shell_test::fail "line ${LINENO} failed to start server"

  # Create and build a test source file.
  local -r GO_PATH=$(shell::tmp_dir)
  local -r BIN_DIR="${GO_PATH}/bin"
  mkdir -p "${BIN_DIR}"
  local -r SRC_DIR="${GO_PATH}/src/test"
  mkdir -p "${SRC_DIR}"
  local -r SRC_FILE="${SRC_DIR}/test.go"
  cat > "${SRC_FILE}" <<EOF
package main

import "fmt"

func main() {
  fmt.Printf("Hello World!\n")
}
EOF
  GOPATH="${GO_PATH}" GOROOT="${GO_ROOT}" TMPDIR="${BIN_DIR}" ./build build "${SERVER}" "test" || shell_test::fail "line ${LINENO}: 'build' failed"
  if [[ ! -e "${BIN_DIR}/test" ]]; then
    shell_test::fail "test binary not found"
  fi
  local -r GOT=$("${BIN_DIR}/test")
  local -r WANT="Hello World!"
  if [[ "${GOT}" != "${WANT}" ]]; then
    shell_test::fail "unexpected result: want '${WANT}', got '${GOT}'"
  fi

  shell_test::pass
}

main "$@"
