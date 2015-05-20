// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package apilog

import "v.io/x/lib/vlog"

func SetLog(l vlog.Logger) {
	logger = l
}

func Log() vlog.Logger {
	return logger
}
