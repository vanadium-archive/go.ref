// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package util

import (
	"fmt"
	"strings"
	"sync"
)

const defaultClass = "users"

// EmailClassifier classifies/categorizes email addresses based on the domain.
type EmailClassifier struct {
	mu sync.RWMutex
	m  map[string]string
}

// Classify returns the classification of email.
func (c *EmailClassifier) Classify(email string) string {
	if c == nil {
		return defaultClass
	}
	parts := strings.Split(email, "@")
	if len(parts) != 2 {
		return defaultClass
	}
	domain := parts[1]
	c.mu.RLock()
	defer c.mu.RUnlock()
	if class := c.m[domain]; len(class) > 0 {
		return class
	}
	return defaultClass
}

// Set implements flag.Value.
//
// value should be a comma-separated list of <domain>=<class> pairs.
func (c *EmailClassifier) Set(value string) error {
	m := make(map[string]string)
	for _, entry := range strings.Split(value, ",") {
		pair := strings.Split(entry, "=")
		if len(pair) != 2 {
			return fmt.Errorf("invalid pair %q: must be in <domain>=<class> format", entry)
		}
		domain := strings.TrimSpace(pair[0])
		class := strings.TrimSpace(pair[1])
		if len(domain) == 0 {
			return fmt.Errorf("empty domain in %q", entry)
		}
		if len(class) == 0 {
			return fmt.Errorf("empty class in %q", entry)
		}
		m[domain] = class
	}
	c.mu.Lock()
	c.m = m
	c.mu.Unlock()
	return nil
}

// Get implements flag.Getter.
func (c *EmailClassifier) Get() interface{} {
	return c
}

func (c *EmailClassifier) String() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return fmt.Sprintf("%v", c.m)
}
