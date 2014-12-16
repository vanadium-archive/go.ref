#!/bin/bash

# Test the profile repository daemon.
#
# This test starts an profile repository daemon and uses the profile
# repository client to verify that <profile>.Put(), <profile>.Label(),
# <profile>.Description(), <profile>.Speficiation(), and
# <profile>.Remove() work as expected.

source "$(go list -f {{.Dir}} veyron.io/veyron/shell/lib)/shell_test.sh"

readonly WORKDIR="${shell_test_WORK_DIR}"

build() {
  PROFILED_BIN="$(shell_test::build_go_binary 'veyron.io/veyron/veyron/services/mgmt/profile/profiled')"
  PROFILE_BIN="$(shell_test::build_go_binary 'veyron.io/veyron/veyron/tools/profile')"
}

main() {
  local GOT OUTPUT RESULT WANT

  cd "${WORKDIR}"
  build

  shell_test::setup_server_test

  # Start the profile repository daemon.
  local -r REPO="profiled-test-repo"
  local -r STORE=$(shell::tmp_dir)
  shell_test::start_server "${PROFILED_BIN}" --name="${REPO}" --veyron.tcp.address=127.0.0.1:0 --store="${STORE}" \
    || shell_test::fail "line ${LINENO} failed to start server"

  # Create a profile.
  local -r PROFILE="${REPO}/test-profile"
  "${PROFILE_BIN}" put "${PROFILE}" || shell_test::fail "line ${LINENO}: 'put' failed"

  # Retrieve the profile label.
  OUTPUT=$(shell::tmp_file)
  "${PROFILE_BIN}" label "${PROFILE}" | tee "${OUTPUT}" || shell_test::fail "line ${LINENO}: 'label' failed"
  GOT=$(cat "${OUTPUT}")
  WANT="example"
  shell_test::assert_eq "${GOT}" "${WANT}" "${LINENO}"

  # Retrieve the profile description.
  OUTPUT=$(shell::tmp_file)
  "${PROFILE_BIN}" description "${PROFILE}" | tee "${OUTPUT}" || shell_test::fail "line ${LINENO}: 'description' failed"
  GOT=$(cat "${OUTPUT}")
  WANT="Example profile to test the profile manager implementation."
  shell_test::assert_eq "${GOT}" "${WANT}" "${LINENO}"

  # Retrieve the profile specification.
  OUTPUT=$(shell::tmp_file)
  "${PROFILE_BIN}" specification "${PROFILE}" | tee "${OUTPUT}" || shell_test::fail "line ${LINENO}: 'spec' failed"
  GOT=$(cat "${OUTPUT}")
  WANT='profile.Specification{Arch:"amd64", Description:"Example profile to test the profile manager implementation.", Format:"ELF", Libraries:map[profile.Library]struct {}{profile.Library{Name:"foo", MajorVersion:"1", MinorVersion:"0"}:struct {}{}}, Label:"example", OS:"linux"}'
  shell_test::assert_eq "${GOT}" "${WANT}" "${LINENO}"

  # Remove the profile.
  "${PROFILE_BIN}" remove "${PROFILE}" || shell_test::fail "line ${LINENO}: 'remove' failed"

  # Check the profile no longer exists.
  RESULT=$(shell::check_result "${PROFILE_BIN}" label "${PROFILE}")
  shell_test::assert_ne "${RESULT}" "0" "${LINENO}"
  RESULT=$(shell::check_result "${PROFILE_BIN}" description "${PROFILE}")
  shell_test::assert_ne "${RESULT}" "0" "${LINENO}"
  RESULT=$(shell::check_result "${PROFILE_BIN}" specification "${PROFILE}")
  shell_test::assert_ne "${RESULT}" "0" "${LINENO}"

  shell_test::pass
}

main "$@"
