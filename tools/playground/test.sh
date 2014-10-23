#!/bin/bash

# Tests the playground builder tool.

# TODO(sadovsky): Much of the setup code below also exists in
# veyron-www/test/playground_test.sh.

source "${VEYRON_ROOT}/scripts/lib/shell_test.sh"

# Installs the veyron.js library and makes it accessible to javascript files in
# the veyron playground test folder under the module name 'veyron'.
install_veyron_js() {
  # TODO(nlacasse): Once veyron.js is publicly available in npm, replace this
  # with "npm install veyron".
  pushd "${VEYRON_ROOT}/veyron.js"
  npm link
  popd
  npm link veyron
}

# Installs the pgbundle tool.
install_pgbundle() {
  pushd "${VEYRON_ROOT}/veyron/go/src/veyron.io/veyron/veyron/tools/playground/pgbundle"
  npm link
  popd
  npm link pgbundle
}

# Installs various go binaries.
build_go_binaries() {
  # Note that "go build" puts built binaries in $(pwd), but only if they are
  # built one at a time. So much for the principle of least surprise...
  local -r V="veyron.io/veyron/veyron"
  veyron go build $V/tools/identity || shell_test::fail "line ${LINENO}: failed to build 'identity'"
  veyron go build $V/services/proxy/proxyd || shell_test::fail "line ${LINENO}: failed to build 'proxyd'"
  veyron go build $V/services/mounttable/mounttabled || shell_test::fail "line ${LINENO}: failed to build 'mounttabled'"
  veyron go build $V/tools/playground/builder || shell_test::fail "line ${LINENO}: failed to build 'builder'"
  veyron go build veyron.io/veyron/veyron2/vdl/vdl || shell_test::fail "line ${LINENO}: failed to build 'vdl'"
  veyron go build veyron.io/wspr/veyron/services/wsprd || shell_test::fail "line ${LINENO}: failed to build 'wsprd'"
}

# Sets up a directory with the given files, then runs builder.
test_with_files() {
  local -r TESTDATA_DIR="$(shell::go_package_dir veyron.io/veyron/veyron/tools/playground/testdata)"

  # Write input files to a fresh dir, then run pgbundle.
  local -r PGBUNDLE_DIR=$(shell::tmp_dir)
  for f in $@; do
    fdir="${PGBUNDLE_DIR}/$(dirname ${f})"
    mkdir -p "${fdir}"
    cp "${TESTDATA_DIR}/${f}" "${fdir}/"
  done

  ./node_modules/.bin/pgbundle "${PGBUNDLE_DIR}"

  # Create a fresh dir to run bundler from.
  local -r ORIG_DIR=$(pwd)
  pushd $(shell::tmp_dir)
  ln -s "${ORIG_DIR}/node_modules" ./  # for veyron.js
  "${ORIG_DIR}/builder" < "${PGBUNDLE_DIR}/bundle.json" 2>&1 | tee builder.out
  # Move builder output to original dir for verification.
  mv builder.out "${ORIG_DIR}"
  popd
}

main() {
  cd $(shell::tmp_dir)

  export GOPATH="$(pwd):$(veyron env GOPATH)"
  export VDLPATH="$(pwd):$(veyron env VDLPATH)"
  export PATH="$(pwd):${VEYRON_ROOT}/environment/cout/node/bin:${PATH}"

  build_go_binaries
  install_veyron_js
  install_pgbundle

  echo -e "\n\n>>>>> Test without identities\n\n"

  test_with_files "src/pingpong/wire.vdl" "src/pong/pong.go" "src/ping/ping.go" || shell_test::fail "line ${LINENO}: basic ping (go -> go)"
  grep -q PING builder.out || shell_test::fail "line ${LINENO}: no PING"
  grep -q PONG builder.out || shell_test::fail "line ${LINENO}: no PONG"

  test_with_files "src/pong/pong.js" "src/ping/ping.js" || shell_test::fail "line ${LINENO}: basic ping (js -> js)"
  grep -q PING builder.out || shell_test::fail "line ${LINENO}: no PING"
  grep -q PONG builder.out || shell_test::fail "line ${LINENO}: no PONG"

  test_with_files "src/pong/pong.go" "src/ping/ping.js" "src/pingpong/wire.vdl" || shell_test::fail "line ${LINENO}: basic ping (js -> go)"
  grep -q PING builder.out || shell_test::fail "line ${LINENO}: no PING"
  grep -q PONG builder.out || shell_test::fail "line ${LINENO}: no PONG"

  test_with_files "src/pong/pong.js" "src/ping/ping.go" "src/pingpong/wire.vdl" || shell_test::fail "line ${LINENO}: basic ping (go -> js)"
  grep -q PING builder.out || shell_test::fail "line ${LINENO}: no PING"
  grep -q PONG builder.out || shell_test::fail "line ${LINENO}: no PONG"

  echo -e "\n\n>>>>> Test with authorized identities\n\n"

  test_with_files "src/pong/pong.go" "src/ping/ping.go" "src/pingpong/wire.vdl" "src/ids/authorized.id" || shell_test::fail "line ${LINENO}: authorized id (go -> go)"
  grep -q PING builder.out || shell_test::fail "line ${LINENO}: no PING"
  grep -q PONG builder.out || shell_test::fail "line ${LINENO}: no PONG"

  test_with_files "src/pong/pong.js" "src/ping/ping.js" "src/ids/authorized.id" || shell_test::fail "line ${LINENO}: authorized id (js -> js)"
  grep -q PING builder.out || shell_test::fail "line ${LINENO}: no PING"
  grep -q PONG builder.out || shell_test::fail "line ${LINENO}: no PONG"

  echo -e "\n\n>>>>> Test with expired identities\n\n"

  test_with_files "src/pong/pong.go" "src/ping/ping.go" "src/pingpong/wire.vdl" "src/ids/expired.id" || shell_test::fail  "line ${LINENO}: failed to build with expired id (go -> go)"
  grep -q "ipc: not authorized" builder.out || shell_test::fail "line ${LINENO}: rpc with expired id succeeded"

  test_with_files "src/pong/pong.js" "src/ping/ping.js" "src/ids/expired.id" || shell_test::fail  "line ${LINENO}: failed to build with expired id (js -> js)"
  grep -q "ipc: not authorized" builder.out || shell_test::fail "line ${LINENO}: rpc with expired id succeeded"

  echo -e "\n\n>>>>> Test with unauthorized identities\n\n"

  test_with_files "src/pong/pong.go" "src/ping/ping.go" "src/pingpong/wire.vdl" "src/ids/unauthorized.id" || shell_test::fail  "line ${LINENO}: failed to build with unauthorized id (go -> go)"
  grep -q "ipc: not authorized" builder.out || shell_test::fail "line ${LINENO}: rpc with unauthorized id succeeded"

  # TODO(nlacasse): Write the javascript version of this test once the
  # javascript implementation is capable of checking that an identity is
  # authorized.

  shell_test::pass
}

main "$@"
