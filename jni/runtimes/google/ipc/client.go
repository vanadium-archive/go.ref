// +build android

package ipc

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"veyron/jni/runtimes/google/util"
	"veyron2/ipc"
	"veyron2/rt"
)

// #cgo LDFLAGS: -ljniwrapper
// #include <stdlib.h>
// #include "jni_wrapper.h"
import "C"

type client struct {
	client ipc.Client
}

func newClient(c ipc.Client) *client {
	return &client{
		client: c,
	}
}

// TODO(mattr): Remove the jTimeout param. after we move deadlines to contexts on java.
func (c *client) StartCall(env *C.JNIEnv, jContext C.jobject, name, method string, jArgs C.jobjectArray, jPath C.jstring, jTimeout C.jlong) (*clientCall, error) {
	// NOTE(spetrovic): In the long-term, we will decode JSON arguments into an
	// array of vom.Value instances and send this array across the wire.

	// Convert Java argument array into []string.
	argStrs := make([]string, int(C.GetArrayLength(env, C.jarray(jArgs))))
	for i := 0; i < len(argStrs); i++ {
		argStrs[i] = util.GoString(env, C.GetObjectArrayElement(env, jArgs, C.jsize(i)))
	}
	// Get argument instances that correspond to the provided method.
	vdlPackagePath := strings.Join(strings.Split(util.GoString(env, jPath), ".")[1:], "/")
	getter, err := newArgGetter([]string{vdlPackagePath})
	if err != nil {
		return nil, err
	}
	mArgs := getter.FindMethod(method, len(argStrs))
	if mArgs == nil {
		return nil, fmt.Errorf("couldn't find method %s with %d args in VDL interface at path %q", method, len(argStrs), util.GoString(env, jPath))
	}
	argptrs := mArgs.InPtrs()
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
		args[i] = util.DerefOrDie(argptrs[i])
	}

	// TODO(mattr): It's not clear what needs to be done with the result of newContext.
	// We should be getting access to the veyron context object perhaps maintained inside
	// jContext somehow.  For now I'll create a new context with a deadline derived from
	// jTimeout.  Eventually we should remove jTimeout altogether.
	_, err = newContext(env, jContext)
	if err != nil {
		return nil, err
	}
	context, _ := rt.R().NewContext().WithTimeout(time.Duration(int(jTimeout)) * time.Millisecond)

	// Invoke StartCall
	call, err := c.client.StartCall(context, name, method, args)
	if err != nil {
		return nil, err
	}
	return &clientCall{
		stream: newStream(call, mArgs),
		call:   call,
	}, nil
}

func (c *client) Close() {
	c.client.Close()
}

type clientCall struct {
	stream
	call ipc.Call
}

func (c *clientCall) Finish(env *C.JNIEnv) (C.jobjectArray, error) {
	var resultptrs []interface{}
	if c.mArgs.IsStreaming() {
		resultptrs = c.mArgs.StreamFinishPtrs()
	} else {
		resultptrs = c.mArgs.OutPtrs()
	}
	// argGetter doesn't store the (mandatory) error result, so we add it here.
	var appErr error
	if err := c.call.Finish(append(resultptrs, &appErr)...); err != nil {
		// invocation error
		return nil, fmt.Errorf("Invocation error: %v", err)
	}
	if appErr != nil { // application error
		return nil, appErr
	}
	// JSON encode the results.
	jsonResults := make([][]byte, len(resultptrs))
	for i, resultptr := range resultptrs {
		// Remove the pointer from the result.  Simply *resultptr doesn't work
		// as resultptr is of type interface{}.
		result := util.DerefOrDie(resultptr)
		var err error
		jsonResults[i], err = json.Marshal(result)
		if err != nil {
			return nil, fmt.Errorf("error marshalling %q into JSON", resultptr)
		}
	}

	// Convert to Java array of C.jstring.
	ret := C.NewObjectArray(env, C.jsize(len(jsonResults)), jStringClass, nil)
	for i, result := range jsonResults {
		C.SetObjectArrayElement(env, ret, C.jsize(i), C.jobject(util.JStringPtr(env, string(result))))
	}
	return ret, nil
}

func (c *clientCall) Cancel() {
	c.call.Cancel()
}
