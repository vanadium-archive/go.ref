// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main_test

import (
	"fmt"
	"sync"
)

// tape is not thread-safe.
// TODO(caprita): We should make it thread-safe just in case.
type tape struct {
	stimuli   []interface{}
	responses []interface{}
}

func (t *tape) Record(call interface{}) interface{} {
	t.stimuli = append(t.stimuli, call)

	if len(t.responses) < 1 {
		return fmt.Errorf("Record(%#v) had no response", call)
	}
	resp := t.responses[0]
	t.responses = t.responses[1:]
	return resp
}

func (t *tape) SetResponses(responses []interface{}) {
	t.responses = responses
}

func (t *tape) Rewind() {
	t.stimuli = make([]interface{}, 0)
	t.responses = make([]interface{}, 0)
}

func (t *tape) Play() []interface{} {
	return t.stimuli
}

func newTape() *tape {
	t := new(tape)
	t.Rewind()
	return t
}

// tapeMap provides thread-safe access to the tapes, but each tape is not
// thread-safe.
type tapeMap struct {
	sync.Mutex
	tapes map[string]*tape
}

func newTapeMap() *tapeMap {
	tm := &tapeMap{
		tapes: make(map[string]*tape),
	}
	return tm
}

func (tm *tapeMap) forSuffix(s string) *tape {
	tm.Lock()
	defer tm.Unlock()
	t, ok := tm.tapes[s]
	if !ok {
		t = new(tape)
		tm.tapes[s] = t
	}
	return t
}
