#!/bin/bash

source "${VEYRON_ROOT}/environment/scripts/lib/shell_test.sh"

build() {
  local -r GO="${REPO_ROOT}/scripts/build/go"
  "${GO}" build veyron/examples/inspector/inspector || shell_test::fail "line ${LINENO}: failed to build inspector"
  "${GO}" build veyron/examples/inspector/inspectord || shell_test::fail "line ${LINENO}: failed to build inspectord"
  "${GO}" build veyron/tools/identity || shell_test::fail "line ${LINENO}: failed to build identity"
}

main() {
  cd "${TMPDIR}"
  build

  # Generate an identity for the client and server
  # For now, using the same one for both
  local -r ID="${TMPDIR}/id"
  ./identity generate inspector >"${ID}" || shell_test::fail "line ${LINENO}: failed to generate an identity"
  export VEYRON_IDENTITY="${ID}"

  ./inspectord --address=127.0.0.1:0 >ep &
  for i in 1 2 3 4; do
    local EP=$(cat ep)
    if [[ -n "${EP}" ]]; then
      break
    fi
    sleep "${i}"
  done
  [[ -z "${EP}" ]] && shell_test::fail "line ${LINENO}: no inspectord"

  ./inspector --service "/${EP}/stubbed/files" -glob='m*' || shell_test::fail "line ${LINENO}: stubbed/files"
  ./inspector --service "/${EP}/stubless/files" -glob='m*'|| shell_test::fail "line ${LINENO}: stubless/files"

  shell_test::pass
}

main "$@"
