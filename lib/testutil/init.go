// Package testutil provides initalization and utility routines for unit tests.
//
// All tests should import it, even if only for its initialization:
//   import _ "veyron/lib/testutil"
//
package testutil

import (
	"flag"
	"math/rand"
	"os"
	"runtime"
	"strconv"
	// Need to import all of the packages that could possibly
	// define flags that we care about. In practice, this is the
	// flags defined by the testing package, the logging library
	// and any flags defined by the blackbox package below.
	_ "testing"
	"time"

	// Import blackbox to ensure that it gets to define its flags.
	_ "veyron/lib/testutil/blackbox"

	"veyron2/vlog"
)

const (
	SeedEnv = "VEYRON_RNG_SEED"
)

var (
	Rand *rand.Rand
)

func init() {
	if os.Getenv("GOMAXPROCS") == "" {
		// Set the number of logical processors to the number of CPUs,
		// if GOMAXPROCS is not set in the environment.
		runtime.GOMAXPROCS(runtime.NumCPU())
	}
	// Initialize pseudo-random number generator.
	seed := time.Now().UnixNano()
	seedString := os.Getenv(SeedEnv)
	if seedString != "" {
		var err error
		base, bitSize := 0, 64
		seed, err = strconv.ParseInt(seedString, 0, 64)
		if err != nil {
			vlog.Fatalf("ParseInt(%v, %v, %v) failed: %v", seedString, base, bitSize, err)
		}
	}
	vlog.Infof("Seeding pseudo-random number generator with %v", seed)
	Rand = rand.New(rand.NewSource(seed))
	// At this point all of the flags that we're going to use for
	// tests must be defined.
	flag.Parse()
	vlog.ConfigureLibraryLoggerFromFlags()
}
