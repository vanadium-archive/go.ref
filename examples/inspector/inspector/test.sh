#!/bin/bash

readonly repo_root=$(git rev-parse --show-toplevel)
readonly thisscript="$0"
readonly workdir=$(mktemp -d "${repo_root}/go/tmp.XXXXXXXXXX")

export TMPDIR="${workdir}"
trap onexit EXIT

onexit() {
  cd /
  exec 2> /dev/null
  kill -9 $(jobs -p)
  rm -rf "${workdir}"
}

fail() {
  [[ $# -gt 0 ]] && echo "${thisscript} $*"
  echo FAIL
  exit 1
}

pass() {
  echo PASS
  exit 0
}

build() {
  local go="${repo_root}/scripts/build/go"
  "${go}" build veyron/examples/inspector/inspector || fail "line ${LINENO}: failed to build inspector"
  "${go}" build veyron/examples/inspector/inspectord || fail "line ${LINENO}: failed to build inspectord"
  "${go}" build veyron/tools/identity || fail "line ${LINENO}: failed to build identity"
}

main() {
  cd "${workdir}"
  build

  # Generate an identity for the client and server
  # For now, using the same one for both
  local id="${workdir}/id"
  ./identity generate inspector >"${id}" || fail "line ${LINENO}: failed to generate an identity"
  export VEYRON_IDENTITY="${id}"

  ./inspectord >ep &
  for i in 1 2 3 4; do
    ep=$(cat ep)
    if [[ -n "${ep}" ]]; then
      break
    fi
    sleep $i
  done
  [[ -z "${ep}" ]] && fail "line ${LINENO}: no inspectord"

  ./inspector --service "/${ep}/stubbed/files" -glob='m*' || fail "line ${LINENO}: stubbed/files"
  ./inspector --service "/${ep}/stubless/files" -glob='m*'|| fail "line ${LINENO}: stubless/files"

  pass
}

main "$@"
