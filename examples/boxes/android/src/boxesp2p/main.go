// TODO(kash): Rewrite this to use the new dir/object store api.
// +build ignore

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
	"bytes"
	"fmt"
	"io"
	"net"
	"os"
	"runtime"
	"strings"
	"unsafe"

	"veyron/examples/boxes"
	inaming "veyron/runtimes/google/naming"
	vsync "veyron/runtimes/google/vsync"
	vsecurity "veyron/security"
	sstore "veyron/services/store/server"

	"veyron2"
	"veyron2/context"
	"veyron2/ipc"
	"veyron2/naming"
	"veyron2/rt"
	"veyron2/security"
	"veyron2/services/watch/types"
	"veyron2/storage"
	"veyron2/storage/vstore"
	"veyron2/vom"
)

type jniState struct {
	jVM  *C.JavaVM
	jObj C.jobject
	jMID C.jmethodID
}

type boxesDispatcher struct {
	drawAuth, syncAuth     security.Authorizer
	drawServer, syncServer ipc.Invoker
	storeDispatcher        ipc.Dispatcher
}

type goState struct {
	runtime       veyron2.Runtime
	ipc           ipc.Server
	disp          boxesDispatcher
	drawStream    boxes.DrawInterfaceServiceDrawStream
	signalling    boxes.BoxSignalling
	boxList       chan boxes.Box
	myIPAddr      string
	storeEndpoint string
}

var (
	gs         goState
	nativeJava jniState
)

