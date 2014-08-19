#!/bin/bash

# Test the applicationd binary.
#
# This test starts an application repository server and uses the
# application repository client to verify that <application>.Put(),
# <application>.Match(), and <application>.Remove() work as expected.

source "${VEYRON_ROOT}/environment/scripts/lib/shell_test.sh"

build() {
  local -r GO="${REPO_ROOT}/scripts/build/go"
  "${GO}" build veyron/services/mgmt/application/applicationd || shell_test::fail "line ${LINENO}: failed to build 'applicationd'"
  "${GO}" build veyron/services/mounttable/mounttabled || shell_test::fail "line ${LINENO}: failed to build 'mounttabled'"
  "${GO}" build veyron/services/store/stored || shell_test::fail "line ${LINENO}: failed to build 'stored'"
  "${GO}" build veyron/tools/application || shell_test::fail "line ${LINENO}: failed to build 'application'"
  "${GO}" build veyron/tools/findunusedport || shell_test::fail "line ${LINENO}: failed to build 'findunusedport'"
  "${GO}" build veyron/tools/identity || shell_test::fail "line ${LINENO}: failed to build 'identity'"
}

main() {
  cd "${TMPDIR}"
  build

  # Generate a self-signed identity.
  local -r ID_FILE=$(shell::tmp_file)
  ./identity generate > "${ID_FILE}"

  # Start the mounttable daemon.
  local -r MT_PORT=$(./findunusedport)
  local -r MT_LOG=$(shell::tmp_file)
  ./mounttabled --address="127.0.0.1:${MT_PORT}" &> "${MT_LOG}" &
  shell_test::wait_for "${MT_LOG}" "Mount table service"

  export VEYRON_IDENTITY="${ID_FILE}"
  export NAMESPACE_ROOT="/127.0.0.1:${MT_PORT}"

  # Start the store daemon.
  local -r DB_DIR=$(shell::tmp_dir)
  local -r STORE="application-test-store"
  local -r STORE_LOG=$(shell::tmp_file)
  ./stored --name="${STORE}" --address=127.0.0.1:0 --db="${DB_DIR}" &> "${STORE_LOG}" &
  shell_test::wait_for "${STORE_LOG}" "Viewer running"

  # Start the application repository daemon.
  local -r REPO="applicationd-test-repo"
  local -r REPO_LOG=$(shell::tmp_file)
  ./applicationd --name="${REPO}" --address=127.0.0.1:0 --store="${STORE}" &> "${REPO_LOG}" &
  shell_test::wait_for "${REPO_LOG}" "Application repository published"

  # Create an application envelope.
  local -r APPLICATION="${REPO}/test-application/v1"
  local -r PROFILE="test-profile"
  local -r ENVELOPE=$(shell::tmp_file)
  cat > "${ENVELOPE}" <<EOF
{"Title":"title", "Args":[], "Binary":"foo", "Env":[]}
EOF
  ./application put "${APPLICATION}" "${PROFILE}" "${ENVELOPE}" || shell_test::fail "line ${LINENO}: 'put' failed"

  # Match an application envelope.
  local -r ENVELOPE2=$(shell::tmp_file)
  ./application match "${APPLICATION}" "${PROFILE}" | tee "${ENVELOPE2}" || shell_test::fail "line ${LINENO}: 'match' failed"

  # Remove an application envelope.
  ./application remove "${APPLICATION}" "${PROFILE}" || shell_test::fail "line ${LINENO}: 'remove' failed"

  shell_test::pass
}

main "$@"
