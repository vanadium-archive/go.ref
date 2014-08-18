package ipc

import (
	"testing"
	"time"

	"veyron2/context"
)

type fakeContext struct{}

func (*fakeContext) Deadline() (deadline time.Time, ok bool) { return }
func (*fakeContext) Done() <-chan struct{}                   { return nil }
func (*fakeContext) Err() error                              { return nil }
func (*fakeContext) Value(key interface{}) interface{}       { return nil }
func (*fakeContext) WithCancel() (context.T, context.CancelFunc) {
	return &fakeContext{}, func() {}
}
func (*fakeContext) WithDeadline(time.Time) (context.T, context.CancelFunc) {
	return &fakeContext{}, func() {}
}
func (*fakeContext) WithTimeout(time.Duration) (context.T, context.CancelFunc) {
	return &fakeContext{}, func() {}
}
func (*fakeContext) WithValue(k, v interface{}) context.T {
	return &fakeContext{}
}

func TestSuccessfulCalls(t *testing.T) {

	method1ExpectedResult := []interface{}{"one", 2}
	method2ExpectedResult := []interface{}{"one"}
	method3ExpectedResult := []interface{}{nil}

	client := NewSimpleClient(map[string][]interface{}{
		"method1": method1ExpectedResult,
		"method2": method2ExpectedResult,
		"method3": method3ExpectedResult,
	})

	ctx := &fakeContext{}

	// method1
	method1Call, err := client.StartCall(ctx, "name/obj", "method1", []interface{}{})
	if err != nil {
		t.Errorf("StartCall: did not expect an error return")
		return
	}
	var resultOne string
	var resultTwo int
	method1Call.Finish(&resultOne, &resultTwo)
	if resultOne != "one" {
		t.Errorf(`FinishCall: first result was "%v", want "one"`, resultOne)
		return
	}
	if resultTwo != 2 {
		t.Errorf(`FinishCall: second result was "%v", want 2`, resultTwo)
		return
	}

	// method2
	method2Call, err := client.StartCall(ctx, "name/obj", "method2", []interface{}{})
	if err != nil {
		t.Errorf(`StartCall: did not expect an error return`)
		return
	}
	method2Call.Finish(&resultOne)
	if resultOne != "one" {
		t.Errorf(`FinishCall: result "%v", want "one"`, resultOne)
		return
	}

	// method3
	var result interface{}
	method3Call, err := client.StartCall(ctx, "name/obj", "method3", []interface{}{})
	if err != nil {
		t.Errorf(`StartCall: did not expect an error return`)
		return
	}
	method3Call.Finish(&result)
	if result != nil {
		t.Errorf(`FinishCall: result "%v", want nil`, result)
		return
	}
}

type sampleStruct struct {
	name string
}

func TestStructResult(t *testing.T) {
	client := NewSimpleClient(map[string][]interface{}{
		"foo": []interface{}{
			sampleStruct{name: "bar"},
		},
	})
	call, _ := client.StartCall(&fakeContext{}, "name/obj", "foo", []interface{}{})
	var result sampleStruct
	call.Finish(&result)
	if result.name != "bar" {
		t.Errorf(`FinishCall: second result was "%v", want "bar"`, result.name)
		return
	}
}

func TestErrorCall(t *testing.T) {
	client := NewSimpleClient(map[string][]interface{}{
		"bar": []interface{}{},
	})
	_, err := client.StartCall(&fakeContext{}, "name/obj", "wrongMethodName", []interface{}{})
	if err == nil {
		t.Errorf(`StartCall: should have returned an error on invalid method name`)
		return
	}
}

func TestNumberOfCalls(t *testing.T) {
	client := NewSimpleClient(map[string][]interface{}{
		"method1": []interface{}{},
		"method2": []interface{}{},
	})

	errMsg := "Expected method to be called %d times but it was called %d"
	ctx := &fakeContext{}

	// method 1
	if n := client.TimesCalled("method1"); n != 0 {
		t.Errorf(errMsg, 0, n)
		return
	}
	client.StartCall(ctx, "name/of/object", "method1", []interface{}{})
	if n := client.TimesCalled("method1"); n != 1 {
		t.Errorf(errMsg, 1, n)
		return
	}
	client.StartCall(ctx, "name/of/object", "method1", []interface{}{})
	if n := client.TimesCalled("method1"); n != 2 {
		t.Errorf(errMsg, 2, n)
		return
	}

	// method 2
	if n := client.TimesCalled("method2"); n != 0 {
		t.Errorf(errMsg, 0, n)
		return
	}
	client.StartCall(ctx, "name/of/object", "method2", []interface{}{})
	if n := client.TimesCalled("method2"); n != 1 {
		t.Errorf(errMsg, 1, n)
		return
	}
}
