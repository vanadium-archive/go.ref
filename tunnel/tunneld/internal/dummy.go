package internal

// This file is required to ensure the tunneld v23test uses the generic
// profile. Using the roaming profile (the one used by tunneld itself) causes
// test failures on GCE.
import (
	_ "v.io/core/veyron/profiles"
)
