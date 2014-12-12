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
  # Create three principals, one called "alice" one called "bob" and one called "carol"
  "${PRINCIPAL_BIN}" create --overwrite=true ./alice alice >/dev/null || shell_test::fail "line ${LINENO}: create failed"
  "${PRINCIPAL_BIN}" create ./bob bob >/dev/null || shell_test::fail "line ${LINENO}: create failed"
  "${PRINCIPAL_BIN}" create --overwrite=true ./bob bob >/dev/null || shell_test::fail "line ${LINENO}: create failed"
  "${PRINCIPAL_BIN}" create ./carol carol >/dev/null || shell_test::fail "line ${LINENO}: create failed"

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

  # Run recvblessings on carol, and have alice send blessings over
  # (blessings received must be set as default and shareable with all peers.)
  "${PRINCIPAL_BIN}" --veyron.credentials=./carol --veyron.tcp.address=127.0.0.1:0 recvblessings >carol.recvblessings&
  shell::timed_wait_for "${shell_test_DEFAULT_MESSAGE_TIMEOUT}" carol.recvblessings "bless --remote_key" || shell_test::fail "line ${LINENO}: recvblessings did not print command for sender"
  local -r PRINCIPAL_BIN_DIR=$(dirname "${PRINCIPAL_BIN}")
  local SEND_BLESSINGS_CMD=$(grep "bless --remote_key" carol.recvblessings | sed -e 's|extension[0-9]*|friend/carol|')
  SEND_BLESSINGS_CMD="${PRINCIPAL_BIN_DIR}/${SEND_BLESSINGS_CMD}"
  $(${SEND_BLESSINGS_CMD}) || shell_test::fail "line ${LINENO}: ${SEND_BLESSINGS_CMD} failed"
  grep "Received blessings: alice/friend/carol" carol.recvblessings >/dev/null || shell_test::fail "line ${LINENO}: recvblessings did not log any blessings received $(cat carol.recvblessings)"
  # Run recvblessings on carol, and have alice send blessings over
  # (blessings received must be set as shareable with peers matching 'alice/...'.)
  "${PRINCIPAL_BIN}" --veyron.credentials=./carol --veyron.tcp.address=127.0.0.1:0 recvblessings --for_peer=alice/... --set_default=false >carol.recvblessings&
  shell::timed_wait_for "${shell_test_DEFAULT_MESSAGE_TIMEOUT}" carol.recvblessings "bless --remote_key" || shell_test::fail "line ${LINENO}: recvblessings did not print command for sender"
  SEND_BLESSINGS_CMD=$(grep "bless --remote_key" carol.recvblessings | sed -e 's|extension[0-9]*|friend/carol/foralice|')
  SEND_BLESSINGS_CMD="${PRINCIPAL_BIN_DIR}/${SEND_BLESSINGS_CMD}"
  $(${SEND_BLESSINGS_CMD}) || shell_test::fail "line ${LINENO}: ${SEND_BLESSINGS_CMD} failed"
  grep "Received blessings: alice/friend/carol/foralice" carol.recvblessings >/dev/null || shell_test::fail "line ${LINENO}: recvblessings did not log any blessings received $(cat carol.recvblessings)"
  # Mucking around with the public key should fail
  "${PRINCIPAL_BIN}" --veyron.credentials=./carol --veyron.tcp.address=127.0.0.1:0 recvblessings >carol.recvblessings&
  local -r RECV_BLESSINGS_PID="$!"
  shell::timed_wait_for "${shell_test_DEFAULT_MESSAGE_TIMEOUT}" carol.recvblessings "bless --remote_key" || shell_test::fail "line ${LINENO}: recvblessings did not print command for sender"
  SEND_BLESSINGS_CMD=$(grep "bless --remote_key" carol.recvblessings | sed -e 's|remote_key=|remote_key=BAD|')
  SEND_BLESSINGS_CMD="${PRINCIPAL_BIN_DIR}/${SEND_BLESSINGS_CMD}"
  $(${SEND_BLESSINGS_CMD} 2>error) && shell_test::fail "line ${LINENO}: ${SEND_BLESSINGS_CMD} should have failed"
  grep "key mismatch" error >/dev/null || shell_test::fail "line ${LINENO}: key mismatch error not printed"
  # Mucking around with the token should fail
  SEND_BLESSINGS_CMD=$(grep "bless --remote_key" carol.recvblessings | sed -e 's|remote_token=|remote_token=BAD|')
  SEND_BLESSINGS_CMD="${PRINCIPAL_BIN_DIR}/${SEND_BLESSINGS_CMD}"
  $(${SEND_BLESSINGS_CMD} 2>error) && shell_test::fail "line ${LINENO}: ${SEND_BLESSINGS_CMD} should have failed"
  grep "blessings received from unexpected sender" error >/dev/null || shell_test::fail "line ${LINENO}: unexpected sender error not printed"
  # Dump carol out, the only blessing that survives should be from the first
  # "bless" command. (alice/friend/carol).
  kill -9 "${RECV_BLESSINGS_PID}"
  "${PRINCIPAL_BIN}" --veyron.credentials=./carol dump >carol.dump || shell_test::fail "line ${LINENO}: dump failed"

  # Any other commands to be run without VEYRON_CREDENTIALS set.
  unset VEYRON_CREDENTIALS

  # Validate the output of various commands (mostly using "principal dump" or "principal dumpblessings")
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
  if ! diff -C 5 got want; then
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
  if ! diff -C 5 got want; then
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
  if ! diff -C 5 got want; then
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
  if ! diff -C 5 got want; then
    shell_test::fail "line ${LINENO}"
  fi

  cat carol.dump | rmpublickey >got || shell_test::fail "line ${LINENO}: cat carol.dump | rmpublickey failed"
  cat >want <<EOF
Public key : XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX
---------------- BlessingStore ----------------
Default blessings: alice/friend/carol
Peer pattern                   : Blessings
...                            : alice/friend/carol
alice/...                      : alice/friend/carol/foralice
---------------- BlessingRoots ----------------
Public key                                      : Pattern
XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX : [alice/...]
XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX : [carol/...]
EOF
  if ! diff -C 5 got want; then
    shell_test::fail "line ${LINENO}"
  fi
  shell_test::pass
}

main "$@"
