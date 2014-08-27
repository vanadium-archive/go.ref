// +build android

package security

import (
	"veyron/jni/runtimes/google/util"
	"veyron2/security"
)

// #cgo LDFLAGS: -ljniwrapper
// #include "jni_wrapper.h"
import "C"

// newServiceCaveatArray converts a Java ServiceCaveat array into a Go ServiceCaveat array.
func newServiceCaveatArray(env *C.JNIEnv, jServiceCaveats C.jobjectArray) []security.ServiceCaveat {
	length := int(C.GetArrayLength(env, C.jarray(jServiceCaveats)))
	sCaveats := make([]security.ServiceCaveat, length)
	for i := 0; i < length; i++ {
		jServiceCaveat := C.GetObjectArrayElement(env, jServiceCaveats, C.jsize(i))
		jBlessingPattern := C.jobject(util.CallObjectMethodOrCatch(env, jServiceCaveat, "getServices", nil, util.ClassSign("com.veyron2.security.BlessingPattern")))
		services := util.CallStringMethodOrCatch(env, jBlessingPattern, "getValue", nil)
		jCaveat := C.jobject(util.CallObjectMethodOrCatch(env, jServiceCaveat, "getCaveat", nil, util.ClassSign("com.veyron2.security.Caveat")))
		// TODO(spetrovic): we get native pointer for PublicID and it works because the plan is for
		// PublicID to be an interface with only a few implementations in veyron2: folks aren't
		// supposed to create their own PublicID implementations.  Caveat is different in that
		// people are supposed to create their own Caveat implementation and the servers are
		// supposed to be able to understand these custom Caveats.  So we really won't have
		// access to native caveats here, which means we have to provide a way to Vom-Encode/Decode
		// them from Java and then un-serialize them into a Java object.
		caveatPtr := util.CallLongMethodOrCatch(env, jCaveat, "getNativePtr", nil)
		caveat := (*(*security.Caveat)(util.Ptr(caveatPtr)))
		sCaveats[i] = security.ServiceCaveat{
			Service: security.BlessingPattern(services),
			Caveat:  caveat,
		}
	}
	return sCaveats
}

// newJavaServiceCaveatArray converts a Go ServiceCaveat array into a Java ServiceCaveat array.
func newJavaServiceCaveatArray(env *C.JNIEnv, sCaveats []security.ServiceCaveat) C.jobjectArray {
	jServiceCaveats := C.NewObjectArray(env, C.jsize(len(sCaveats)), jServiceCaveatClass, nil)
	for i, sCaveat := range sCaveats {
		caveat := sCaveat.Caveat
		util.GoRef(&caveat) // Un-refed when the Java Caveat object is finalized.
		jCaveat := C.jobject(util.NewObjectOrCatch(env, jCaveatImplClass, []util.Sign{util.LongSign}, &caveat))
		services := string(sCaveat.Service)
		jPattern := C.jobject(util.NewObjectOrCatch(env, jBlessingPatternClass, []util.Sign{util.StringSign}, services))
		patternSign := util.ClassSign("com.veyron2.security.BlessingPattern")
		caveatSign := util.ClassSign("com.veyron2.security.Caveat")
		jServiceCaveat := C.jobject(util.NewObjectOrCatch(env, jServiceCaveatClass, []util.Sign{patternSign, caveatSign}, jPattern, jCaveat))
		C.SetObjectArrayElement(env, jServiceCaveats, C.jsize(i), jServiceCaveat)
	}
	return jServiceCaveats
}
