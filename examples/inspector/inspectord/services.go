package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"veyron2/ipc"
	"veyron2/security"

	"veyron/examples/inspector"
)

// There are three service types: files, /proc and /dev.
type serviceType int

const (
	fileSvc serviceType = iota
	procSvc
	deviceSvc
)

// Dispatchers create servers, based on the names used. Dispatchers
// create stubbed or stubless servers.
type dispatcher struct {
	service serviceType
	stubbed bool
}

// A service represents one of the file, proc or device service
type server struct {
	service      serviceType
	root, suffix string
}

// ServerInterface defines the common methods that both stubbed and stubless
// implementations must provided.
type serverInterface interface {
	send(fi os.FileInfo, details bool) error
	cancelled() bool
}

type stublessServer struct {
	call ipc.ServerCall
}

func (s *stublessServer) send(fi os.FileInfo, details bool) error {
	if !details {
		return s.call.Send(fi.Name())
	}
	d := struct {
		Name    string
		Size    int64
		Mode    os.FileMode
		ModTime time.Time
		IsDir   bool
	}{
		Name:    fi.Name(),
		Size:    fi.Size(),
		Mode:    fi.Mode(),
		ModTime: fi.ModTime(),
		IsDir:   fi.IsDir(),
	}
	return s.call.Send(d)
}

func (s *stublessServer) cancelled() bool {
	return s.call.IsClosed()
}

type stubbedServer struct {
	context ipc.ServerContext
	names   inspector.InspectorServiceLsStream
	details inspector.InspectorServiceLsDetailsStream
}

func (s *stubbedServer) send(fi os.FileInfo, details bool) error {
	if !details {
		return s.names.Send(fi.Name())
	}
	return s.details.Send(inspector.Details{
		Name:        fi.Name(),
		Size:        fi.Size(),
		Mode:        uint32(fi.Mode()),
		ModUnixSecs: fi.ModTime().Unix(),
		ModNano:     int32(fi.ModTime().Nanosecond()),
		IsDir:       fi.IsDir(),
	})
}

func (s *stubbedServer) cancelled() bool {
	return s.context.IsClosed()
}

// ls is shared by both stubbed and stubless calls and contains
// the bulk of the actual server code.
func (s *server) ls(glob string, details bool, impl serverInterface) error {
	// validate the glob pattern
	if _, err := filepath.Match(glob, ""); err != nil {
		return err
	}
	ch := make(chan []os.FileInfo, 10)
	errch := make(chan error, 1)
	go readdir(filepath.Join(s.root, s.suffix), glob, ch, errch)
	for {
		select {
		case fs := <-ch:
			if len(fs) == 0 {
				return nil
			}
			for _, f := range fs {
				if err := impl.send(f, details); err != nil {
					return err
				}
			}
		case err := <-errch:
			// Flush any buffered data in the data channel when
			// an error is encountered.
			for fs := range ch {
				for _, f := range fs {
					impl.send(f, details)
				}
			}
			return err
		case <-time.After(time.Second):
			return fmt.Errorf("timed out reading info")
		}
		if impl.cancelled() {
			// TODO(cnicolaou): we most likely need to flush
			// and clear channels here, otherwise we're leaking
			// goroutines and channels.
			// TODO(cnicolaou): test this...
			return fmt.Errorf("cancelled")
		}
	}
	return nil
}

// List is a stubless server method
func (s *server) List(call ipc.ServerCall, glob string, details bool) error {
	log.Infof("List: %q details %t", glob, details)
	return s.ls(glob, details, &stublessServer{call})
}

// Ls is a stubbed server method
func (s *server) Ls(context ipc.ServerContext, Glob string, Stream inspector.InspectorServiceLsStream) error {
	log.Infof("Ls %q", Glob)
	return s.ls(Glob, false, &stubbedServer{context: context, names: Stream})
}

// LsDetails is a stubbed server method
func (s *server) LsDetails(context ipc.ServerContext, Glob string, Stream inspector.InspectorServiceLsDetailsStream) error {
	log.Infof("LsDetails %q", Glob)
	return s.ls(Glob, true, &stubbedServer{context: context, details: Stream})
}

func (d *dispatcher) Lookup(suffix string) (ipc.Invoker, security.Authorizer, error) {
	s := &server{service: d.service, suffix: suffix}
	switch d.service {
	case fileSvc:
		cwd, err := os.Getwd()
		if err != nil {
			return nil, nil, err
		}
		s.root = cwd
	case deviceSvc:
		s.root = "/dev"
	case procSvc:
		s.root = "/proc"
	}
	if d.stubbed {
		return ipc.ReflectInvoker(inspector.NewServerInspector(s)), nil, nil
	} else {
		return ipc.ReflectInvoker(s), nil, nil
	}
}

func NewFileSvc(stubbed bool) ipc.Dispatcher {
	return &dispatcher{service: fileSvc, stubbed: stubbed}
}

func NewProcSvc(stubbed bool) ipc.Dispatcher {
	return &dispatcher{service: procSvc, stubbed: stubbed}
}

func NewDeviceSvc(stubbed bool) ipc.Dispatcher {
	return &dispatcher{service: deviceSvc, stubbed: stubbed}
}

func readdir(dirname, glob string, ch chan []os.FileInfo, errch chan error) {
	defer close(ch)
	defer close(errch)
	dir, err := os.Open(dirname)
	if err != nil {
		errch <- err
		return
	}
	n := cap(ch)
	for {
		entries, err := dir.Readdir(n)
		if err != nil && err != io.EOF {
			errch <- err
			return
		}
		if len(glob) == 0 {
			ch <- entries
		} else {
			matches := make([]os.FileInfo, 0, len(entries))
			for _, e := range entries {
				if m, _ := filepath.Match(glob, e.Name()); m {
					matches = append(matches, e)
				}
			}
			if len(matches) > 0 {
				ch <- matches
			}
		}
		if err == io.EOF {
			return
		}
	}
}
