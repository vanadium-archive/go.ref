// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Use a different package for the tests to ensure that only the exported API is used.

package vc_test

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"testing"

	"v.io/v23/context"
	"v.io/v23/naming"
	"v.io/v23/rpc/version"
	"v.io/v23/security"
	"v.io/v23/verror"

	_ "v.io/x/ref/runtime/factories/generic"
	"v.io/x/ref/runtime/internal/lib/bqueue"
	"v.io/x/ref/runtime/internal/lib/bqueue/drrqueue"
	"v.io/x/ref/runtime/internal/lib/iobuf"
	"v.io/x/ref/runtime/internal/rpc/stream"
	"v.io/x/ref/runtime/internal/rpc/stream/crypto"
	"v.io/x/ref/runtime/internal/rpc/stream/id"
	"v.io/x/ref/runtime/internal/rpc/stream/vc"
	iversion "v.io/x/ref/runtime/internal/rpc/version"
	"v.io/x/ref/test"
	"v.io/x/ref/test/testutil"
)

var (
	clientEP = endpoint(naming.FixedRoutingID(0xcccccccccccccccc))
	serverEP = endpoint(naming.FixedRoutingID(0x5555555555555555))
)

//go:generate jiri test generate

const (
	// Convenience alias to avoid conflicts between the package name "vc" and variables called "vc".
	DefaultBytesBufferedPerFlow = vc.DefaultBytesBufferedPerFlow
)

var LatestVersion = iversion.SupportedRange.Max

type testSecurityLevel int

const (
	SecurityDefault testSecurityLevel = iota
	SecurityPreAuthenticated
	SecurityNone
)

// testFlowEcho writes a random string of 'size' bytes on the flow and then
// ensures that the same string is read back.
func testFlowEcho(t *testing.T, flow stream.Flow, size int) {
	testutil.InitRandGenerator(t.Logf)
	defer flow.Close()
	wrote := testutil.RandomBytes(size)
	go func() {
		buf := wrote
		for len(buf) > 0 {
			limit := 1 + testutil.RandomIntn(len(buf)) // Random number in [1, n]
			n, err := flow.Write(buf[:limit])
			if n != limit || err != nil {
				t.Errorf("Write returned (%d, %v) want (%d, nil)", n, err, limit)
			}
			buf = buf[limit:]
		}
	}()

	total := 0
	read := make([]byte, size)
	buf := read
	for total < size {
		n, err := flow.Read(buf)
		if err != nil {
			t.Error(err)
			return
		}
		total += n
		buf = buf[n:]
	}
	if bytes.Compare(read, wrote) != 0 {
		t.Errorf("Data read != data written")
	}
}

func TestHandshakeNoSecurity(t *testing.T) {
	ctx, shutdown := test.V23Init()
	defer shutdown()
	// When the principals are nil, no blessings should be sent over the wire.
	clientH, serverH := newVC(ctx)
	if err := handshakeVCNoAuthentication(LatestVersion, clientH.VC, serverH.VC); err != nil {
		t.Fatal(err)
	}
	defer clientH.Close()
	flow, err := clientH.VC.Connect()
	if err != nil {
		t.Fatal(err)
	}
	if !flow.RemoteBlessings().IsZero() {
		t.Errorf("Server sent blessing %v over insecure transport", flow.RemoteBlessings())
	}
	if !flow.LocalBlessings().IsZero() {
		t.Errorf("Client sent blessing %v over insecure transport", flow.LocalBlessings())
	}
}

func testFlowAuthN(flow stream.Flow, serverBlessings security.Blessings, serverDischarges map[string]security.Discharge, clientPublicKey security.PublicKey) error {
	if got, want := flow.RemoteBlessings(), serverBlessings; !reflect.DeepEqual(got, want) {
		return fmt.Errorf("Server shared blessings %v, want %v", got, want)
	}
	if got, want := flow.RemoteDischarges(), serverDischarges; !reflect.DeepEqual(got, want) {
		return fmt.Errorf("Server shared discharges %#v, want %#v", got, want)
	}
	if got, want := flow.LocalBlessings().PublicKey(), clientPublicKey; !reflect.DeepEqual(got, want) {
		return fmt.Errorf("Client shared %v, want %v", got, want)
	}
	return nil
}

