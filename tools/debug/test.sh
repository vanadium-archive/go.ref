#!/bin/bash

# Test the debug binary
#
# This test starts a mounttable server and then runs the debug command against
# it.

source "$(go list -f {{.Dir}} v.io/veyron/shell/lib)/shell_test.sh"

# Run the test under the security agent.
shell_test::enable_agent "$@"

readonly WORKDIR="${shell_test_WORK_DIR}"
readonly DEBUG_FLAGS="--veyron.vtrace.sample_rate=1"

build() {
  DEBUG_BIN="$(shell_test::build_go_binary 'v.io/core/veyron/tools/debug')"
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
  build
  mkdir "tmp"
  export TMPDIR="${WORKDIR}/tmp"

  shell_test::setup_server_test || shell_test::fail "setup_server_test failed"
  local -r EP="${NAMESPACE_ROOT}"
  unset NAMESPACE_ROOT

  # Test top level glob.
  local -r DBGLOG="${WORKDIR}/debug.log"
  GOT=$("${DEBUG_BIN}" "${DEBUG_FLAGS}" glob "${EP}/__debug/*" 2> "${DBGLOG}") \
    || (dumplogs "${DBGLOG}"; shell_test::fail "line ${LINENO}: failed to run debug")
  WANT="${EP}/__debug/logs
${EP}/__debug/pprof
${EP}/__debug/stats
${EP}/__debug/vtrace"
  shell_test::assert_eq "${GOT}" "${WANT}" "${LINENO}"

  # Test logs glob.
  GOT=$("${DEBUG_BIN}" "${DEBUG_FLAGS}" glob "${EP}/__debug/logs/*" 2> "${DBGLOG}" | wc -l) \
    || (dumplogs "${DBGLOG}"; shell_test::fail "line ${LINENO}: failed to run debug")
  shell_test::assert_gt "${GOT}" "0" "${LINENO}"
  
  # Test logs size.
  echo "This is a log file" > "${TMPDIR}/my-test-log-file"
  GOT=$("${DEBUG_BIN}" "${DEBUG_FLAGS}" logs size "${EP}/__debug/logs/my-test-log-file" 2> "${DBGLOG}") \
    || (dumplogs "${DBGLOG}"; shell_test::fail "line ${LINENO}: failed to run debug")
  WANT=$(echo "This is a log file" | wc -c | tr -d ' ')
  shell_test::assert_eq "${GOT}" "${WANT}" "${LINENO}"

  # Test logs read.
  GOT=$("${DEBUG_BIN}" "${DEBUG_FLAGS}" logs read "${EP}/__debug/logs/my-test-log-file" 2> "${DBGLOG}") \
    || (dumplogs "${DBGLOG}"; shell_test::fail "line ${LINENO}: failed to run debug")
  WANT="This is a log file"
  shell_test::assert_eq "${GOT}" "${WANT}" "${LINENO}"

  # Test stats read.
  GOT=$("${DEBUG_BIN}" "${DEBUG_FLAGS}" stats read "${EP}/__debug/stats/ipc/server/routing-id/*/methods/ReadLog/latency-ms" 2> "${DBGLOG}" | wc -l) \
    || (dumplogs "${DBGLOG}"; shell_test::fail "line ${LINENO}: failed to run debug")
  shell_test::assert_gt "${GOT}" "0" "${LINENO}"

  # Test fetching all vtrace traces.
  GOT=$("${DEBUG_BIN}" "${DEBUG_FLAGS}" vtrace "${EP}/__debug/vtrace" 2> "${DBGLOG}" | egrep -o "^Trace - ([^ ]+)" | cut -b 9- | sort) \
    || (dumplogs "${DBGLOG}"; shell_test::fail "line ${LINENO}: failed to run debug")
  shell_test::assert_eq $(echo "${GOT}" | wc -l | tr -d ' ') "6" "${LINENO}"

  # Test fetching individual traces.
  IDS=$(echo "$GOT" | tr '\n' ' ')
  GOT2=$("${DEBUG_BIN}" "${DEBUG_FLAGS}" vtrace "${EP}/__debug/vtrace" ${IDS}  2> "${DBGLOG}" | egrep -o "^Trace - ([^ ]+)" | cut -b 9- | sort) \
    || (dumplogs "${DBGLOG}"; shell_test::fail "line ${LINENO}: failed to run debug")
  shell_test::assert_eq "${GOT2}" "${GOT}" "${LINENO}"

  # Test stats watch.
  local TMP=$(shell::tmp_file)
  touch "${TMP}"
  local -r DEBUG_PID=$(shell::run_server "${shell_test_DEFAULT_SERVER_TIMEOUT}" "${TMP}" "${DBGLOG}" \
    "${DEBUG_BIN}" stats watch -raw "${EP}/__debug/stats/ipc/server/routing-id/*/methods/ReadLog/latency-ms")
  shell::timed_wait_for "${shell_test_DEFAULT_MESSAGE_TIMEOUT}" "${TMP}" "ReadLog/latency-ms"
  kill "${DEBUG_PID}"
  grep -q "Count:1 " "${TMP}" || (dumplogs "${TMP}"; shell_test::fail "line ${LINENO}: failed to find expected output")

  # Test pprof.
  if ! "${DEBUG_BIN}" pprof run "${EP}/__debug/pprof" heap --text &> "${DBGLOG}"; then
    dumplogs "${DBGLOG}"
    shell_test::fail "line ${LINENO}: unexpected failure."
  fi

  shell_test::pass
}

main "$@"
