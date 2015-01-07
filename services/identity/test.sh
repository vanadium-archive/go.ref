#!/bin/bash

# Test that tests the routes of the identityd server.

source "$(go list -f {{.Dir}} v.io/core/shell/lib)/shell_test.sh"

# Run the test under the security agent.
shell_test::enable_agent "$@"

readonly WORKDIR="${shell_test_WORK_DIR}"

build() {
  IDENTITYD_BIN="$(shell_test::build_go_binary 'v.io/core/veyron/services/identity/identityd_test')"
  PRINCIPAL_BIN="$(shell_test::build_go_binary 'v.io/core/veyron/tools/principal')"
}

# runprincipal starts the principal tool, extracts the url and curls it, to avoid the
# dependence the principal tool has on a browser.
runprincipal() {
  local PFILE="${WORKDIR}/principalfile"
  # Start the tool in the background.
  "${PRINCIPAL_BIN}"  seekblessings --browser=false --from=https://localhost:8125/google -v=3 2> "${PFILE}" &
  sleep 2
  # Search for the url and run it.
  cat "${PFILE}" | grep https |
  while read url; do
    RESULT=$(curl -L --insecure -c ${WORKDIR}/cookiejar $url);
    # Clear out the file
    echo $RESULT;
    break;
  done;
  rm "${PFILE}";
}

main() {
  cd "${WORKDIR}"
  build
 
  shell_test::setup_server_test || shell_test::fail "line ${LINENO} failed to setup server test"

  # Start the identityd server in test identity server.
  shell_test::start_server "${VRUN}" "${IDENTITYD_BIN}" --host=localhost -veyron.tcp.address=127.0.0.1:0
  echo Identityd Log File: $START_SERVER_LOG_FILE

  # Test an initial seekblessings call, with a specified VEYRON_CREDENTIALS.
  WANT="Received blessings"
  GOT=$(runprincipal)
  if [[ ! "${GOT}" =~ "${WANT}" ]]; then
    shell_test::fail "line ${LINENO} failed first seekblessings call"
  fi
  # Test that a subsequent call succeed with the same credentials. This means that the blessings and principal from the first call works correctly.
  GOT=$(runprincipal)
  if [[ ! "${GOT}" =~ "${WANT}" ]]; then
    shell_test::fail "line ${LINENO} failed second seekblessings call"
  fi

  shell_test::pass
}

main "$@"
