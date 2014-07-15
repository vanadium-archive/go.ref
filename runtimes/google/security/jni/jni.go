// +build android

package jni

import (
	"encoding/asn1"
	"fmt"
	"time"
	"unsafe"

	"veyron/runtimes/google/jni/util"
	isecurity "veyron/runtimes/google/security"
	"veyron2/security"
)

// #cgo LDFLAGS: -ljniwrapper
// #include "jni_wrapper.h"
//
// // CGO doesn't support variadic functions so we have to hard-code these
// // functions to match the invoking code. Ugh!
// static jobject CallNewECPublicKeyInfoObject(JNIEnv* env, jclass class, jmethodID id, jbyteArray keyX, jbyteArray keyY, jbyteArray encodedKey, jint fieldBitSize) {
//   return (*env)->NewObject(env, class, id, keyX, keyY, encodedKey, fieldBitSize);
// }
// static jobject CallNewCaveatObject(JNIEnv* env, jclass class, jmethodID id, jlong nativePtr) {
//   return (*env)->NewObject(env, class, id, nativePtr);
// }
// static jobject CallNewServiceCaveatObject(JNIEnv* env, jclass class, jmethodID id, jstring service, jobject caveat) {
//   return (*env)->NewObject(env, class, id, service, caveat);
// }
import "C"

var (
	// Global reference for com.veyron.runtimes.google.security.PublicID class.
	jPublicIDImplClass C.jclass
	// Global reference for com.veyron.runtimes.google.security.PublicID$ECPublicKeyInfo class.
	jECPublicKeyInfoClass C.jclass
	// Global reference for com.veyron.runtimes.google.security.Caveat class.
	jCaveatImplClass C.jclass
	// Global reference for com.veyron.runtimes.google.security.Context class.
	jContextImplClass C.jclass
	// Global reference for com.veyron2.security.Context class.
	jContextClass C.jclass
	// Global reference for com.veyron2.security.Caveat class.
	jCaveatClass C.jclass
	// Global reference for com.veyron2.security.ServiceCaveat class.
	jServiceCaveatClass C.jclass
)

// Init initializes the JNI code with the given Java evironment. This method
// must be called from the main Java thread.
// NOTE: Because CGO creates package-local types and because this method may be
// invoked from a different package, Java environment is passed in an empty
// interface and then cast into the package-local environment type.
func Init(jEnv interface{}) {
	env := (*C.JNIEnv)(unsafe.Pointer(util.PtrValue(jEnv)))
	// Cache global references to all Java classes used by the package.  This is
	// necessary because JNI gets access to the class loader only in the system
	// thread, so we aren't able to invoke FindClass in other threads.
	jPublicIDImplClass = C.jclass(util.JFindClassPtrOrDie(env, "com/veyron/runtimes/google/security/PublicID"))
	jECPublicKeyInfoClass = C.jclass(util.JFindClassPtrOrDie(env, "com/veyron/runtimes/google/security/PublicID$ECPublicKeyInfo"))
	jCaveatImplClass = C.jclass(util.JFindClassPtrOrDie(env, "com/veyron/runtimes/google/security/Caveat"))
	jContextClass = C.jclass(util.JFindClassPtrOrDie(env, "com/veyron2/security/Context"))
	jContextImplClass = C.jclass(util.JFindClassPtrOrDie(env, "com/veyron/runtimes/google/security/Context"))
	jCaveatClass = C.jclass(util.JFindClassPtrOrDie(env, "com/veyron2/security/Caveat"))
	jServiceCaveatClass = C.jclass(util.JFindClassPtrOrDie(env, "com/veyron2/security/ServiceCaveat"))
}

//export Java_com_veyron_runtimes_google_security_PublicID_nativeNames
func Java_com_veyron_runtimes_google_security_PublicID_nativeNames(env *C.JNIEnv, jPublicID C.jobject, goPublicIDPtr C.jlong) C.jobjectArray {
	names := (*(*security.PublicID)(util.Ptr(goPublicIDPtr))).Names()
	return C.jobjectArray(util.JStringArrayPtr(env, names))
}

