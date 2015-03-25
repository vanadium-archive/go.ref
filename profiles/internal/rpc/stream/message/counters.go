// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package message

import (
	"fmt"

	"v.io/x/ref/profiles/internal/rpc/stream/id"
)

// CounterID encapsulates the VCI and Flow used for flow control counter
// accounting.
type CounterID uint64

// VCI returns the VCI encoded within the CounterID
func (c *CounterID) VCI() id.VC { return id.VC(*c >> 32) }

// Flow returns the Flow identifier encoded within the CounterID
func (c *CounterID) Flow() id.Flow { return id.Flow(*c & 0xffffffff) }

func (c *CounterID) String() string { return fmt.Sprintf("Flow:%d/VCI:%d", c.Flow(), c.VCI()) }

// MakeCounterID creates a CounterID from the provided (vci, fid) pair.
func MakeCounterID(vci id.VC, fid id.Flow) CounterID {
	return CounterID(uint64(vci)<<32 | uint64(fid))
}

// Counters is a map from (VCI, Flow) to the number of bytes for that (VCI,
// Flow) pair that the receiver is willing to read.
//
// Counters are not safe for concurrent access from multiple goroutines.
//
// When received in Control messages, clients can iterate over the map:
//	for cid, bytes := range counters {
//		fmt.Println("VCI=%d Flow=%d Bytes=%d", cid.VCI(), cid.Flow(), bytes)
//	}
type Counters map[CounterID]uint32

// NewCounters creates a new Counters object.
func NewCounters() Counters { return Counters(make(map[CounterID]uint32)) }

// Add should be called by the receiving end of a Flow to indicate that it is
// ready to read 'bytes' more data for the flow identified by (vci, fid).
func (c Counters) Add(vci id.VC, fid id.Flow, bytes uint32) {
	c[MakeCounterID(vci, fid)] += bytes
}

func (c Counters) String() string {
	ret := "map[ "
	for cid, bytes := range c {
		ret += fmt.Sprintf("%d@%d:%d ", cid.Flow(), cid.VCI(), bytes)
	}
	ret += "]"
	return ret
}
