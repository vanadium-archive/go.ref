#!/bin/bash

source "${VEYRON_ROOT}/environment/scripts/lib/shell.sh"

trap at_exit INT TERM EXIT

readonly REPO_ROOT=$(git rev-parse --show-toplevel)
readonly ID_FILE=$(shell::tmp_file)
readonly DB_DIR=$(shell::tmp_dir)

at_exit() {
  exec 2>/dev/null
  shell::at_exit  # deletes ID_FILE and DB_DIR
  kill -9 $(jobs -p) || true
}

main() {
  # Used by test.sh to get the store viewer port.
  local VIEWER_PORT_FILE=""
  if [[ "$#" -eq 1 ]]; then
    VIEWER_PORT_FILE="$1"
  fi
  local -r VIEWER_PORT_FILE
  local -r VEYRON_BIN="${REPO_ROOT}/go/bin"

  # Generate a self-signed identity.
  "${VEYRON_BIN}/identity" generate > "${ID_FILE}"

  # Start the mounttable daemon.
  local -r MT_PORT=$("${VEYRON_BIN}/findunusedport")
  "${VEYRON_BIN}/mounttabled" --address="127.0.0.1:${MT_PORT}" &

  # Wait for mounttabled to start up.
  sleep 1

  export VEYRON_IDENTITY="${ID_FILE}"
  export NAMESPACE_ROOT="/127.0.0.1:${MT_PORT}"

  # Start the store daemon.
  local -r VIEWER_PORT=$("${VEYRON_BIN}/findunusedport")
  "${VEYRON_BIN}/stored" --address=127.0.0.1:0 --db="${DB_DIR}" --viewerPort="${VIEWER_PORT}" &

  # Wait for stored to start up.
  sleep 1

  # Initialize the store with data and templates.
  "${VEYRON_BIN}/todos_init" --data-path=todos_init/data.json

  if [[ -n "${VIEWER_PORT_FILE}" ]]; then
    echo "${VIEWER_PORT}" > "${VIEWER_PORT_FILE}"
  fi

  echo
  echo "Visit http://localhost:${VIEWER_PORT} to browse the store."
  echo "Hit Ctrl-C to kill all running services."
  wait
}

main "$@"
