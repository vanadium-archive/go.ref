package impl_test

import (
	"fmt"
)

type Tape struct {
	stimuli   []interface{}
	responses []interface{}
}

func (r *Tape) Record(call interface{}) interface{} {
	r.stimuli = append(r.stimuli, call)

	if len(r.responses) < 1 {
		return fmt.Errorf("Record(%#v) had no response", call)
	}
	resp := r.responses[0]
	r.responses = r.responses[1:]
	return resp
}

func (r *Tape) SetResponses(responses []interface{}) {
	r.responses = responses
}

func (r *Tape) Rewind() {
	r.stimuli = make([]interface{}, 0)
	r.responses = make([]interface{}, 0)
}

func (r *Tape) Play() []interface{} {
	return r.stimuli
}

func NewTape() *Tape {
	tape := new(Tape)
	tape.Rewind()
	return tape
}
