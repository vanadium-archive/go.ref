#!/bin/bash

# Test the application repository daemon.
#
# This test starts an application repository daemon and uses the
# application repository client to verify that <application>.Put(),
# <application>.Match(), and <application>.Remove() work as expected.

source "$(go list -f {{.Dir}} v.io/core/shell/lib)/shell_test.sh"

# Run the test under the security agent.
shell_test::enable_agent "$@"

readonly WORKDIR="${shell_test_WORK_DIR}"

build() {
  APPLICATIOND_BIN="$(shell_test::build_go_binary 'v.io/core/veyron/services/mgmt/application/applicationd')"
  APPLICATION_BIN="$(shell_test::build_go_binary 'v.io/core/veyron/tools/application')"
}

main() {
  cd "${WORKDIR}"
  build

  shell_test::setup_server_test

  # Start the application repository daemon.
  local -r REPO="applicationd-test-repo"
  local -r STORE=$(shell::tmp_dir)
  shell_test::start_server "${VRUN}" "${APPLICATIOND_BIN}" --name="${REPO}" --store="${STORE}" --veyron.tcp.address=127.0.0.1:0 \
    || shell_test::fail "line ${LINENO} failed to start applicationd"

  # Create an application envelope.
  local -r APPLICATION="${REPO}/test-application/v1"
  local -r PROFILE="test-profile"
  local -r ENVELOPE_WANT=$(shell::tmp_file)
  cat > "${ENVELOPE_WANT}" <<EOF
{
  "Title": "title",
  "Args": null,
  "Binary": {
    "File": "foo",
    "Signature": {
      "Purpose": null,
      "Hash": "",
      "R": null,
      "S": null
    }
  },
  "Publisher": {
    "CertificateChains": null
  },
  "Env": null,
  "Packages": null
}
EOF
  "${APPLICATION_BIN}" put "${APPLICATION}" "${PROFILE}" "${ENVELOPE_WANT}" || shell_test::fail "line ${LINENO}: 'put' failed"

  # Match the application envelope.
  local -r ENVELOPE_GOT=$(shell::tmp_file)
  "${APPLICATION_BIN}" match "${APPLICATION}" "${PROFILE}" | tee "${ENVELOPE_GOT}" || shell_test::fail "line ${LINENO}: 'match' failed"

  cmp "${ENVELOPE_WANT}" "${ENVELOPE_GOT}" &> /dev/null || shell_test::fail "mismatching application envelopes"

  # Remove the application envelope.
  "${APPLICATION_BIN}" remove "${APPLICATION}" "${PROFILE}" || shell_test::fail "line ${LINENO}: 'remove' failed"

  # Check the application envelope no longer exists.
  local -r RESULT=$(shell::check_result "${APPLICATION_BIN}" match "${APPLICATION}" "${PROFILE}")
  shell_test::assert_ne "${RESULT}" "0" "${LINENO}"

  shell_test::pass
}

main "$@"
