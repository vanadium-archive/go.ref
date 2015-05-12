// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main_test

import (
	"fmt"
	"reflect"
	"sort"
	"sync"
	"testing"
)

type tape struct {
	sync.Mutex
	stimuli   []interface{}
	responses []interface{}
}

func (t *tape) Record(call interface{}) interface{} {
	t.Lock()
	defer t.Unlock()
	t.stimuli = append(t.stimuli, call)

	if len(t.responses) < 1 {
		return fmt.Errorf("Record(%#v) had no response", call)
	}
	resp := t.responses[0]
	t.responses = t.responses[1:]
	return resp
}

func (t *tape) SetResponses(responses ...interface{}) {
	t.Lock()
	defer t.Unlock()
	t.responses = make([]interface{}, len(responses))
	copy(t.responses, responses)
}

func (t *tape) Rewind() {
	t.Lock()
	defer t.Unlock()
	t.stimuli = make([]interface{}, 0)
	t.responses = make([]interface{}, 0)
}

func (t *tape) Play() []interface{} {
	t.Lock()
	defer t.Unlock()
	resp := make([]interface{}, len(t.stimuli))
	copy(resp, t.stimuli)
	return resp
}

func newTape() *tape {
	t := new(tape)
	t.Rewind()
	return t
}

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

func TestOneTape(t *testing.T) {
	tm := newTapeMap()
	if _, ok := tm.forSuffix("mytape").Record("foo").(error); !ok {
		t.Errorf("Expected error")
	}
	tm.forSuffix("mytape").SetResponses("b", "c")
	if want, got := "b", tm.forSuffix("mytape").Record("bar"); want != got {
		t.Errorf("Expected %v, got %v", want, got)
	}
	if want, got := "c", tm.forSuffix("mytape").Record("baz"); want != got {
		t.Errorf("Expected %v, got %v", want, got)
	}
	if want, got := []interface{}{"foo", "bar", "baz"}, tm.forSuffix("mytape").Play(); !reflect.DeepEqual(want, got) {
		t.Errorf("Expected %v, got %v", want, got)
	}
	tm.forSuffix("mytape").Rewind()
	if want, got := []interface{}{}, tm.forSuffix("mytape").Play(); !reflect.DeepEqual(want, got) {
		t.Errorf("Expected %v, got %v", want, got)
	}
}

func TestManyTapes(t *testing.T) {
	tm := newTapeMap()
	tapes := []string{"duct tape", "cassette tape", "watergate tape", "tape worm"}
	for _, tp := range tapes {
		tm.forSuffix(tp).SetResponses(tp + "resp")
	}
	for _, tp := range tapes {
		if want, got := tp+"resp", tm.forSuffix(tp).Record(tp+"stimulus"); want != got {
			t.Errorf("Expected %v, got %v", want, got)
		}
	}
	for _, tp := range tapes {
		if want, got := []interface{}{tp + "stimulus"}, tm.forSuffix(tp).Play(); !reflect.DeepEqual(want, got) {
			t.Errorf("Expected %v, got %v", want, got)
		}
	}
}

func TestTapeParallelism(t *testing.T) {
	tm := newTapeMap()
	var resp []interface{}
	const N = 100
	for i := 0; i < N; i++ {
		resp = append(resp, fmt.Sprintf("resp%010d", i))
	}
	tm.forSuffix("mytape").SetResponses(resp...)
	results := make(chan string, N)
	for i := 0; i < N; i++ {
		go func(index int) {
			results <- tm.forSuffix("mytape").Record(fmt.Sprintf("stimulus%010d", index)).(string)
		}(i)
	}
	var res []string
	for i := 0; i < N; i++ {
		r := <-results
		res = append(res, r)
	}
	sort.Strings(res)
	var expectedRes []string
	for i := 0; i < N; i++ {
		expectedRes = append(expectedRes, fmt.Sprintf("resp%010d", i))
	}
	if want, got := expectedRes, res; !reflect.DeepEqual(want, got) {
		t.Errorf("Expected %v, got %v", want, got)
	}
	var expectedStimuli []string
	for i := 0; i < N; i++ {
		expectedStimuli = append(expectedStimuli, fmt.Sprintf("stimulus%010d", i))
	}
	var gotStimuli []string
	for _, s := range tm.forSuffix("mytape").Play() {
		gotStimuli = append(gotStimuli, s.(string))
	}
	sort.Strings(gotStimuli)
	if want, got := expectedStimuli, gotStimuli; !reflect.DeepEqual(want, got) {
		t.Errorf("Expected %v, got %v", want, got)
	}
}
