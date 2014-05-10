// +build !darwin

package main

/*
// NOTE: This example builds a shared-library that gets used in android. We use the goandroid
// binary to build this and need the Android NDK to be installed. We create a local copy of jni.h
// to get this compiling for normal builds (source: android-ndk-r9d-linux-x86_64.tar.bz2 downloaded
// from https://developer.android.com/tools/sdk/ndk/index.html).
#include "jni.h"
#include <stdlib.h>

// Use GetEnv to discover the thread's JNIEnv. JNIEnv cannot be shared
// between threads. Once we are done we need to detach the thread.
static JNIEnv* getJNIEnv(JavaVM *jvm) {
  JNIEnv *env;
  (*jvm)->GetEnv(jvm, (void**)&env, JNI_VERSION_1_6);
  (*jvm)->AttachCurrentThread(jvm, &env, NULL);
  return env;
}

// Free the thread that has been attached to the JNIEnv.
static void freeJNIEnv(JavaVM *jvm, JNIEnv* env) {
  (*jvm)->DetachCurrentThread(jvm);
}

// Conversions from JString to CString and vice-versa.
static jstring CToJString(JNIEnv *env, const char* c) {
 return (*env)->NewStringUTF(env, c);
}

static const char* JToCString(JNIEnv *env, jstring jstr) {
 return (*env)->GetStringUTFChars(env, jstr, NULL);
}

// Get the Method ID for method "name" with given args.
static jmethodID getMethodID(JNIEnv *env, jobject obj, const char* name, const char* args) {
 jclass jc = (*env)->GetObjectClass(env, obj);
 return (*env)->GetMethodID(env, jc, name, args);
}

// Calls the Java method in Android with the box description.
static void callMethod(JNIEnv *env, jobject obj, jmethodID mid, jstring boxId, jfloat points[4]) {
 (*env)->CallVoidMethod(env, obj, mid, boxId, points[0], points[1], points[2], points[3]);
}

// Gets the global reference for a given object.
static jobject newGlobalRef(JNIEnv *env, jobject obj) {
 return (*env)->NewGlobalRef(env, obj);
}

// Delete the global reference to a given object
static void freeGlobalRef(JNIEnv *env, jobject obj) {
 (*env)->DeleteGlobalRef(env, obj);
}
*/
import "C"

import (
	"fmt"
	"io"
	"net"
	"unsafe"

	"veyron/examples/boxes"

	"veyron2"
	"veyron2/ipc"
	"veyron2/naming"
	"veyron2/rt"
)

type jniState struct {
	jVM  *C.JavaVM
	jObj C.jobject
	jMID C.jmethodID
}

type veyronState struct {
	runtime    veyron2.Runtime
	drawStream boxes.DrawInterfaceDrawStream
	signalling boxes.BoxSignalling
	boxList    chan boxes.Box
}

var (
	veyron     veyronState
	nativeJava jniState
)

const (
	signallingServer  = "@1@tcp@54.200.34.44:8509@9282acd6784022b1945ee1044bddb804@@"
	signallingService = "signalling"
	drawService       = "draw"
	drawServicePort   = ":8509"
)

func (jni *jniState) registerAddBox(env *C.JNIEnv, obj C.jobject) {
	jni.jObj = C.newGlobalRef(env, obj)
	jni.jMID = C.getMethodID(env, nativeJava.jObj, C.CString("AddBox"), C.CString("(Ljava/lang/String;FFFF)V"))
}

func (jni *jniState) addBox(box *boxes.Box) {
	env := C.getJNIEnv(jni.jVM)
	defer C.freeJNIEnv(jni.jVM, env)
	// Convert box id/coordinates to java types
	var jPoints [4]C.jfloat
	for i := 0; i < 4; i++ {
		jPoints[i] = C.jfloat(box.Points[i])
	}
	jBoxId := C.CToJString(env, C.CString(box.BoxId))
	// Invoke the AddBox method in Java
	C.callMethod(env, jni.jObj, jni.jMID, jBoxId, &jPoints[0])
}

