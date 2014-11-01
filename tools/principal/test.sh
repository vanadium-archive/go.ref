#!/bin/bash

# Test the principal command-line tool.
#
# This tests most operations of the principal command-line tool.
# Not the "seekblessing" command yet, since that requires
# starting a separate server.

source "${VEYRON_ROOT}/scripts/lib/shell_test.sh"

readonly WORKDIR=${shell_test_WORK_DIR}

build() {
  PRINCIPAL_BIN="$(shell_test::build_go_binary 'veyron.io/veyron/veyron/tools/principal')"
}

# rmpublickey replaces public keys (16 hex bytes, :-separated) with XX:....
# This substitution enables comparison with golden output even when keys are freshly
# minted by the "principal create" command.
rmpublickey() {
    sed -e "s/\([0-9a-f]\{2\}:\)\{15\}[0-9a-f]\{2\}/XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX/g"
}

rmcaveats() {
    sed -e "s/security.unixTimeExpiryCaveat([^)]*)/security.unixTimeExpiryCaveat/"
}

dumpblessings() {
    "${PRINCIPAL_BIN}" dumpblessings "$1" | rmpublickey | rmcaveats
}

main() {
  cd "${WORKDIR}"
  build

  # Prevent any VEYRON_CREDENTIALS in the environment from interfering with this test.
  unset VEYRON_CREDENTIALS
  # Create two principals, one called "alice" one called "bob"
  "${PRINCIPAL_BIN}" create --overwrite=true ./alice alice >/dev/null || shell_test::fail "line ${LINENO}: create failed"
  "${PRINCIPAL_BIN}" create ./bob bob >/dev/null || shell_test::fail "line ${LINENO}: create failed"
  "${PRINCIPAL_BIN}" create --overwrite=true ./bob bob >/dev/null || shell_test::fail "line ${LINENO}: create failed"
  # Run dump, bless, blessself on alice
  export VEYRON_CREDENTIALS=./alice
  "${PRINCIPAL_BIN}" blessself alicereborn >alice.blessself || shell_test::fail "line ${LINENO}: blessself failed"
  "${PRINCIPAL_BIN}" bless ./bob friend >alice.bless || shell_test::fail "line ${LINENO}: bless failed"
  "${PRINCIPAL_BIN}" dump >alice.dump || shell_test::fail "line ${LINENO}: dump failed"
  # Run store setdefault, store default, store set, store forpeer on bob
  # This time use the --veyron.credentials flag to set the principal.
  "${PRINCIPAL_BIN}" --veyron.credentials=./bob store setdefault alice.bless || shell_test::fail "line ${LINENO}: store setdefault failed"
  "${PRINCIPAL_BIN}" --veyron.credentials=./bob store default >bob.store.default || shell_test::fail "line ${LINENO}: store default failed"
  "${PRINCIPAL_BIN}" --veyron.credentials=./bob store set alice.bless alice/... || shell_test::fail "line ${LINENO}: store set failed"
  "${PRINCIPAL_BIN}" --veyron.credentials=./bob store forpeer alice/server >bob.store.forpeer || shell_test::fail "line ${LINENO}: store forpeer failed" 
  # Any other commands to be run without VEYRON_CREDENTIALS set.
  unset VEYRON_CREDENTIALS

  # Validate the output of various commands (mostly using "principal dumpblessings")
  cat alice.dump | rmpublickey >got || shell_test::fail "line ${LINENO}: cat alice.dump | rmpublickey failed"
  cat >want <<EOF
Public key : XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX
---------------- BlessingStore ----------------
Default blessings: alice
Peer pattern                   : Blessings
...                            : alice
---------------- BlessingRoots ----------------
Public key                                      : Pattern
XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX : [alice/...]
EOF
  if ! diff got want; then
  	shell_test::fail "line ${LINENO}"
  fi

  dumpblessings alice.blessself >got || shell_test::fail "line ${LINENO}: dumpblessings failed"
  cat >want <<EOF
Blessings          : alicereborn
PublicKey          : XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX
Certificate chains : 1
Chain #0 (1 certificates). Root certificate public key: XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX
  Certificate #0: alicereborn with 0 caveats
EOF
  if ! diff got want; then
  	shell_test::fail "line ${LINENO}"
  fi

  dumpblessings bob.store.default >got || shell_test::fail "line ${LINENO}: dumpblessings failed"
  cat >want <<EOF
Blessings          : alice/friend
PublicKey          : XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX
Certificate chains : 1
Chain #0 (2 certificates). Root certificate public key: XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX
  Certificate #0: alice with 0 caveats
  Certificate #1: friend with 1 caveat
    (0) security.unixTimeExpiryCaveat
EOF
  if ! diff got want; then
	shell_test::fail "line ${LINENO}"
  fi

  dumpblessings bob.store.forpeer >got || shell_test::fail "line ${LINENO}: dumpblessings failed"
  cat >want <<EOF
Blessings          : bob#alice/friend
PublicKey          : XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX
Certificate chains : 2
Chain #0 (1 certificates). Root certificate public key: XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX
  Certificate #0: bob with 0 caveats
Chain #1 (2 certificates). Root certificate public key: XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX
  Certificate #0: alice with 0 caveats
  Certificate #1: friend with 1 caveat
    (0) security.unixTimeExpiryCaveat
EOF
  if ! diff got want; then
	shell_test::fail "line ${LINENO}"
  fi

  shell_test::pass
}

main "$@"
