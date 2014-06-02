package config

// ConfigService exists to get things going when you plug a device into a network.
// Anything that we currently pass to apps as environment variables (proxy server,
// global name server, ...) should come from here.

// Both keys and values can have embedded white space but they can't start
// or end with whitespace.  keys cannot include ':'s.

type Pair struct {
	Key         string
	Value       string
	Nonexistant bool
}

type ConfigService interface {
	// Stop stops a config service.
	Stop()

	// Get returns the value associated with name or an error if no value exists
	// or can be determined.
	Get(name string) (string, error)

	// GetAll returns all attribute/value pairs as a map or an error if no config
	// can be found.
	GetAll() (map[string]string, error)

	// Watch returns a stream of values for a particuar key.
	Watch(key string) chan Pair

	// Watch returns a stream key, value pairs.
	WatchAll() chan Pair

	// Reread the config info (for example, from a file).  In particular
	// this says nothing about where the config resides or how it is read.
	Reread() error
}
