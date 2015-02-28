package ipc

import (
	"testing"

	"v.io/v23/context"
)

func testContext() *context.T {
	ctx, _ := context.RootContext()
	return ctx
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

	ctx := testContext()

	// method1
	method1Call, err := client.StartCall(ctx, "name/obj", "method1", []interface{}{})
	if err != nil {
		t.Errorf("StartCall: did not expect an error return")
		return
	}
	var resultOne string
	var resultTwo int64
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
	Name string
}

func TestStructResult(t *testing.T) {
	client := NewSimpleClient(map[string][]interface{}{
		"foo": []interface{}{
			sampleStruct{Name: "bar"},
		},
	})
	ctx := testContext()
	call, _ := client.StartCall(ctx, "name/obj", "foo", []interface{}{})
	var result sampleStruct
	call.Finish(&result)
	if result.Name != "bar" {
		t.Errorf(`FinishCall: second result was "%v", want "bar"`, result.Name)
		return
	}
}

func TestErrorCall(t *testing.T) {
	client := NewSimpleClient(map[string][]interface{}{
		"bar": []interface{}{},
	})
	ctx := testContext()
	_, err := client.StartCall(ctx, "name/obj", "wrongMethodName", []interface{}{})
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
	ctx := testContext()

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
