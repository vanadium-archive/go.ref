#!/bin/bash

# Test the debug binary
#
# This test starts a mounttable server and then runs the debug command against
# it.

. "${VEYRON_ROOT}/scripts/lib/shell_test.sh"

readonly WORKDIR=$(shell::tmp_dir)
set +e

build() {
  veyron go build veyron.io/veyron/veyron/services/mounttable/mounttabled || shell_test::fail "line ${LINENO}: failed to build mounttabled"
  veyron go build veyron.io/veyron/veyron/tools/debug || shell_test::fail "line ${LINENO}: failed to build debug"
  veyron go build veyron.io/veyron/veyron/tools/identity || shell_test::fail "line ${LINENO}: failed to build identity"
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

  # Generate an identity that is shared by the client and the server.
  local -r ID="${WORKDIR}/id"
  VEYRON_IDENTITY="" ./identity generate test > "${ID}"
  export VEYRON_IDENTITY="${ID}"

  # Start mounttabled and find its endpoint.
  local -r MTLOG="${WORKDIR}/mt.log"
  touch "${MTLOG}"
  ./mounttabled --veyron.tcp.address=127.0.0.1:0 > "${MTLOG}" 2>&1 &
  shell::wait_for "${MTLOG}" "Mount table service at:"
  local EP=$(grep "Mount table service at:" "${MTLOG}" | sed -e 's/^.*endpoint: //')
  [[ -z "${EP}" ]] && shell_test::fail "line ${LINENO}: no mounttable server"

  # Test top level glob
  local -r DBGLOG="${WORKDIR}/debug.log"
  local GOT=$(./debug glob "${EP}/__debug/*" 2> "${DBGLOG}")
  local WANT="${EP}/__debug/logs
${EP}/__debug/pprof
${EP}/__debug/stats"

  if [[ "${GOT}" != "${WANT}" ]]; then
    dumplogs "${DBGLOG}" "${MTLOG}"
    shell_test::fail "line ${LINENO}: unexpected output. Got ${GOT}, want ${WANT}"
  fi

  # Test logs glob
  GOT=$(./debug glob "${EP}/__debug/logs/*" 2> "${DBGLOG}" | wc -l)
  if [[ "${GOT}" == 0 ]]; then
    dumplogs "${DBGLOG}" "${MTLOG}"
    shell_test::fail "line ${LINENO}: unexpected output. Got ${GOT}, want >=0"
  fi

  # Test logs size
  echo "This is a log file" > "${TMPDIR}/my-test-log-file"
  GOT=$(./debug logs size "${EP}/__debug/logs/my-test-log-file" 2> "${DBGLOG}")
  WANT=$(echo "This is a log file" | wc -c | tr -d ' ')
  if [[ "${GOT}" != "${WANT}" ]]; then
    dumplogs "${DBGLOG}" "${MTLOG}"
    shell_test::fail "line ${LINENO}: unexpected output. Got ${GOT}, want ${WANT}"
  fi

  # Test logs read
  GOT=$(./debug logs read "${EP}/__debug/logs/my-test-log-file" 2> "${DBGLOG}")
  WANT="This is a log file"
  if [[ "${GOT}" != "${WANT}" ]]; then
    dumplogs "${DBGLOG}" "${MTLOG}"
    shell_test::fail "line ${LINENO}: unexpected output. Got ${GOT}, want ${WANT}"
  fi

  # Test stats watchglob
  local TMP=$(shell::tmp_file)
  touch "${TMP}"
  ./debug stats watchglob -raw "${EP}/__debug/stats/ipc/server/*/ReadLog/latency-ms" 2> "${DBGLOG}" > "${TMP}" &
  local pid=$!
  shell::wait_for "${TMP}" "ReadLog/latency-ms"
  kill "${pid}"
  GOT=$(grep "Count:1 " "${TMP}")
  if [[ -z "${GOT}" ]]; then
    dumplogs "${DBGLOG}" "${MTLOG}"
    shell_test::fail "line ${LINENO}: unexpected empty output."
  fi

  # Test pprof
  if ! ./debug pprof run "${EP}/__debug/pprof" heap --text > "${DBGLOG}" 2>&1; then
    dumplogs "${DBGLOG}" "${MTLOG}"
    shell_test::fail "line ${LINENO}: unexpected failure."
  fi

  shell_test::pass
}

main "$@"
