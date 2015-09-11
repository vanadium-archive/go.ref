// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package interfaces

func (in GenVector) DeepCopy() GenVector {
	out := make(GenVector)
	for p, inpgv := range in {
		out[p] = inpgv.DeepCopy()
	}
	return out
}

func (in PrefixGenVector) DeepCopy() PrefixGenVector {
	out := make(PrefixGenVector)
	for id, gen := range in {
		out[id] = gen
	}
	return out
}
