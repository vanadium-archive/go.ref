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
	"net"
	"os"
	"runtime"
	"strings"
	"unsafe"

	"veyron/examples/boxes"
	inaming "veyron/runtimes/google/naming"
	vsync "veyron/runtimes/google/vsync"
	"veyron/services/store/raw"
	storage "veyron/services/store/server"

	"veyron2"
	"veyron2/ipc"
	"veyron2/naming"
	"veyron2/query"
	"veyron2/rt"
	"veyron2/security"
	"veyron2/services/watch"
	"veyron2/storage/vstore"
	"veyron2/storage/vstore/primitives"
	"veyron2/vom"
)

type jniState struct {
	jVM  *C.JavaVM
	jObj C.jobject
	jMID C.jmethodID
}

type goState struct {
	runtime       veyron2.Runtime
	store         *storage.Server
	ipc           ipc.Server
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

func (gs *goState) SyncBoxes(context ipc.ServerContext) error {
	// Get the endpoint of the remote process
	endPt, err := inaming.NewEndpoint(context.RemoteAddr().String())
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
		for {
			box, err := gs.drawStream.Recv()
			if err != nil {
				return
			}
			nativeJava.addBox(&box)
		}
	}()
	// Loop to send boxes to remote peer
	for {
		if err := gs.drawStream.Send(<-gs.boxList); err != nil {
			break
		}
	}
}

func (gs *goState) monitorStore() {
	ctx := gs.runtime.NewContext()

	// Watch for any box updates from the store
	go func() {
		rst, err := raw.BindStore(naming.JoinAddressName(gs.storeEndpoint, raw.RawStoreSuffix))
		if err != nil {
			panic(fmt.Errorf("Failed to raw.Bind Store:%v", err))
		}
		req := watch.Request{Query: query.Query{}}
		stream, err := rst.Watch(ctx, req, veyron2.CallTimeout(ipc.NoTimeout))
		if err != nil {
			panic(fmt.Errorf("Can't watch store: %s: %s", gs.storeEndpoint, err))
		}
		for {
			cb, err := stream.Recv()
			if err != nil {
				panic(fmt.Errorf("Can't receive watch event: %s: %s", gs.storeEndpoint, err))
			}
			for _, change := range cb.Changes {
				if mu, ok := change.Value.(*raw.Mutation); ok && len(mu.Dir) == 0 {
					if box, ok := mu.Value.(boxes.Box); ok && box.DeviceId != gs.myIPAddr {
						nativeJava.addBox(&box)
					}
				}
			}
		}
	}()
	// Send any box updates to the store
	vst, err := vstore.New(gs.storeEndpoint)
	if err != nil {
		panic(fmt.Errorf("Failed to init veyron store:%v", err))
	}
	root := vst.Bind("/")
	tr := primitives.NewTransaction(ctx)
	if _, err := root.Put(ctx, tr, ""); err != nil {
		panic(fmt.Errorf("Put for %s failed:%v", root, err))
	}
	if err := tr.Commit(ctx); err != nil {
		panic(fmt.Errorf("Commit transaction failed:%v", err))
	}
	for {
		box := <-gs.boxList
		boxes := vst.Bind("/" + box.BoxId)
		tr = primitives.NewTransaction(ctx)
		if _, err := boxes.Put(ctx, tr, box); err != nil {
			panic(fmt.Errorf("Put for %s failed:%v", boxes, err))
		}
		if err := tr.Commit(ctx); err != nil {
			panic(fmt.Errorf("Commit transaction failed:%v", err))
		}
	}
}

