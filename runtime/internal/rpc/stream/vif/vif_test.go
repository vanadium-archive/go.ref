// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Tests in a separate package to ensure that only the exported API is used in the tests.
//
// All tests are run with the default security level on VCs (SecurityConfidential).

package vif_test

import (
	"bytes"
	"fmt"
	"io"
	"net"
	"reflect"
	"runtime"
	"sort"
	"sync"
	"testing"
	"time"

	"v.io/v23"
	"v.io/v23/context"
	"v.io/v23/naming"
	"v.io/v23/rpc/version"

	inaming "v.io/x/ref/runtime/internal/naming"
	"v.io/x/ref/runtime/internal/rpc/stream"
	"v.io/x/ref/runtime/internal/rpc/stream/vc"
	"v.io/x/ref/runtime/internal/rpc/stream/vif"
	iversion "v.io/x/ref/runtime/internal/rpc/version"
	"v.io/x/ref/test"
	"v.io/x/ref/test/testutil"
)

//go:generate jiri test generate

func TestSingleFlowCreatedAtClient(t *testing.T) {
	ctx, shutdown := test.V23InitSimple()
	defer shutdown()
	pclient := testutil.NewPrincipal("client")
	pserver := testutil.NewPrincipal("server")
	cctx, _ := v23.WithPrincipal(ctx, pclient)
	sctx, _ := v23.WithPrincipal(ctx, pserver)
	client, server := NewClientServer(cctx, sctx)
	defer client.Close()

	clientVC, _, err := createVC(cctx, client, server, makeEP(0x5))
	if err != nil {
		t.Fatal(err)
	}
	writer, err := clientVC.Connect()
	if err != nil {
		t.Fatal(err)
	}
	// Test with an empty message to ensure that we correctly
	// handle closing empty flows.
	rwSingleFlow(t, writer, acceptFlowAtServer(server), "")
	writer, err = clientVC.Connect()
	if err != nil {
		t.Fatal(err)
	}
	rwSingleFlow(t, writer, acceptFlowAtServer(server), "the dark knight")
}

func TestSingleFlowCreatedAtServer(t *testing.T) {
	ctx, shutdown := test.V23InitSimple()
	defer shutdown()
	pclient := testutil.NewPrincipal("client")
	pserver := testutil.NewPrincipal("server")
	cctx, _ := v23.WithPrincipal(ctx, pclient)
	sctx, _ := v23.WithPrincipal(ctx, pserver)
	client, server := NewClientServer(cctx, sctx)
	defer client.Close()

	clientVC, serverConnector, err := createVC(cctx, client, server, makeEP(0x5))
	if err != nil {
		t.Fatal(err)
	}
	ln, err := clientVC.Listen()
	if err != nil {
		t.Fatal(err)
	}
	writer, err := serverConnector.Connect()
	if err != nil {
		t.Fatal(err)
	}
	reader, err := ln.Accept()
	if err != nil {
		t.Fatal(err)
	}
	rwSingleFlow(t, writer, reader, "the dark knight")
	ln.Close()
}

