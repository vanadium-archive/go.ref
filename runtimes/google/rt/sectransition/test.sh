#!/bin/bash

# Test compatibility of clients and servers using a combination of the old
# and new security models (triggered by environment variables).

. "${VEYRON_ROOT}/scripts/lib/shell_test.sh"

readonly WORKDIR="${shell_test_WORK_DIR}"

build() {
  SECTRANSITION_BIN="$(shell_test::build_go_binary 'veyron.io/veyron/veyron/runtimes/google/rt/sectransition')"
  IDENTITY_BIN="$(shell_test::build_go_binary 'veyron.io/veyron/veyron/tools/identity')"
}

startserver() {
  # The server has access to both the old and new security model.
  export VEYRON_IDENTITY="${WORKDIR}/old"
  export VEYRON_CREDENTIALS="${WORKDIR}/new"
  shell::run_server "${shell_test_DEFAULT_SERVER_TIMEOUT}" "${SERVERLOG}" /dev/null \
    "${SECTRANSITION_BIN}" --server --logtostderr &> /dev/null \
    || shell_test::fail "line ${LINENO}: failed to start sectransaction"
  shell::timed_wait_for "${shell_test_DEFAULT_MESSAGE_TIMEOUT}" "${SERVERLOG}" "SERVER" \
    || shell_test::fail "line ${LINENO}: failed to read expected output from log file"
  local -r EP=$(grep "SERVER: " "${SERVERLOG}" | sed -e 's/SERVER: //') \
    || shell_test::fail "line ${LINENO}: failed to identify the endpoint"
  echo "${EP}"
}

runclient() {
  "${SECTRANSITION_BIN}" "${EP}" &>"${CLIENTLOG}"
}

oldmodel() {
  awk '/ClientPublicID:/ {print $2}' "${CLIENTLOG}"
}

newmodel() {
  awk '/ClientBlessings:/ {print $2}' "${CLIENTLOG}"
}

main() {
  cd "${WORKDIR}"
  build

  # Generate an identity (old security model) that may be used by the client.
  local -r OLD="${WORKDIR}/old"
  "${IDENTITY_BIN}" generate "old" > "${OLD}"

  local -r SERVERLOG="${WORKDIR}/server.log"
  local -r CLIENTLOG="${WORKDIR}/client.log"
  local -r EP=$(startserver)

  # No environment variables set: PublicIDs from the old model should be exchanged.
  unset VEYRON_IDENTITY
  unset VEYRON_CREDENTIALS
  runclient || shell_test::fail "line ${LINENO}: failed to run client"
  echo "            No environment variables: PublicID:$(oldmodel), Blessings:$(newmodel)"
  if [[ $(oldmodel) == "<nil>" ]]; then
    shell_test::fail "line ${LINENO}: PublicID not set when neither environment variable is set"
  fi
  if [[ $(newmodel) != "<nil>" ]]; then
    shell_test::fail "line ${LINENO}: Blessings should not be set when neither environment variable is set (was $(newmodel))"
  fi


  # Old model envvar is set: not the new one: PublicIDs from the old model should be exchanged.
  export VEYRON_IDENTITY="${WORKDIR}/old"
  unset VEYRON_CREDENTIALS
  runclient || shell_test::fail "line ${LINENO}: failed to run client"
  echo "                     VEYRON_IDENTITY: PublicID:$(oldmodel), Blessings:$(newmodel)"
  if [[ $(oldmodel) == "<nil>" ]]; then
    shell_test::fail "line ${LINENO}: PublicID not set when only VEYRON_IDENTITY is set"
  fi
  if [[ $(newmodel) != "<nil>" ]]; then
    shell_test::fail "line ${LINENO}: Blessings should not be set when only VEYRON_IDENTITY is set (was $(newmodel))"
  fi

  # New model envvar is set:  Blessings should be exchanged.
  unset VEYRON_IDENTITY
  export VEYRON_CREDENTIALS="${WORKDIR}/new"
  runclient || shell_test::fail "line ${LINENO}: failed to run client"
  echo "                  VEYRON_CREDENTIALS: PublicID:$(oldmodel), Blessings:$(newmodel)"
  if [[ $(oldmodel) != "<nil>" ]]; then
    shell_test::fail "line ${LINENO}: PublicID should not be exchanged when VEYRON_CREDENTIALS is set (was $(oldmodel))"
  fi
  if [[ $(newmodel) == "<nil>" ]]; then
    shell_test::fail "line ${LINENO}: Blessings should be exchanged when VEYRON_CREDENTIALS is set (was $(newmodel))"
  fi

  # Both environment variables are set: Blessings should be exchanged.
  export VEYRON_IDENTITY="${WORKDIR}/old"
  export VEYRON_CREDENTIALS="${WORKDIR}/new"
  runclient || shell_test::fail "line ${LINENO}: failed to run client"
  echo "VEYRON_IDENTITY & VEYRON_CREDENTIALS: PublicID:$(oldmodel), Blessings:$(newmodel)"
  if [[ $(oldmodel) != "<nil>" ]]; then
    shell_test::fail "line ${LINENO}: PublicID should not be exchanged when VEYRON_CREDENTIALS is set (was $(oldmodel))"
  fi
  if [[ $(newmodel) == "<nil>" ]]; then
    shell_test::fail "line ${LINENO}: Blessings should be exchanged when VEYRON_CREDENTIALS is set (was $(newmodel))"
  fi

  shell_test::pass
}

main "$@"