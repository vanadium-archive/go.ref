#!/bin/bash

# Test the playground builder tool.

source "${VEYRON_ROOT}/environment/scripts/lib/shell_test.sh"

build() {
  local -r GO="${REPO_ROOT}/scripts/build/go"
  "${GO}" build veyron/tools/identity || shell_test::fail "line ${LINENO}: failed to build 'identity'"
  "${GO}" build veyron/services/mounttable/mounttabled || shell_test::fail "line ${LINENO}: failed to build 'mounttabled'"
  "${GO}" build veyron2/vdl/vdl || shell_test::fail "line ${LINENO}: failed to build 'vdl'"
  "${GO}" build veyron/tools/playground/builder || shell_test::fail "line ${LINENO}: failed to build 'builder'"
  "${GO}" build veyron/tools/playground/testdata/escaper || shell_test::fail "line ${LINENO}: failed to build 'escaper'"
}

test_with_files() {
    echo '{"Files":[' > request.json
    while [[ $# > 0 ]]; do
	echo '{"Name":"'"$(basename $1)"'","Body":' >>request.json
	grep -v OMIT $1 | ./escaper >>request.json
	shift
	if [[ $# > 0 ]]; then
	    echo '},' >>request.json
	else
	    echo '}' >>request.json
	fi
    done
    echo ']}' >>request.json
    rm -f builder.out
    ./builder <request.json 2>&1 | tee builder.out
}

main() {
  cd $(shell::tmp_dir)
  build

  local -r DIR="${REPO_ROOT}/go/src/veyron/tools/playground/testdata"

  export GOPATH="$(pwd)"
  export PATH="$(pwd):$PATH"
  test_with_files $DIR/pingpong/wire.vdl $DIR/pong/pong.go $DIR/ping/ping.go || shell_test::fail "line ${LINENO}: basic ping"
  grep -q ping builder.out || shell_test::fail "line ${LINENO}: no ping"
  grep -q pong builder.out || shell_test::fail "line ${LINENO}: no pong"

  test_with_files $DIR/pingpong/wire.vdl $DIR/pong/pong.go $DIR/ping/ping.go $DIR/ids/authorized.id || shell_test::fail "line ${LINENO}: authorized id"
  grep -q ping builder.out || shell_test::fail "line ${LINENO}: no ping"
  grep -q pong builder.out || shell_test::fail "line ${LINENO}: no pong"

  test_with_files $DIR/pingpong/wire.vdl $DIR/pong/pong.go $DIR/ping/ping.go $DIR/ids/expired.id || shell_test::fail  "line ${LINENO}: failed to build with expired id"
  grep -q "ipc: not authorized" builder.out || shell_test::fail "line ${LINENO}: rpc with expired id succeeded"

  test_with_files $DIR/pingpong/wire.vdl $DIR/pong/pong.go $DIR/ping/ping.go $DIR/ids/unauthorized.id || shell_test::fail  "line ${LINENO}: failed to build with unauthorized id"
  grep -q "ipc: not authorized" builder.out || shell_test::fail "line ${LINENO}: rpc with unauthorized id succeeded"

  shell_test::pass
}

main "$@"
