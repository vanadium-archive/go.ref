#!/bin/bash

set -e

# Path of the go binary that was built with the goandroid patches
GOANDROID=$1
# Path of where eclipse installs the libs for the DragAndDraw android app
SHAREDLIB=$2

CC="$NDK_ROOT/bin/arm-linux-androideabi-gcc"
CC=$CC GOPATH="`pwd`:$GOPATH" GOROOT="" GOOS=linux GOARCH=arm GOARM=7 CGO_ENABLED=1 $GOANDROID install $GOFLAGS -v -ldflags="-android -shared -extld $CC -extldflags '-march=armv7-a -mfloat-abi=softfp -mfpu=vfpv3-d16'" -tags android boxesp2p
cp bin/linux_arm/boxesp2p $SHAREDLIB
