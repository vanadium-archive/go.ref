package rt

import (
	"os"
	"os/signal"
	"syscall"
	"veyron2/vlog"
)

func (r *vrt) initSignalHandling() {
	// Automatically handle SIGHUP to prevent applications started as
	// daemons from being killed.  The developer can choose to still listen
	// on SIGHUP and take a different action if desired.
	r.signals = make(chan os.Signal, 1)
	signal.Notify(r.signals, syscall.SIGHUP)
	go func() {
		for {
			vlog.Infof("Received signal %v", <-r.signals)
		}
	}()
}

func (r *vrt) shutdownSignalHandling() {
	signal.Stop(r.signals)
}
