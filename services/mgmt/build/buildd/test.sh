#!/bin/bash

# Test the build server daemon.
#
# This test starts a build server daemon and uses the build client to
# verify that <build>.Build() works as expected.

source "$(go list -f {{.Dir}} veyron.io/veyron/shell/lib)/shell_test.sh"

# Run the test under the security agent.
shell_test::enable_agent "$@"

readonly WORKDIR="${shell_test_WORK_DIR}"

build() {
  BUILDD_BIN="$(shell_test::build_go_binary 'veyron.io/veyron/veyron/services/mgmt/build/buildd')"
  BUILD_BIN="$(shell_test::build_go_binary 'veyron.io/veyron/veyron/tools/build')"
}

main() {
  cd "${WORKDIR}"
  build

  shell_test::setup_server_test

  # Start the binary repository daemon.
  local -r SERVER="buildd-test-server"
  local GO_BIN=$(which go)
  local -r GO_ROOT=$("${GO_BIN}" env GOROOT)
  shell_test::start_server "${VRUN}" "${BUILDD_BIN}" --name="${SERVER}" --gobin="${GO_BIN}" --goroot="${GO_ROOT}" --veyron.tcp.address=127.0.0.1:0 \
    || shell_test::fail "line ${LINENO} failed to start server"

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
  GOPATH="${GO_PATH}" GOROOT="${GO_ROOT}" TMPDIR="${BIN_DIR}" "${BUILD_BIN}" build "${SERVER}" "test" || shell_test::fail "line ${LINENO}: 'build' failed"
  if [[ ! -e "${BIN_DIR}/test" ]]; then
    shell_test::fail "test binary not found"
  fi
  local -r GOT=$("${BIN_DIR}/test")
  local -r WANT="Hello World!"
  shell_test::assert_eq "${GOT}" "${WANT}" "${LINENO}"

  shell_test::pass
}

main "$@"