// auth implements security.Authorizer.
type auth struct {
	localPrincipal   security.Principal
	remoteBlessings  security.Blessings
	remoteDischarges map[string]security.Discharge
	suffix, method   string
	err              error
}

// Authorize tests that the context passed to the authorizer is the expected one.
func (a *auth) Authorize(ctx *context.T, call security.Call) error {
	if a.err != nil {
		return a.err
	}
	if got, want := call.LocalPrincipal(), a.localPrincipal; !reflect.DeepEqual(got, want) {
		return fmt.Errorf("ctx.LocalPrincipal: got %v, want %v", got, want)
	}
	if got, want := call.RemoteBlessings(), a.remoteBlessings; !reflect.DeepEqual(got, want) {
		return fmt.Errorf("ctx.RemoteBlessings: got %v, want %v", got, want)
	}
	if got, want := call.RemoteDischarges(), a.remoteDischarges; !reflect.DeepEqual(got, want) {
		return fmt.Errorf("ctx.RemoteDischarges: got %v, want %v", got, want)
	}
	if got, want := call.LocalEndpoint(), clientEP; !reflect.DeepEqual(got, want) {
		return fmt.Errorf("ctx.LocalEndpoint: got %v, want %v", got, want)
	}
	if got, want := call.RemoteEndpoint(), serverEP; !reflect.DeepEqual(got, want) {
		return fmt.Errorf("ctx.RemoteEndpoint: got %v, want %v", got, want)
	}
	if got, want := call.Suffix(), a.suffix; got != want {
		return fmt.Errorf("ctx.RemoteEndpoint: got %v, want %v", got, want)
	}
	if got, want := call.Method(), a.method; got != want {
		return fmt.Errorf("ctx.RemoteEndpoint: got %v, want %v", got, want)
	}
	return nil
}

// mockDischargeClient implements vc.DischargeClient.
type mockDischargeClient []security.Discharge

func (m mockDischargeClient) PrepareDischarges(_ *context.T, forcaveats []security.Caveat, impetus security.DischargeImpetus) []security.Discharge {
	return m
}
func (mockDischargeClient) Invalidate(*context.T, ...security.Discharge) {}
func (mockDischargeClient) RPCStreamListenerOpt()                        {}
func (mockDischargeClient) RPCStreamVCOpt()                              {}

// Test that mockDischargeClient implements vc.DischargeClient.
var _ vc.DischargeClient = (mockDischargeClient)(nil)

