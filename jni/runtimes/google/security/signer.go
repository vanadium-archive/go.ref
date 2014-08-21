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

// newSigner creates an instance of security.Signer that uses the provided
// Java Signer as its underlying implementation.
// NOTE: Because CGO creates package-local types and because this method may be
// invoked from a different package, Java types are passed in an empty interface
// and then cast into their package local types.
func newSigner(env *C.JNIEnv, jSigner C.jobject) security.Signer {
	// We cannot cache Java environments as they are only valid in the current
	// thread.  We can, however, cache the Java VM and obtain an environment
	// from it in whatever thread happens to be running at the time.
	var jVM *C.JavaVM
	if status := C.GetJavaVM(env, &jVM); status != 0 {
		panic("couldn't get Java VM from the (Java) environment")
	}
	// Reference Java Signer; it will be de-referenced when the Go Signer
	// created below is garbage-collected (through the finalizer callback we
	// setup just below).
	jSigner = C.NewGlobalRef(env, jSigner)
	s := &signer{
		jVM:     jVM,
		jSigner: jSigner,
	}
	runtime.SetFinalizer(s, func(s *signer) {
		envPtr, freeFunc := util.GetEnv(s.jVM)
		env := (*C.JNIEnv)(envPtr)
		defer freeFunc()
		C.DeleteGlobalRef(env, s.jSigner)
	})
	return s
}

type signer struct {
	jVM     *C.JavaVM
	jSigner C.jobject
}

func (s *signer) Sign(message []byte) (security.Signature, error) {
	envPtr, freeFunc := util.GetEnv(s.jVM)
	env := (*C.JNIEnv)(envPtr)
	defer freeFunc()
	signatureSign := util.ClassSign("com.veyron2.security.Signature")
	jSig, err := util.CallObjectMethod(env, s.jSigner, "sign", []util.Sign{util.ArraySign(util.ByteSign)}, signatureSign, message)
	if err != nil {
		return security.Signature{}, err
	}
	jHash := util.CallObjectMethodOrCatch(env, jSig, "getHash", nil, util.ClassSign("com.veyron2.security.Hash"))
	sig := security.Signature{
		Hash: security.Hash(util.CallStringMethodOrCatch(env, jHash, "getValue", nil)),
		R:    util.CallByteArrayMethodOrCatch(env, jSig, "getR", nil),
		S:    util.CallByteArrayMethodOrCatch(env, jSig, "getS", nil),
	}
	return sig, nil
}

func (s *signer) PublicKey() *ecdsa.PublicKey {
	envPtr, freeFunc := util.GetEnv(s.jVM)
	env := (*C.JNIEnv)(envPtr)
	defer freeFunc()
	publicKeySign := util.ClassSign("java.security.interfaces.ECPublicKey")
	jPublicKey := C.jobject(util.CallObjectMethodOrCatch(env, s.jSigner, "publicKey", nil, publicKeySign))
	// Get the encoded version of the public key.
	encoded := util.CallByteArrayMethodOrCatch(env, jPublicKey, "getEncoded", nil)
	key, err := parsePKIXPublicKey(encoded)
	if err != nil {
		panic("couldn't parse Java ECDSA public key: " + err.Error())
	}
	return key
}