//export Java_com_veyron_runtimes_google_security_PublicID_nativeMatch
func Java_com_veyron_runtimes_google_security_PublicID_nativeMatch(env *C.JNIEnv, jPublicID C.jobject, goPublicIDPtr C.jlong, jPattern C.jstring) C.jboolean {
	if (*(*security.PublicID)(util.Ptr(goPublicIDPtr))).Match(security.PrincipalPattern(util.GoString(env, jPattern))) {
		return C.JNI_TRUE
	}
	return C.JNI_FALSE
}

//export Java_com_veyron_runtimes_google_security_PublicID_nativePublicKey
func Java_com_veyron_runtimes_google_security_PublicID_nativePublicKey(env *C.JNIEnv, jPublicID C.jobject, goPublicIDPtr C.jlong) C.jobject {
	key := (*(*security.PublicID)(util.Ptr(goPublicIDPtr))).PublicKey()
	encoded, err := marshalPKIXPublicKey(key)
	if err != nil {
		util.JThrowV(env, err)
		return C.jobject(nil)
	}
	cid := C.jmethodID(util.JMethodIDPtrOrDie(env, jECPublicKeyInfoClass, "<init>", fmt.Sprintf("([%s[%s[%s%s)%s", util.ByteSign, util.ByteSign, util.ByteSign, util.IntSign, util.VoidSign)))
	return C.CallNewECPublicKeyInfoObject(env, jECPublicKeyInfoClass, cid, C.jbyteArray(util.JByteArrayPtr(env, key.X.Bytes())), C.jbyteArray(util.JByteArrayPtr(env, key.Y.Bytes())), C.jbyteArray(util.JByteArrayPtr(env, encoded)), C.jint(key.Params().BitSize))
}

//export Java_com_veyron_runtimes_google_security_PublicID_nativeAuthorize
func Java_com_veyron_runtimes_google_security_PublicID_nativeAuthorize(env *C.JNIEnv, jPublicID C.jobject, goPublicIDPtr C.jlong, jContext C.jobject) C.jlong {
	id, err := (*(*security.PublicID)(util.Ptr(goPublicIDPtr))).Authorize(newContext(env, jContext))
	if err != nil {
		util.JThrowV(env, err)
		return C.jlong(0)
	}
	util.GoRef(&id) // Un-refed when the Java PublicID is finalized.
	return C.jlong(util.PtrValue(&id))
}

//export Java_com_veyron_runtimes_google_security_PublicID_nativeThirdPartyCaveats
func Java_com_veyron_runtimes_google_security_PublicID_nativeThirdPartyCaveats(env *C.JNIEnv, jPublicID C.jobject, goPublicIDPtr C.jlong) C.jobjectArray {
	sCaveats := (*(*security.PublicID)(util.Ptr(goPublicIDPtr))).ThirdPartyCaveats()
	caveatSign := "Lcom/veyron2/security/Caveat;"
	jServiceCaveats := C.NewObjectArray(env, C.jsize(len(sCaveats)), jServiceCaveatClass, nil)
	for i, sCaveat := range sCaveats {
		util.GoRef(&sCaveat) // Un-refed when the Java Caveat object is finalized.
		cid := C.jmethodID(util.JMethodIDPtrOrDie(env, jCaveatImplClass, "<init>", fmt.Sprintf("(%s)%s", util.LongSign, util.VoidSign)))
		jCaveat := C.CallNewCaveatObject(env, jCaveatImplClass, cid, C.jlong(util.PtrValue(&sCaveat)))
		scid := C.jmethodID(util.JMethodIDPtrOrDie(env, jServiceCaveatClass, "<init>", fmt.Sprintf("(%s%s)%s", util.StringSign, caveatSign, util.VoidSign)))
		jServiceCaveat := C.CallNewServiceCaveatObject(env, jServiceCaveatClass, scid, C.jstring(util.JStringPtr(env, string(sCaveat.Service))), jCaveat)
		C.SetObjectArrayElement(env, jServiceCaveats, C.jsize(i), jServiceCaveat)
	}
	return jServiceCaveats
}

//export Java_com_veyron_runtimes_google_security_PublicID_nativeFinalize
func Java_com_veyron_runtimes_google_security_PublicID_nativeFinalize(env *C.JNIEnv, jPublicID C.jobject, goPublicIDPtr C.jlong) {
	util.GoUnref((*security.PublicID)(util.Ptr(goPublicIDPtr)))
}