func testHandshake(t *testing.T, securityLevel testSecurityLevel) {
	ctx, shutdown := test.V23Init()
	defer shutdown()
	matchesError := func(got error, want string) error {
		if (got == nil) && len(want) == 0 {
			return nil
		}
		if got == nil && !strings.Contains(got.Error(), want) {
			return fmt.Errorf("got error %q, wanted to match %q", got, want)
		}
		return nil
	}
	var (
		root       = testutil.NewIDProvider("root")
		discharger = testutil.NewPrincipal("discharger")
		pclient    = testutil.NewPrincipal()
		pserver    = testutil.NewPrincipal()
	)
	tpcav, err := security.NewPublicKeyCaveat(discharger.PublicKey(), "irrelevant", security.ThirdPartyRequirements{}, security.UnconstrainedUse())
	if err != nil {
		t.Fatal(err)
	}
	dis, err := discharger.MintDischarge(tpcav, security.UnconstrainedUse())
	if err != nil {
		t.Fatal(err)
	}
	// Root blesses the client
	if err := root.Bless(pclient, "client"); err != nil {
		t.Fatal(err)
	}
	// Root blesses the server with a third-party caveat
	if err := root.Bless(pserver, "server", tpcav); err != nil {
		t.Fatal(err)
	}

	testdata := []struct {
		dischargeClient      vc.DischargeClient
		auth                 *vc.ServerAuthorizer
		dialErr              string
		flowRemoteBlessings  security.Blessings
		flowRemoteDischarges map[string]security.Discharge
	}{
		{
			flowRemoteBlessings: pserver.BlessingStore().Default(),
		},
		{
			dischargeClient:      mockDischargeClient([]security.Discharge{dis}),
			flowRemoteBlessings:  pserver.BlessingStore().Default(),
			flowRemoteDischarges: map[string]security.Discharge{dis.ID(): dis},
		},
		{
			dischargeClient: mockDischargeClient([]security.Discharge{dis}),
			auth: &vc.ServerAuthorizer{
				Suffix: "suffix",
				Method: "method",
				Policy: &auth{
					localPrincipal:   pclient,
					remoteBlessings:  pserver.BlessingStore().Default(),
					remoteDischarges: map[string]security.Discharge{dis.ID(): dis},
					suffix:           "suffix",
					method:           "method",
				},
			},
			flowRemoteBlessings:  pserver.BlessingStore().Default(),
			flowRemoteDischarges: map[string]security.Discharge{dis.ID(): dis},
		},
		{
			dischargeClient: mockDischargeClient([]security.Discharge{dis}),
			auth: &vc.ServerAuthorizer{
				Suffix: "suffix",
				Method: "method",
				Policy: &auth{
					err: errors.New("authorization error"),
				},
			},
			dialErr: "authorization error",
		},
	}
	for i, d := range testdata {
		clientH, serverH := newVC(ctx)
		var err error
		switch securityLevel {
		case SecurityPreAuthenticated:
			var serverPK, serverSK *crypto.BoxKey
			if serverPK, serverSK, err = crypto.GenerateBoxKey(); err != nil {
				t.Fatal(err)
			}
			err = handshakeVCPreAuthenticated(LatestVersion, clientH.VC, serverH.VC, pclient, pserver, serverPK, serverSK, d.flowRemoteDischarges, d.dischargeClient, d.auth)
		case SecurityDefault:
			err = handshakeVCWithAuthentication(LatestVersion, clientH.VC, serverH.VC, pclient, pserver, d.flowRemoteDischarges, d.dischargeClient, d.auth)
		}
		if merr := matchesError(err, d.dialErr); merr != nil {
			t.Errorf("Test #%d: HandshakeDialedVC with server authorizer %#v:: %v", i, d.auth.Policy, merr)
		}
		if err != nil {
			continue
		}
		flow, err := clientH.VC.Connect()
		if err != nil {
			clientH.Close()
			t.Errorf("Unable to create flow: %v", err)
			continue
		}
		if err := testFlowAuthN(flow, d.flowRemoteBlessings, d.flowRemoteDischarges, pclient.PublicKey()); err != nil {
			clientH.Close()
			t.Error(err)
			continue
		}
		clientH.Close()
	}
}
func TestHandshakePreAuthenticated(t *testing.T) { testHandshake(t, SecurityPreAuthenticated) }
func TestHandshake(t *testing.T)                 { testHandshake(t, SecurityDefault) }

func testConnect_Small(t *testing.T, version version.RPCVersion, securityLevel testSecurityLevel) {
	h, vc, err := NewSimple(version, securityLevel)
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	flow, err := vc.Connect()
	if err != nil {
		t.Fatal(err)
	}
	testFlowEcho(t, flow, 10)
}
func TestConnect_SmallNoSecurity(t *testing.T) { testConnect_Small(t, LatestVersion, SecurityNone) }
func TestConnect_SmallPreAuthenticated(t *testing.T) {
	testConnect_Small(t, LatestVersion, SecurityPreAuthenticated)
}
func TestConnect_Small(t *testing.T) { testConnect_Small(t, LatestVersion, SecurityDefault) }

