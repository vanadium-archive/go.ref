#!/bin/bash

# Test that tests the routes of the identityd server.

source "$(go list -f {{.Dir}} veyron.io/veyron/shell/lib)/shell_test.sh"

readonly WORKDIR="${shell_test_WORK_DIR}"

build() {
  IDENTITYD_BIN="$(shell_test::build_go_binary 'veyron.io/veyron/veyron/services/identity/identityd_test')"
  PRINCIPAL_BIN="$(shell_test::build_go_binary 'veyron.io/veyron/veyron/tools/principal')"
}

# These certificatese were created with "generate_cert.go  --host=localhost --duration=87600h --ecdsa-curve=P256"
CERT="-----BEGIN CERTIFICATE-----
MIIBbTCCARSgAwIBAgIRANKYmC0v3pK+VohyJOdD1hgwCgYIKoZIzj0EAwIwEjEQ
MA4GA1UEChMHQWNtZSBDbzAeFw0xNDExMjEyMjEwNTJaFw0yNDExMTgyMjEwNTJa
MBIxEDAOBgNVBAoTB0FjbWUgQ28wWTATBgcqhkjOPQIBBggqhkjOPQMBBwNCAASv
heWcWcZT7d5Sm/uoWhBUJJPBSREN4qGzBV7yFYUFvHJ9mNaEcopo/6BopJRbvUmj
CQMVDZVMm5Er/f8HgCngo0swSTAOBgNVHQ8BAf8EBAMCAKAwEwYDVR0lBAwwCgYI
KwYBBQUHAwEwDAYDVR0TAQH/BAIwADAUBgNVHREEDTALgglsb2NhbGhvc3QwCgYI
KoZIzj0EAwIDRwAwRAIgAkwh+mi5YlIxYzxzT7bQj/ZYU5pufxHt+F+a75gbm7AC
IAI9+axCPawySY+UYvjO14hklsyy3LnSf1mNHyeGydMM
-----END CERTIFICATE-----"

KEY="-----BEGIN EC PRIVATE KEY-----
MHcCAQEEIHxiR6vjOn1jF1KS0V//pXrulxss9PwUgV/7/QVeV2zCoAoGCCqGSM49
AwEHoUQDQgAEr4XlnFnGU+3eUpv7qFoQVCSTwUkRDeKhswVe8hWFBbxyfZjWhHKK
aP+gaKSUW71JowkDFQ2VTJuRK/3/B4Ap4A==
-----END EC PRIVATE KEY-----"

# runprincipal starts the principal tool, extracts the url and curls it, to avoid the
# dependence the principal tool has on a browser.
runprincipal() {
  local PFILE="${WORKDIR}/principalfile"
  # Start the tool in the background.
  "${PRINCIPAL_BIN}"  seekblessings --browser=false --from=https://localhost:8125/google -v=3 2> "${PFILE}" &
  sleep 2
  # Search for the url and run it.
  cat "${PFILE}" | grep https |
  while read url; do
    RESULT=$(curl -L --insecure -c ${WORKDIR}/cookiejar $url);
    # Clear out the file
    echo $RESULT;
    break;
  done;
  rm "${PFILE}";
}

main() {
  cd "${WORKDIR}"
  build

  # Setup the certificate files.
  echo "${CERT}" > "${WORKDIR}/cert.pem"
  echo "${KEY}" > "${WORKDIR}/key.pem"

  shell_test::setup_server_test || shell_test::fail "line ${LINENO} failed to setup server test"
  unset VEYRON_CREDENTIALS

  # Start the identityd server in test identity server.
  shell_test::start_server "${IDENTITYD_BIN}" --host=localhost --tlsconfig="${WORKDIR}/cert.pem,${WORKDIR}/key.pem" -veyron.tcp.address=127.0.0.1:0
  echo Identityd Log File: $START_SERVER_LOG_FILE
  export VEYRON_CREDENTIALS="$(shell::tmp_dir)"

  # Test an initial seekblessings call, with a specified VEYRON_CREDENTIALS.
  WANT="Received blessings"
  GOT=$(runprincipal)
  if [[ ! "${GOT}" =~ "${WANT}" ]]; then
    shell_test::fail "line ${LINENO} failed first seekblessings call"
  fi
  # Test that a subsequent call succeed with the same credentials. This means that the blessings and principal from the first call works correctly.
  GOT=$(runprincipal)
  if [[ ! "${GOT}" =~ "${WANT}" ]]; then
    shell_test::fail "line ${LINENO} failed second seekblessings call"
  fi

  shell_test::pass
}

main "$@"