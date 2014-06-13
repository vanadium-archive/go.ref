// Pipe To Browser client.
// This executable pipes its sdtin to p2b service running in the browser
package main

import (
	"flag"
	"fmt"
	"io"
	"os"

	"veyron2/rt"

	"veyron/examples/pipetobrowser"
)

const usage = `
%s is a Pipe To Browser client. It allows one to pipe any stdout stream from console to the browser.
Data being piped to the browser then is displayed in a graphical and formatted way by a "viewer".

Usage:

  %s [<name>/<viewer>]

  For example:

	ls -l | p2b pipetobrowser/jane/DataTable

  where <name> (pipetobrowser/jane) is the veyron name where p2b
  service is running in the browser. <viewer> (DataTable) specifies what
  viewer should be used to display the data.

`

func Usage() {
	fmt.Fprintf(os.Stdout, usage, os.Args[0], os.Args[0])
}

// viewerPipeStreamWriter adapts ViewerPipeStream to io.Writer
type viewerPipeStreamWriter struct {
	pipetobrowser.ViewerPipeStream
}

func (w viewerPipeStreamWriter) Write(p []byte) (n int, err error) {
	w.Send(p)
	return len(p), nil
}

func main() {
	flag.Usage = Usage
	runtime := rt.Init()
	log := runtime.Logger()

	if flag.NArg() != 1 {
		Usage()
		return
	}

	name := flag.Arg(0)

	// bind to the p2b service
	s, err := pipetobrowser.BindViewer(name)
	if err != nil {
		log.Errorf("error binding to server: %v", err)
		return
	}

	stream, err := s.Pipe(runtime.NewContext())
	if err != nil {
		log.Errorf("failed to pipe to '%s' please ensure p2b service is running in the browser and name is correct.\nERR:%v", name, err)
		return
	}

	w := viewerPipeStreamWriter{
		stream,
	}

	numBytes, err := io.Copy(w, os.Stdin)
	if err != nil {
		log.Errorf("failed to copy the stdin pipe to the outgoing stream\nERR:%v", err)
		return
	}

	stream.CloseSend()
	result, err := stream.Finish()
	if err != nil {
		log.Errorf("error finishing stream: %v", err)
		return
	}

	log.Infof("Stream finished with status: %v", result)
	log.Infof("Total of %d bytes were piped to browser", numBytes)

	fmt.Println("Finished piping to browser! Thanks for using p2b.")
}