func testConnect(t *testing.T, securityLevel testSecurityLevel) {
	h, vc, err := NewSimple(LatestVersion, securityLevel)
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	flow, err := vc.Connect()
	if err != nil {
		t.Fatal(err)
	}
	testFlowEcho(t, flow, 10*DefaultBytesBufferedPerFlow)
}
func TestConnectNoSecurity(t *testing.T)       { testConnect(t, SecurityNone) }
func TestConnectPreAuthenticated(t *testing.T) { testConnect(t, SecurityPreAuthenticated) }
func TestConnect(t *testing.T)                 { testConnect(t, SecurityDefault) }

// helper function for testing concurrent operations on multiple flows over the
// same VC.  Such tests are most useful when running the race detector.
// (go test -race ...)
func testConcurrentFlows(t *testing.T, securityLevel testSecurityLevel, flows, gomaxprocs int) {
	mp := runtime.GOMAXPROCS(gomaxprocs)
	defer runtime.GOMAXPROCS(mp)
	h, vc, err := NewSimple(LatestVersion, securityLevel)
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()

	var wg sync.WaitGroup
	wg.Add(flows)
	for i := 0; i < flows; i++ {
		go func(n int) {
			defer wg.Done()
			flow, err := vc.Connect()
			if err != nil {
				t.Error(err)
			} else {
				testFlowEcho(t, flow, (n+1)*DefaultBytesBufferedPerFlow)
			}
		}(i)
	}
	wg.Wait()
}

func TestConcurrentFlows_1NOSecurity(t *testing.T) { testConcurrentFlows(t, SecurityNone, 10, 1) }
func TestConcurrentFlows_1PreAuthenticated(t *testing.T) {
	testConcurrentFlows(t, SecurityPreAuthenticated, 10, 1)
}
func TestConcurrentFlows_1(t *testing.T) { testConcurrentFlows(t, SecurityDefault, 10, 1) }

func TestConcurrentFlows_10NoSecurity(t *testing.T) { testConcurrentFlows(t, SecurityNone, 10, 10) }
func TestConcurrentFlows_10PreAuthenticated(t *testing.T) {
	testConcurrentFlows(t, SecurityPreAuthenticated, 10, 10)
}
func TestConcurrentFlows_10(t *testing.T) { testConcurrentFlows(t, SecurityDefault, 10, 10) }

func testListen(t *testing.T, securityLevel testSecurityLevel) {
	h, vc, err := NewSimple(LatestVersion, securityLevel)
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	if err := h.VC.AcceptFlow(id.Flow(21)); err == nil {
		t.Errorf("Expected AcceptFlow on a new flow to fail as Listen was not called")
	}

	ln, err := vc.Listen()
	if err != nil {
		t.Fatalf("vc.Listen failed: %v", err)
		return
	}
	_, err = vc.Listen()
	if err == nil {
		t.Fatalf("Second call to vc.Listen should have failed")
		return
	}
	if err := h.VC.AcceptFlow(id.Flow(23)); err != nil {
		t.Fatal(err)
	}

	data := "the dark knight"
	cipherdata, err := h.otherEnd.VC.Encrypt(id.Flow(23), iobuf.NewSlice([]byte(data)))
	if err != nil {
		t.Fatal(err)
	}
	if err := h.VC.DispatchPayload(id.Flow(23), cipherdata); err != nil {
		t.Fatal(err)
	}
	flow, err := ln.Accept()
	if err != nil {
		t.Fatal(err)
	}
	if err := ln.Close(); err != nil {
		t.Error(err)
	}
	flow.Close()
	var buf [4096]byte
	if n, err := flow.Read(buf[:]); n != len(data) || err != nil || string(buf[:n]) != data {
		t.Errorf("Got (%d, %v) = %q, want (%d, nil) = %q", n, err, string(buf[:n]), len(data), data)
	}
	if n, err := flow.Read(buf[:]); n != 0 || err != io.EOF {
		t.Errorf("Got (%d, %v) want (0, %v)", n, err, io.EOF)
	}
}
func TestListenNoSecurity(t *testing.T)       { testListen(t, SecurityNone) }
func TestListenPreAuthenticated(t *testing.T) { testListen(t, SecurityPreAuthenticated) }
func TestListen(t *testing.T)                 { testListen(t, SecurityDefault) }

