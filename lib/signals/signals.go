package signals

import (
	"os"
	"os/signal"
	"syscall"

	"veyron.io/veyron/veyron2/rt"
)

type stopSignal string

func (stopSignal) Signal()          {}
func (s stopSignal) String() string { return string(s) }

const (
	STOP               = stopSignal("")
	DoubleStopExitCode = 1
)

// defaultSignals returns a set of platform-specific signals that an application
// is encouraged to listen on.
func defaultSignals() []os.Signal {
	return []os.Signal{syscall.SIGTERM, syscall.SIGINT, STOP}
}

// TODO(caprita): Rename this to Shutdown() and the package to shutdown since
// it's not just signals anymore.

// ShutdownOnSignals registers signal handlers for the specified signals, or, if
// none are specified, the default signals.  The first signal received will be
// made available on the returned channel; upon receiving a second signal, the
// process will exit.
func ShutdownOnSignals(signals ...os.Signal) <-chan os.Signal {
	if len(signals) == 0 {
		signals = defaultSignals()
	}
	// At least a buffer of length two so that we don't drop the first two
	// signals we get on account of the channel being full.
	ch := make(chan os.Signal, 2)
	sawStop := false
	var signalsNoStop []os.Signal
	for _, s := range signals {
		switch s {
		case STOP:
			if !sawStop {
				sawStop = true
				if r := rt.R(); r != nil {
					stopWaiter := make(chan string, 1)
					r.WaitForStop(stopWaiter)
					go func() {
						for {
							ch <- stopSignal(<-stopWaiter)
						}
					}()
				}
			}
		default:
			signalsNoStop = append(signalsNoStop, s)
		}
	}
	if len(signalsNoStop) > 0 {
		signal.Notify(ch, signalsNoStop...)
	}
	// At least a buffer of length one so that we don't block on ret <- sig.
	ret := make(chan os.Signal, 1)
	go func() {
		// First signal received.
		sig := <-ch
		ret <- sig
		// Wait for a second signal, and force an exit if the process is
		// still executing cleanup code.
		<-ch
		os.Exit(DoubleStopExitCode)
	}()
	return ret
}
