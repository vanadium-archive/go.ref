// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package discovery

import (
	"github.com/pborman/uuid"

	"v.io/v23/discovery"
)

const pkgPath = "v.io/x/ref/runtime/internal/discovery"

// ds is an implementation of discovery.T.
type ds struct {
	plugins []Plugin
}

// Advertisement holds a set of service properties to advertise.
type Advertisement struct {
	discovery.Service

	// The service UUID to advertise.
	ServiceUuid uuid.UUID

	// Type of encryption applied to the advertisement so that it can
	// only be decoded by authorized principals.
	EncryptionAlgorithm EncryptionAlgorithm
	// If the advertisement is encrypted, then the data required to
	// decrypt it. The format of this data is a function of the algorithm.
	EncryptionKeys []EncryptionKey

	// TODO(jhahn): Add proximity.
	// TODO(jhahn): Use proximity for Lost.
	Lost bool
}

type EncryptionAlgorithm int
type EncryptionKey []byte

const (
	NoEncryption   EncryptionAlgorithm = 0
	TestEncryption EncryptionAlgorithm = 1
	IbeEncryption  EncryptionAlgorithm = 2
)

// TODO(jhahn): Need a better API.
func New(plugins []Plugin) discovery.T {
	ds := &ds{plugins: make([]Plugin, len(plugins))}
	copy(ds.plugins, plugins)
	return ds
}