func testNewFlowAfterClose(t *testing.T, securityLevel testSecurityLevel) {
	h, _, err := NewSimple(LatestVersion, securityLevel)
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	h.VC.Close(fmt.Errorf("reason"))
	if err := h.VC.AcceptFlow(id.Flow(10)); err == nil {
		t.Fatalf("New flows should not be accepted once the VC is closed")
	}
}
func TestNewFlowAfterCloseNoSecurity(t *testing.T) { testNewFlowAfterClose(t, SecurityNone) }
func TestNewFlowAfterClosePreAuthenticated(t *testing.T) {
	testNewFlowAfterClose(t, SecurityPreAuthenticated)
}
func TestNewFlowAfterClose(t *testing.T) { testNewFlowAfterClose(t, SecurityDefault) }

func testConnectAfterClose(t *testing.T, securityLevel testSecurityLevel) {
	h, vc, err := NewSimple(LatestVersion, securityLevel)
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	h.VC.Close(fmt.Errorf("myerr"))
	if f, err := vc.Connect(); f != nil || err == nil || !strings.Contains(err.Error(), "myerr") {
		t.Fatalf("Got (%v, %v), want (nil, %q)", f, err, "myerr")
	}
}
func TestConnectAfterCloseNoSecurity(t *testing.T) { testConnectAfterClose(t, SecurityNone) }
func TestConnectAfterClosePreAuthenticated(t *testing.T) {
	testConnectAfterClose(t, SecurityPreAuthenticated)
}
func TestConnectAfterClose(t *testing.T) { testConnectAfterClose(t, SecurityDefault) }

// helper implements vc.Helper and also sets up a single VC.
type helper struct {
	VC *vc.VC
	bq bqueue.T

	mu       sync.Mutex
	otherEnd *helper // GUARDED_BY(mu)
}

// NewSimple creates both ends of a VC but returns only the "client" end (i.e.,
// the one that initiated the VC). The "server" end (the one that "accepted" the
// VC) listens for flows and simply echoes data read.
func NewSimple(v version.RPCVersion, securityLevel testSecurityLevel) (*helper, stream.VC, error) {
	ctx, shutdown := test.V23Init()
	defer shutdown()
	clientH, serverH := newVC(ctx)
	pclient := testutil.NewPrincipal("client")
	pserver := testutil.NewPrincipal("server")
	var err error
	switch securityLevel {
	case SecurityNone:
		err = handshakeVCNoAuthentication(v, clientH.VC, serverH.VC)
	case SecurityPreAuthenticated:
		serverPK, serverSK, _ := crypto.GenerateBoxKey()
		err = handshakeVCPreAuthenticated(v, clientH.VC, serverH.VC, pclient, pserver, serverPK, serverSK, nil, nil, nil)
	case SecurityDefault:
		err = handshakeVCWithAuthentication(v, clientH.VC, serverH.VC, pclient, pserver, nil, nil, nil)
	}
	if err != nil {
		clientH.Close()
		return nil, nil, err
	}
	return clientH, clientH.VC, err
}

func newVC(ctx *context.T) (clientH, serverH *helper) {
	clientH = &helper{bq: drrqueue.New(vc.MaxPayloadSizeBytes)}
	serverH = &helper{bq: drrqueue.New(vc.MaxPayloadSizeBytes)}
	clientH.otherEnd = serverH
	serverH.otherEnd = clientH

	vci := id.VC(1234)

	clientParams := vc.Params{
		VCI:      vci,
		Dialed:   true,
		LocalEP:  clientEP,
		RemoteEP: serverEP,
		Pool:     iobuf.NewPool(0),
		Helper:   clientH,
	}
	serverParams := vc.Params{
		VCI:      vci,
		LocalEP:  serverEP,
		RemoteEP: clientEP,
		Pool:     iobuf.NewPool(0),
		Helper:   serverH,
	}

	clientH.VC = vc.InternalNew(ctx, clientParams)
	serverH.VC = vc.InternalNew(ctx, serverParams)
	clientH.AddReceiveBuffers(vci, vc.SharedFlowID, vc.DefaultBytesBufferedPerFlow)

	go clientH.pipeLoop(ctx, serverH.VC)
	go serverH.pipeLoop(ctx, clientH.VC)
	return
}

