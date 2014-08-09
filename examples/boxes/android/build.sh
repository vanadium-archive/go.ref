#!/bin/bash

source "${VEYRON_ROOT}/environment/scripts/lib/shell.sh"

main() {
  # Path of the go binary that was built with the goandroid patches.
  local -r GOANDROID="$1"

  # Path of where eclipse installs the libs for the DragAndDraw
  # android app.
  local -r SHAREDLIB="$2"

  local -r ANDROID_CC="${NDK_ROOT}/bin/arm-linux-androideabi-gcc"
  local ANDROID_LDFLAGS="-android -shared -extld ${CC} -extldflags"
  ANDROID_LDFLAGS+=" '-march=armv7-a -mfloat-abi=softfp -mfpu=vfpv3-d16'"
  CC="${ANDROID_CC}" GOPATH="`pwd`:${GOPATH}" GOROOT="" GOOS=linux \
    GOARCH=arm GOARM=7 CGO_ENABLED=1 "${GOANDROID}" install "${GOFLAGS}" \
    -v -ldflags="${ANDROID_LDFLAGS}" -tags android boxesp2p
  cp bin/linux_arm/boxesp2p "${SHAREDLIB}"
}

main "$@"
