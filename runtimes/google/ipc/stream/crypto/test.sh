#!/bin/bash

# Ensure that tls_old.go is in sync with tls.go

source "${VEYRON_ROOT}/scripts/lib/shell_test.sh"

readonly WORKDIR="${shell_test_WORK_DIR}"

main() {
  local -r DIR="$(dirname $0)"
  local -r OUTFILE="${WORKDIR}/latest.go"
  "${DIR}/tls_generate_old.sh" "${OUTFILE}" || shell_test::fail "failed to generate tls_old.go"
  if diff "${OUTFILE}" "${DIR}/tls_old.go"; then
    shell_test::pass
  fi
  shell_test::fail "tls_old.go is not in sync with tls.go, re-run tls_generate_old.sh?"
}

main "$@"