func handshakeVCWithAuthentication(v version.RPCVersion, client, server *vc.VC, pclient, pserver security.Principal, discharges map[string]security.Discharge, dischargeClient vc.DischargeClient, auth *vc.ServerAuthorizer) error {
	var lopts []stream.ListenerOpt
	if dischargeClient != nil {
		lopts = append(lopts, dischargeClient)
	}
	var vcopts []stream.VCOpt
	if auth != nil {
		vcopts = append(vcopts, auth)
	}

	clientPK, serverPK := make(chan *crypto.BoxKey, 1), make(chan *crypto.BoxKey, 1)
	clientSendSetupVC := func(pubKey *crypto.BoxKey) error {
		clientPK <- pubKey
		return client.FinishHandshakeDialedVC(v, <-serverPK)
	}
	serverExchange := func(pubKey *crypto.BoxKey) (*crypto.BoxKey, error) {
		serverPK <- pubKey
		return <-clientPK, nil
	}

	hrCH := server.HandshakeAcceptedVCWithAuthentication(v, pserver, pserver.BlessingStore().Default(), serverExchange, lopts...)
	if err := client.HandshakeDialedVCWithAuthentication(pclient, clientSendSetupVC, vcopts...); err != nil {
		go func() { <-hrCH }()
		return err
	}
	hr := <-hrCH
	if hr.Error != nil {
		return hr.Error
	}
	go acceptLoop(hr.Listener)
	return nil
}

func handshakeVCPreAuthenticated(v version.RPCVersion, client, server *vc.VC, pclient, pserver security.Principal, serverPK, serverSK *crypto.BoxKey, discharges map[string]security.Discharge, dischargeClient vc.DischargeClient, auth *vc.ServerAuthorizer) error {
	var lopts []stream.ListenerOpt
	if dischargeClient != nil {
		lopts = append(lopts, dischargeClient)
	}
	var vcopts []stream.VCOpt
	if auth != nil {
		vcopts = append(vcopts, auth)
	}
	bserver := pserver.BlessingStore().Default()
	bclient, _ := pclient.BlessSelf("vcauth")

	clientPK, clientSig := make(chan *crypto.BoxKey, 1), make(chan []byte, 1)
	serverAccepted := make(chan struct{})
	sendSetupVC := func(pubKey *crypto.BoxKey, signature []byte) error {
		clientPK <- pubKey
		clientSig <- signature
		// Unlike the real world (in VIF), a message can be delivered to a server before
		// it handles SetupVC message. So we explictly sync in this test.
		<-serverAccepted
		return nil
	}

	var hrCH <-chan vc.HandshakeResult
	go func() {
		params := security.CallParams{LocalPrincipal: pserver, LocalBlessings: bserver, RemoteBlessings: bclient, LocalDischarges: discharges}
		hrCH = server.HandshakeAcceptedVCPreAuthenticated(v, params, <-clientSig, serverPK, serverSK, <-clientPK, lopts...)
		close(serverAccepted)
	}()
	params := security.CallParams{LocalPrincipal: pclient, LocalBlessings: bclient, RemoteBlessings: bserver, RemoteDischarges: discharges}
	if err := client.HandshakeDialedVCPreAuthenticated(v, params, serverPK, sendSetupVC, vcopts...); err != nil {
		go func() { <-hrCH }()
		return err
	}
	hr := <-hrCH
	if hr.Error != nil {
		return hr.Error
	}
	go acceptLoop(hr.Listener)
	return nil
}

