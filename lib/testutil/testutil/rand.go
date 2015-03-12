package testutil

import (
	"math/rand"
	"os"
	"strconv"
	"sync"
	"time"

	"v.io/x/lib/vlog"
)

const (
	SeedEnv = "VANADIUM_RNG_SEED"
)

// An instance of Random initialized by the InitRandomGenerator function.
var (
	Rand *Random
	once sync.Once
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

// RandomBytes generates the given number of random bytes.
func (rand *Random) RandomBytes(size int) []byte {
	buffer := make([]byte, size)
	randomMutex.Lock()
	defer randomMutex.Unlock()
	// Generate a 10MB of random bytes since that is a value commonly
	// used in the tests.
	if len(random) == 0 {
		random = generateRandomBytes(rand, 10<<20)
	}
	if size > len(random) {
		extra := generateRandomBytes(rand, size-len(random))
		random = append(random, extra...)
	}
	start := rand.Intn(len(random) - size + 1)
	copy(buffer, random[start:start+size])
	return buffer
}

// Create a new pseudo-random number generator, the seed may be supplied
// by the VANADIUM_RNG_SEED to allow for reproducing a previous sequence.
func NewRandGenerator() *Random {
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
	return &Random{rand: rand.New(rand.NewSource(seed))}
}

// InitRandGenerator creates an instance of Random in the public variable Rand.
func InitRandGenerator() {
	once.Do(func() {
		Rand = NewRandGenerator()
	})
}

var (
	random      []byte
	randomMutex sync.Mutex
)

func generateRandomBytes(rand *Random, size int) []byte {
	buffer := make([]byte, size)
	offset := 0
	for {
		bits := int64(rand.Int63())
		for i := 0; i < 8; i++ {
			buffer[offset] = byte(bits & 0xff)
			size--
			if size == 0 {
				return buffer
			}
			offset++
			bits >>= 8
		}
	}
}

// Int returns a non-negative pseudo-random int using the public variable Rand.
func Int() int {
	return Rand.Int()
}

// Intn returns a non-negative pseudo-random int in the range [0, n) using
// the public variable Rand.
func Intn(n int) int {
	return Rand.Intn(n)
}

// Int63 returns a non-negative 63-bit pseudo-random integer as an int64
// using the public variable Rand.
func Int63() int64 {
	return Rand.Int63()
}

// RandomBytes generates the given number of random bytes using
// the public variable Rand.
func RandomBytes(size int) []byte {
	return Rand.RandomBytes(size)
}
