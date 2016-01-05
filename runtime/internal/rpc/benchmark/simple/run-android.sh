#!/bin/bash
# Copyright 2015 The Vanadium Authors. All rights reserved.
# Use of this source code is governed by a BSD-style
# license that can be found in the LICENSE file.

set -e
set -x

[ -z "${GOPATH}" ] && echo "Must set GOPATH, for example: export GOPATH=\$HOME/go so that gomobile can be installed there" && exit 1
go get golang.org/x/mobile/cmd/gomobile
GOMOBILE=${GOPATH}/bin/gomobile
jiri run ${GOMOBILE} build v.io/x/ref/runtime/internal/rpc/benchmark/simple
adb install -r ./simple.apk
echo "Start the v23rpc app on the phone connected to adb"
adb logcat *:S GoLog:*

