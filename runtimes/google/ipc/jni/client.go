// +build android

package main

import (
	"encoding/json"
	"fmt"
	"time"

	"veyron2"
	"veyron2/ipc"
	"veyron2/rt"
)

// #include <stdlib.h>
// #include "jni_wrapper.h"
import "C"

type client struct {
	client ipc.Client
}

func newClient() (*client, error) {
	c, err := rt.R().NewClient()
	if err != nil {
		return nil, err
	}
	return &client{
		client: c,
	}, nil
}

func (c *client) StartCall(env *C.JNIEnv, name, method string, jArgs C.jobjectArray, jPath C.jstring, jTimeout C.jlong) (*clientCall, error) {
	// NOTE(spetrovic): In the long-term, we will decode JSON arguments into an
	// array of vom.Value instances and send this array across the wire.

	// Convert Java argument array into []string.
	argStrs := make([]string, int(C.GetArrayLength(env, C.jarray(jArgs))))
	for i := 0; i < len(argStrs); i++ {
		argStrs[i] = goString(env, C.jstring(C.GetObjectArrayElement(env, jArgs, C.jsize(i))))
	}
	// Get argument instances that correspond to the provided method.
	getter := newArgGetter(goString(env, jPath))
	if getter == nil {
		return nil, fmt.Errorf("couldn't find IDL interface corresponding to path %q", goString(env, jPath))
	}
	argptrs, err := getter.GetInArgPtrs(method, len(argStrs))
	if len(argptrs) != len(argStrs) {
		return nil, fmt.Errorf("invalid number of arguments for method %s, want %d, have %d", method, len(argStrs), len(argptrs))
	}
	// JSON decode.
	args := make([]interface{}, len(argptrs))
	for i, argStr := range argStrs {
		if err := json.Unmarshal([]byte(argStr), argptrs[i]); err != nil {
			return nil, err
		}
		// Remove the pointer from the argument.  Simply *argptr[i] doesn't work
		// as argptr[i] is of type interface{}.
		args[i] = derefOrDie(argptrs[i])
	}
	// Process options.
	options := []ipc.CallOpt{}
	if int(jTimeout) >= 0 {
		options = append(options, veyron2.CallTimeout(time.Duration(int(jTimeout))*time.Millisecond))
	}
	// Invoke StartCall
	call, err := c.client.StartCall(name, method, args, options...)
	if err != nil {
		return nil, err
	}
	resultptrs, err := getter.GetOutArgPtrs(method, len(argStrs))
	if err != nil {
		return nil, err
	}
	return &clientCall{
		call:       call,
		resultptrs: resultptrs,
	}, nil
}

func (c *client) Close() {
	c.client.Close()
}

type clientCall struct {
	call       ipc.Call
	resultptrs []interface{}
}

func (c *clientCall) Finish(env *C.JNIEnv) (C.jobjectArray, error) {
	// argGetter doesn't store the (mandatory) error result, so we add it here.
	var appErr error
	if err := c.call.Finish(append(c.resultptrs, &appErr)...); err != nil {
		// invocation error
		return nil, fmt.Errorf("Invocation error: %v", err)
	}
	if appErr != nil { // application error
		return nil, appErr
	}

	// JSON encode results.
	jsonResults := make([][]byte, len(c.resultptrs))
	for i, resultptr := range c.resultptrs {
		// Remove the pointer from the result.  Simply *resultptr doesn't work
		// as resultptr is of type interface{}.
		result := derefOrDie(resultptr)
		var err error
		jsonResults[i], err = json.Marshal(result)
		if err != nil {
			return nil, fmt.Errorf("error marshalling %q into JSON", resultptr)
		}
	}

	// Convert to Java array of C.jstring.
	ret := C.NewObjectArray(env, C.jsize(len(jsonResults)), jStringClass, nil)
	for i, result := range jsonResults {
		C.SetObjectArrayElement(env, ret, C.jsize(i), C.jobject(jString(env, string(result))))
	}
	return ret, nil
}

func (c *clientCall) Cancel() {
	c.call.Cancel()
}
