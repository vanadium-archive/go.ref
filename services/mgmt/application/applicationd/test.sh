#!/bin/bash

# Test the application repository daemon.
#
# This test starts an application repository daemon and uses the
# application repository client to verify that <application>.Put(),
# <application>.Match(), and <application>.Remove() work as expected.

source "${VEYRON_ROOT}/environment/scripts/lib/shell_test.sh"

build() {
  local -r GO="${REPO_ROOT}/scripts/build/go"
  "${GO}" build veyron/services/mgmt/application/applicationd || shell_test::fail "line ${LINENO}: failed to build 'applicationd'"
  "${GO}" build veyron/services/store/stored || shell_test::fail "line ${LINENO}: failed to build 'stored'"
  "${GO}" build veyron/tools/application || shell_test::fail "line ${LINENO}: failed to build 'application'"
}

main() {
  cd "${TMPDIR}"
  build

  shell_test::setup_server_test

  # Start the store daemon.
  local -r DB_DIR=$(shell::tmp_dir)
  local -r STORE="application-test-store"
  shell_test::start_server ./stored --name="${STORE}" --address=127.0.0.1:0 --db="${DB_DIR}"

  # Start the application repository daemon.
  local -r REPO="applicationd-test-repo"
  shell_test::start_server ./applicationd --name="${REPO}" --address=127.0.0.1:0 --store="${STORE}" -logtostderr

  # Create an application envelope.
  local -r APPLICATION="${REPO}/test-application/v1"
  local -r PROFILE="test-profile"
  local -r ENVELOPE_WANT=$(shell::tmp_file)
  cat > "${ENVELOPE_WANT}" <<EOF
{"Title":"title", "Args":[], "Binary":"foo", "Env":[]}
EOF
  ./application put "${APPLICATION}" "${PROFILE}" "${ENVELOPE_WANT}" || shell_test::fail "line ${LINENO}: 'put' failed"

  # Match the application envelope.
  local -r ENVELOPE_GOT=$(shell::tmp_file)
  ./application match "${APPLICATION}" "${PROFILE}" | tee "${ENVELOPE_GOT}" || shell_test::fail "line ${LINENO}: 'match' failed"
  if [[ $(cmp "${ENVELOPE_WANT}" "${ENVELOPE_GOT}" &> /dev/null) ]]; then
    shell_test::fail "mismatching application envelopes"
  fi

  # Remove the application envelope.
  ./application remove "${APPLICATION}" "${PROFILE}" || shell_test::fail "line ${LINENO}: 'remove' failed"

  # Check the application envelope no longer exists.
  ./application match "${APPLICATION}" "${PROFILE}" && "line ${LINENO}: 'match' did not fail when it should"

  shell_test::pass
}

main "$@"