func (v *veyronState) Draw(_ ipc.Context, stream boxes.DrawInterfaceServiceDrawStream) error {
	for {
		box, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		nativeJava.addBox(&box)
	}
	return nil
}

func (v *veyronState) sendBox(box boxes.Box) {
	v.boxList <- box
}

func (v *veyronState) sendDrawLoop() {
	v.boxList = make(chan boxes.Box, 256)
	for {
		if err := v.drawStream.Send(<-v.boxList); err != nil {
			break
		}
	}
}

func (v *veyronState) registerAsPeer() {
	var registerIP string
	// Get an IP that can be published to the signal server
	ifaces, err := net.InterfaceAddrs()
	for _, addr := range ifaces {
		if ipa, ok := addr.(*net.IPNet); ok {
			if ip4 := ipa.IP.To4(); ip4 != nil && ip4.String() != "127.0.0.1" {
				registerIP = ip4.String()
				break
			}
		}
	}

	rtServer, err := veyron.runtime.NewServer()
	if err != nil {
		panic(fmt.Errorf("failed runtime.NewServer:%v\n", err))
	}
	drawServer := boxes.NewServerDrawInterface(&veyron)
	if err := rtServer.Register(drawService, ipc.SoloDispatcher(drawServer, nil)); err != nil {
		panic(fmt.Errorf("failed Register:%v\n", err))
	}
	endPt, err := rtServer.Listen("tcp", registerIP+drawServicePort)
	if err != nil {
		panic(fmt.Errorf("failed to Listen:%v\n", err))
	}
	if err := v.signalling.Add(endPt.String()); err != nil {
		panic(fmt.Errorf("failed to Add endpoint to signalling server:%v\n", err))
	}
	if err := rtServer.Publish("/" + drawService); err != nil {
		panic(fmt.Errorf("failed to Publish:%v\n", err))
	}
}

func (v *veyronState) connectPeer() {
	endPt, err := v.signalling.Get()
	if err != nil {
		panic(fmt.Errorf("failed to Get peer endpoint from signalling server:%v\n", err))
	}
	drawInterface, err := boxes.BindDrawInterface(naming.JoinAddressName(endPt, drawService))
	if err != nil {
		panic(fmt.Errorf("failed BindDrawInterface:%v\n", err))
	}
	if v.drawStream, err = drawInterface.Draw(); err != nil {
		panic(fmt.Errorf("failed to get handle to Draw stream:%v\n", err))
	}
	go v.sendDrawLoop()
}

//export JNI_OnLoad
func JNI_OnLoad(vm *C.JavaVM, reserved unsafe.Pointer) C.jint {
	nativeJava.jVM = vm
	return C.JNI_VERSION_1_6
}

//export Java_com_boxes_GoNative_registerAsPeer
func Java_com_boxes_GoNative_registerAsPeer(env *C.JNIEnv) {
	veyron.registerAsPeer()
}

//export Java_com_boxes_GoNative_connectPeer
func Java_com_boxes_GoNative_connectPeer(env *C.JNIEnv) {
	veyron.connectPeer()
}

//export Java_com_boxes_GoNative_sendDrawBox
func Java_com_boxes_GoNative_sendDrawBox(env *C.JNIEnv, class C.jclass,
	boxId C.jstring, ox C.jfloat, oy C.jfloat, cx C.jfloat, cy C.jfloat) {
	veyron.sendBox(boxes.Box{BoxId: C.GoString(C.JToCString(env, boxId)), Points: [4]float32{float32(ox), float32(oy), float32(cx), float32(cy)}})
}

//export Java_com_boxes_GoNative_registerAddBox
func Java_com_boxes_GoNative_registerAddBox(env *C.JNIEnv, thisObj C.jobject, obj C.jobject) {
	nativeJava.registerAddBox(env, obj)
}

func main() {
	veyron.runtime = rt.Init()
	var err error
	if veyron.signalling, err = boxes.BindBoxSignalling(naming.JoinAddressName(signallingServer, signallingService)); err != nil {
		panic(fmt.Errorf("failed to bind to signalling server:%v\n", err))
	}
}
