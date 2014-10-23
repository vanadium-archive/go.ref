package modules

import (
	"bufio"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"strings"
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

// envSliceToMap returns a map representation of a string slive
// of environment variables.
func envSliceToMap(env []string) map[string]string {
	m := make(map[string]string)
	if env == nil {
		return m
	}
	for _, osv := range env {
		if len(osv) == 0 {
			continue
		}
		parts := strings.SplitN(osv, "=", 2)
		key := parts[0]
		if len(parts) == 2 {
			m[key] = parts[1]
		} else {
			m[key] = ""
		}
	}
	return m
}

// mergeMaps merges two maps, a & b, with b taking preference over a.
func mergeMaps(a, b map[string]string) map[string]string {
	if len(b) == 0 {
		return a
	}
	merged := make(map[string]string)
	for k, v := range a {
		merged[k] = v
	}
	for k, v := range b {
		merged[k] = v
	}
	return merged
}
