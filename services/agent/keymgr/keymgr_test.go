// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package keymgr

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"v.io/v23/security"
	"v.io/v23/verror"
	"v.io/x/ref/services/agent"
	"v.io/x/ref/services/agent/agentlib"
	"v.io/x/ref/services/agent/internal/ipc"
	"v.io/x/ref/services/agent/internal/server"

	_ "v.io/x/ref/runtime/factories/generic"
)

func createAgent(path string) (agent.KeyManager, func(), error) {
	var defers []func()
	cleanup := func() {
		for _, f := range defers {
			f()
		}
	}
	i := ipc.NewIPC()
	if err := server.ServeKeyManager(i, path, nil); err != nil {
		return nil, cleanup, err
	}
	defers = append(defers, func() { os.RemoveAll(path) })
	sock := filepath.Join(path, "keymgr.sock")
	if err := i.Listen(sock); err != nil {
		return nil, cleanup, err
	}
	defers = append(defers, i.Close)

	m, err := NewKeyManager(sock)
	return m, cleanup, err
}

func TestNoKeyPath(t *testing.T) {
	agent, cleanup, err := createAgent("")
	defer cleanup()
	if err == nil {
		t.Fatal("An error should be returned when the key path is empty")
	}
	if agent != nil {
		t.Fatal("No agent should be created when key path is empty")
	}
}

func createClient(deviceAgent agent.KeyManager, id [64]byte) (security.Principal, error) {
	dir, err := ioutil.TempDir("", "conn")
	if err != nil {
		return nil, err
	}
	path := filepath.Join(dir, "sock")
	if err := deviceAgent.ServePrincipal(id, path); err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	return agentlib.NewAgentPrincipalX(path)
}

func TestSigning(t *testing.T) {
	path, err := ioutil.TempDir("", "agent")
	if err != nil {
		t.Fatal(err)
	}
	agent, cleanup, err := createAgent(path)
	defer cleanup()
	if err != nil {
		t.Fatal(err)
	}

	id1, err := agent.NewPrincipal(false)
	if err != nil {
		t.Fatal(err)
	}
	id2, err := agent.NewPrincipal(false)
	if err != nil {
		t.Fatal(err)
	}

	dir, err := os.Open(filepath.Join(path, "keys"))
	if err != nil {
		t.Fatal(err)
	}
	files, err := dir.Readdir(-1)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 2 {
		t.Errorf("Expected 2 files created, found %d", len(files))
	}

	a, err := createClient(agent, id1)
	if err != nil {
		t.Fatal(err)
	}
	// Serving again should be an error
	if _, err := createClient(agent, id1); verror.ErrorID(err) != verror.ErrExist.ID {
		t.Fatalf("Expected ErrExist, got %v", err)
	}

	b, err := createClient(agent, id2)
	if err != nil {
		t.Fatal(err)
	}
	if reflect.DeepEqual(a.PublicKey(), b.PublicKey()) {
		t.Fatal("Keys should not be equal")
	}
	sig1, err := a.Sign([]byte("foobar"))
	if err != nil {
		t.Fatal(err)
	}
	sig2, err := b.Sign([]byte("foobar"))
	if err != nil {
		t.Fatal(err)
	}
	if !sig1.Verify(a.PublicKey(), []byte("foobar")) {
		t.Errorf("Signature a fails verification")
	}
	if !sig2.Verify(b.PublicKey(), []byte("foobar")) {
		t.Errorf("Signature b fails verification")
	}
	if sig2.Verify(a.PublicKey(), []byte("foobar")) {
		t.Errorf("Signatures should not cross verify")
	}
}

func TestInMemorySigning(t *testing.T) {
	path, err := ioutil.TempDir("", "agent")
	if err != nil {
		t.Fatal(err)
	}
	agent, cleanup, err := createAgent(path)
	defer cleanup()
	if err != nil {
		t.Fatal(err)
	}

	id, err := agent.NewPrincipal(true)
	if err != nil {
		t.Fatal(err)
	}

	dir, err := os.Open(filepath.Join(path, "keys"))
	if err != nil {
		t.Fatal(err)
	}
	files, err := dir.Readdir(-1)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 0 {
		t.Errorf("Expected 0 files created, found %d", len(files))
	}

	c, err := createClient(agent, id)
	if err != nil {
		t.Fatal(err)
	}
	sig, err := c.Sign([]byte("foobar"))
	if err != nil {
		t.Fatal(err)
	}
	if !sig.Verify(c.PublicKey(), []byte("foobar")) {
		t.Errorf("Signature a fails verification")
	}
}