func handshakeVCNoAuthentication(v version.RPCVersion, client, server *vc.VC) error {
	clientCH, serverCH := make(chan struct{}), make(chan struct{})
	clientSendSetupVC := func() error {
		close(clientCH)
		return client.FinishHandshakeDialedVC(v, nil)
	}
	serverSendSetupVC := func() error {
		close(serverCH)
		return nil
	}

	hrCH := server.HandshakeAcceptedVCNoAuthentication(v, serverSendSetupVC)
	if err := client.HandshakeDialedVCNoAuthentication(clientSendSetupVC); err != nil {
		go func() { <-hrCH }()
		return err
	}
	hr := <-hrCH
	if hr.Error != nil {
		return hr.Error
	}
	go acceptLoop(hr.Listener)
	return nil
}

// pipeLoop forwards slices written to h.bq to dst.
func (h *helper) pipeLoop(ctx *context.T, dst *vc.VC) {
	for {
		w, bufs, err := h.bq.Get(nil)
		if err != nil {
			return
		}
		fid := id.Flow(w.ID())
		for _, b := range bufs {
			cipher, err := h.VC.Encrypt(fid, b)
			if err != nil {
				ctx.Infof("vc encrypt failed: %v", err)
			}
			if err := dst.DispatchPayload(fid, cipher); err != nil {
				ctx.Infof("dispatch payload failed: %v", err)
				return
			}
		}
		if w.IsDrained() {
			h.VC.ShutdownFlow(fid)
			dst.ShutdownFlow(fid)
		}
	}
}

func acceptLoop(ln stream.Listener) {
	for {
		f, err := ln.Accept()
		if err != nil {
			return
		}
		go echoLoop(f)
	}
}

func echoLoop(flow stream.Flow) {
	var buf [vc.DefaultBytesBufferedPerFlow * 20]byte
	for {
		n, err := flow.Read(buf[:])
		if err == io.EOF {
			return
		}
		if err == nil {
			_, err = flow.Write(buf[:n])
		}
		if err != nil {
			panic(err)
		}
	}
}

func (h *helper) NotifyOfNewFlow(vci id.VC, fid id.Flow, bytes uint) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.otherEnd != nil {
		if err := h.otherEnd.VC.AcceptFlow(fid); err != nil {
			panic(verror.DebugString(err))
		}
		h.otherEnd.VC.ReleaseCounters(fid, uint32(bytes))
	}
}

func (h *helper) SendHealthCheck(vci id.VC) {}

func (h *helper) AddReceiveBuffers(vci id.VC, fid id.Flow, bytes uint) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.otherEnd != nil {
		h.otherEnd.VC.ReleaseCounters(fid, uint32(bytes))
	}
}

func (h *helper) NewWriter(vci id.VC, fid id.Flow, priority bqueue.Priority) (bqueue.Writer, error) {
	return h.bq.NewWriter(bqueue.ID(fid), priority, DefaultBytesBufferedPerFlow)
}

func (h *helper) Close() {
	h.VC.Close(fmt.Errorf("helper closed"))
	h.bq.Close()
	h.mu.Lock()
	otherEnd := h.otherEnd
	h.otherEnd = nil
	h.mu.Unlock()
	if otherEnd != nil {
		otherEnd.mu.Lock()
		otherEnd.otherEnd = nil
		otherEnd.mu.Unlock()
		otherEnd.Close()
	}
}

type endpoint naming.RoutingID

func (e endpoint) Network() string                          { return "test" }
func (e endpoint) VersionedString(int) string               { return e.String() }
func (e endpoint) String() string                           { return naming.RoutingID(e).String() }
func (e endpoint) Name() string                             { return naming.JoinAddressName(e.String(), "") }
func (e endpoint) RoutingID() naming.RoutingID              { return naming.RoutingID(e) }
func (e endpoint) Routes() []string                         { return nil }
func (e endpoint) Addr() net.Addr                           { return nil }
func (e endpoint) ServesMountTable() bool                   { return false }
func (e endpoint) ServesLeaf() bool                         { return false }
func (e endpoint) BlessingNames() []string                  { return nil }
func (e endpoint) RPCVersionRange() version.RPCVersionRange { return version.RPCVersionRange{} }
