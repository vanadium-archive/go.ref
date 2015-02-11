package exec

import (
	"sync"

	"v.io/core/veyron2/verror"
	"v.io/core/veyron2/vom"
)

// Config defines a simple key-value configuration.  Keys and values are
// strings, and a key can have exactly one value.  The client is responsible for
// encoding structured values, or multiple values, in the provided string.
//
// Config data can come from several sources:
// - passed from parent process to child process through pipe;
// - using environment variables or flags;
// - via the neighborhood-based config service;
// - by RPCs using the Config idl;
// - manually, by calling the Set method.
//
// This interface makes no assumptions about the source of the configuration,
// but provides a unified API for accessing it.
type Config interface {
	// Set sets the value for the key.  If the key already exists in the
	// config, its value is overwritten.
	Set(key, value string)
	// Get returns the value for the key. If the key doesn't exist
	// in the config, Get returns an error.
	Get(key string) (string, error)
	// Clear removes the specified key from the config.
	Clear(key string)
	// Serialize serializes the config to a string.
	Serialize() (string, error)
	// MergeFrom deserializes config information from a string created using
	// Serialize(), and merges this information into the config, updating
	// values for keys that already exist and creating new key-value pairs
	// for keys that don't.
	MergeFrom(string) error
	// Dump returns the config information as a map from ket to value.
	Dump() map[string]string
}

type cfg struct {
	sync.RWMutex
	m map[string]string
}

// New creates a new empty config.
func NewConfig() Config {
	return &cfg{m: make(map[string]string)}
}

func (c *cfg) Set(key, value string) {
	c.Lock()
	defer c.Unlock()
	c.m[key] = value
}

func (c *cfg) Get(key string) (string, error) {
	c.RLock()
	defer c.RUnlock()
	v, ok := c.m[key]
	if !ok {
		return "", verror.New(verror.NoExist, nil, "config.Get", key)
	}
	return v, nil
}

func (c *cfg) Dump() (res map[string]string) {
	res = make(map[string]string)
	c.RLock()
	defer c.RUnlock()
	for k, v := range c.m {
		res[k] = v
	}
	return
}

func (c *cfg) Clear(key string) {
	c.Lock()
	defer c.Unlock()
	delete(c.m, key)
}

func (c *cfg) Serialize() (string, error) {
	c.RLock()
	data, err := vom.Encode(c.m)
	c.RUnlock()
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func (c *cfg) MergeFrom(serialized string) error {
	var newM map[string]string
	if err := vom.Decode([]byte(serialized), &newM); err != nil {
		return err
	}
	c.Lock()
	for k, v := range newM {
		c.m[k] = v
	}
	c.Unlock()
	return nil
}
