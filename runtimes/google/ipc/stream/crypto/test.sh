#!/bin/bash

# Ensure that tls_old.go is in sync with tls.go

source "${VEYRON_ROOT}/environment/scripts/lib/shell_test.sh"

main() {
  local -r DIR="$(dirname $0)"
  local -r OUTFILE="${TMPDIR}/latest.go"
  "${DIR}/tls_generate_old.sh" "${OUTFILE}"
  if diff "${OUTFILE}" "${DIR}/tls_old.go"; then
    shell_test::pass
  fi
  shell_test::fail "tls_old.go is not in sync with tls.go, re-run tls_generate_old.sh?"
}

main "$@"
