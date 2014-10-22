#!/bin/bash

# Test the debug binary
#
# This test starts a mounttable server and then runs the debug command against
# it.

source "${VEYRON_ROOT}/scripts/lib/shell_test.sh"

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
  local GOT WANT

  cd "${WORKDIR}"
  mkdir "tmp"
  export TMPDIR="${WORKDIR}/tmp"
  build

  export VEYRON_CREDENTIALS=$(shell::tmp_dir)
  shell_test::setup_server_test || shell_test::fail "setup_server_test failed"
  local -r EP="${NAMESPACE_ROOT}"
  unset NAMESPACE_ROOT

  # Test top level glob.
  local -r DBGLOG="${WORKDIR}/debug.log"
  GOT=$(./debug glob "${EP}/__debug/*" 2> "${DBGLOG}") \
    || (dumplogs "${DBGLOG}"; shell_test::fail "line ${LINENO}: failed to run debug")
  WANT="${EP}/__debug/logs
${EP}/__debug/pprof
${EP}/__debug/stats"
  shell_test::assert_eq "${GOT}" "${WANT}" "${LINENO}"

  # Test logs glob.
  GOT=$(./debug glob "${EP}/__debug/logs/*" 2> "${DBGLOG}" | wc -l) \
    || (dumplogs "${DBGLOG}"; shell_test::fail "line ${LINENO}: failed to run debug")
  shell_test::assert_gt "${GOT}" "0" "${LINENO}"

  # Test logs size.
  echo "This is a log file" > "${TMPDIR}/my-test-log-file"
  GOT=$(./debug logs size "${EP}/__debug/logs/my-test-log-file" 2> "${DBGLOG}") \
    || (dumplogs "${DBGLOG}"; shell_test::fail "line ${LINENO}: failed to run debug")
  WANT=$(echo "This is a log file" | wc -c | tr -d ' ')
  shell_test::assert_eq "${GOT}" "${WANT}" "${LINENO}"

  # Test logs read.
  GOT=$(./debug logs read "${EP}/__debug/logs/my-test-log-file" 2> "${DBGLOG}") \
    || (dumplogs "${DBGLOG}"; shell_test::fail "line ${LINENO}: failed to run debug")
  WANT="This is a log file"
  shell_test::assert_eq "${GOT}" "${WANT}" "${LINENO}"

  # Test stats read.
  GOT=$(./debug stats read "${EP}/__debug/stats/ipc/server/*/ReadLog/latency-ms" 2> "${DBGLOG}" | wc -l) \
    || (dumplogs "${DBGLOG}"; shell_test::fail "line ${LINENO}: failed to run debug")
  shell_test::assert_gt "${GOT}" "0" "${LINENO}"

  # Test stats watch.
  local TMP=$(shell::tmp_file)
  touch "${TMP}"
  local -r DEBUG_PID=$(shell::run_server "${shell_test_DEFAULT_SERVER_TIMEOUT}" "${TMP}" "${DBGLOG}" \
    ./debug stats watch -raw "${EP}/__debug/stats/ipc/server/*/ReadLog/latency-ms")
  shell::timed_wait_for "${shell_test_DEFAULT_MESSAGE_TIMEOUT}" "${TMP}" "ReadLog/latency-ms"
  kill "${DEBUG_PID}"
  grep -q "Count:1 " "${TMP}" || (dumplogs "${TMP}"; shell_test::fail "line ${LINENO}: failed to find expected output")

  # Test pprof.
  if ! ./debug pprof run "${EP}/__debug/pprof" heap --text &> "${DBGLOG}"; then
    dumplogs "${DBGLOG}"
    shell_test::fail "line ${LINENO}: unexpected failure."
  fi

  shell_test::pass
}

main "$@"
