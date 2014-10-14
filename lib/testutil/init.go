// Package testutil provides initalization and utility routines for unit tests.
//
// All tests should import it, even if only for its initialization:
//   import _ "veyron.io/veyron/veyron/lib/testutil"
//
package testutil

import (
	"flag"
	"math/rand"
	"os"
	"runtime"
	"strconv"
	"sync"
	// Need to import all of the packages that could possibly
	// define flags that we care about. In practice, this is the
	// flags defined by the testing package, the logging library
	// and any flags defined by the blackbox package below.
	// TODO(cnicolau,ashankar): This is painful to ensure. Not calling
	// flag.Parse in init() is the right solution?
	_ "testing"
	"time"

	_ "veyron.io/veyron/veyron/services/mgmt/suidhelper/impl/flag"

	// Import blackbox to ensure that it gets to define its flags.
	_ "veyron.io/veyron/veyron/lib/testutil/blackbox"

	"veyron.io/veyron/veyron2/vlog"
)

const (
	SeedEnv = "VEYRON_RNG_SEED"
)

// Random is a concurrent-access friendly source of randomness.
type Random struct {
	mu   sync.Mutex
	rand *rand.Rand
}

// Int returns a non-negative pseudo-random int.
func (r *Random) Int() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.rand.Int()
}

// Intn returns a non-negative pseudo-random int in the range [0, n).
func (r *Random) Intn(n int) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.rand.Intn(n)
}

// Int63 returns a non-negative 63-bit pseudo-random integer as an int64.
func (r *Random) Int63() int64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.rand.Int63()
}

var (
	Rand *Random
)

func init() {
	if os.Getenv("GOMAXPROCS") == "" {
		// Set the number of logical processors to the number of CPUs,
		// if GOMAXPROCS is not set in the environment.
		runtime.GOMAXPROCS(runtime.NumCPU())
	}
	// At this point all of the flags that we're going to use for
	// tests must be defined.
	flag.Parse()
	vlog.ConfigureLibraryLoggerFromFlags()
	// Initialize pseudo-random number generator.
	seed := time.Now().UnixNano()
	seedString := os.Getenv(SeedEnv)
	if seedString != "" {
		var err error
		base, bitSize := 0, 64
		seed, err = strconv.ParseInt(seedString, base, bitSize)
		if err != nil {
			vlog.Fatalf("ParseInt(%v, %v, %v) failed: %v", seedString, base, bitSize, err)
		}
	}
	vlog.Infof("Seeding pseudo-random number generator with %v", seed)
	Rand = &Random{rand: rand.New(rand.NewSource(seed))}
}
