// +build android

package security

import (
	"crypto/ecdsa"
	"runtime"

	"veyron/jni/runtimes/google/util"
	"veyron2/security"
)

// #cgo LDFLAGS: -ljniwrapper
// #include "jni_wrapper.h"
import "C"

func newPublicID(env *C.JNIEnv, jPublicID C.jobject) *publicID {
	// We cannot cache Java environments as they are only valid in the current
	// thread.  We can, however, cache the Java VM and obtain an environment
	// from it in whatever thread happens to be running at the time.
	var jVM *C.JavaVM
	if status := C.GetJavaVM(env, &jVM); status != 0 {
		panic("couldn't get Java VM from the (Java) environment")
	}
	// Reference Java public id; it will be de-referenced when the go public id
	// created below is garbage-collected (through the finalizer callback we
	// setup just below).
	jPublicID = C.NewGlobalRef(env, jPublicID)
	id := &publicID{
		jVM:       jVM,
		jPublicID: jPublicID,
	}
	runtime.SetFinalizer(id, func(id *publicID) {
		var env *C.JNIEnv
		C.AttachCurrentThread(id.jVM, &env, nil)
		defer C.DetachCurrentThread(id.jVM)
		C.DeleteGlobalRef(env, id.jPublicID)
	})
	return id
}

type publicID struct {
	jVM       *C.JavaVM
	jPublicID C.jobject
}

func (id *publicID) Names() []string {
	envPtr, freeFunc := util.GetEnv(id.jVM)
	env := (*C.JNIEnv)(envPtr)
	defer freeFunc()
	return util.CallStringArrayMethodOrCatch(env, id.jPublicID, "names", nil)
}

func (id *publicID) PublicKey() *ecdsa.PublicKey {
	envPtr, freeFunc := util.GetEnv(id.jVM)
	env := (*C.JNIEnv)(envPtr)
	defer freeFunc()
	publicKeySign := util.ClassSign("java.security.interfaces.ECPublicKey")
	jPublicKey := C.jobject(util.CallObjectMethodOrCatch(env, id.jPublicID, "publicKey", nil, publicKeySign))
	// Get the encoded version of the public key.
	encoded := util.CallByteArrayMethodOrCatch(env, jPublicKey, "getEncoded", nil)
	key, err := parsePKIXPublicKey(encoded)
	if err != nil {
		panic("couldn't parse Java ECDSA public key: " + err.Error())
	}
	return key
}

func (id *publicID) Authorize(context security.Context) (security.PublicID, error) {
	envPtr, freeFunc := util.GetEnv(id.jVM)
	env := (*C.JNIEnv)(envPtr)
	defer freeFunc()
	jContext := newJavaContext(env, context)
	contextSign := util.ClassSign("com.veyron2.security.Context")
	publicIDSign := util.ClassSign("com.veyron2.security.PublicID")
	jPublicID, err := util.CallObjectMethod(env, id.jPublicID, "authorize", []util.Sign{contextSign}, publicIDSign, jContext)
	if err != nil {
		return nil, err
	}
	return newPublicID(env, C.jobject(jPublicID)), nil
}

func (id *publicID) ThirdPartyCaveats() []security.ServiceCaveat {
	envPtr, freeFunc := util.GetEnv(id.jVM)
	env := (*C.JNIEnv)(envPtr)
	defer freeFunc()
	serviceCaveatSign := util.ClassSign("com.veyron2.security.ServiceCaveat")
	jServiceCaveats := util.CallObjectArrayMethodOrCatch(env, id.jPublicID, "thirdPartyCaveats", nil, util.ArraySign(serviceCaveatSign))
	sCaveats := make([]security.ServiceCaveat, len(jServiceCaveats))
	for i, jcaveat := range jServiceCaveats {
		sCaveats[i] = security.ServiceCaveat{
			Service: security.BlessingPattern(util.JStringField(env, C.jobject(jcaveat), "service")),
			Caveat:  newCaveat(env, C.jobject(jcaveat)),
		}
	}
	return sCaveats
}
