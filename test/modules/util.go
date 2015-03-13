package modules

import (
	"fmt"
	"hash/adler32"
	"io"
	"io/ioutil"
	"os"
	"strings"

	vsecurity "v.io/x/ref/security"

	"v.io/v23/security"
	"v.io/x/lib/vlog"
)

func newLogfile(prefix, name string) (*os.File, error) {
	nameHash := adler32.Checksum([]byte(name))
	f, err := ioutil.TempFile("", fmt.Sprintf("__modules__%s-%x-", prefix, nameHash))
	if err != nil {
		return nil, err
	}
	return f, nil
}

func outputFromFile(f *os.File, out io.Writer) {
	f.Close()
	fName := f.Name()
	defer os.Remove(fName)
	if out == nil {
		return
	}
	var err error
	if f, err = os.Open(fName); err != nil {
		vlog.VI(1).Infof("failed to open %q: %s\n", fName, err)
		return
	}
	io.Copy(out, f)
	f.Close()
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

func principalFromDir(dir string) (security.Principal, error) {
	p, err := vsecurity.LoadPersistentPrincipal(dir, nil)
	if err == nil {
		return p, nil
	}
	if !os.IsNotExist(err) {
		return nil, err
	}
	p, err = vsecurity.CreatePersistentPrincipal(dir, nil)
	if err != nil {
		return nil, err
	}
	if err := vsecurity.InitDefaultBlessings(p, shellBlessingExtension); err != nil {
		return nil, err
	}
	return p, nil
}
