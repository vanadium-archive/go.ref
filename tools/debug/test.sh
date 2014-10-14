#!/bin/bash

# Test the debug binary
#
# This test starts a mounttable server and then runs the debug command against
# it.

. "${VEYRON_ROOT}/scripts/lib/shell_test.sh"

readonly WORKDIR=$(shell::tmp_dir)

build() {
  veyron go build veyron.io/veyron/veyron/tools/debug || shell_test::fail "line ${LINENO}: failed to build debug"
}

dumplogs() {
  for x in $*; do
    echo "-- $(basename "${x}") --"
    cat "${x}"
  done
}

main() {
  cd "${WORKDIR}"
  build

  export VEYRON_CREDENTIALS=$(shell::tmp_dir)
  shell_test::setup_server_test || shell_test::fail "setup_server_test failed"
  local -r EP="${NAMESPACE_ROOT}"
  unset NAMESPACE_ROOT

  # Test top level glob
  local -r DBGLOG="${WORKDIR}/debug.log"
  local GOT=$(./debug -v=2 glob "${EP}/__debug/*" 2> "${DBGLOG}")
  local WANT="${EP}/__debug/logs
${EP}/__debug/pprof
${EP}/__debug/stats"

  if [[ "${GOT}" != "${WANT}" ]]; then
    dumplogs "${DBGLOG}"
    shell_test::fail "line ${LINENO}: unexpected output. Got ${GOT}, want ${WANT}"
  fi

  # Test logs glob
  local GOT=$(./debug glob "${EP}/__debug/logs/*" 2> "${DBGLOG}" | wc -l)
  if [[ "${GOT}" == 0 ]]; then
    dumplogs "${DBGLOG}"
    shell_test::fail "line ${LINENO}: unexpected output. Got ${GOT}, want >=0"
  fi

  # Test logs size
  echo "This is a log file" > "${TMPDIR}/my-test-log-file"
  local GOT=$(./debug logs size "${EP}/__debug/logs/my-test-log-file" 2> "${DBGLOG}")
  WANT=$(echo "This is a log file" | wc -c | tr -d ' ')
  if [[ "${GOT}" != "${WANT}" ]]; then
    dumplogs "${DBGLOG}"
    shell_test::fail "line ${LINENO}: unexpected output. Got ${GOT}, want ${WANT}"
  fi

  # Test logs read
  local GOT=$(./debug logs read "${EP}/__debug/logs/my-test-log-file" 2> "${DBGLOG}")
  WANT="This is a log file"
  if [[ "${GOT}" != "${WANT}" ]]; then
    dumplogs "${DBGLOG}"
    shell_test::fail "line ${LINENO}: unexpected output. Got ${GOT}, want ${WANT}"
  fi

  # Test stats watchglob
  local TMP=$(shell::tmp_file)
  touch "${TMP}"
  local -r DEBUG_PID=$(shell::run_server "${shell_test_DEFAULT_SERVER_TIMEOUT}" "${TMP}" "${DBGLOG}" ./debug stats watchglob -raw "${EP}/__debug/stats/ipc/server/*/ReadLog/latency-ms")
  shell::timed_wait_for "${shell_test_DEFAULT_MESSAGE_TIMEOUT}" "${TMP}" "ReadLog/latency-ms"
  kill "${DEBUG_PID}"
  local GOT=$(grep "Count:1 " "${TMP}")
  if [[ -z "${GOT}" ]]; then
    dumplogs "${DBGLOG}"
    shell_test::fail "line ${LINENO}: unexpected empty output."
  fi

  # Test pprof
  if ! ./debug pprof run "${EP}/__debug/pprof" heap --text > "${DBGLOG}" 2>&1; then
    dumplogs "${DBGLOG}"
    shell_test::fail "line ${LINENO}: unexpected failure."
  fi

  shell_test::pass
}

main "$@"
