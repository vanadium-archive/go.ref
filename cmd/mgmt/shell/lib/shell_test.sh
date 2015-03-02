#!/bin/bash

# The shell_test library is used to execute shell tests.

# IMPORTANT: If your script registers its own "trap" handler, that
# handler must call shell_test::at_exit to clean up all temporary
# files and directories created by this library.

source "$(go list -f {{.Dir}} v.io/x/ref/cmd/mgmt)/shell/lib/shell.sh"

trap shell_test::at_exit INT TERM EXIT

# ensure that we print a 'FAIL' message unless shell_test::pass is
# explicitly called.
shell_test_EXIT_MESSAGE="FAIL_UNEXPECTED_EXIT"

# default timeout values.
shell_test_DEFAULT_SERVER_TIMEOUT="10"
shell_test_DEFAULT_MESSAGE_TIMEOUT="30"

# default binary dir and working dir.
shell_test_BIN_DIR="${shell_test_BIN_DIR:-$(shell::tmp_dir)}"
shell_test_WORK_DIR="$(shell::tmp_dir)"

# shell_test::assert_eq GOT WANT LINENO checks that GOT == WANT and if
# not, fails the test, outputting the offending line number.
shell_test::assert_eq() {
  local -r GOT="$1"
  local -r WANT="$2"
  local -r LINENO="$3"
  if [[ "${GOT}" != "${WANT}" ]]; then
    shell_test::fail "line ${LINENO}: expected '${GOT}' == '${WANT}'"
  fi
}

# shell_test::assert_ge GOT WANT LINENO checks that GOT >= WANT and if
# not, fails the test, outputting the given line number and error
# message.
shell_test::assert_ge() {
  local -r GOT="$1"
  local -r WANT="$2"
  local -r LINENO="$3"
  if [[ "${GOT}" -lt "${WANT}" ]]; then
    shell_test::fail "line ${LINENO}: expected ${GOT} >= ${WANT}"
  fi
}

# shell_test::assert_gt GOT WANT LINENO checks that GOT > WANT and if
# not, fails the test, outputting the given line number and error
# message.
shell_test::assert_gt() {
  local -r GOT="$1"
  local -r WANT="$2"
  local -r LINENO="$3"
  if [[ "${GOT}" -le "${WANT}" ]]; then
    shell_test::fail "line ${LINENO}: expected ${GOT} > ${WANT}"
  fi
}

# shell_test::assert_le GOT WANT LINENO checks that GOT <= WANT and if
# not, fails the test, outputting the given line number and error
# message.
shell_test::assert_le() {
  local -r GOT="$1"
  local -r WANT="$2"
  local -r LINENO="$3"
  if [[ "${GOT}" -gt "${WANT}" ]]; then
    shell_test::fail "line ${LINENO}: expected ${GOT} <= ${WANT}"
  fi
}

# shell_test::assert_lt GOT WANT LINENO checks that GOT < WANT and if
# not, fails the test, outputting the given line number and error
# message.
shell_test::assert_lt() {
  local -r GOT="$1"
  local -r WANT="$2"
  local -r LINENO="$3"
  if [[ "${GOT}" -ge "${WANT}" ]]; then
    shell_test::fail "line ${LINENO}: expected ${GOT} < ${WANT}"
  fi
}

# shell_test::assert_ne GOT WANT LINENO checks that GOT != WANT and if
# not, fails the test, outputting the offending line number.
shell_test::assert_ne() {
  local -r GOT="$1"
  local -r WANT="$2"
  local -r LINENO="$3"
  if [[ "${GOT}" == "${WANT}" ]]; then
    shell_test::fail "line ${LINENO}: expected '${GOT}' != '${WANT}'"
  fi
}

# shell_test::assert_contains GOT WANT LINENO checks that GOT contains WANT and
# if not, fails the test, outputting the offending line number.
shell_test::assert_contains() {
  local -r GOT="$1"
  local -r WANT="$2"
  local -r LINENO="$3"
  if [[ ! "${GOT}" =~ "${WANT}" ]]; then
    shell_test::fail "line ${LINENO}: expected '${GOT}' contains '${WANT}'"
  fi
}

# shell_test::at_exit is executed when the shell script exits.
shell_test::at_exit() {
  echo "${shell_test_EXIT_MESSAGE}"
  # Note: shell::at_exit unsets our trap, so it won't run again on exit.
  shell::at_exit
  shell::kill_child_processes
}

