// +build android

package jni

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"math/big"
	"runtime"

	"veyron/runtimes/google/jni/util"
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
	var env *C.JNIEnv
	C.AttachCurrentThread(id.jVM, &env, nil)
	defer C.DetachCurrentThread(id.jVM)
	return util.CallStringArrayMethodOrCatch(env, id.jPublicID, "names", nil)
}

func (id *publicID) Match(pattern security.PrincipalPattern) bool {
	var env *C.JNIEnv
	C.AttachCurrentThread(id.jVM, &env, nil)
	defer C.DetachCurrentThread(id.jVM)
	return util.CallBooleanMethodOrCatch(env, id.jPublicID, "match", []util.Sign{util.StringSign})
}

func (id *publicID) PublicKey() *ecdsa.PublicKey {
	var env *C.JNIEnv
	C.AttachCurrentThread(id.jVM, &env, nil)
	defer C.DetachCurrentThread(id.jVM)
	jPublicKey := C.jobject(util.CallObjectMethodOrCatch(env, id.jPublicID, "publicKey", nil, util.ObjectSign))
	return newPublicKey(env, jPublicKey)
}

func (id *publicID) Authorize(context security.Context) (security.PublicID, error) {
	var env *C.JNIEnv
	C.AttachCurrentThread(id.jVM, &env, nil)
	defer C.DetachCurrentThread(id.jVM)
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
	var env *C.JNIEnv
	C.AttachCurrentThread(id.jVM, &env, nil)
	defer C.DetachCurrentThread(id.jVM)
	serviceCaveatSign := util.ClassSign("com.veyron2.security.ServiceCaveat")
	jServiceCaveats := util.CallObjectArrayMethodOrCatch(env, id.jPublicID, "thirdPartyCaveats", nil, util.ArraySign(serviceCaveatSign))
	sCaveats := make([]security.ServiceCaveat, len(jServiceCaveats))
	for i, jcaveat := range jServiceCaveats {
		sCaveats[i] = security.ServiceCaveat{
			Service: security.PrincipalPattern(util.JStringField(env, C.jobject(jcaveat), "service")),
			Caveat:  newCaveat(env, C.jobject(jcaveat)),
		}
	}
	return sCaveats
}

func newPublicKey(env *C.JNIEnv, jPublicKey C.jobject) *ecdsa.PublicKey {
	keySign := util.ClassSign("java.security.interfaces.ECPublicKey")
	keyInfoSign := util.ClassSign("com.veyron.runtimes.google.security.PublicID$ECPublicKeyInfo")
	jKeyInfo := C.jobject(util.CallStaticObjectMethodOrCatch(env, jPublicIDImplClass, "getKeyInfo", []util.Sign{keySign}, keyInfoSign, jPublicKey))
	keyX := new(big.Int).SetBytes(util.JByteArrayField(env, jKeyInfo, "keyX"))
	keyY := new(big.Int).SetBytes(util.JByteArrayField(env, jKeyInfo, "keyY"))
	var curve elliptic.Curve
	switch util.JIntField(env, jKeyInfo, "curveFieldBitSize") {
	case 224:
		curve = elliptic.P224()
	case 256:
		curve = elliptic.P256()
	case 384:
		curve = elliptic.P384()
	case 521:
		curve = elliptic.P521()
	default: // Unknown curve
		return nil
	}
	return &ecdsa.PublicKey{
		Curve: curve,
		X:     keyX,
		Y:     keyY,
	}
}
