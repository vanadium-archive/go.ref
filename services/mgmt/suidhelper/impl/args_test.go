package impl

import (
	"flag"
	"fmt"
	"reflect"
	"testing"
)

func TestParseArguments(t *testing.T) {
	cases := []struct {
		cmdline  []string
		env      []string
		err      error
		expected WorkParameters
	}{

		{
			[]string{"setuidhelper"},
			[]string{},
			fmt.Errorf("--username missing"),
			WorkParameters{},
		},

		{
			[]string{"setuidhelper", "--username", testUserName},
			[]string{"A=B"},
			nil,
			WorkParameters{
				uid:       testUidString,
				gid:       testGidString,
				workspace: "",
				stderrLog: "",
				stdoutLog: "",
				argv0:     "",
				argv:      []string{},
				envv:      []string{"A=B"},
			},
		},

		{
			[]string{"setuidhelper", "--username", testUserName, "--workspace", "/hello",
				"--stdoutlog", "/stdout", "--stderrlog", "/stderr", "--run", "/bin/veyron", "--", "one", "two"},
			[]string{"A=B"},
			nil,
			WorkParameters{
				uid:       testUidString,
				gid:       testGidString,
				workspace: "/hello",
				stderrLog: "/stderr",
				stdoutLog: "/stdout",
				argv0:     "/bin/veyron",
				argv:      []string{"one", "two"},
				envv:      []string{"A=B"},
			},
		},
	}

	for _, c := range cases {
		var wp WorkParameters
		fs := flag.NewFlagSet(c.cmdline[0], flag.ExitOnError)
		setupFlags(fs)
		fs.Parse(c.cmdline[1:])
		if err := wp.ProcessArguments(fs, c.env); !reflect.DeepEqual(err, c.err) {
			t.Fatalf("got %v, expected %v error", err, c.err)
		}
		if !reflect.DeepEqual(wp, c.expected) {
			t.Fatalf("got %#v expected %#v", wp, c.expected)
		}
	}
}