# shell_test::fail is used to identify a failing test.
#
# Example:
#   <command> || shell_test::fail "line ${LINENO}: <error message>"
shell_test::fail() {
  [[ "$#" -gt 0 ]] && echo "$0 $*"
  shell_test_EXIT_MESSAGE="FAIL"
  exit 1
}

# shell_test::pass is used to identify a passing test.
shell_test::pass() {
  shell_test_EXIT_MESSAGE="PASS"
  exit 0
}

# shell_test::build_go_binary builds the binary for the given go package ($1) with
# the given output ($2) in $shell_test_BIN_DIR. Before building a binary, it will
# check whether it already exists. If so, it will skip it.
shell_test::build_go_binary() {
  pushd "${shell_test_BIN_DIR}" > /dev/null
  local -r PACKAGE="$1"
  local OUTPUT="${2:-$(basename ${PACKAGE})}"
  if [[ -f "${OUTPUT}" ]]; then
    echo "${shell_test_BIN_DIR}/${OUTPUT}"
    return
  fi

  go build -o "${OUTPUT}" "${PACKAGE}" 2>/dev/null || shell_test::fail "line ${LINENO}: failed to build ${OUTPUT}"
  echo "${shell_test_BIN_DIR}/${OUTPUT}"
  popd > /dev/null
}

# shell_test::setup_server_test is common boilerplate used for testing veyron
# servers. In particular, this function sets up an instance of the mount table
# daemon, and sets the NAMESPACE_ROOT environment variable accordingly.  It also
# sets up credentials as needed.
shell_test::setup_server_test() {
  if [[ -n ${shell_test_RUNNING_UNDER_AGENT+1} ]]; then
    shell_test::setup_server_test_agent
  else
    shell_test::setup_server_test_no_agent
  fi
}

# shell_test::setup_mounttable brings up a mounttable and sets the
# NAMESPACE_ROOT to point to it.
shell_test::setup_mounttable() {
  # Build the mount table daemon and related tools.
  MOUNTTABLED_BIN="$(shell_test::build_go_binary 'v.io/x/ref/services/mounttable/mounttabled')"

  # Start the mounttable daemon.
  local -r MT_LOG=$(shell::tmp_file)
  if [[ -n ${shell_test_RUNNING_UNDER_AGENT+1} ]]; then
    shell::run_server "${shell_test_DEFAULT_SERVER_TIMEOUT}" "${MT_LOG}" "${MT_LOG}" "${VRUN}" "${MOUNTTABLED_BIN}" --veyron.tcp.address="127.0.0.1:0" &> /dev/null || (cat "${MT_LOG}" && shell_test::fail "line ${LINENO}: failed to start mounttabled")
  else
    shell::run_server "${shell_test_DEFAULT_SERVER_TIMEOUT}" "${MT_LOG}" "${MT_LOG}" "${MOUNTTABLED_BIN}" --veyron.tcp.address="127.0.0.1:0" &> /dev/null || (cat "${MT_LOG}" && shell_test::fail "line ${LINENO}: failed to start mounttabled")
  fi
  shell::timed_wait_for "${shell_test_DEFAULT_MESSAGE_TIMEOUT}" "${MT_LOG}" "Mount table service" || shell_test::fail "line ${LINENO}: failed to find expected output"

  export NAMESPACE_ROOT=$(grep "Mount table service at:" "${MT_LOG}" | sed -e 's/^.*endpoint: //')
  [[ -n "${NAMESPACE_ROOT}" ]] || shell_test::fail "line ${LINENO}: failed to read endpoint from logfile"
}

# shell_test::setup_server_test_no_agent does the work of setup_server_test when
# not running under enable_agent (using credentials directories).
shell_test::setup_server_test_no_agent() {
  [[ ! -n ${shell_test_RUNNING_UNDER_AGENT+1} ]] \
    || shell_test::fail "setup_server_test_no_agent called when running under enable_agent"
  shell_test::setup_mounttable

  # Create and use a new credentials directory /after/ the mount table is
  # started so that servers or clients started after this point start with different
  # credentials.
  local -r TMP=${VEYRON_CREDENTIALS:-}
  if [[ -z "${TMP}" ]]; then
    export VEYRON_CREDENTIALS=$(shell::tmp_dir)
  fi
}

