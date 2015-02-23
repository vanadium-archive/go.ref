#!/bin/bash

# The shell library is used to execute veyron shell scripts.

# IMPORTANT: If your script registers its own "trap" handler, that handler must
# call shell::at_exit to clean up all temporary files and directories created by
# this library.

set -e
set -u

TMPDIR="${TMPDIR-/tmp}"
VEYRON_SHELL_TMP_DIRS=${VEYRON_SHELL_TMP_DIRS-$(mktemp "${TMPDIR}/XXXXXXXX")}
VEYRON_SHELL_TMP_FILES=${VEYRON_SHELL_TMP_FILES-$(mktemp "${TMPDIR}/XXXXXXXX")}
VEYRON_SHELL_TMP_PIDS=${VEYRON_SHELL_TMP_PIDS-$(mktemp "${TMPDIR}/XXXXXXXX")}

trap shell::at_exit INT TERM EXIT

# When a user of this library needs cleanup to be performed by root
# because different processes are running with different system
# identities, it should set this variable to root to escalate
# privilege appropriately.
SUDO_USER="$(whoami)"

# shell::at_exit is executed when the shell script exits. It is used,
# for example, to garbage collect any temporary files and directories
# created by invocations of shell::tmp_file and shell:tmp_dir.
shell::at_exit() {
  # If the variable VEYRON_SHELL_CMD_LOOP_AT_EXIT is non-empty, accept commands
  # from the user in a command loop.  This can preserve the state in the logs
  # directories while the user examines them.
  case "${VEYRON_SHELL_CMD_LOOP_AT_EXIT-}" in
  ?*)
    local cmdline
    set +u
    echo 'Entering debug shell.  Type control-D or "exit" to exit.' >&2
    while echo -n "test> " >&2; read cmdline; do
      eval "$cmdline"
    done
    set -u
    ;;
  esac
  # Unset the trap so that it doesn't run again on exit.
  trap - INT TERM EXIT
  for pid in $(cat "${VEYRON_SHELL_TMP_PIDS}"); do
    sudo -u "${SUDO_USER}" kill "${pid}" &> /dev/null || true
  done
  for tmp_dir in $(cat "${VEYRON_SHELL_TMP_DIRS}"); do
     sudo -u "${SUDO_USER}" rm -rf "${tmp_dir}" &>/dev/null
  done
  for tmp_file in $(cat "${VEYRON_SHELL_TMP_FILES}"); do
     sudo -u "${SUDO_USER}" rm -f "${tmp_file}" &>/dev/null
  done
   sudo -u "${SUDO_USER}" rm -f "${VEYRON_SHELL_TMP_DIRS}" "${VEYRON_SHELL_TMP_FILES}" "${VEYRON_SHELL_TMP_PIDS}" &>/dev/null
}

# shell::kill_child_processes kills all child processes.
# Note, child processes must set their own "at_exit: kill_child_processes"
# signal handlers in order for this to kill their descendants.
shell::kill_child_processes() {
  # Attempt to shutdown child processes by sending the TERM signal.
  if [[ -n "$(jobs -p -r)" ]]; then
    shell::silence pkill -P $$
    sleep 1
    # Force shutdown of any remaining child processes using the KILL signal.
    # Note, we only "sleep 1" and "kill -9" if there were child processes.
    if [[ -n "$(jobs -p -r)" ]]; then
      shell::silence sudo -u "${SUDO_USER}"  pkill -9 -P $$
    fi
  fi
}

# shell::check_deps can be used to check which dependencies are
# missing on the host.
#
# Example:
#   local -r DEPS=$(shell::check_deps git golang-go)
#   if [[ -n "${DEPS} ]]; then
#     sudo apt-get install $DEPS
#   fi
shell::check_deps() {
  local RESULT=""
  case $(uname -s) in
    "Linux")
      set +e
      for pkg in "$@"; do
        dpkg -s "${pkg}" &> /dev/null
        if [[ "$?" -ne 0 ]]; then
          RESULT+=" ${pkg}"
        fi
      done
      set -e
      ;;
    "Darwin")
      set +e
      for pkg in "$@"; do
        if [[ -z "$(brew ls --versions ${pkg})" ]]; then
          RESULT+=" ${pkg}"
        fi
      done
      set -e
      ;;
    *)
      echo "Operating system $(uname -s) is not supported."
      exit 1
  esac
  echo "${RESULT}"
}