func (gs *goState) registerAsPeer() {
	auth := security.NewACLAuthorizer(security.ACL{security.AllPrincipals: security.LabelSet(security.AdminLabel)})
	srv, err := gs.runtime.NewServer()
	if err != nil {
		panic(fmt.Errorf("Failed runtime.NewServer:%v", err))
	}
	drawServer := boxes.NewServerDrawInterface(gs)
	if err := srv.Register("draw", ipc.SoloDispatcher(drawServer, auth)); err != nil {
		panic(fmt.Errorf("Failed Register:%v", err))
	}
	endPt, err := srv.Listen("tcp", gs.myIPAddr+drawServicePort)
	if err != nil {
		panic(fmt.Errorf("Failed to Listen:%v", err))
	}
	if err := gs.signalling.Add(gs.runtime.TODOContext(), endPt.String()); err != nil {
		panic(fmt.Errorf("Failed to Add endpoint to signalling server:%v", err))
	}
}

func (gs *goState) connectPeer() {
	endpointStr, err := gs.signalling.Get(gs.runtime.TODOContext())
	if err != nil {
		panic(fmt.Errorf("failed to Get peer endpoint from signalling server:%v", err))
	}
	drawInterface, err := boxes.BindDrawInterface(naming.JoinAddressName(endpointStr, "draw"))
	if err != nil {
		panic(fmt.Errorf("failed BindDrawInterface:%v", err))
	}
	if !useStoreService {
		if gs.drawStream, err = drawInterface.Draw(gs.runtime.TODOContext()); err != nil {
			panic(fmt.Errorf("failed to get handle to Draw stream:%v\n", err))
		}
		go gs.streamBoxesLoop()
	} else {
		// Initialize the store sync service that listens for updates from a peer
		endpoint, err := inaming.NewEndpoint(endpointStr)
		if err != nil {
			panic(fmt.Errorf("failed to parse endpoint:%v", err))
		}
		if err = drawInterface.SyncBoxes(gs.runtime.TODOContext()); err != nil {
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
	gs.registerAsPeer()
}

//export Java_com_boxes_GoNative_connectPeer
func Java_com_boxes_GoNative_connectPeer(env *C.JNIEnv) {
	gs.connectPeer()
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
	var err error
	storeDBName := storePath + "/" + storeDatabase
	if gs.store, err = storage.New(storage.ServerConfig{Admin: publicID, DBName: storeDBName}); err != nil {
		panic(fmt.Errorf("storage.New() failed:%v", err))
	}

	// Create ACL Authorizer with read/write permissions for the identity
	acl, err := security.LoadACL(bytes.NewBufferString("{\"" + publicID.Names()[0] + "\":\"RW\"}"))
	if err != nil {
		panic(fmt.Errorf("LoadACL failed:%v", err))
	}
	auth := security.NewACLAuthorizer(acl)

	// Register the services
	if err = gs.ipc.Register("", storage.NewStoreDispatcher(gs.store, auth)); err != nil {
		panic(fmt.Errorf("s.Register(store) failed:%v", err))
	}

	// Create an endpoint and start listening
	if _, err = gs.ipc.Listen("tcp", gs.myIPAddr+storeServicePort); err != nil {
		panic(fmt.Errorf("s.Listen() failed:%v", err))
	}
	gs.storeEndpoint = "/" + gs.myIPAddr + storeServicePort
}

func initSyncService(peerEndpoint string) {
	peerSyncAddr := strings.Split(peerEndpoint, ":")[0]
	srv := vsync.NewServerSync(vsync.NewSyncd(peerSyncAddr+syncServicePort, peerSyncAddr /* peer deviceID */, gs.myIPAddr /* my deviceID */, storePath, gs.storeEndpoint, 0))
	if err := gs.ipc.Register("sync", ipc.SoloDispatcher(srv, nil)); err != nil {
		panic(fmt.Errorf("syncd:: error registering service: err %v", err))
	}
	if _, err := gs.ipc.Listen("tcp", gs.myIPAddr+syncServicePort); err != nil {
		panic(fmt.Errorf("syncd:: error listening to service: err %v", err))
	}
}

func init() {
	vom.Register(&raw.Mutation{})
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
	privateID, err := security.LoadIdentity(strings.NewReader(myIdentity))
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
	gs.runtime = rt.Init(veyron2.LocalID(privateID))
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
