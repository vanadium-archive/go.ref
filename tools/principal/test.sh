#!/bin/bash

# Test the principal command-line tool.
#
# This tests most operations of the principal command-line tool.

source "${VEYRON_ROOT}/scripts/lib/shell_test.sh"

readonly WORKDIR=$(shell::tmp_dir)

build() {
  veyron go build veyron.io/veyron/veyron/tools/principal || shell_test::fail "line ${LINENO}: failed to build principal"
}

extractBlessings() {
    awk '/Blessings/ { st = index($0," "); print substr($0,st+1)}'
}

main() {
  # Build binaries.
  build

  # Set VEYRON_CREDENTIALS.
  export VEYRON_CREDENTIALS="${WORKDIR}"

  ./principal store.default >/dev/null || shell_test::fail "line ${LINENO}: store.default failed"
  ./principal store.forpeer >/dev/null || shell_test::fail "line ${LINENO}: store.forpeer failed"
  ./principal blessself >/dev/null || shell_test::fail "line ${LINENO}: blessself failed"
  ./principal blessself alice >alice || shell_test::fail "line ${LINENO}: blessself alice failed"

  # Test print
  local GOT=$(./principal print alice | extractBlessings)
  local WANT="alice(0 caveats)"
  if [ "${GOT}" != "${WANT}" ]; then
    shell_test::fail "line ${LINENO}: Got ${GOT}, want ${WANT}"
  fi
  local GOT=$(./principal blessself bob | ./principal print - | extractBlessings)
  local WANT="bob(0 caveats)"
  if [ "${GOT}" != "${WANT}" ]; then
    shell_test::fail "line ${LINENO}: Got ${GOT}, want ${WANT}"
  fi
  local GOT=$(./principal blessself --for=1h bob| ./principal print - | extractBlessings)
  local WANT="bob(1 caveats)"
  if [ "${GOT}" != "${WANT}" ]; then
    shell_test::fail "line ${LINENO}: Got ${GOT}, want ${WANT}"
  fi

  # Test store.default, store.setdefault
  ./principal blessself testFile >f || shell_test::fail "line ${LINENO}: blessself testFile failed"
  ./principal store.setdefault f || shell_test::fail "line ${LINENO}: store.setdefault failed"
  local GOT=$(./principal store.default | ./principal print - | extractBlessings)
  local WANT="testFile(0 caveats)"
  if [ "${GOT}" != "${WANT}" ]; then
    shell_test::fail "line ${LINENO}: Got ${GOT}, want ${WANT}"
  fi

  ./principal blessself testStdin | ./principal store.setdefault - || shell_test::fail "line ${LINENO}: blessself testStdin | store.setdefault - failed"
  local GOT=$(./principal store.default | ./principal print - | extractBlessings)
  local WANT="testStdin(0 caveats)"
  if [ "${GOT}" != "${WANT}" ]; then
    shell_test::fail "line ${LINENO}: Got ${GOT}, want ${WANT}"
  fi

  # Test store.forpeer, store.set
  ./principal blessself forAlice >f || shell_test::fail "line ${LINENO}: blessself forAlice failed"
  ./principal store.set f alice/... || shell_test::fail "line ${LINENO}: store.set failed"

  ./principal blessself forAll | ./principal store.set - ... || shell_test::fail "line ${LINENO}: blessself forAll | store.set - ... failed"

  local GOT=$(./principal store.forpeer | ./principal print - | extractBlessings)
  local WANT="forAll(0 caveats)"
  if [ "${GOT}" != "${WANT}" ]; then
    shell_test::fail "line ${LINENO}: Got ${GOT}, want ${WANT}"
  fi
  local GOT=$(./principal store.forpeer bob | ./principal print - | extractBlessings)
  local WANT="forAll(0 caveats)"
  if [ "${GOT}" != "${WANT}" ]; then
    shell_test::fail "line ${LINENO}: Got ${GOT}, want ${WANT}"
  fi
  local GOT=$(./principal store.forpeer alice | ./principal print - | extractBlessings)
  local WANT="forAlice(0 caveats)#forAll(0 caveats)"
  if [ "${GOT}" != "${WANT}" ]; then
    shell_test::fail "line ${LINENO}: Got ${GOT}, want ${WANT}"
  fi
  local GOT=$(./principal store.forpeer alice/friend | ./principal print - | extractBlessings)
  local WANT="forAlice(0 caveats)#forAll(0 caveats)"
  if [ "${GOT}" != "${WANT}" ]; then
    shell_test::fail "line ${LINENO}: Got ${GOT}, want ${WANT}"
  fi
  local GOT=$(./principal store.forpeer alice/friend bob/spouse | ./principal print - | extractBlessings)
  local WANT="forAlice(0 caveats)#forAll(0 caveats)"
  if [ "${GOT}" != "${WANT}" ]; then
    shell_test::fail "line ${LINENO}: Got ${GOT}, want ${WANT}"
  fi
  shell_test::pass
}

main "$@"