# shell_test::setup_server_test_agent does the work of setup_server_test when
# running under enable_agent.
shell_test::setup_server_test_agent() {
  [[ -n ${shell_test_RUNNING_UNDER_AGENT+1} ]] \
    || shell_test::fail "setup_server_test_agent called when not running under enable_agent"
  shell_test::setup_mounttable
}

# shell_test::start_server is used to start a server. The server is
# considered started when it successfully mounts itself into a mount
# table; an event that is detected using the shell::wait_for
# function.
#
# Example:
#   shell_test::start_server <server> <args>
shell_test::start_server() {
  START_SERVER_LOG_FILE=$(shell::tmp_file)
  START_SERVER_PID=$(shell::run_server "${shell_test_DEFAULT_SERVER_TIMEOUT}" "${START_SERVER_LOG_FILE}" "${START_SERVER_LOG_FILE}" "$@" -logtostderr -vmodule=publisher=2)
  shell::timed_wait_for "${shell_test_DEFAULT_MESSAGE_TIMEOUT}" "${START_SERVER_LOG_FILE}" "ipc pub: mount"
}

# shell_test::credentials is used to create a new VeyronCredentials directory.
# The directory is initialized with a new principal that has a self-signed
# blessing of the specified name set as default and shareable with all peers.
#
# Example:
#   shell_test::credentials <self blessing name>
shell_test::credentials() {
  [[ ! -n ${shell_test_RUNNING_UNDER_AGENT+1} ]] \
    || shell_test::fail "credentials called when running under enable_agent"
  local -r PRINCIPAL_BIN="$(shell_test::build_go_binary 'v.io/x/ref/cmd/principal')"
  local -r CRED=$(shell::tmp_dir)
  "${PRINCIPAL_BIN}" create --overwrite=true "${CRED}" "$1" >/dev/null || shell_test::fail "line ${LINENO}: create failed"
  echo "${CRED}"
}

# shell_test::forkcredentials is used to create a new VeyronCredentials directory
# that is initialized with a principal blessed by the provided principal under the
# provided extension.
#
# Example:
#   shell_test::forkcredentials <blesser principal's VeyronCredentials> <extension>
shell_test::forkcredentials() {
  [[ ! -n ${shell_test_RUNNING_UNDER_AGENT+1} ]] \
    || shell_test::fail "forkcredentials called when running under enable_agent"

  local -r PRINCIPAL_BIN="$(shell_test::build_go_binary 'v.io/x/ref/cmd/principal')"
  local -r FORKCRED=$(shell::tmp_dir)
  "${PRINCIPAL_BIN}" create --overwrite=true "${FORKCRED}" self >/dev/null || shell_test::fail "line ${LINENO}: create failed"
  "${PRINCIPAL_BIN}" --veyron.credentials="$1" bless --require_caveats=false "${FORKCRED}" "$2" >blessing || shell_test::fail "line ${LINENO}: bless failed"
  "${PRINCIPAL_BIN}" --veyron.credentials="${FORKCRED}" store setdefault blessing || shell_test::fail "line ${LINENO}: store setdefault failed"
  "${PRINCIPAL_BIN}" --veyron.credentials="${FORKCRED}" store set blessing ... || shell_test::fail "line ${LINENO}: store set failed"
  echo "${FORKCRED}"
}

# shell_test::enable_agent causes the test to be executed under the security
# agent.  It sets up the agent and then runs itself under that agent.
#
# Example:
#   shell_test::enable_agent "$@"
#   build() {
#     ...
#   }
#   main() {
#     build
#     ...
#     shell_test::pass
#   }
#   main "$@"
shell_test::enable_agent() {
  if [[ ! -n ${shell_test_RUNNING_UNDER_AGENT+1} ]]; then
    local -r AGENTD="$(shell_test::build_go_binary 'v.io/x/ref/security/agent/agentd')"
    local -r WORKDIR="${shell_test_WORK_DIR}"
    export shell_test_RUNNING_UNDER_AGENT=1
    VEYRON_CREDENTIALS="${WORKDIR}/credentials" exec ${AGENTD} --no_passphrase --additional_principals="${WORKDIR}/childcredentials" bash -"$-" "$0" "$@"
    shell_test::fail "failed to run test under agent"
  else
    VRUN="$(shell_test::build_go_binary 'v.io/x/ref/cmd/vrun')"
  fi
}
