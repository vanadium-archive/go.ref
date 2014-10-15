#!/bin/bash

# Test the principal command-line tool.
#
# This tests most operations of the principal command-line tool.
# Not the "seekblessing" command yet, since that requires
# starting a separate server.

source "${VEYRON_ROOT}/scripts/lib/shell_test.sh"

readonly WORKDIR=$(shell::tmp_dir)

build() {
  veyron go build veyron.io/veyron/veyron/tools/principal || shell_test::fail "line ${LINENO}: failed to build principal"
}

extractBlessings() {
    awk '/Blessings/ { st = index($0," "); print substr($0,st+1)}'
}

main() {
  local GOT WANT

  # Build binaries.
  cd "${WORKDIR}"
  build

  # Set VEYRON_CREDENTIALS.
  export VEYRON_CREDENTIALS="${WORKDIR}"

  ./principal dump >/dev/null || shell_test::fail "line ${LINENO}: dump failed"
  ./principal blessself >/dev/null || shell_test::fail "line ${LINENO}: blessself failed"
  ./principal blessself alice >alice || shell_test::fail "line ${LINENO}: blessself alice failed"
  ./principal store.default >/dev/null || shell_test::fail "line ${LINENO}: store.default failed"
  ./principal store.forpeer >/dev/null || shell_test::fail "line ${LINENO}: store.forpeer failed"

  # Test print
  GOT=$(./principal print alice | extractBlessings) \
    || shell_test::fail "line ${LINENO}: failed to run principal"
  WANT="alice(0 caveats)"
  shell_test::assert_eq "${GOT}" "${WANT}" "${LINENO}"

  GOT=$(./principal blessself bob | ./principal print - | extractBlessings) \
    || shell_test::fail "line ${LINENO}: failed to run principal"
  WANT="bob(0 caveats)"
  shell_test::assert_eq "${GOT}" "${WANT}" "${LINENO}"

  GOT=$(./principal blessself --for=1h bob| ./principal print - | extractBlessings) \
    || shell_test::fail "line ${LINENO}: failed to run principal"
  WANT="bob(1 caveats)"
  shell_test::assert_eq "${GOT}" "${WANT}" "${LINENO}"

  # Test store.default, store.setdefault
  ./principal blessself testFile >f || shell_test::fail "line ${LINENO}: blessself testFile failed"
  ./principal store.setdefault f || shell_test::fail "line ${LINENO}: store.setdefault failed"
  GOT=$(./principal store.default | ./principal print - | extractBlessings) \
    || shell_test::fail "line ${LINENO}: failed to run principal"
  WANT="testFile(0 caveats)"
  shell_test::assert_eq "${GOT}" "${WANT}" "${LINENO}"

  ./principal blessself testStdin | ./principal store.setdefault - || shell_test::fail "line ${LINENO}: blessself testStdin | store.setdefault - failed"
  GOT=$(./principal store.default | ./principal print - | extractBlessings) \
    || shell_test::fail "line ${LINENO}: failed to run principal"
  WANT="testStdin(0 caveats)"
  shell_test::assert_eq "${GOT}" "${WANT}" "${LINENO}"

  # Test store.forpeer, store.set
  ./principal blessself forAlice >f || shell_test::fail "line ${LINENO}: blessself forAlice failed"
  ./principal store.set f alice/... || shell_test::fail "line ${LINENO}: store.set failed"
  ./principal blessself forAll | ./principal store.set - ... || shell_test::fail "line ${LINENO}: blessself forAll | store.set - ... failed"

  GOT=$(./principal store.forpeer | ./principal print - | extractBlessings) \
    || shell_test::fail "line ${LINENO}: failed to run principal"
  WANT="forAll(0 caveats)"
  shell_test::assert_eq "${GOT}" "${WANT}" "${LINENO}"

  GOT=$(./principal store.forpeer bob | ./principal print - | extractBlessings) \
    || shell_test::fail "line ${LINENO}: failed to run principal"
  WANT="forAll(0 caveats)"
  shell_test::assert_eq "${GOT}" "${WANT}" "${LINENO}"

  GOT=$(./principal store.forpeer alice | ./principal print - | extractBlessings) \
    || shell_test::fail "line ${LINENO}: failed to run principal"
  WANT="forAlice(0 caveats)#forAll(0 caveats)"
  shell_test::assert_eq "${GOT}" "${WANT}" "${LINENO}"

  GOT=$(./principal store.forpeer alice/friend | ./principal print - | extractBlessings) \
    || shell_test::fail "line ${LINENO}: failed to run principal"
  WANT="forAlice(0 caveats)#forAll(0 caveats)"
  shell_test::assert_eq "${GOT}" "${WANT}" "${LINENO}"

  GOT=$(./principal store.forpeer alice/friend bob/spouse | ./principal print - | extractBlessings) \
    || shell_test::fail "line ${LINENO}: failed to run principal"
  WANT="forAlice(0 caveats)#forAll(0 caveats)"
  shell_test::assert_eq "${GOT}" "${WANT}" "${LINENO}"

  shell_test::pass
}

main "$@"
