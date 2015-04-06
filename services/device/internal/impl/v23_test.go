// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// This file was auto-generated via go generate.
// DO NOT UPDATE MANUALLY
package impl_test

import "v.io/x/ref/test/modules"

func init() {
	modules.RegisterChild("execScript", `execScript launches the script passed as argument.`, execScript)
	modules.RegisterChild("deviceManager", `deviceManager sets up a device manager server.  It accepts the name to
publish the server under as an argument.  Additional arguments can optionally
specify device manager config settings.`, deviceManager)
	modules.RegisterChild("deviceManagerV10", `This is the same as deviceManager above, except that it has a different major version number`, deviceManagerV10)
	modules.RegisterChild("app", ``, app)
}
