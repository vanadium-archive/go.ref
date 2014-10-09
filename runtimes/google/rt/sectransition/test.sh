#!/bin/bash

# Test compatibility of clients and servers using a combination of the old
# and new security models (triggered by environment variables).

. "${VEYRON_ROOT}/scripts/lib/shell_test.sh"

readonly WORKDIR=$(shell::tmp_dir)
set +e

build() {
  veyron go build veyron.io/veyron/veyron/runtimes/google/rt/sectransition || shell_test::fail "line ${LINENO}: failed to build sectransition binary"
  veyron go build veyron.io/veyron/veyron/tools/identity || shell_test::fail "line ${LINENO}: failed to build identity"
}


startserver() {
  # The server has access to both the old and new security model.
  export VEYRON_IDENTITY="${WORKDIR}/old"
  export VEYRON_CREDENTIALS="${WORKDIR}/new"
  ./sectransition --server --logtostderr >"${SERVERLOG}" 2>&1 &
  shell::wait_for "${SERVERLOG}" "SERVER"
  local EP=$(grep "SERVER: " "${SERVERLOG}" | sed -e 's/SERVER: //')
  echo "${EP}"
}

runclient() {
  ./sectransition "${EP}" >"${CLIENTLOG}" 2>&1
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
  ./identity generate "old" > "${OLD}"

  local -r SERVERLOG="${WORKDIR}/server.log"
  local -r CLIENTLOG="${WORKDIR}/client.log"
  local -r EP=$(startserver)

  # No environment variables set: PublicIDs from the old model should be exchanged.
  unset VEYRON_IDENTITY
  unset VEYRON_CREDENTIALS
  runclient
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
  runclient
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
  runclient
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
  runclient
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
