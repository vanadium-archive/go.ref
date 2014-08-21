// +build android

package security

import (
	"runtime"
	"unsafe"

	"veyron/jni/runtimes/google/util"
	"veyron2/security"
)

// #cgo LDFLAGS: -ljniwrapper
// #include "jni_wrapper.h"
import "C"

// NewPublicIDStore creates an instance of security.PublicIDStore that uses the
// provided Java PublicIDStore as its underlying implementation.
// NOTE: Because CGO creates package-local types and because this method may be
// invoked from a different package, Java types are passed in an empty interface
// and then cast into their package local types.
func NewPublicIDStore(jEnv, jStore interface{}) security.PublicIDStore {
	env := (*C.JNIEnv)(unsafe.Pointer(util.PtrValue(jEnv)))
	jPublicIDStore := C.jobject(unsafe.Pointer(util.PtrValue(jStore)))

	// We cannot cache Java environments as they are only valid in the current
	// thread.  We can, however, cache the Java VM and obtain an environment
	// from it in whatever thread happens to be running at the time.
	var jVM *C.JavaVM
	if status := C.GetJavaVM(env, &jVM); status != 0 {
		panic("couldn't get Java VM from the (Java) environment")
	}

	// Reference Java PublicIDStore; it will be de-referenced when the Go
	// PublicIDStore created below is garbage-collected (through the finalizer
	// callback we setup just below).
	jPublicIDStore = C.NewGlobalRef(env, jPublicIDStore)
	// Create Go PublicIDStore.
	s := &publicIDStore{
		jVM:            jVM,
		jPublicIDStore: jPublicIDStore,
	}
	runtime.SetFinalizer(s, func(s *publicIDStore) {
		envPtr, freeFunc := util.GetEnv(s.jVM)
		env := (*C.JNIEnv)(envPtr)
		defer freeFunc()
		C.DeleteGlobalRef(env, s.jPublicIDStore)
	})
	return s
}

type publicIDStore struct {
	jVM            *C.JavaVM
	jPublicIDStore C.jobject
}

func (s *publicIDStore) Add(id security.PublicID, peerPattern security.PrincipalPattern) error {
	envPtr, freeFunc := util.GetEnv(s.jVM)
	env := (*C.JNIEnv)(envPtr)
	defer freeFunc()
	util.GoRef(&id) // Un-refed when the Java PublicID object created below is finalized.
	jPublicID := C.jobject(util.NewObjectOrCatch(env, jPublicIDImplClass, []util.Sign{util.LongSign}, &id))
	jPrincipalPattern := C.jobject(util.NewObjectOrCatch(env, jPrincipalPatternClass, []util.Sign{util.StringSign}, string(peerPattern)))
	return util.CallVoidMethod(env, s.jPublicIDStore, "add", []util.Sign{publicIDSign, principalPatternSign}, jPublicID, jPrincipalPattern)
}

func (s *publicIDStore) ForPeer(peer security.PublicID) (security.PublicID, error) {
	envPtr, freeFunc := util.GetEnv(s.jVM)
	env := (*C.JNIEnv)(envPtr)
	defer freeFunc()
	util.GoRef(&peer) // Un-refed when the Java peer object created below is finalized.
	jPeer := C.jobject(util.NewObjectOrCatch(env, jPublicIDImplClass, []util.Sign{util.LongSign}, &peer))
	jPublicID, err := util.CallObjectMethod(env, s.jPublicIDStore, "forPeer", []util.Sign{publicIDSign}, publicIDSign, jPeer)
	if err != nil {
		return nil, err
	}
	publicIDPtr := util.CallLongMethodOrCatch(env, jPublicID, "getNativePtr", nil)
	return (*(*security.PublicID)(util.Ptr(publicIDPtr))), nil
}

func (s *publicIDStore) DefaultPublicID() (security.PublicID, error) {
	envPtr, freeFunc := util.GetEnv(s.jVM)
	env := (*C.JNIEnv)(envPtr)
	defer freeFunc()
	jPublicID, err := util.CallObjectMethod(env, s.jPublicIDStore, "defaultPublicID", []util.Sign{}, publicIDSign)
	if err != nil {
		return nil, err
	}
	publicIDPtr := util.CallLongMethodOrCatch(env, jPublicID, "getNativePtr", nil)
	return (*(*security.PublicID)(util.Ptr(publicIDPtr))), nil
}

func (s *publicIDStore) SetDefaultPrincipalPattern(pattern security.PrincipalPattern) error {
	envPtr, freeFunc := util.GetEnv(s.jVM)
	env := (*C.JNIEnv)(envPtr)
	defer freeFunc()
	jPattern := C.jobject(util.NewObjectOrCatch(env, jPrincipalPatternClass, []util.Sign{util.StringSign}, string(pattern)))
	return util.CallVoidMethod(env, s.jPublicIDStore, "setDefaultPrincipalPattern", []util.Sign{principalPatternSign}, jPattern)
}
