// +build android

package ipc

import (
	"encoding/json"
	"fmt"
	"runtime"

	"veyron/jni/runtimes/google/util"
	"veyron2/ipc"
	"veyron2/security"
	"veyron2/verror"
)

// #cgo LDFLAGS: -llog -ljniwrapper
// #include "jni_wrapper.h"
import "C"

func newInvoker(env *C.JNIEnv, jVM *C.JavaVM, jObj C.jobject) (*invoker, error) {
	// Create a new Java VDLInvoker object.
	tempJInvoker, err := util.NewObject(env, jVDLInvokerClass, []util.Sign{util.ObjectSign}, jObj)
	jInvoker := C.jobject(tempJInvoker)
	if err != nil {
		return nil, fmt.Errorf("error creating Java VDLInvoker object: %v", err)
	}
	// Fetch the argGetter for the object.
	jPathArray := C.jobjectArray(util.CallObjectMethodOrCatch(env, jInvoker, "getImplementedServices", nil, util.ArraySign(util.StringSign)))
	paths := util.GoStringArray(env, jPathArray)
	getter, err := newArgGetter(paths)
	if err != nil {
		return nil, err
	}
	// Reference Java invoker; it will be de-referenced when the go invoker
	// created below is garbage-collected (through the finalizer callback we
	// setup just below).
	jInvoker = C.NewGlobalRef(env, jInvoker)
	i := &invoker{
		jVM:       jVM,
		jInvoker:  jInvoker,
		argGetter: getter,
	}
	runtime.SetFinalizer(i, func(i *invoker) {
		envPtr, freeFunc := util.GetEnv(i.jVM)
		env := (*C.JNIEnv)(envPtr)
		defer freeFunc()
		C.DeleteGlobalRef(env, i.jInvoker)
	})
	return i, nil
}

type invoker struct {
	jVM       *C.JavaVM
	jInvoker  C.jobject
	argGetter *argGetter
}

func (i *invoker) Prepare(method string, numArgs int) (argptrs []interface{}, label security.Label, err error) {
	// NOTE(spetrovic): In the long-term, this method will return an array of
	// []vom.Value.  This will in turn result in VOM decoding all input
	// arguments into vom.Value objects, which we shall then de-serialize into
	// Java objects (see Invoke comments below).  This approach is blocked on
	// pending VOM encoder/decoder changes as well as Java (de)serializer.
	envPtr, freeFunc := util.GetEnv(i.jVM)
	env := (*C.JNIEnv)(envPtr)
	defer freeFunc()

	mArgs := i.argGetter.FindMethod(method, numArgs)
	if mArgs == nil {
		err = fmt.Errorf("couldn't find VDL method %q with %d args", method, numArgs)
		return
	}
	argptrs = mArgs.InPtrs()

	// Get the security label.
	labelSign := util.ClassSign("com.veyron2.security.Label")
	jLabel, err := util.CallObjectMethod(env, i.jInvoker, "getSecurityLabel", []util.Sign{util.StringSign}, labelSign, util.CamelCase(method))
	if err != nil {
		return nil, security.Label(0), err
	}
	label = security.Label(util.JIntField(env, jLabel, "value"))
	return
}

