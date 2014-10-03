package modules

import (
	"bufio"
	"fmt"
	"io"
	"io/ioutil"
	"os"
)

func newLogfile(prefix string) (*os.File, error) {
	f, err := ioutil.TempFile("", "__modules__"+prefix)
	if err != nil {
		return nil, err
	}
	return f, nil
}

func readTo(r io.Reader, w io.Writer) {
	if w == nil {
		return
	}
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		fmt.Fprintf(w, "%s\n", scanner.Text())
	}
}
