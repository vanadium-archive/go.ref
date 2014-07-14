package main

import (
	"errors"
	"flag"
	"fmt"
	"log"
	"time"

	"veyron/lib/signals"
	isecurity "veyron/services/security"
	"veyron2/ipc"
	"veyron2/rt"
	"veyron2/security"
	"veyron2/vdl/vdlutil"
)

// TODO(ataly, andreser): ideally, the expiration time (and other caveats) of
// the discharge would be determined by a function much like ThirdPartyCaveat.Validate
var expiration = flag.String("expiration", "10s", "time interval after which a discharge will expire")
var protocol = flag.String("protocol", "bluetooth", "protocol to listen on")
var address = flag.String("address", "", "address to listen on")
var port = flag.Int("port", 0, "port to listen on")
var publish = flag.String("publish", "", "the namespace where to publish this service")

type dischargeAuthorizer struct{}

func (dischargeAuthorizer) Authorize(c security.Context) error {
	if c.Method() == "Discharge" {
		return nil
	}
	return fmt.Errorf("Only authorized for method \"Discharge\"")
}

// discharged implements ipc.Dispatcher. It issues discharges for all caveats
// present in the current namespace with no additional caveats iff the caveat
// is valid.
type discharged struct {
	id         security.PrivateID
	expiration time.Duration
}

func (d *discharged) Discharge(ctx ipc.ServerContext, Caveat vdlutil.Any) (
	Discharge vdlutil.Any, err error) {
	caveat, ok := Caveat.(security.ThirdPartyCaveat)
	if !ok {
		err = errors.New("unknown caveat")
		return
	}
	return d.id.MintDischarge(caveat, ctx, d.expiration, nil)
}

func main() {
	flag.Parse()
	expiration, err := time.ParseDuration(*expiration)
	if err != nil {
		log.Fatalf("--expiration: ", err)
	}

	r := rt.Init()
	server, err := r.NewServer()
	if err != nil {
		log.Fatal(err)
	}

	discharger := isecurity.NewServerDischarger(&discharged{
		id: r.Identity(), expiration: expiration})
	dispatcher := ipc.SoloDispatcher(discharger, dischargeAuthorizer{})
	endpoint, err := server.Listen(*protocol, *address+":"+fmt.Sprint(*port))
	if err != nil {
		log.Fatal(err)
	}
	if err := server.Serve(*publish, dispatcher); err != nil {
		log.Fatal(err)
	}
	fmt.Println(endpoint)
	<-signals.ShutdownOnSignals()
}