func (i *invoker) Invoke(method string, call ipc.ServerCall, argptrs []interface{}) (results []interface{}, err error) {
	// NOTE(spetrovic): In the long-term, all input arguments will be of
	// vom.Value type (see comments for Prepare() method above).  Through JNI,
	// we will call Java functions that transform a serialized vom.Value into
	// Java objects. We will then pass those Java objects to Java's Invoke
	// method.  The returned Java objects will be converted into serialized
	// vom.Values, which will then be returned.  This approach is blocked on VOM
	// encoder/decoder changes as well as Java's (de)serializer.
	envPtr, freeFunc := util.GetEnv(i.jVM)
	env := (*C.JNIEnv)(envPtr)
	defer freeFunc()

	// Create a new Java server call instance.
	mArgs := i.argGetter.FindMethod(method, len(argptrs))
	if mArgs == nil {
		err = fmt.Errorf("couldn't find VDL method %q with %d args", method, len(argptrs))
	}
	sCall := newServerCall(call, mArgs)
	jServerCall := C.jobject(util.NewObjectOrCatch(env, jServerCallClass, []util.Sign{util.LongSign}, sCall))
	util.GoRef(sCall) // unref-ed when jServerCall is garbage-collected

	// Translate input args to JSON.
	jArgs, err := i.encodeArgs(env, argptrs)
	if err != nil {
		return
	}
	// Invoke the method.
	callSign := util.ClassSign("com.veyron2.ipc.ServerCall")
	replySign := util.ClassSign("com.veyron.runtimes.google.VDLInvoker$InvokeReply")
	jReply, err := util.CallObjectMethod(env, i.jInvoker, "invoke", []util.Sign{util.StringSign, callSign, util.ArraySign(util.StringSign)}, replySign, util.CamelCase(method), jServerCall, jArgs)
	if err != nil {
		return nil, fmt.Errorf("error invoking Java method %q: %v", method, err)
	}
	// Decode and return results.
	return i.decodeResults(env, method, len(argptrs), C.jobject(jReply))
}

// encodeArgs JSON-encodes the provided argument pointers, converts them into
// Java strings, and returns a Java string array response.
func (*invoker) encodeArgs(env *C.JNIEnv, argptrs []interface{}) (C.jobjectArray, error) {
	// JSON encode.
	jsonArgs := make([][]byte, len(argptrs))
	for i, argptr := range argptrs {
		// Remove the pointer from the argument.  Simply *argptr doesn't work
		// as argptr is of type interface{}.
		arg := util.DerefOrDie(argptr)
		var err error
		jsonArgs[i], err = json.Marshal(arg)
		if err != nil {
			return nil, fmt.Errorf("error marshalling %q into JSON", arg)
		}
	}

	// Convert to Java array of C.jstring.
	ret := C.NewObjectArray(env, C.jsize(len(argptrs)), jStringClass, nil)
	for i, arg := range jsonArgs {
		C.SetObjectArrayElement(env, ret, C.jsize(i), C.jobject(util.JStringPtr(env, string(arg))))
	}
	return ret, nil
}

// decodeResults JSON-decodes replies stored in the Java reply object and
// returns an array of Go reply objects.
func (i *invoker) decodeResults(env *C.JNIEnv, method string, numArgs int, jReply C.jobject) ([]interface{}, error) {
	// Unpack the replies.
	results := util.JStringArrayField(env, jReply, "results")
	hasAppErr := util.JBoolField(env, jReply, "hasApplicationError")
	errorID := util.JStringField(env, jReply, "errorID")
	errorMsg := util.JStringField(env, jReply, "errorMsg")

	// Get result instances.
	mArgs := i.argGetter.FindMethod(method, numArgs)
	if mArgs == nil {
		return nil, fmt.Errorf("couldn't find method %q with %d input args: %v", method, numArgs)
	}
	argptrs := mArgs.OutPtrs()

	// Check for app error.
	if hasAppErr {
		return resultsWithError(argptrs, verror.Make(verror.ID(errorID), errorMsg)), nil
	}
	// JSON-decode.
	if len(results) != len(argptrs) {
		return nil, fmt.Errorf("mismatch in number of output arguments, have: %d want: %d", len(results), len(argptrs))
	}
	for i, result := range results {
		if err := json.Unmarshal([]byte(result), argptrs[i]); err != nil {
			return nil, err
		}
	}
	return resultsWithError(argptrs, nil), nil
}

// resultsWithError dereferences the provided result pointers and appends the
// given error to the returned array.
func resultsWithError(resultptrs []interface{}, err error) []interface{} {
	ret := make([]interface{}, len(resultptrs)+1)
	for i, resultptr := range resultptrs {
		ret[i] = util.DerefOrDie(resultptr)
	}
	ret[len(resultptrs)] = err
	return ret
}
