#!/bin/bash

# Test the identity command-line tool.
#
# This tests most operations of the identity command-line tool.
# Not the "seekblessing" command yet, since that requires
# starting a separate server.

source "${VEYRON_ROOT}/scripts/lib/shell_test.sh"

readonly WORKDIR="${shell_test_WORK_DIR}"

build() {
  IDENTITY_BIN="$(shell_test::build_go_binary 'veyron.io/veyron/veyron/tools/identity')"
}

main() {
  local GOT
  local WANT

  cd "${WORKDIR}"
  build

  "${IDENTITY_BIN}" print >/dev/null || shell_test::fail "line ${LINENO}: print failed"
  "${IDENTITY_BIN}" generate >/dev/null || shell_test::fail "line ${LINENO}: generate failed"
  "${IDENTITY_BIN}" generate root >root || shell_test::fail "line ${LINENO}: generate root failed"

  export VEYRON_IDENTITY="root"

  # Generate an identity and get it blessed by root using "identity bless"
  GOT=$("${IDENTITY_BIN}" generate ignoreme | "${IDENTITY_BIN}" bless - child | "${IDENTITY_BIN}" print - | awk '/Name/ {print $3}') \
    || shell_test::fail "line ${LINENO}: failed to run identity"
  WANT="root/child"
  shell_test::assert_eq "${GOT}" "${WANT}" "${LINENO}"

  # Generate an identity and get it blessed by root using "identity bless --with"
  "${IDENTITY_BIN}" generate other >other || shell_test::fail
  GOT=$("${IDENTITY_BIN}" generate ignoreme | "${IDENTITY_BIN}" bless --with=other - child | "${IDENTITY_BIN}" print - | awk '/Name/ {print $3}') \
    || shell_test::fail "line ${LINENO}: failed to run identity"
  WANT="unknown/other/child"
  shell_test::assert_eq "${GOT}" "${WANT}" "${LINENO}"

  # Test that previously generated identities can be interpreted
  # (i.e., any changes to the Certificate or Signature scheme are backward compatible).
  # To regenerate testdata:
  # identity generate "root" >testdata/root.id
  # identity generate "other" | VEYRON_IDENTITY=testdata/root.id identity bless - "blessed" >testdata/blessed.id
  local -r TESTDATA_DIR="${VEYRON_ROOT}/veyron/go/src/veyron.io/veyron/veyron/tools/identity/testdata"
  GOT=$(VEYRON_IDENTITY="${TESTDATA_DIR}/root.id" "${IDENTITY_BIN}" print | awk '/Name/ {print $3}') \
    || shell_test::fail "line ${LINENO}: failed to run identity"
  WANT="root"
  shell_test::assert_eq "${GOT}" "${WANT}" "${LINENO}"

  GOT=$(VEYRON_IDENTITY="${TESTDATA_DIR}/root.id" "${IDENTITY_BIN}" print "${TESTDATA_DIR}/blessed.id" | awk '/Name/ {print $3}') \
    || shell_test::fail "line ${LINENO}: failed to run identity"
  WANT="root/blessed"
  shell_test::assert_eq "${GOT}" "${WANT}" "${LINENO}"

  shell_test::pass
}

main "$@"