func testMultipleVCsAndMultipleFlows(t *testing.T, gomaxprocs int) {
	testutil.InitRandGenerator(t.Logf)
	ctx, shutdown := test.V23InitSimple()
	defer shutdown()
	// This test dials multiple VCs from the client to the server.
	// On each VC, it creates multiple flows, writes to them and verifies
	// that the other process received what was written.

	// Knobs configuring this test
	//
	// In case the test breaks, the knobs can be tuned down to isolate the problem.
	// In normal circumstances, the knows should be tuned up to stress test the code.
	const (
		nVCs                  = 6 // Number of VCs created by the client process Dialing.
		nFlowsFromClientPerVC = 3 // Number of flows initiated by the client process, per VC
		nFlowsFromServerPerVC = 4 // Number of flows initiated by the server process, per VC

		// Maximum number of bytes to write and read per flow.
		// The actual size is selected randomly.
		maxBytesPerFlow = 512 << 10 // 512KB
	)

	mp := runtime.GOMAXPROCS(gomaxprocs)
	defer runtime.GOMAXPROCS(mp)

	pclient := testutil.NewPrincipal("client")
	pserver := testutil.NewPrincipal("server")
	cctx, _ := v23.WithPrincipal(ctx, pclient)
	sctx, _ := v23.WithPrincipal(ctx, pserver)
	client, server := NewClientServer(cctx, sctx)
	defer client.Close()

	// Create all the VCs
	// clientVCs[i] is the VC at the client process
	// serverConnectors[i] is the corresponding VC at the server process.
	clientVCs, serverConnectors, err := createNVCs(cctx, client, server, 0, nVCs)
	if err != nil {
		t.Fatal(err)
	}

	// Create listeners for flows on the client VCs.
	// Flows are implicitly being listened to at the server (available through server.Accept())
	clientLNs, err := createListeners(clientVCs)
	if err != nil {
		t.Fatal(err)
	}

	// Create flows:
	// Over each VC, create nFlowsFromClientPerVC initiated by the client
	// and nFlowsFromServerPerVC initiated by the server.
	nFlows := nVCs * (nFlowsFromClientPerVC + nFlowsFromServerPerVC)

	// Fill in random strings that will be written over the Flows.
	dataWritten := make([]string, nFlows)
	for i := 0; i < nFlows; i++ {
		dataWritten[i] = string(testutil.RandomBytes(maxBytesPerFlow))
	}

	// write writes data to flow in randomly sized chunks.
	write := func(flow stream.Flow, data string) {
		defer flow.Close()
		buf := []byte(data)
		// Split into a random number of Write calls.
		for len(buf) > 0 {
			size := 1 + testutil.RandomIntn(len(buf)) // Random number in [1, len(buf)]
			n, err := flow.Write(buf[:size])
			if err != nil {
				t.Errorf("Write failed: (%d, %v)", n, err)
				return
			}
			buf = buf[size:]
		}
	}

	dataReadChan := make(chan string, nFlows)
	// read reads from a flow and writes out the data to dataReadChan
	read := func(flow stream.Flow) {
		var buf bytes.Buffer
		var tmp [1024]byte
		for {
			n, err := flow.Read(tmp[:testutil.RandomIntn(len(tmp))])
			buf.Write(tmp[:n])
			if err == io.EOF {
				break
			}
			if err != nil {
				t.Errorf("Read error: %v", err)
				break
			}
		}
		dataReadChan <- buf.String()
	}

	index := 0
	for i := 0; i < len(clientVCs); i++ {
		for j := 0; j < nFlowsFromClientPerVC; j++ {
			// Flow initiated by client, read by server
			writer, err := clientVCs[i].Connect()
			if err != nil {
				t.Errorf("clientVCs[%d], flow %d: %v", i, j, err)
				continue
			}
			go write(writer, dataWritten[index])
			go read(acceptFlowAtServer(server))
			index++
		}
	}
	for i := 0; i < len(serverConnectors); i++ {
		for j := 0; j < nFlowsFromServerPerVC; j++ {
			// Flow initiated by server, read by client
			writer, err := serverConnectors[i].Connect()
			if err != nil {
				t.Errorf("serverConnectors[%d], flow %d: %v", i, j, err)
				continue
			}
			go write(writer, dataWritten[index])
			go read(acceptFlowAtClient(clientLNs[i]))
			index++
		}
	}
	if index != nFlows {
		t.Errorf("Created %d flows, wanted %d", index, nFlows)
	}

	// Collect all data read and compare against the data written.
	// Since flows might be accepted in arbitrary order, sort the data before comparing.
	dataRead := make([]string, index)
	for i := 0; i < index; i++ {
		dataRead[i] = <-dataReadChan
	}
	sort.Strings(dataWritten)
	sort.Strings(dataRead)
	if !reflect.DeepEqual(dataRead, dataWritten) {
		// Since the strings can be very large, only print out the first few diffs.
		nDiffs := 0
		for i := 0; i < len(dataRead); i++ {
			if dataRead[i] != dataWritten[i] {
				nDiffs++
				t.Errorf("Diff %d out of %d items: Got %q want %q", nDiffs, i, atmostNbytes(dataRead[i], 20), atmostNbytes(dataWritten[i], 20))
			}
		}
		if nDiffs > 0 {
			t.Errorf("#Mismatches:%d #ReadSamples:%d #WriteSamples:%d", nDiffs, len(dataRead), len(dataWritten))
		}
	}
}

func TestMultipleVCsAndMultipleFlows_1(t *testing.T) {
	// Test with a single goroutine since that is typically easier to debug
	// in case of problems.
	testMultipleVCsAndMultipleFlows(t, 1)
}

func TestMultipleVCsAndMultipleFlows_5(t *testing.T) {
	// Test with multiple goroutines, particularly useful for checking
	// races with
	// go test -race
	testMultipleVCsAndMultipleFlows(t, 5)
}