//export Java_com_veyron_runtimes_google_security_PrivateID_nativeCreate
func Java_com_veyron_runtimes_google_security_PrivateID_nativeCreate(env *C.JNIEnv, jPrivateIDClass C.jclass, name C.jstring) C.jlong {
	id, err := isecurity.NewPrivateID(util.GoString(env, name))
	if err != nil {
		util.JThrowV(env, err)
		return C.jlong(0)
	}
	util.GoRef(&id) // Un-refed when the Java PrivateID is finalized.
	return C.jlong(util.PtrValue(&id))
}

//export Java_com_veyron_runtimes_google_security_PrivateID_nativePublicID
func Java_com_veyron_runtimes_google_security_PrivateID_nativePublicID(env *C.JNIEnv, jPrivateID C.jobject, goPrivateIDPtr C.jlong) C.jlong {
	id := (*(*security.PrivateID)(util.Ptr(goPrivateIDPtr))).PublicID()
	util.GoRef(&id) // Un-refed when the Java PublicID is finalized.
	return C.jlong(util.PtrValue(&id))
}

//export Java_com_veyron_runtimes_google_security_PrivateID_nativeSign
func Java_com_veyron_runtimes_google_security_PrivateID_nativeSign(env *C.JNIEnv, jPrivateID C.jobject, goPrivateIDPtr C.jlong, msg C.jbyteArray) C.jbyteArray {
	s, err := (*(*security.PrivateID)(util.Ptr(goPrivateIDPtr))).Sign(util.GoByteArray(env, msg))
	if err != nil {
		util.JThrowV(env, err)
		return nil
	}
	data, err := asn1.Marshal(s)
	if err != nil {
		util.JThrowV(env, err)
		return nil
	}
	return C.jbyteArray(util.JByteArrayPtr(env, data))
}

//export Java_com_veyron_runtimes_google_security_PrivateID_nativeBless
func Java_com_veyron_runtimes_google_security_PrivateID_nativeBless(env *C.JNIEnv, jPrivateID C.jobject, goPrivateIDPtr C.jlong, jPublicID C.jobject, name C.jstring, jDurationMS C.jlong, jServiceCaveats C.jobjectArray) C.jlong {
	blessee := newPublicID(env, jPublicID)
	duration := time.Duration(jDurationMS) * time.Millisecond
	length := int(C.GetArrayLength(env, C.jarray(jServiceCaveats)))
	caveats := make([]security.ServiceCaveat, length)
	for i := 0; i < length; i++ {
		jServiceCaveat := C.GetObjectArrayElement(env, jServiceCaveats, C.jsize(i))
		caveats[i] = security.ServiceCaveat{
			Service: security.PrincipalPattern(util.JStringField(env, jServiceCaveat, "service")),
			Caveat:  newCaveat(env, jServiceCaveat),
		}
	}
	id, err := (*(*security.PrivateID)(util.Ptr(goPrivateIDPtr))).Bless(blessee, util.GoString(env, name), duration, caveats)
	if err != nil {
		util.JThrowV(env, err)
		return C.jlong(0)
	}
	util.GoRef(&id) // Un-refed when the Java PublicID is finalized
	return C.jlong(util.PtrValue(&id))
}

//export Java_com_veyron_runtimes_google_security_PrivateID_nativeDerive
func Java_com_veyron_runtimes_google_security_PrivateID_nativeDerive(env *C.JNIEnv, jPrivateID C.jobject, goPrivateIDPtr C.jlong, jPublicID C.jobject) C.jlong {
	id, err := (*(*security.PrivateID)(util.Ptr(goPrivateIDPtr))).Derive(newPublicID(env, jPublicID))
	if err != nil {
		util.JThrowV(env, err)
		return C.jlong(0)
	}
	util.GoRef(&id) // Un-refed when the Java PrivateID is finalized.
	return C.jlong(util.PtrValue(&id))
}

//export Java_com_veyron_runtimes_google_security_PrivateID_nativeFinalize
func Java_com_veyron_runtimes_google_security_PrivateID_nativeFinalize(env *C.JNIEnv, jPrivateID C.jobject, goPrivateIDPtr C.jlong) {
	util.GoUnref((*security.PrivateID)(util.Ptr(goPrivateIDPtr)))
}

