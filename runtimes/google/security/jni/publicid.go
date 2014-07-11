// +build android

package jni

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"fmt"
	"math/big"
	"runtime"

	"veyron/runtimes/google/jni/util"
	"veyron2/security"
)

// #cgo LDFLAGS: -ljniwrapper
// #include "jni_wrapper.h"
//
// // CGO doesn't support variadic functions so we have to hard-code these
// // functions to match the invoking code. Ugh!
// static jobjectArray CallPublicIDNamesMethod(JNIEnv* env, jobject obj, jmethodID id) {
// 	return (jobjectArray)(*env)->CallObjectMethod(env, obj, id);
// }
// static jboolean CallPublicIDMatchMethod(JNIEnv* env, jobject obj, jmethodID id, jstring pattern) {
// 	return (*env)->CallBooleanMethod(env, obj, id, pattern);
// }
// static jobject CallPublicIDPublicKeyMethod(JNIEnv* env, jobject obj, jmethodID id) {
// 	return (*env)->CallObjectMethod(env, obj, id);
// }
// static jobject CallPublicIDAuthorizeMethod(JNIEnv* env, jobject obj, jmethodID id, jobject context) {
// 	return (*env)->CallObjectMethod(env, obj, id, context);
// }
// static jobjectArray CallPublicIDThirdPartyCaveatsMethod(JNIEnv* env, jobject obj, jmethodID id) {
// 	return (jobjectArray)(*env)->CallObjectMethod(env, obj, id);
// }
// static jobject CallPublicIDNewContextObject(JNIEnv* env, jclass class, jmethodID id, jlong goContextPtr) {
// 	return (*env)->NewObject(env, class, id, goContextPtr);
// }
// static jobject CallPublicIDGetKeyInfoMethod(JNIEnv* env, jclass class, jmethodID id, jobject key) {
// 	return (*env)->CallStaticObjectMethod(env, class, id, key);
// }
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
	mid := C.jmethodID(util.JMethodIDPtr(env, C.GetObjectClass(env, id.jPublicID), "names", fmt.Sprintf("()[%s", util.StringSign)))
	names := C.CallPublicIDNamesMethod(env, id.jPublicID, mid)
	ret := make([]string, int(C.GetArrayLength(env, C.jarray(names))))
	for i := 0; i < len(ret); i++ {
		ret[i] = util.GoString(env, C.GetObjectArrayElement(env, names, C.jsize(i)))
	}
	return ret
}

func (id *publicID) Match(pattern security.PrincipalPattern) bool {
	var env *C.JNIEnv
	C.AttachCurrentThread(id.jVM, &env, nil)
	defer C.DetachCurrentThread(id.jVM)
	mid := C.jmethodID(util.JMethodIDPtr(env, C.GetObjectClass(env, id.jPublicID), "match", fmt.Sprintf("(%s)%s", util.StringSign, util.BoolSign)))
	return C.CallPublicIDMatchMethod(env, id.jPublicID, mid, C.jstring(util.JStringPtr(env, string(pattern)))) == C.JNI_TRUE
}

func (id *publicID) PublicKey() *ecdsa.PublicKey {
	var env *C.JNIEnv
	C.AttachCurrentThread(id.jVM, &env, nil)
	defer C.DetachCurrentThread(id.jVM)
	mid := C.jmethodID(util.JMethodIDPtr(env, C.GetObjectClass(env, id.jPublicID), "publicKey", fmt.Sprintf("()%s", util.ObjectSign)))
	jPublicKey := C.CallPublicIDPublicKeyMethod(env, id.jPublicID, mid)
	return newPublicKey(env, jPublicKey)
}

func (id *publicID) Authorize(context security.Context) (security.PublicID, error) {
	var env *C.JNIEnv
	C.AttachCurrentThread(id.jVM, &env, nil)
	defer C.DetachCurrentThread(id.jVM)
	util.GoRef(&context) // un-refed when the Java Context object is finalized.
	contextSign := "Lcom/veyron2/security/Context;"
	publicIDSign := "Lcom/veyron2/security/PublicID;"
	cid := C.jmethodID(util.JMethodIDPtr(env, jContextImplClass, "<init>", fmt.Sprintf("(%s)%s", util.LongSign, util.VoidSign)))
	jContext := C.CallPublicIDNewContextObject(env, jContextImplClass, cid, C.jlong(util.PtrValue(&context)))
	mid := C.jmethodID(util.JMethodIDPtr(env, C.GetObjectClass(env, id.jPublicID), "authorize", fmt.Sprintf("(%s)%s", contextSign, publicIDSign)))
	jPublicID := C.CallPublicIDAuthorizeMethod(env, id.jPublicID, mid, jContext)
	if err := util.JExceptionMsg(env); err != nil {
		return nil, err
	}
	return newPublicID(env, jPublicID), nil
}

func (id *publicID) ThirdPartyCaveats() []security.ServiceCaveat {
	var env *C.JNIEnv
	C.AttachCurrentThread(id.jVM, &env, nil)
	defer C.DetachCurrentThread(id.jVM)
	serviceCaveatSign := "Lcom/veyron2/security/ServiceCaveat;"
	mid := C.jmethodID(util.JMethodIDPtr(env, C.GetObjectClass(env, id.jPublicID), "thirdPartyCaveats", fmt.Sprintf("()[%s", serviceCaveatSign)))
	jServiceCaveats := C.CallPublicIDThirdPartyCaveatsMethod(env, id.jPublicID, mid)
	length := int(C.GetArrayLength(env, C.jarray(jServiceCaveats)))
	sCaveats := make([]security.ServiceCaveat, length)
	for i := 0; i < length; i++ {
		jServiceCaveat := C.GetObjectArrayElement(env, jServiceCaveats, C.jsize(i))
		sCaveats[i] = security.ServiceCaveat{
			Service: security.PrincipalPattern(util.JStringField(env, jServiceCaveat, "service")),
			Caveat:  newCaveat(env, jServiceCaveat),
		}
	}
	return sCaveats
}

func newPublicKey(env *C.JNIEnv, jPublicKey C.jobject) *ecdsa.PublicKey {
	keySign := "Ljava/security/interfaces/ECPublicKey;"
	keyInfoSign := "Lcom/veyron/runtimes/google/security/JNIPublicID$ECPublicKeyInfo;"
	mid := C.jmethodID(util.JMethodIDPtr(env, jPublicIDImplClass, "getKeyInfo", fmt.Sprintf("(%s)%s", keySign, keyInfoSign)))
	jKeyInfo := C.CallPublicIDGetKeyInfoMethod(env, jPublicIDImplClass, mid, jPublicKey)
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
