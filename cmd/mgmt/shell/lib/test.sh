#!/bin/bash
# Copyright 2015 The Vanadium Authors. All rights reserved.
# Use of this source code is governed by a BSD-style
# license that can be found in the LICENSE file.

#
# Unit tests for the shell functions in this directory
#

source "$(go list -f {{.Dir}} v.io/x/ref/cmd/mgmt)/shell/lib/shell_test.sh"

test_assert() {
  shell_test::assert_eq "foo" "foo" "${LINENO}"
  shell_test::assert_eq "42" "42" "${LINENO}"
  shell_test::assert_ge "1" "1" "${LINENO}"
  shell_test::assert_ge "42" "1" "${LINENO}"
  shell_test::assert_gt "42" "1" "${LINENO}"
  shell_test::assert_le "1" "1" "${LINENO}"
  shell_test::assert_le "1" "42" "${LINENO}"
  shell_test::assert_lt "1" "42" "${LINENO}"
  shell_test::assert_ne "1" "42" "${LINENO}"
  shell_test::assert_ne "foo" "bar" "${LINENO}"
}

test_run_server() {
  shell::run_server 1 /dev/null /dev/null foobar > /dev/null 2>&1
  shell_test::assert_eq "$?" "1" "${LINENO}"

  shell::run_server 1 /dev/null /dev/null sleep 10 > /dev/null 2>&1
  shell_test::assert_eq "$?" "0" "${LINENO}"
}

test_timed_wait_for() {
  local GOT WANT
  local -r TMPFILE=$(shell::tmp_file)
  touch "${TMPFILE}"

  shell::timed_wait_for 2 "${TMPFILE}" "doesn't matter, it's all zeros anyway"
  shell_test::assert_eq "$?" "1" "${LINENO}"

  echo "foo" > "${TMPFILE}"
  shell::timed_wait_for 2 "${TMPFILE}" "bar"
  shell_test::assert_eq "$?" "1" "${LINENO}"

  echo "bar" >> "${TMPFILE}"
  echo "baz" >> "${TMPFILE}"
  shell::timed_wait_for 2 "${TMPFILE}" "bar"
  shell_test::assert_eq "$?" "0" "${LINENO}"
}

# rmpublickey replaces public keys (16 hex bytes, :-separated) with XX:....
# This substitution enables comparison with golden output even when keys are freshly
# minted by the "principal create" command.
rmpublickey() {
    sed -e "s/\([0-9a-f]\{2\}:\)\{15\}[0-9a-f]\{2\}/XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX/g"
}

test_credentials() {
  local -r CRED=$(shell_test::credentials alice)

  local -r PRINCIPAL_BIN="$(shell_test::build_go_binary 'v.io/x/ref/cmd/principal')"

  "${PRINCIPAL_BIN}" --veyron.credentials="${CRED}" dump >alice.dump ||  shell_test::fail "line ${LINENO}: ${PRINCIPAL_BIN} dump ${CRED} failed"
  cat alice.dump | rmpublickey >got || shell_test::fail "line ${LINENO}: cat alice.dump | rmpublickey failed"
  cat >want <<EOF
Public key : XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX
---------------- BlessingStore ----------------
Default blessings: alice
Peer pattern                   : Blessings
...                            : alice
---------------- BlessingRoots ----------------
Public key                                      : Pattern
XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX : [alice]
EOF
  if ! diff got want; then
    shell_test::fail "line ${LINENO}"
  fi
}

test_forkcredentials() {
  local -r CRED=$(shell_test::credentials alice)
  local -r FORKCRED=$(shell_test::forkcredentials "${CRED}" fork)

  local -r PRINCIPAL_BIN="$(shell_test::build_go_binary 'v.io/x/ref/cmd/principal')"

  "${PRINCIPAL_BIN}" --veyron.credentials="${FORKCRED}" dump >alice.dump ||  shell_test::fail "line ${LINENO}: ${PRINCIPAL_BIN} dump ${CRED} failed"
  cat alice.dump | rmpublickey >got || shell_test::fail "line ${LINENO}: cat alice.dump | rmpublickey failed"
  cat >want <<EOF
Public key : XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX
---------------- BlessingStore ----------------
Default blessings: alice/fork
Peer pattern                   : Blessings
...                            : alice/fork
---------------- BlessingRoots ----------------
Public key                                      : Pattern
XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX : [alice]
XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX : [self]
EOF
  if ! diff got want; then
    shell_test::fail "line ${LINENO}"
  fi
}

main() {
  test_assert || shell_test::fail "assert"
  test_run_server || shell_test::fail "test_run_server"
  test_timed_wait_for  || shell_test::fail "test_run_server"
  test_credentials  || shell_test::fail "test_credentials"
  test_forkcredentials  || shell_test::fail "test_forkcredentials"

  shell_test::pass
}

main "$@"