func TestClose(t *testing.T) {
	ctx, shutdown := test.V23InitSimple()
	defer shutdown()
	pclient := testutil.NewPrincipal("client")
	pserver := testutil.NewPrincipal("server")
	cctx, _ := v23.WithPrincipal(ctx, pclient)
	sctx, _ := v23.WithPrincipal(ctx, pserver)
	client, server := NewClientServer(cctx, sctx)
	vc, _, err := createVC(cctx, client, server, makeEP(0x5))
	if err != nil {
		t.Fatal(err)
	}

	clientFlow, err := vc.Connect()
	if err != nil {
		t.Fatal(err)
	}
	serverFlow := acceptFlowAtServer(server)

	var message = []byte("bugs bunny")
	go func() {
		if n, err := clientFlow.Write(message); n != len(message) || err != nil {
			t.Fatalf("Wrote (%d, %v), want (%d, nil)", n, err, len(message))
		}
		client.Close()
	}()

	buf := make([]byte, 1024)
	// client.Close should drain all pending writes first.
	if n, err := serverFlow.Read(buf); n != len(message) || err != nil {
		t.Fatalf("Got (%d, %v) = %q, want (%d, nil) = %q", n, err, buf[:n], len(message), message)
	}
	// subsequent reads should fail, since the VIF should be closed.
	if n, err := serverFlow.Read(buf); n != 0 || err == nil {
		t.Fatalf("Got (%d, %v) = %q, want (0, nil)", n, err, buf[:n])
	}
	server.Close()
}

