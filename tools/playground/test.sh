#!/bin/bash

# Test the playground builder tool.

source "${VEYRON_ROOT}/scripts/lib/shell_test.sh"

# Installs the veyron.js library and makes it accessible to javascript files in
# the veyron playground test folder under the module name 'veyron'.
install_veyron_js() {
  # TODO(nlacasse): Once veyron.js is publicly available in npm, replace this
  # with "npm install veyron".
  pushd "${VEYRON_ROOT}/veyron.js"
  "${VEYRON_ROOT}/environment/cout/node/bin/npm" link
  popd
  "${VEYRON_ROOT}/environment/cout/node/bin/npm" link veyron
}

# Installs the pgbundle tool.
install_pgbundle() {
  pushd "${VEYRON_ROOT}/veyron/go/src/veyron.io/veyron/veyron/tools/playground/pgbundle"
  "${VEYRON_ROOT}/environment/cout/node/bin/npm" link
  popd
  "${VEYRON_ROOT}/environment/cout/node/bin/npm" link pgbundle
}

build() {
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

test_with_files() {
  local -r TESTDIR=$(shell::tmp_dir)
  cp "$@" ${TESTDIR}
  ./node_modules/.bin/pgbundle ${TESTDIR}
  rm -f builder.out
  ./builder < "${TESTDIR}/bundle.json" 2>&1 | tee builder.out
}

main() {
  cd $(shell::tmp_dir)

  build
  install_veyron_js
  install_pgbundle

  local -r DIR="$(shell::go_package_dir veyron.io/veyron/veyron/tools/playground/testdata)"

  export GOPATH="$(pwd):$(veyron env GOPATH)"
  export VDLPATH="$(pwd):$(veyron env VDLPATH)"
  export PATH="$(pwd):${PATH}"

  # Test without identities

  test_with_files "${DIR}/pingpong/wire.vdl" "${DIR}/pong/pong.go" "${DIR}/ping/ping.go" || shell_test::fail "line ${LINENO}: basic ping (go -> go)"
  grep -q PING builder.out || shell_test::fail "line ${LINENO}: no PING"
  grep -q PONG builder.out || shell_test::fail "line ${LINENO}: no PONG"

  test_with_files "${DIR}/pong/pong.js" "${DIR}/ping/ping.js" || shell_test::fail "line ${LINENO}: basic ping (js -> js)"
  grep -q PING builder.out || shell_test::fail "line ${LINENO}: no PING"
  grep -q PONG builder.out || shell_test::fail "line ${LINENO}: no PONG"

  test_with_files "${DIR}/pong/pong.go" "${DIR}/ping/ping.js" "${DIR}/pingpong/wire.vdl" || shell_test::fail "line ${LINENO}: basic ping (js -> go)"
  grep -q PING builder.out || shell_test::fail "line ${LINENO}: no PING"
  grep -q PONG builder.out || shell_test::fail "line ${LINENO}: no PONG"

  test_with_files "${DIR}/pong/pong.js" "${DIR}/ping/ping.go" "${DIR}/pingpong/wire.vdl" || shell_test::fail "line ${LINENO}: basic ping (go -> js)"
  grep -q PING builder.out || shell_test::fail "line ${LINENO}: no PING"
  grep -q PONG builder.out || shell_test::fail "line ${LINENO}: no PONG"

  # Test with authorized identities

  test_with_files "${DIR}/pong/pong.go" "${DIR}/ping/ping.go" "${DIR}/pingpong/wire.vdl" "${DIR}/ids/authorized.id" || shell_test::fail "line ${LINENO}: authorized id (go -> go)"
  grep -q PING builder.out || shell_test::fail "line ${LINENO}: no PING"
  grep -q PONG builder.out || shell_test::fail "line ${LINENO}: no PONG"

  test_with_files "${DIR}/pong/pong.js" "${DIR}/ping/ping.js" "${DIR}/ids/authorized.id" || shell_test::fail "line ${LINENO}: authorized id (js -> js)"
  grep -q PING builder.out || shell_test::fail "line ${LINENO}: no PING"
  grep -q PONG builder.out || shell_test::fail "line ${LINENO}: no PONG"

  # Test with expired identities

  test_with_files "${DIR}/pong/pong.go" "${DIR}/ping/ping.go" "${DIR}/pingpong/wire.vdl" "${DIR}/ids/expired.id" || shell_test::fail  "line ${LINENO}: failed to build with expired id (go -> go)"
  grep -q "ipc: not authorized" builder.out || shell_test::fail "line ${LINENO}: rpc with expired id succeeded"

  test_with_files "${DIR}/pong/pong.js" "${DIR}/ping/ping.js" "${DIR}/ids/expired.id" || shell_test::fail  "line ${LINENO}: failed to build with expired id (js -> js)"
  grep -q "ipc: not authorized" builder.out || shell_test::fail "line ${LINENO}: rpc with expired id succeeded"

  # Test with unauthorized identities

  test_with_files "${DIR}/pong/pong.go" "${DIR}/ping/ping.go" "${DIR}/pingpong/wire.vdl" "${DIR}/ids/unauthorized.id" || shell_test::fail  "line ${LINENO}: failed to build with unauthorized id (go -> go)"
  grep -q "ipc: not authorized" builder.out || shell_test::fail "line ${LINENO}: rpc with unauthorized id succeeded"

  # TODO(nlacasse): Write the javascript version of this test once the
  # javascript implementation is capable of checking that an identity is
  # authorized.

  shell_test::pass
}

main "$@"
