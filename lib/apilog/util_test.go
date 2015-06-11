// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package apilog

import "v.io/v23/logging"

// TODO(cnicolaou): remove these when we are using the context for logging.
func SetLog(l logging.Logger) {
	logger = l
}

func Log() logging.Logger {
	return logger
}
