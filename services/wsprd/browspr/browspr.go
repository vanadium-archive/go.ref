// Browspr is the browser version of WSPR, intended to communicate with javascript through postMessage.
package browspr

import (
	"fmt"
	"net"
	"regexp"
	"time"

	"veyron.io/veyron/veyron2"
	"veyron.io/veyron/veyron2/ipc"
	"veyron.io/veyron/veyron2/naming"
	"veyron.io/veyron/veyron2/rt"
	"veyron.io/veyron/veyron2/vlog"
	"veyron.io/wspr/veyron/services/wsprd/account"
	"veyron.io/wspr/veyron/services/wsprd/principal"
)

// Browspr is an intermediary between our javascript code and the veyron network that allows our javascript library to use veyron.
type Browspr struct {
	rt             veyron2.Runtime
	profileFactory func() veyron2.Profile
	listenSpec     ipc.ListenSpec
	identdEP       string
	namespaceRoots []string
	logger         vlog.Logger
	accountManager *account.AccountManager
	postMessage    func(instanceId int32, ty, msg string)

	activeInstances map[int32]*pipe
}

// Create a new Browspr instance.
func NewBrowspr(postMessage func(instanceId int32, ty, msg string), profileFactory func() veyron2.Profile, listenSpec ipc.ListenSpec, identdEP string, namespaceRoots []string, opts ...veyron2.ROpt) *Browspr {
	if listenSpec.Proxy == "" {
		vlog.Fatalf("a veyron proxy must be set")
	}
	if identdEP == "" {
		vlog.Fatalf("an identd server must be set")
	}

	runtime, err := rt.New(opts...)
	if err != nil {
		vlog.Fatalf("rt.New failed: %s", err)
	}

	wsNamespaceRoots, err := wsNames(namespaceRoots)
	if err != nil {
		vlog.Fatal(err)
	}

	runtime.Namespace().SetRoots(wsNamespaceRoots...)

	browspr := &Browspr{
		profileFactory:  profileFactory,
		listenSpec:      listenSpec,
		identdEP:        identdEP,
		namespaceRoots:  wsNamespaceRoots,
		postMessage:     postMessage,
		rt:              runtime,
		logger:          runtime.Logger(),
		activeInstances: make(map[int32]*pipe),
	}

	// TODO(nlacasse, bjornick) use a serializer that can actually persist.
	var principalManager *principal.PrincipalManager
	if principalManager, err = principal.NewPrincipalManager(runtime.Principal(), &principal.InMemorySerializer{}); err != nil {
		vlog.Fatalf("principal.NewPrincipalManager failed: %s", err)
	}

	browspr.accountManager = account.NewAccountManager(runtime, identdEP, principalManager)

	return browspr
}

func (browspr *Browspr) Shutdown() {
	browspr.rt.Cleanup()
}

// tcpKeepAliveListener sets TCP keep-alive timeouts on accepted connections.
// It's used by ListenAndServe and ListenAndServeTLS so dead TCP connections
// (e.g. closing laptop mid-download) eventually go away.
// Copied from http/server.go, since it's not exported.
type tcpKeepAliveListener struct {
	*net.TCPListener
}

func (ln tcpKeepAliveListener) Accept() (c net.Conn, err error) {
	tc, err := ln.AcceptTCP()
	if err != nil {
		return
	}
	tc.SetKeepAlive(true)
	tc.SetKeepAlivePeriod(3 * time.Minute)
	return tc, nil
}

// HandleMessage handles most messages from javascript and forwards them to a
// Controller.
func (b *Browspr) HandleMessage(instanceId int32, msg string) error {
	instance, ok := b.activeInstances[instanceId]
	if !ok {
		instance = newPipe(b, instanceId)
		b.activeInstances[instanceId] = instance
	}

	return instance.handleMessage(msg)
}

// HandleCleanupMessage cleans up the specified instance state. (For instance,
// when a browser tab is closed)
func (b *Browspr) HandleCleanupMessage(instanceId int32) {
	if instance, ok := b.activeInstances[instanceId]; ok {
		instance.cleanup()
		delete(b.activeInstances, instanceId)
	}
}

// HandleCreateAccountMessage creates an account for the specified instance.
func (b *Browspr) HandleCreateAccountMessage(instanceId int32, accessToken string) error {
	account, err := b.accountManager.CreateAccount(accessToken)
	if err != nil {
		b.postMessage(instanceId, "createAccountFailedResponse", err.Error())
		return err
	}

	b.postMessage(instanceId, "createAccountResponse", account)
	return nil
}

// HandleAssociateAccountMessage associates an account with the specified origin.
func (b *Browspr) HandleAssociateAccountMessage(origin, account string, cavs []account.Caveat) error {
	if err := b.accountManager.AssociateAccount(origin, account, cavs); err != nil {
		return err
	}
	return nil
}

// Turns a list of names into a list of names that use the "ws" protocol.
func wsNames(names []string) ([]string, error) {
	runtime, err := rt.New()
	if err != nil {
		return nil, fmt.Errorf("rt.New() failed: %v", err)
	}
	outNames := []string{}
	tcpRegexp := regexp.MustCompile(`@tcp\d*@`)
	for _, name := range names {
		addr, suff := naming.SplitAddressName(name)
		ep, err := runtime.NewEndpoint(addr)
		if err != nil {
			return nil, fmt.Errorf("runtime.NewEndpoint(%v) failed: %v", addr, err)
		}
		// Replace only the first match.
		first := true
		wsEp := tcpRegexp.ReplaceAllFunc([]byte(ep.String()), func(s []byte) []byte {
			if first {
				first = false
				return []byte("@ws@")
			}
			return s
		})
		wsName := naming.JoinAddressName(string(wsEp), suff)

		outNames = append(outNames, wsName)
	}
	return outNames, nil
}
