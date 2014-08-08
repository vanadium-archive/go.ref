#!/bin/bash

set -e
set -u

# Used by test.sh to get the store viewer port.
VIEWER_PORT_FILE=""
if [ $# -eq 1 ]; then
  VIEWER_PORT_FILE="$1"
fi
readonly VIEWER_PORT_FILE

readonly VEYRON_BIN=${VEYRON_ROOT}/veyron/go/bin

readonly repo_root=$(git rev-parse --show-toplevel)
readonly id_file=$(mktemp "${repo_root}/go/tmp.XXXXXXXXXXX")
readonly db_dir=$(mktemp -d "${repo_root}/go/tmp.XXXXXXXXXXX")

trap onexit INT TERM EXIT

onexit() {
  exec 2>/dev/null
  kill $(jobs -p)
  rm -rf "${id_file}" "${db_dir}"
}

# Generate a self-signed identity.
${VEYRON_BIN}/identity generate > ${id_file}

# Start the mounttable daemon.
readonly mt_port=$(${VEYRON_BIN}/findunusedport)
${VEYRON_BIN}/mounttabled --address=":${mt_port}" &

# Wait for mounttabled to start up.
sleep 1

export VEYRON_IDENTITY=${id_file}
export NAMESPACE_ROOT="/127.0.0.1:${mt_port}"

# Start the store daemon.
readonly viewer_port=$(${VEYRON_BIN}/findunusedport)
${VEYRON_BIN}/stored --db="${db_dir}" --viewerPort="${viewer_port}" &

# Wait for stored to start up.
sleep 1

# Initialize the store with data and templates.
${VEYRON_BIN}/mdb_init --load-all

if [ -n "${VIEWER_PORT_FILE}" ]; then
  echo "${viewer_port}" > "${VIEWER_PORT_FILE}"
fi

echo
echo "Visit http://localhost:${viewer_port} to browse the store."
echo "Hit Ctrl-C to kill all running services."
wait
