#!/bin/bash

set -e
set -u

VEYRON_BIN=${VEYRON_ROOT}/veyron/go/bin
ID_FILE=/var/tmp/id

# Generate a self-signed identity.
${VEYRON_BIN}/identity generate > ${ID_FILE}

# Start the mounttable daemon.
${VEYRON_BIN}/mounttabled --address=':8100' &

export VEYRON_IDENTITY=${ID_FILE}
export NAMESPACE_ROOT='/127.0.0.1:8100'

sleep 1  # Wait for mounttabled to start up.

# Start the store daemon.
rm -rf /var/tmp/veyron_store.db
${VEYRON_BIN}/stored &

sleep 1  # Wait for stored to start up.

# Initialize the store with mdb data and templates.
${VEYRON_BIN}/mdb_init --load-all

echo
echo 'Visit http://localhost:5000 to browse the mdb data.'
echo 'Hit Ctrl-C to kill all running services.'

trap 'kill $(jobs -pr)' INT TERM EXIT
wait
