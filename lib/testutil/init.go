// Package testutil provides initalization and utility routines for unit tests.
//
// Configures logging, random number generators and other global state.
// Typical usage in _test.go files:
//   import "v.io/core/veyron/lib/testutil"
//   func init() { testutil.Init() }
package testutil

import (
	"flag"
	"math/rand"
	"os"
	"runtime"
	"strconv"
	"sync"
	"time"

	tsecurity "v.io/core/veyron/lib/testutil/security"

	"v.io/core/veyron2"
	"v.io/core/veyron2/context"
	"v.io/core/veyron2/vlog"
)

const (
	SeedEnv      = "VEYRON_RNG_SEED"
	TestBlessing = "test-blessing"
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

var Rand *Random
var once sync.Once
var RunIntegrationTests bool

const IntegrationTestsFlag = "v23.tests"

func init() {
	flag.BoolVar(&RunIntegrationTests, IntegrationTestsFlag, false, "Run integration tests.")
}

// Init sets up state for running tests: Adjusting GOMAXPROCS,
// configuring the vlog logging library, setting up the random number generator
// etc.
//
// Doing so requires flags to be parse, so this function explicitly parses
// flags. Thus, it is NOT a good idea to call this from the init() function
// of any module except "main" or _test.go files.
func Init() {
	init := func() {
		if os.Getenv("GOMAXPROCS") == "" {
			// Set the number of logical processors to the number of CPUs,
			// if GOMAXPROCS is not set in the environment.
			runtime.GOMAXPROCS(runtime.NumCPU())
		}
		// At this point all of the flags that we're going to use for
		// tests must be defined.
		// This will be the case if this is called from the init()
		// function of a _test.go file.
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
	once.Do(init)
}

// UnsetPrincipalEnvVars unsets all environment variables pertaining to principal
// initialization.
func UnsetPrincipalEnvVars() {
	os.Setenv("VEYRON_CREDENTIALS", "")
	os.Setenv("VEYRON_AGENT_FD", "")
}

// InitForTest initializes a new context.T and sets a freshly created principal
// (with a single self-signed blessing) on it. The principal setting step is skipped
// if this function is invoked from a process run using the modules package.
func InitForTest() (*context.T, veyron2.Shutdown) {
	ctx, shutdown := veyron2.Init()
	if len(os.Getenv("VEYRON_SHELL_HELPER_PROCESS_ENTRY_POINT")) != 0 {
		return ctx, shutdown
	}
	var err error
	if ctx, err = veyron2.SetPrincipal(ctx, tsecurity.NewPrincipal(TestBlessing)); err != nil {
		panic(err)
	}
	return ctx, shutdown
}