const (
	drawServicePort  = ":8509"
	storeServicePort = ":8000"
	syncServicePort  = ":8001"
	storePath        = "/data/data/com.boxes.android.draganddraw/files/vsync"
	storeDatabase    = "veyron_store.db"
	useStoreService  = true
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

func (d *boxesDispatcher) Lookup(suffix, method string) (ipc.Invoker, security.Authorizer, error) {
	if strings.HasSuffix(suffix, "draw") {
		return d.drawServer, d.drawAuth, nil
	}
	if strings.HasSuffix(suffix, "sync") {
		return d.syncServer, d.syncAuth, nil
	}
	return d.storeDispatcher.Lookup(suffix, method)
}

func (gs *goState) SyncBoxes(context ipc.ServerContext) error {
	// Get the endpoint of the remote process
	endPt, err := inaming.NewEndpoint(context.RemoteEndpoint().String())
	if err != nil {
		return err
	}
	// Launch the sync service
	initSyncService(endPt.Addr().String())
	// Watch/Update the store for any box changes
	go gs.monitorStore()
	return nil
}

func (gs *goState) Draw(context ipc.ServerContext, stream boxes.DrawInterfaceServiceDrawStream) error {
	gs.drawStream = stream
	gs.streamBoxesLoop()
	return nil
}

func (gs *goState) sendBox(box boxes.Box) {
	gs.boxList <- box
}

func (gs *goState) streamBoxesLoop() {
	// Loop to receive boxes from remote peer
	go func() {
		rStream := gs.drawStream.RecvStream()
		for rStream.Advance() {
			box := rStream.Value()
			nativeJava.addBox(&box)
		}
	}()
	// Loop to send boxes to remote peer
	sender := gs.drawStream.SendStream()
	for {
		if err := sender.Send(<-gs.boxList); err != nil {
			break
		}
	}
}

func (gs *goState) monitorStore() {
	ctx := gs.runtime.NewContext()

	root := vstore.New().Bind(gs.storeEndpoint)
	if _, err := root.Put(ctx, ""); err != nil {
		panic(fmt.Errorf("Put for / failed:%v", err))
	}

	// Watch for any box updates from the store
	go func() {
		req := types.GlobRequest{Pattern: "*"}
		stream, err := root.WatchGlob(ctx, req)
		if err != nil {
			panic(fmt.Errorf("Can't watch store: %s: %s", gs.storeEndpoint, err))
		}
		rStream := stream.RecvStream()
		for rStream.Advance() {
			change := rStream.Value()
			if entry, ok := change.Value.(*storage.Entry); ok {
				if box, ok := entry.Value.(boxes.Box); ok && box.DeviceId != gs.myIPAddr {
					nativeJava.addBox(&box)
				}
			}
		}

		err = rStream.Err()
		if err == nil {
			err = io.EOF
		}
		panic(fmt.Errorf("Can't receive watch event: %s: %s", gs.storeEndpoint, err))
	}()

	// Send any box updates to the store
	for {
		box := <-gs.boxList
		if _, err := root.Bind(box.BoxId).Put(ctx, box); err != nil {
			panic(fmt.Errorf("Put for %s failed:%v", box.BoxId, err))
		}
	}
}

func (gs *goState) registerAsPeer(ctx context.T) {
	auth := vsecurity.NewACLAuthorizer(security.ACL{In: map[security.BlessingPattern]security.LabelSet{
		security.AllPrincipals: security.LabelSet(security.AdminLabel),
	}})
	gs.disp.drawAuth = auth
	gs.disp.drawServer = ipc.ReflectInvoker(boxes.NewServerDrawInterface(gs))
	endPt, err := gs.ipc.Listen("tcp", gs.myIPAddr+drawServicePort)
	if err != nil {
		panic(fmt.Errorf("Failed to Listen:%v", err))
	}
	if err := gs.ipc.Serve("", &gs.disp); err != nil {
		panic(fmt.Errorf("Failed Register:%v", err))
	}
	if err := gs.signalling.Add(ctx, endPt.String()); err != nil {
		panic(fmt.Errorf("Failed to Add endpoint to signalling server:%v", err))
	}
}

// wrapper is an object that modifies the signature of DrawInterfaceDrawCall
// to match DrawInterfaceServiceDrawStream.  This needs to happen because the
// anonymous interface returned by SendStream in DrawInterfaceDrawCall has
// an extra method (Close) that we need to remove from the function signataure.
type wrapper struct {
	boxes.DrawInterfaceDrawCall
}

func (w *wrapper) SendStream() interface {
	Send(b boxes.Box) error
} {
	return w.DrawInterfaceDrawCall.SendStream()
}
func (gs *goState) connectPeer(ctx context.T) {
	endpointStr, err := gs.signalling.Get(ctx)
	if err != nil {
		panic(fmt.Errorf("failed to Get peer endpoint from signalling server:%v", err))
	}
	drawInterface, err := boxes.BindDrawInterface(naming.JoinAddressName(endpointStr, "draw"))
	if err != nil {
		panic(fmt.Errorf("failed BindDrawInterface:%v", err))
	}
	if !useStoreService {
		val, err := drawInterface.Draw(ctx)
		if err != nil {
			panic(fmt.Errorf("failed to get handle to Draw stream:%v\n", err))
		}
		gs.drawStream = &wrapper{val}
		go gs.streamBoxesLoop()
	} else {
		// Initialize the store sync service that listens for updates from a peer
		endpoint, err := inaming.NewEndpoint(endpointStr)
		if err != nil {
			panic(fmt.Errorf("failed to parse endpoint:%v", err))
		}
		if err = drawInterface.SyncBoxes(ctx); err != nil {
			panic(fmt.Errorf("failed to setup remote sync service:%v", err))
		}
		initSyncService(endpoint.Addr().String())
		go gs.monitorStore()
	}
}

//export JNI_OnLoad
func JNI_OnLoad(vm *C.JavaVM, reserved unsafe.Pointer) C.jint {
	nativeJava.jVM = vm
	return C.JNI_VERSION_1_6
}

//export Java_com_boxes_GoNative_registerAsPeer
func Java_com_boxes_GoNative_registerAsPeer(env *C.JNIEnv) {
	ctx := gs.runtime.NewContext()
	gs.registerAsPeer(ctx)
}

//export Java_com_boxes_GoNative_connectPeer
func Java_com_boxes_GoNative_connectPeer(env *C.JNIEnv) {
	ctx := gs.runtime.NewContext()
	gs.connectPeer(ctx)
}

//export Java_com_boxes_GoNative_sendDrawBox
func Java_com_boxes_GoNative_sendDrawBox(env *C.JNIEnv, class C.jclass,
	boxId C.jstring, ox C.jfloat, oy C.jfloat, cx C.jfloat, cy C.jfloat) {
	gs.sendBox(boxes.Box{DeviceId: gs.myIPAddr, BoxId: C.GoString(C.JToCString(env, boxId)), Points: [4]float32{float32(ox), float32(oy), float32(cx), float32(cy)}})
}

//export Java_com_boxes_GoNative_registerAddBox
func Java_com_boxes_GoNative_registerAddBox(env *C.JNIEnv, thisObj C.jobject, obj C.jobject) {
	nativeJava.registerAddBox(env, obj)
}

func initStoreService() {
	publicID := gs.runtime.Identity().PublicID()
	if len(publicID.Names()) == 0 {
		panic(fmt.Errorf("invalid PublicID:%v\n", publicID))
	}

	// Create a new store server
	storeDBName := storePath + "/" + storeDatabase
	store, err := sstore.New(sstore.ServerConfig{Admin: publicID, DBName: storeDBName})
	if err != nil {
		panic(fmt.Errorf("store.New() failed:%v", err))
	}

	// Create ACL Authorizer with read/write permissions for the identity
	acl, err := vsecurity.LoadACL(bytes.NewBufferString("{\"" + publicID.Names()[0] + "\":\"RW\"}"))
	if err != nil {
		panic(fmt.Errorf("LoadACL failed:%v", err))
	}
	auth := vsecurity.NewACLAuthorizer(acl)
	gs.disp.storeDispatcher = sstore.NewStoreDispatcher(store, auth)

	// Create an endpoint and start listening
	if _, err = gs.ipc.Listen("tcp", gs.myIPAddr+storeServicePort); err != nil {
		panic(fmt.Errorf("s.Listen() failed:%v", err))
	}
	// Register the services
	if err = gs.ipc.Serve("", &gs.disp); err != nil {
		panic(fmt.Errorf("s.Serve(store) failed:%v", err))
	}
	gs.storeEndpoint = "/" + gs.myIPAddr + storeServicePort
}

func initSyncService(peerEndpoint string) {
	peerSyncAddr := strings.Split(peerEndpoint, ":")[0]
	srv := vsync.NewServerSync(vsync.NewSyncd(peerSyncAddr+syncServicePort, peerSyncAddr /* peer deviceID */, gs.myIPAddr /* my deviceID */, storePath, gs.storeEndpoint, 0))
	gs.disp.syncAuth = nil
	gs.disp.syncServer = ipc.ReflectInvoker(srv)
	if _, err := gs.ipc.Listen("tcp", gs.myIPAddr+syncServicePort); err != nil {
		panic(fmt.Errorf("syncd:: error listening to service: err %v", err))
	}
	if err := gs.ipc.Serve("", &gs.disp); err != nil {
		panic(fmt.Errorf("syncd:: error serving service: err %v", err))
	}
}

func init() {
	// Register *storage.Entry for WatchGlob.
	// TODO(tilaks): storage.Entry is declared in vdl, vom should register the
	// pointer automatically.
	vom.Register(&storage.Entry{})
	runtime.GOMAXPROCS(runtime.NumCPU())
}

func main() {
	// Get an IP that can be published to the signal server
	ifaces, _ := net.InterfaceAddrs()
	for _, addr := range ifaces {
		if ipa, ok := addr.(*net.IPNet); ok {
			if ip4 := ipa.IP.To4(); ip4 != nil && ip4.String() != "127.0.0.1" {
				gs.myIPAddr = ip4.String()
				break
			}
		}
	}
	if len(gs.myIPAddr) <= 0 {
		panic(fmt.Errorf("Failed to get value IPAddr:%v", ifaces))
	}

	myIdentity := "_4EEGgFCAP-DNBoBQwEudmV5cm9uL3J1bnRpbWVzL2dvb2dsZS9zZWN1cml0eS5jaGFpblByaXZhdGVJRAD_hVEYAQIBRAEIUHVibGljSUQAAQQBBlNlY3JldAABM3ZleXJvbi9ydW50aW1lcy9nb29nbGUvc2VjdXJpdHkvd2lyZS5DaGFpblByaXZhdGVJRAD_hwQaAUUA_4lJGAEBAUYBDENlcnRpZmljYXRlcwABMnZleXJvbi9ydW50aW1lcy9nb29nbGUvc2VjdXJpdHkvd2lyZS5DaGFpblB1YmxpY0lEAP-LBBIBRwD_jWcYAQQBAwEETmFtZQABSAEJUHVibGljS2V5AAFJAQdDYXZlYXRzAAFKAQlTaWduYXR1cmUAATB2ZXlyb24vcnVudGltZXMvZ29vZ2xlL3NlY3VyaXR5L3dpcmUuQ2VydGlmaWNhdGUA_49FGAECAUsBBUN1cnZlAAEEAQJYWQABLnZleXJvbi9ydW50aW1lcy9nb29nbGUvc2VjdXJpdHkvd2lyZS5QdWJsaWNLZXkA_5UzEAEyAS12ZXlyb24vcnVudGltZXMvZ29vZ2xlL3NlY3VyaXR5L3dpcmUua2V5Q3VydmUA_5EEEgFMAP-XRxgBAgFNAQdTZXJ2aWNlAAEEAQVCeXRlcwABK3ZleXJvbi9ydW50aW1lcy9nb29nbGUvc2VjdXJpdHkvd2lyZS5DYXZlYXQA_5knEAEDASF2ZXlyb24yL3NlY3VyaXR5LlByaW5jaXBhbFBhdHRlcm4A_5NAGAECAQQBAVIAAQQBAVMAAS52ZXlyb24vcnVudGltZXMvZ29vZ2xlL3NlY3VyaXR5L3dpcmUuU2lnbmF0dXJlAP-C_8ABAwEFAQEBCGdhdXRoYW10AQJBBL1M858IVO3sxJTYFxv1EiDVLFG6WdH-l4OpOHQXlZn5MO8LXNdnRhJ_r_Zwe92VHpbemsPNek_SJOfsSmsVRA8AAgEgm5eZNI1lUqwPCqlQOgesp-7zx0zLxvJe9IcwRbnMycoBINYA7GSPqFxDkbWYScL3Kj0k4AZK-e9sF01a7RAjcGMRAAAAASBldL9q-34I_W5yrapTmqaItm66RGNGtiUrFTlfh8VaNwA="

	// Load identity that has been generated.
	privateID, err := vsecurity.LoadIdentity(strings.NewReader(myIdentity))
	if err != nil {
		panic(fmt.Errorf("Failed to load identity:%v\n", err))
	}

	// Remove any remnant files from the store directory
	if err = os.RemoveAll(storePath); err != nil {
		panic(fmt.Errorf("Failed to remove remnant store files:%v\n", err))
	}

	gs.boxList = make(chan boxes.Box, 256)

	// Initialize veyron runtime and bind to the signalling server used to rendezvous with
	// another peer device. TODO(gauthamt): Switch to using the nameserver for signalling.
	gs.runtime = rt.Init(veyron2.RuntimeID(privateID))
	if gs.signalling, err = boxes.BindBoxSignalling(naming.JoinAddressName("@2@tcp@162.222.181.93:8509@08a93d90836cd94d4dc1acbe40b9048d@1@1@@", "signalling")); err != nil {
		panic(fmt.Errorf("failed to bind to signalling server:%v\n", err))
	}

	// Create a new server instance
	if gs.ipc, err = gs.runtime.NewServer(); err != nil {
		panic(fmt.Errorf("r.NewServer() failed:%v", err))
	}

	// Initialize an instance of the store service for this device
	initStoreService()
}
