#!/bin/bash

# Test the profile repository daemon.
#
# This test starts an profile repository daemon and uses the profile
# repository client to verify that <profile>.Put(), <profile>.Label(),
# <profile>.Description(), <profile>.Speficiation(), and
# <profile>.Remove() work as expected.

source "${VEYRON_ROOT}/environment/scripts/lib/shell_test.sh"

build() {
  local -r GO="${VEYRON_ROOT}/scripts/build/go"
  "${GO}" build veyron.io/veyron/veyron/services/mgmt/profile/profiled || shell_test::fail "line ${LINENO}: failed to build 'profiled'"
  "${GO}" build veyron.io/veyron/veyron/tools/profile || shell_test::fail "line ${LINENO}: failed to build 'profile'"
}

main() {
  cd "${TMPDIR}"
  build

  shell_test::setup_server_test

  # Start the profile repository daemon.
  local -r REPO="profiled-test-repo"
  local -r STORE=$(shell::tmp_dir)
  shell_test::start_server ./profiled --name="${REPO}" --address=127.0.0.1:0 --store="${STORE}"

  # Create a profile.
  local -r PROFILE="${REPO}/test-profile"
  ./profile put "${PROFILE}" || shell_test::fail "line ${LINENO}: 'put' failed"

  # Retrieve the profile label.
  local OUTPUT=$(shell::tmp_file)
  ./profile label "${PROFILE}" | tee "${OUTPUT}" || shell_test::fail "line ${LINENO}: 'label' failed"
  local GOT=$(cat "${OUTPUT}")
  local WANT="example"
  if [[ "${GOT}" != "${WANT}" ]]; then
    shell_test::fail "unexpected result: want '${WANT}', got '${GOT}'"
  fi

  # Retrieve the profile description.
  local OUTPUT=$(shell::tmp_file)
  ./profile description "${PROFILE}" | tee "${OUTPUT}" || shell_test::fail "line ${LINENO}: 'description' failed"
  local GOT=$(cat "${OUTPUT}")
  local WANT="Example profile to test the profile manager implementation."
  if [[ "${GOT}" != "${WANT}" ]]; then
    shell_test::fail "unexpected result: want '${WANT}', got '${GOT}'"
  fi

  # Retrieve the profile specification.
  local OUTPUT=$(shell::tmp_file)
  ./profile spec "${PROFILE}" | tee "${OUTPUT}" || shell_test::fail "line ${LINENO}: 'spec' failed"
  local GOT=$(cat "${OUTPUT}")
  local WANT='profile.Specification{Arch:"amd64", Description:"Example profile to test the profile manager implementation.", Format:"ELF", Libraries:map[profile.Library]struct {}{profile.Library{Name:"foo", MajorVersion:"1", MinorVersion:"0"}:struct {}{}}, Label:"example", OS:"linux"}'
  if [[ "${GOT}" != "${WANT}" ]]; then
    shell_test::fail "unexpected result: want '${WANT}', got '${GOT}'"
  fi

  # Remove the profile.
  ./profile remove "${PROFILE}" || shell_test::fail "line ${LINENO}: 'remove' failed"

  # Check the profile no longer exists.
  ./profile label "${PROFILE}" && "line ${LINENO}: 'label' did not fail when it should"
  ./profile description "${PROFILE}" && "line ${LINENO}: 'description' did not fail when it should"
  ./profile spec "${PROFILE}" && "line ${LINENO}: 'spec' did not fail when it should"

  shell_test::pass
}

main "$@"
