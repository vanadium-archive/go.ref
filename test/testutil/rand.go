// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package testutil

import (
	"fmt"
	"math/rand"
	"os"
	"strconv"
	"sync"
	"time"
)

const (
	SeedEnv = "V23_RNG_SEED"
)

// An instance of Random initialized by the InitRandomGenerator function.
var (
	Rand *Random
	once sync.Once
)

const randPanicMsg = "It looks like the singleton random number generator has not been initialized, please call InitRandGenerator."

// Random is a concurrent-access friendly source of randomness.
type Random struct {
	mu   sync.Mutex
	seed int64
	rand *rand.Rand
}

// RandomInt returns a non-negative pseudo-random int.
func (r *Random) RandomInt() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.rand.Int()
}

// RandomIntn returns a non-negative pseudo-random int in the range [0, n).
func (r *Random) RandomIntn(n int) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.rand.Intn(n)
}

// RandomInt63 returns a non-negative 63-bit pseudo-random integer as an int64.
func (r *Random) RandomInt63() int64 {
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
	start := rand.RandomIntn(len(random) - size + 1)
	copy(buffer, random[start:start+size])
	return buffer
}

// Create a new pseudo-random number generator, the seed may be supplied
// by V23_RNG_SEED to allow for reproducing a previous sequence.
func NewRandGenerator() *Random {
	seed := time.Now().UnixNano()
	seedString := os.Getenv(SeedEnv)
	if seedString != "" {
		var err error
		base, bitSize := 0, 64
		seed, err = strconv.ParseInt(seedString, base, bitSize)
		if err != nil {
			panic(fmt.Sprintf("ParseInt(%v, %v, %v) failed: %v", seedString, base, bitSize, err))
		}
	}
	return &Random{seed: seed, rand: rand.New(rand.NewSource(seed))}
}

// InitRandGenerator creates an instance of Random in the public variable Rand
// and returns a function intended to be defer'ed that prints out the
// seed use when creating the number number generator using the supplied
// logging function.
func InitRandGenerator(loggingFunc func(format string, args ...interface{})) {
	once.Do(func() {
		Rand = NewRandGenerator()
		loggingFunc("Seeded pseudo-random number generator with %v", Rand.seed)
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
		bits := int64(rand.RandomInt63())
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

// RandomInt returns a non-negative pseudo-random int using the public variable Rand.
func RandomInt() int {
	if Rand == nil {
		panic(randPanicMsg)
	}
	return Rand.RandomInt()
}

// RandomIntn returns a non-negative pseudo-random int in the range [0, n) using
// the public variable Rand.
func RandomIntn(n int) int {
	if Rand == nil {
		panic(randPanicMsg)
	}
	return Rand.RandomIntn(n)
}

// RandomInt63 returns a non-negative 63-bit pseudo-random integer as an int64
// using the public variable Rand.
func RandomInt63() int64 {
	if Rand == nil {
		panic(randPanicMsg)
	}
	return Rand.RandomInt63()
}

// RandomBytes generates the given number of random bytes using
// the public variable Rand.
func RandomBytes(size int) []byte {
	if Rand == nil {
		panic(randPanicMsg)
	}
	return Rand.RandomBytes(size)
}