# shell::check_result can be used to obtain the exit status of a bash
# command. By the semantics of "set -e", a script exits immediately
# upon encountering a command that fails. This function should be used
# if instead, a script wants to take an action depending on the exit
# status.
#
# Example:
#   local -r RESULT=$(shell::check_result <command>)
#   if [[ "${RESULT}" -ne 0 ]]; then
#     <handle failure>
#   else
#     <handle success>
#   fi
shell::check_result() {
  set +e
  "$@" &> /dev/null
  echo "$?"
  set -e
}

# shell::tmp_dir can be used to create a temporary directory.
#
# Example:
#   local -R TMP_DIR=$(shell::tmp_dir)
shell::tmp_dir() {
  local -r RESULT=$(mktemp -d "${TMPDIR}/XXXXXXXX")
  echo "${RESULT}" >> "${VEYRON_SHELL_TMP_DIRS}"
  echo "${RESULT}"
}

# shell::tmp_file can be used to create a temporary file.
#
# Example:
#   local -R TMP_FILE=$(shell::tmp_file)
shell::tmp_file() {
  local -r RESULT=$(mktemp "${TMPDIR}/XXXXXXXX")
  echo "${RESULT}" >> "${VEYRON_SHELL_TMP_FILES}"
  echo "${RESULT}"
}

# shell::run_server is used to start a long running server process and
# to verify that it is still running after a set timeout period. This
# is useful for catching cases where the server either fails to start or
# fails soon after it has started.
# The script will return 0 if the command is running at that point in time
# and a non-zero value otherwise. It echos the server's pid.
#
# Example:
#   shell::run_server 5 stdout stderr command arg1 arg2 || exit 1
shell::run_server() {
  local -r TIMEOUT="$1"
  local -r STDOUT="$2"
  local -r STDERR="$3"
  shift; shift; shift
  if [[ "${STDOUT}" = "${STDERR}" ]]; then
    "$@" > "${STDOUT}" 2>&1 &
  else
    "$@" > "${STDOUT}" 2> "${STDERR}" &
  fi
  local -r SERVER_PID=$!
  echo "${SERVER_PID}" >> "${VEYRON_SHELL_TMP_PIDS}"
  echo "${SERVER_PID}"

  for i in $(seq 1 "${TIMEOUT}"); do
    sleep 1
    local RESULT=$(shell::check_result kill -0 "${SERVER_PID}")
    if [[ "${RESULT}" = "0" ]]; then
      # server's up, can return early
      return 0
    fi
  done
  return "${RESULT}"
}

# shell::timed_wait_for is used to wait until the given pattern appears
# in the given file within a set timeout. It returns 0 if the pattern
# was successfully matched and 1 if the timeout expires before the pattern
# is found.
#
# Example:
#   local -r LOG_FILE=$(shell::tmp_file)
#   shell::run_server 1 "${LOG_FILE}" "${LOG_FILE}" <server> <args>...
#   shell::timed_wait_for 1 "${LOG_FILE}" "server started"
shell::timed_wait_for() {
  local -r TIMEOUT="$1"
  local -r FILE="$2"
  local -r PATTERN="$3"
  for i in $(seq 1 "${TIMEOUT}"); do
    sleep 1
    grep -m 1 "${PATTERN}" "${FILE}" > /dev/null && return 0 || true
  done
  # grep has timed out.
  echo "Timed out. Here's the file:"
  echo "====BEGIN FILE==="
  cat "${FILE}"
  echo "====END FILE==="
  return 1
}

# shell::silence runs the given command with the supplied arguments,
# redirecting all of its output to /dev/null and ignoring its exit
# code.
shell::silence() {
  "$@" &> /dev/null || true
}
