#!/bin/bash

set -e
set -u

readonly VEYRON_BIN=${VEYRON_ROOT}/veyron/go/bin
readonly ID_FILE=/var/tmp/id

trap onexit INT TERM EXIT

onexit() {
  exec 2>/dev/null
  kill $(jobs -p)
  rm -rf "${ID_FILE}"
}

# Generate a self-signed identity.
${VEYRON_BIN}/identity generate > ${ID_FILE}

# Start the mounttable daemon.
${VEYRON_BIN}/mounttabled --address=':8100' &

# Wait for mounttabled to start up.
sleep 1

export VEYRON_IDENTITY=${ID_FILE}
export NAMESPACE_ROOT='/127.0.0.1:8100'

# Start the store daemon.
rm -rf /var/tmp/veyron_store.db
${VEYRON_BIN}/stored &

# Wait for stored to start up.
sleep 1

# Initialize the store with data and templates.
${VEYRON_BIN}/mdb_init --load-all

echo
echo 'Visit http://localhost:5000 to browse the store.'
echo 'Hit Ctrl-C to kill all running services.'
wait