func TestOnClose(t *testing.T) {
	ctx, shutdown := test.V23InitSimple()
	defer shutdown()
	pclient := testutil.NewPrincipal("client")
	pserver := testutil.NewPrincipal("server")
	cctx, _ := v23.WithPrincipal(ctx, pclient)
	sctx, _ := v23.WithPrincipal(ctx, pserver)
	notifyC, notifyS := make(chan *vif.VIF), make(chan *vif.VIF)
	notifyFuncC := func(vf *vif.VIF) { notifyC <- vf }
	notifyFuncS := func(vf *vif.VIF) { notifyS <- vf }

	// Close the client VIF. Both client and server should be notified.
	client, server, err := New(nil, nil, cctx, sctx, notifyFuncC, notifyFuncS, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	client.Close()
	if got := <-notifyC; got != client {
		t.Errorf("Want notification for %v; got %v", client, got)
	}
	if got := <-notifyS; got != server {
		t.Errorf("Want notification for %v; got %v", server, got)
	}

	// Same as above, but close the server VIF at this time.
	client, server, err = New(nil, nil, cctx, sctx, notifyFuncC, notifyFuncS, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	server.Close()
	if got := <-notifyC; got != client {
		t.Errorf("Want notification for %v; got %v", client, got)
	}
	if got := <-notifyS; got != server {
		t.Errorf("Want notification for %v; got %v", server, got)
	}
}

func testCloseWhenEmpty(t *testing.T, testServer bool) {
	const (
		waitTime = 5 * time.Millisecond
	)
	ctx, shutdown := test.V23InitSimple()
	defer shutdown()
	pclient := testutil.NewPrincipal("client")
	pserver := testutil.NewPrincipal("server")
	cctx, _ := v23.WithPrincipal(ctx, pclient)
	sctx, _ := v23.WithPrincipal(ctx, pserver)
	notify := make(chan interface{})
	notifyFunc := func(vf *vif.VIF) { notify <- vf }

	newVIF := func() (vf, remote *vif.VIF) {
		var err error
		vf, remote, err = New(nil, nil, cctx, sctx, notifyFunc, notifyFunc, nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		if err = vf.StartAccepting(); err != nil {
			t.Fatal(err)
		}
		if testServer {
			vf, remote = remote, vf
		}
		return
	}

	// Initially empty. Should not be closed.
	vf, remote := newVIF()
	if err := vif.WaitWithTimeout(notify, waitTime); err != nil {
		t.Error(err)
	}

	// Open one VC. Should not be closed.
	vf, remote = newVIF()
	if _, _, err := createVC(cctx, vf, remote, makeEP(0x10)); err != nil {
		t.Fatal(err)
	}
	if err := vif.WaitWithTimeout(notify, waitTime); err != nil {
		t.Error(err)
	}

	// Close the VC. Should be closed.
	vf.ShutdownVCs(makeEP(0x10))
	if err := vif.WaitForNotifications(notify, vf, remote); err != nil {
		t.Error(err)
	}

	// Same as above, but open a VC from the remote side.
	vf, remote = newVIF()
	_, _, err := createVC(cctx, remote, vf, makeEP(0x10))
	if err != nil {
		t.Fatal(err)
	}
	if err := vif.WaitWithTimeout(notify, waitTime); err != nil {
		t.Error(err)
	}
	remote.ShutdownVCs(makeEP(0x10))
	if err := vif.WaitForNotifications(notify, vf, remote); err != nil {
		t.Error(err)
	}

	// Create two VCs.
	vf, remote = newVIF()
	if _, _, err := createNVCs(cctx, vf, remote, 0x10, 2); err != nil {
		t.Fatal(err)
	}

	// Close the first VC twice. Should not be closed.
	vf.ShutdownVCs(makeEP(0x10))
	vf.ShutdownVCs(makeEP(0x10))
	if err := vif.WaitWithTimeout(notify, waitTime); err != nil {
		t.Error(err)
	}

	// Close the second VC. Should be closed.
	vf.ShutdownVCs(makeEP(0x10 + 1))
	if err := vif.WaitForNotifications(notify, vf, remote); err != nil {
		t.Error(err)
	}
}

func TestCloseWhenEmpty(t *testing.T)       { testCloseWhenEmpty(t, false) }
func TestCloseWhenEmptyServer(t *testing.T) { testCloseWhenEmpty(t, true) }

func testStartTimeout(t *testing.T, testServer bool) {
	const (
		startTime = 5 * time.Millisecond
		// We use a long wait time here since it takes some time for the underlying network
		// connection of the other side to be closed especially in race testing.
		waitTime = 150 * time.Millisecond
	)
	ctx, shutdown := test.V23InitSimple()
	defer shutdown()
	pclient := testutil.NewPrincipal("client")
	pserver := testutil.NewPrincipal("server")
	cctx, _ := v23.WithPrincipal(ctx, pclient)
	sctx, _ := v23.WithPrincipal(ctx, pserver)
	notify := make(chan interface{})
	notifyFunc := func(vf *vif.VIF) { notify <- vf }

	newVIF := func() (vf, remote *vif.VIF, triggerTimers func()) {
		triggerTimers = vif.SetFakeTimers()
		var vfStartTime, remoteStartTime time.Duration = startTime, 0
		if testServer {
			vfStartTime, remoteStartTime = remoteStartTime, vfStartTime
		}
		var err error
		vf, remote, err = New(nil, nil, cctx, sctx, notifyFunc, notifyFunc, []stream.VCOpt{vc.StartTimeout{
			Duration: vfStartTime,
		}}, []stream.ListenerOpt{vc.StartTimeout{
			Duration: remoteStartTime,
		}})
		if err != nil {
			t.Fatal(err)
		}
		if err = vf.StartAccepting(); err != nil {
			t.Fatal(err)
		}
		if testServer {
			vf, remote = remote, vf
		}
		return
	}

	// No VC opened. Should be closed after the start timeout.
	vf, remote, triggerTimers := newVIF()
	triggerTimers()
	if err := vif.WaitForNotifications(notify, vf, remote); err != nil {
		t.Error(err)
	}

	// Open one VC. Should not be closed.
	vf, remote, triggerTimers = newVIF()
	if _, _, err := createVC(cctx, vf, remote, makeEP(0x10)); err != nil {
		t.Fatal(err)
	}
	triggerTimers()
	if err := vif.WaitWithTimeout(notify, waitTime); err != nil {
		t.Error(err)
	}

	// Close the VC. Should be closed.
	vf.ShutdownVCs(makeEP(0x10))
	if err := vif.WaitForNotifications(notify, vf, remote); err != nil {
		t.Error(err)
	}
}

func TestStartTimeout(t *testing.T)       { testStartTimeout(t, false) }
func TestStartTimeoutServer(t *testing.T) { testStartTimeout(t, true) }

func testIdleTimeout(t *testing.T, testServer bool) {
	const (
		idleTime = 10 * time.Millisecond
		waitTime = idleTime * 2
	)
	ctx, shutdown := test.V23InitSimple()
	defer shutdown()
	pclient := testutil.NewPrincipal("client")
	pserver := testutil.NewPrincipal("server")
	cctx, _ := v23.WithPrincipal(ctx, pclient)
	sctx, _ := v23.WithPrincipal(ctx, pserver)
	notify := make(chan interface{})
	notifyFunc := func(vf *vif.VIF) { notify <- vf }

	newVIF := func() (vf, remote *vif.VIF) {
		var err error
		if vf, remote, err = New(nil, nil, cctx, sctx, notifyFunc, notifyFunc, nil, nil); err != nil {
			t.Fatal(err)
		}
		if err = vf.StartAccepting(); err != nil {
			t.Fatal(err)
		}
		if testServer {
			vf, remote = remote, vf
		}
		return
	}
	newVC := func(vf, remote *vif.VIF) (VC stream.VC, ln stream.Listener, remoteVC stream.Connector, triggerTimers func()) {
		triggerTimers = vif.SetFakeTimers()
		var err error
		VC, remoteVC, err = createVC(cctx, vf, remote, makeEP(0x10), vc.IdleTimeout{Duration: idleTime})
		if err != nil {
			t.Fatal(err)
		}
		if ln, err = VC.Listen(); err != nil {
			t.Fatal(err)
		}
		return
	}
	newFlow := func(vc stream.VC, remote *vif.VIF) stream.Flow {
		f, err := vc.Connect()
		if err != nil {
			t.Fatal(err)
		}
		acceptFlowAtServer(remote)
		return f
	}

	// No active flow. Should be notified.
	vf, remote := newVIF()
	_, _, _, triggerTimers := newVC(vf, remote)
	triggerTimers()
	if err := vif.WaitForNotifications(notify, vf, remote); err != nil {
		t.Error(err)
	}

	// Same as above, but with multiple VCs.
	vf, remote = newVIF()
	triggerTimers = vif.SetFakeTimers()
	if _, _, err := createNVCs(cctx, vf, remote, 0x10, 5, vc.IdleTimeout{Duration: idleTime}); err != nil {
		t.Fatal(err)
	}
	triggerTimers()
	if err := vif.WaitForNotifications(notify, vf, remote); err != nil {
		t.Error(err)
	}

	// Open one flow. Should not be notified.
	vf, remote = newVIF()
	vc, _, _, triggerTimers := newVC(vf, remote)
	f1 := newFlow(vc, remote)
	triggerTimers()
	if err := vif.WaitWithTimeout(notify, waitTime); err != nil {
		t.Fatal(err)
	}

	// Close the flow. Should be notified.
	f1.Close()
	if err := vif.WaitForNotifications(notify, vf, remote); err != nil {
		t.Error(err)
	}

	// Open two flows.
	vf, remote = newVIF()
	vc, _, _, triggerTimers = newVC(vf, remote)
	f1 = newFlow(vc, remote)
	f2 := newFlow(vc, remote)
	triggerTimers()

	// Close the first flow twice. Should not be notified.
	f1.Close()
	f1.Close()
	if err := vif.WaitWithTimeout(notify, waitTime); err != nil {
		t.Fatal(err)
	}
	// Close the second flow. Should be notified now.
	f2.Close()
	if err := vif.WaitForNotifications(notify, vf, remote); err != nil {
		t.Error(err)
	}

	// Same as above, but open a flow from the remote side.
	vf, remote = newVIF()
	_, ln, remoteVC, triggerTimers := newVC(vf, remote)
	f1, err := remoteVC.Connect()
	if err != nil {
		t.Fatal(err)
	}
	acceptFlowAtClient(ln)
	triggerTimers()
	if err := vif.WaitWithTimeout(notify, waitTime); err != nil {
		t.Fatal(err)
	}
	f1.Close()
	if err := vif.WaitForNotifications(notify, vf, remote); err != nil {
		t.Error(err)
	}
}

func TestIdleTimeout(t *testing.T)       { testIdleTimeout(t, false) }
func TestIdleTimeoutServer(t *testing.T) { testIdleTimeout(t, true) }

func TestShutdownVCs(t *testing.T) {
	ctx, shutdown := test.V23InitSimple()
	defer shutdown()
	pclient := testutil.NewPrincipal("client")
	pserver := testutil.NewPrincipal("server")
	cctx, _ := v23.WithPrincipal(ctx, pclient)
	sctx, _ := v23.WithPrincipal(ctx, pserver)
	client, server := NewClientServer(cctx, sctx)
	defer server.Close()
	defer client.Close()

	testN := func(N int) error {
		c := client.NumVCs()
		if c != N {
			return fmt.Errorf("%d VCs on client VIF, expected %d", c, N)
		}
		return nil
	}

	if _, _, err := createVC(cctx, client, server, makeEP(0x5)); err != nil {
		t.Fatal(err)
	}
	if err := testN(1); err != nil {
		t.Error(err)
	}
	if _, _, err := createVC(cctx, client, server, makeEP(0x5)); err != nil {
		t.Fatal(err)
	}
	if err := testN(2); err != nil {
		t.Error(err)
	}
	if _, _, err := createVC(cctx, client, server, makeEP(0x7)); err != nil {
		t.Fatal(err)
	}
	if err := testN(3); err != nil {
		t.Error(err)
	}
	// Client does not have any VCs to the endpoint with routing id 0x9,
	// so nothing should be closed
	if n := client.ShutdownVCs(makeEP(0x9)); n != 0 {
		t.Errorf("Expected 0 VCs to be closed, closed %d", n)
	}
	if err := testN(3); err != nil {
		t.Error(err)
	}
	// But it does have to 0x5
	if n := client.ShutdownVCs(makeEP(0x5)); n != 2 {
		t.Errorf("Expected 2 VCs to be closed, closed %d", n)
	}
	if err := testN(1); err != nil {
		t.Error(err)
	}
	// And 0x7
	if n := client.ShutdownVCs(makeEP(0x7)); n != 1 {
		t.Errorf("Expected 2 VCs to be closed, closed %d", n)
	}
	if err := testN(0); err != nil {
		t.Error(err)
	}
}

type versionTestCase struct {
	client, server, ep *iversion.Range
	expectError        bool
	expectVIFError     bool
}

func (tc *versionTestCase) Run(t *testing.T) {
	ctx, shutdown := test.V23InitSimple()
	defer shutdown()
	pclient := testutil.NewPrincipal("client")
	pserver := testutil.NewPrincipal("server")
	cctx, _ := v23.WithPrincipal(ctx, pclient)
	sctx, _ := v23.WithPrincipal(ctx, pserver)
	client, server, err := NewVersionedClientServer(tc.client, tc.server, cctx, sctx)
	if (err != nil) != tc.expectVIFError {
		t.Errorf("Error mismatch.  Wanted error: %v, got %v; client: %v, server: %v", tc.expectVIFError, err, tc.client, tc.server)
	}
	if err != nil {
		return
	}
	defer client.Close()

	ep := &inaming.Endpoint{
		Protocol: "test",
		Address:  "addr",
		RID:      naming.FixedRoutingID(0x5),
	}
	clientVC, _, err := createVC(cctx, client, server, ep)
	if (err != nil) != tc.expectError {
		t.Errorf("Error mismatch.  Wanted error: %v, got %v (client:%v, server:%v ep:%v)", tc.expectError, err, tc.client, tc.server, tc.ep)

	}
	if err != nil {
		return
	}

	writer, err := clientVC.Connect()
	if err != nil {
		t.Errorf("Unexpected error on case %+v: %v", tc, err)
		return
	}

	rwSingleFlow(t, writer, acceptFlowAtServer(server), "the dark knight")
}

// TestIncompatibleVersions tests many cases where the client and server
// have compatible or incompatible supported version ranges.  It ensures
// that overlapping ranges work properly, but non-overlapping ranges generate
// errors.
func TestIncompatibleVersions(t *testing.T) {
	unknown := &iversion.Range{Min: version.UnknownRPCVersion, Max: version.UnknownRPCVersion}
	tests := []versionTestCase{
		{&iversion.Range{Min: 2, Max: 2}, &iversion.Range{Min: 2, Max: 2}, &iversion.Range{Min: 2, Max: 2}, false, false},
		{&iversion.Range{Min: 2, Max: 3}, &iversion.Range{Min: 3, Max: 5}, &iversion.Range{Min: 3, Max: 5}, false, false},
		{&iversion.Range{Min: 2, Max: 3}, &iversion.Range{Min: 3, Max: 5}, unknown, false, false},

		// VIF error since there are no versions in common.
		{&iversion.Range{Min: 2, Max: 3}, &iversion.Range{Min: 4, Max: 5}, &iversion.Range{Min: 4, Max: 5}, true, true},
		{&iversion.Range{Min: 2, Max: 3}, &iversion.Range{Min: 4, Max: 5}, unknown, true, true},
	}

	for _, tc := range tests {
		tc.Run(t)
	}
}

func TestNetworkFailure(t *testing.T) {
	ctx, shutdown := test.V23InitSimple()
	defer shutdown()
	pclient := testutil.NewPrincipal("client")
	pserver := testutil.NewPrincipal("server")
	cctx, _ := v23.WithPrincipal(ctx, pclient)
	sctx, _ := v23.WithPrincipal(ctx, pserver)
	c1, c2 := pipe()
	result := make(chan *vif.VIF)
	closed := make(chan struct{})
	go func() {
		client, err := vif.InternalNewDialedVIF(sctx, c1, naming.FixedRoutingID(0xc), nil, func(vf *vif.VIF) { close(closed) })
		if err != nil {
			t.Fatal(err)
		}
		result <- client
	}()
	server, err := vif.InternalNewAcceptedVIF(sctx, c2, naming.FixedRoutingID(0x5), pserver.BlessingStore().Default(), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	client := <-result
	// If the network connection dies, Dial and Accept should fail.
	c1.Close()
	// Wait until the VIF is closed, since Dial() may run before the underlying VC is closed.
	<-closed
	if _, err := client.Dial(cctx, makeEP(0x5)); err == nil {
		t.Errorf("Expected client.Dial to fail")
	}
	if _, err := server.Accept(); err == nil {
		t.Errorf("Expected server.Accept to fail")
	}
}

func TestPreAuthentication(t *testing.T) {
	ctx, shutdown := test.V23InitSimple()
	defer shutdown()
	pclient := testutil.NewPrincipal("client")
	pserver := testutil.NewPrincipal("server")
	cctx, _ := v23.WithPrincipal(ctx, pclient)
	sctx, _ := v23.WithPrincipal(ctx, pserver)
	client, server := NewClientServer(cctx, sctx)
	defer client.Close()

	check := func(numVCs, numPreAuth uint) error {
		stats := client.Stats()
		if stats.NumDialedVCs != numVCs {
			return fmt.Errorf("Unexpected NumDialedVCs in client; got %d want %d", stats.NumDialedVCs, numVCs)
		}
		if stats.NumPreAuthenticated != numPreAuth {
			return fmt.Errorf("Unexpected NumPreAuthenticated in client; got %d want %d", stats.NumPreAuthenticated, numPreAuth)
		}
		stats = server.Stats()
		if stats.NumAcceptedVCs != numVCs {
			return fmt.Errorf("Unexpected NumAcceptedVCs in server; got %d want %d", stats.NumAcceptedVCs, numVCs)
		}
		if stats.NumPreAuthenticated != numPreAuth {
			return fmt.Errorf("Unexpected PreAuthUsed in server; got %d want %d", stats.NumPreAuthenticated, numPreAuth)
		}
		return nil
	}

	// Use a different routing ID. Should not use pre-auth.
	_, _, err := createVC(cctx, client, server, makeEP(0x55))
	if err != nil {
		t.Fatal(err)
	}
	if err := check(1, 0); err != nil {
		t.Error(err)
	}

	// Use the same routing ID. Should use pre-auth.
	_, _, err = createVC(cctx, client, server, makeEP(0x5))
	if err != nil {
		t.Fatal(err)
	}
	if err := check(2, 1); err != nil {
		t.Error(err)
	}

	// Use the null routing ID. Should use VIF pre-auth.
	_, _, err = createVC(cctx, client, server, makeEP(0x0))
	if err != nil {
		t.Fatal(err)
	}
	if err := check(3, 2); err != nil {
		t.Error(err)
	}

	// Use a different principal. Should not use pre-auth.
	nctx, _ := v23.WithPrincipal(ctx, testutil.NewPrincipal("client2"))
	_, _, err = createVC(nctx, client, server, makeEP(0x5))
	if err != nil {
		t.Fatal(err)
	}
	if err := check(4, 2); err != nil {
		t.Error(err)
	}
}

func makeEP(rid uint64) naming.Endpoint {
	return &inaming.Endpoint{
		Protocol: "test",
		Address:  "addr",
		RID:      naming.FixedRoutingID(rid),
	}
}

// pipeAddr provides a more descriptive String implementation than provided by net.Pipe.
type pipeAddr struct{ name string }

func (a pipeAddr) Network() string { return "pipe" }
func (a pipeAddr) String() string  { return a.name }

// pipeConn provides a buffered net.Conn, with pipeAddr addressing.
type pipeConn struct {
	lock sync.Mutex
	// w is guarded by lock, to prevent Close from racing with Write.  This is a
	// quick way to prevent the race, but it allows a Write to block the Close.
	// This isn't a problem in the tests currently.
	w            chan<- []byte
	r            <-chan []byte
	rdata        []byte
	laddr, raddr pipeAddr
}

func (c *pipeConn) Read(data []byte) (int, error) {
	for len(c.rdata) == 0 {
		d, ok := <-c.r
		if !ok {
			return 0, io.EOF
		}
		c.rdata = d
	}
	n := copy(data, c.rdata)
	c.rdata = c.rdata[n:]
	return n, nil
}

func (c *pipeConn) Write(data []byte) (int, error) {
	c.lock.Lock()
	defer c.lock.Unlock()
	if c.w == nil {
		return 0, io.EOF
	}
	d := make([]byte, len(data))
	copy(d, data)
	c.w <- d
	return len(data), nil
}

func (c *pipeConn) Close() error {
	c.lock.Lock()
	defer c.lock.Unlock()
	if c.w == nil {
		return io.EOF
	}
	close(c.w)
	c.w = nil
	return nil
}

func (c *pipeConn) LocalAddr() net.Addr                { return c.laddr }
func (c *pipeConn) RemoteAddr() net.Addr               { return c.raddr }
func (c *pipeConn) SetDeadline(t time.Time) error      { return nil }
func (c *pipeConn) SetReadDeadline(t time.Time) error  { return nil }
func (c *pipeConn) SetWriteDeadline(t time.Time) error { return nil }

func pipe() (net.Conn, net.Conn) {
	clientAddr := pipeAddr{"client"}
	serverAddr := pipeAddr{"server"}
	c1 := make(chan []byte, 10)
	c2 := make(chan []byte, 10)
	p1 := &pipeConn{w: c1, r: c2, laddr: clientAddr, raddr: serverAddr}
	p2 := &pipeConn{w: c2, r: c1, laddr: serverAddr, raddr: clientAddr}
	return p1, p2
}

func NewClientServer(cctx, sctx *context.T) (client, server *vif.VIF) {
	var err error
	client, server, err = New(nil, nil, cctx, sctx, nil, nil, nil, nil)
	if err != nil {
		panic(err)
	}
	return
}

func NewVersionedClientServer(clientVersions, serverVersions *iversion.Range, cctx, sctx *context.T) (client, server *vif.VIF, verr error) {
	return New(clientVersions, serverVersions, cctx, sctx, nil, nil, nil, nil)
}

func New(clientVersions, serverVersions *iversion.Range, cctx, sctx *context.T, clientOnClose, serverOnClose func(*vif.VIF), opts []stream.VCOpt, lopts []stream.ListenerOpt) (client, server *vif.VIF, verr error) {
	c1, c2 := pipe()
	var cerr error
	cl := make(chan *vif.VIF)
	go func() {
		c, err := vif.InternalNewDialedVIF(cctx, c1, naming.FixedRoutingID(0xc), clientVersions, clientOnClose, opts...)
		if err != nil {
			cerr = err
			close(cl)
		} else {
			cl <- c
		}
	}()
	blessings := v23.GetPrincipal(sctx).BlessingStore().Default()
	s, err := vif.InternalNewAcceptedVIF(sctx, c2, naming.FixedRoutingID(0x5), blessings, serverVersions, serverOnClose, lopts...)
	c, ok := <-cl
	if err != nil {
		verr = err
		return
	}
	if !ok {
		verr = cerr
		return
	}
	server = s
	client = c
	return
}

// rwSingleFlow writes out data on writer and ensures that the reader sees the same string.
func rwSingleFlow(t *testing.T, writer io.WriteCloser, reader io.Reader, data string) {
	go func() {
		if n, err := writer.Write([]byte(data)); n != len(data) || err != nil {
			t.Errorf("Write failure. Got (%d, %v) want (%d, nil)", n, err, len(data))
		}
		writer.Close()
	}()

	var buf bytes.Buffer
	var tmp [4096]byte
	for {
		n, err := reader.Read(tmp[:])
		buf.Write(tmp[:n])
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Errorf("Read error: %v", err)
		}
	}
	if buf.String() != data {
		t.Errorf("Wrote %q but read %q", data, buf.String())
	}
}

// createVC creates a VC by dialing from the client process to the server
// process.  It returns the VC at the client and the Connector at the server
// (which the server can use to create flows over the VC)).
func createVC(ctx *context.T, client, server *vif.VIF, ep naming.Endpoint, opts ...stream.VCOpt) (clientVC stream.VC, serverConnector stream.Connector, err error) {
	vcChan := make(chan stream.VC)
	scChan := make(chan stream.Connector)
	errChan := make(chan error)
	go func() {
		vc, err := client.Dial(ctx, ep, opts...)
		errChan <- err
		vcChan <- vc
	}()
	go func() {
		cAndf, err := server.Accept()
		errChan <- err
		if err == nil {
			scChan <- cAndf.Connector
		}
	}()
	if err = <-errChan; err != nil {
		return
	}
	if err = <-errChan; err != nil {
		return
	}
	clientVC = <-vcChan
	serverConnector = <-scChan
	return
}

func createNVCs(ctx *context.T, client, server *vif.VIF, startRID uint64, N int, opts ...stream.VCOpt) (clientVCs []stream.VC, serverConnectors []stream.Connector, err error) {
	var c stream.VC
	var s stream.Connector
	for i := 0; i < N; i++ {
		c, s, err = createVC(ctx, client, server, makeEP(startRID+uint64(i)), opts...)
		if err != nil {
			return
		}
		clientVCs = append(clientVCs, c)
		serverConnectors = append(serverConnectors, s)
	}
	return
}

func createListeners(vcs []stream.VC) ([]stream.Listener, error) {
	var ret []stream.Listener
	for _, vc := range vcs {
		ln, err := vc.Listen()
		if err != nil {
			return nil, err
		}
		ret = append(ret, ln)
	}
	return ret, nil
}

func acceptFlowAtServer(vf *vif.VIF) stream.Flow {
	for {
		cAndf, err := vf.Accept()
		if err != nil {
			panic(err)
		}
		if cAndf.Flow != nil {
			return cAndf.Flow
		}
	}
}

func acceptFlowAtClient(ln stream.Listener) stream.Flow {
	f, err := ln.Accept()
	if err != nil {
		panic(err)
	}
	return f
}

func atmostNbytes(s string, n int) string {
	if n > len(s) {
		return s
	}
	b := []byte(s)
	return string(b[:n/2]) + "..." + string(b[len(s)-n/2:])
}