//export Java_com_veyron_runtimes_google_security_Context_nativeMethod
func Java_com_veyron_runtimes_google_security_Context_nativeMethod(env *C.JNIEnv, jContext C.jobject, goContextPtr C.jlong) C.jstring {
	return C.jstring(util.JStringPtr(env, (*(*security.Context)(util.Ptr(goContextPtr))).Method()))
}

//export Java_com_veyron_runtimes_google_security_Context_nativeName
func Java_com_veyron_runtimes_google_security_Context_nativeName(env *C.JNIEnv, jServerCall C.jobject, goContextPtr C.jlong) C.jstring {
	return C.jstring(util.JStringPtr(env, (*(*security.Context)(util.Ptr(goContextPtr))).Name()))
}

//export Java_com_veyron_runtimes_google_security_Context_nativeSuffix
func Java_com_veyron_runtimes_google_security_Context_nativeSuffix(env *C.JNIEnv, jServerCall C.jobject, goContextPtr C.jlong) C.jstring {
	return C.jstring(util.JStringPtr(env, (*(*security.Context)(util.Ptr(goContextPtr))).Suffix()))
}

//export Java_com_veyron_runtimes_google_security_Context_nativeLabel
func Java_com_veyron_runtimes_google_security_Context_nativeLabel(env *C.JNIEnv, jServerCall C.jobject, goContextPtr C.jlong) C.jint {
	return C.jint((*(*security.Context)(util.Ptr(goContextPtr))).Label())
}

//export Java_com_veyron_runtimes_google_security_Context_nativeLocalID
func Java_com_veyron_runtimes_google_security_Context_nativeLocalID(env *C.JNIEnv, jServerCall C.jobject, goContextPtr C.jlong) C.jlong {
	id := (*(*security.Context)(util.Ptr(goContextPtr))).LocalID()
	util.GoRef(&id) // Un-refed when the Java PublicID object is finalized.
	return C.jlong(util.PtrValue(&id))
}

//export Java_com_veyron_runtimes_google_security_Context_nativeRemoteID
func Java_com_veyron_runtimes_google_security_Context_nativeRemoteID(env *C.JNIEnv, jServerCall C.jobject, goContextPtr C.jlong) C.jlong {
	id := (*(*security.Context)(util.Ptr(goContextPtr))).RemoteID()
	util.GoRef(&id)
	return C.jlong(util.PtrValue(&id))
}

//export Java_com_veyron_runtimes_google_security_Context_nativeLocalEndpoint
func Java_com_veyron_runtimes_google_security_Context_nativeLocalEndpoint(env *C.JNIEnv, jServerCall C.jobject, goContextPtr C.jlong) C.jstring {
	return C.jstring(util.JStringPtr(env, (*(*security.Context)(util.Ptr(goContextPtr))).LocalEndpoint().String()))
}

//export Java_com_veyron_runtimes_google_security_Context_nativeRemoteEndpoint
func Java_com_veyron_runtimes_google_security_Context_nativeRemoteEndpoint(env *C.JNIEnv, jServerCall C.jobject, goContextPtr C.jlong) C.jstring {
	return C.jstring(util.JStringPtr(env, (*(*security.Context)(util.Ptr(goContextPtr))).RemoteEndpoint().String()))
}

//export Java_com_veyron_runtimes_google_security_Context_nativeFinalize
func Java_com_veyron_runtimes_google_security_Context_nativeFinalize(env *C.JNIEnv, jServerCall C.jobject, goContextPtr C.jlong) {
	util.GoUnref((*security.Context)(util.Ptr(goContextPtr)))
}

//export Java_com_veyron_runtimes_google_security_Caveat_nativeValidate
func Java_com_veyron_runtimes_google_security_Caveat_nativeValidate(env *C.JNIEnv, jServerCall C.jobject, goCaveatPtr C.jlong, jContext C.jobject) {
	if err := (*(*security.Caveat)(util.Ptr(goCaveatPtr))).Validate(newContext(env, jContext)); err != nil {
		util.JThrowV(env, err)
	}
}

//export Java_com_veyron_runtimes_google_security_Caveat_nativeFinalize
func Java_com_veyron_runtimes_google_security_Caveat_nativeFinalize(env *C.JNIEnv, jServerCall C.jobject, goCaveatPtr C.jlong) {
	util.GoUnref((*security.Caveat)(util.Ptr(goCaveatPtr)))
}
