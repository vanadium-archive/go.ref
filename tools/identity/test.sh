#!/bin/bash

# Test the identity command-line tool.
#
# This tests most operations of the identity command-line tool.
# Not the "seekblessing" command yet, since that requires
# starting a separate server.

source "${VEYRON_ROOT}/environment/scripts/lib/shell_test.sh"

main() {
  # Build binaries.
  cd "${TMPDIR}"
  local -r GO="${VEYRON_ROOT}/scripts/build/go"
  "${GO}" build veyron.io/veyron/veyron/tools/identity || shell_test::fail "line ${LINENO}: failed to build identity"

  ./identity print >/dev/null || shell_test::fail "line ${LINENO}: print failed"
  ./identity generate >/dev/null || shell_test::fail "line ${LINENO}: generate failed"
  ./identity generate root >root || shell_test::fail "line ${LINENO}: generate root failed"

  export VEYRON_IDENTITY="root"

  # Generate an identity and get it blessed by root using "identity bless"
  local GOT=$(./identity generate ignoreme | ./identity bless - child | ./identity print - | awk '/Name/ {print $3}')
  local WANT="root/child"
  if [ "${GOT}" != "${WANT}" ]; then
    shell_test::fail "line ${LINENO}: Got ${GOT}, want ${WANT}"
  fi

  # Generate an identity and get it blessed by root using "identity bless --with"
  ./identity generate other >other || shell_test::fail
  GOT=$(./identity generate ignoreme | ./identity bless --with=other - child | ./identity print - | awk '/Name/ {print $3}')
  WANT="unknown/other/child"
  if [ "${GOT}" != "${WANT}" ]; then
    shell_test::fail "line ${LINENO}: Got ${GOT}, want ${WANT}"
  fi

  # Test that previously generated identities can be interpreted
  # (i.e., any changes to the Certificate or Signature scheme are backward compatible).
  # To regenerate testdata:
  # identity generate "root" >testdata/root.id
  # identity generate "other" | VEYRON_IDENTITY=testdata/root.id identity bless - "blessed" >testdata/blessed.id
  local -r TESTDATA_DIR="${VEYRON_ROOT}/veyron/go/src/veyron.io/veyron/veyron/tools/identity/testdata"
  GOT=$(VEYRON_IDENTITY="${TESTDATA_DIR}/root.id" ./identity print | awk '/Name/ {print $3}')
  WANT="root"
  if [ "${GOT}" != "${WANT}" ]; then
    shell_test::fail "line ${LINENO}: Got '${GOT}' from previously generated root.id, want '${WANT}'"
  fi
  GOT=$(VEYRON_IDENTITY="${TESTDATA_DIR}/root.id" ./identity print "${TESTDATA_DIR}/blessed.id" | awk '/Name/ {print $3}')
  WANT="root/blessed"
  if [ "${GOT}" != "${WANT}" ]; then
    shell_test::fail "line ${LINENO}: Got '${GOT}' from previously generated blessed.id, want '${WANT}'"
  fi
  shell_test::pass
}

main "$@"
