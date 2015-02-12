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
			[]string{"setuidhelper", "--minuid", "1", "--username", testUserName},
			[]string{"A=B"},
			nil,
			WorkParameters{
				uid:       testUid,
				gid:       testGid,
				workspace: "",
				logDir:    "",
				argv0:     "",
				argv:      []string{""},
				envv:      []string{"A=B"},
				dryrun:    false,
				remove:    false,
			},
		},

		{
			[]string{"setuidhelper", "--minuid", "1", "--username", testUserName, "--workspace", "/hello",
				"--logdir", "/logging", "--run", "/bin/veyron", "--", "one", "two"},
			[]string{"A=B"},
			nil,
			WorkParameters{
				uid:       testUid,
				gid:       testGid,
				workspace: "/hello",
				logDir:    "/logging",
				argv0:     "/bin/veyron",
				argv:      []string{"/bin/veyron", "one", "two"},
				envv:      []string{"A=B"},
				dryrun:    false,
				remove:    false,
			},
		},
		{
			[]string{"setuidhelper", "--username", testUserName},
			[]string{"A=B"},
			fmt.Errorf("suidhelper uid %d is not permitted because it is less than 501", testUid),
			WorkParameters{},
		},

		{
			[]string{"setuidhelper", "--rm", "hello", "vanadium"},
			[]string{"A=B"},
			nil,
			WorkParameters{
				uid:       0,
				gid:       0,
				workspace: "",
				logDir:    "",
				argv0:     "",
				argv:      []string{"hello", "vanadium"},
				envv:      nil,
				dryrun:    false,
				remove:    true,
			},
		},
		{
			[]string{"setuidhelper", "--username", testUserName},
			[]string{"A=B"},
			fmt.Errorf("suidhelper uid %d is not permitted because it is less than 501", testUid),
			WorkParameters{},
		},

		{
			[]string{"setuidhelper", "--minuid", "1", "--username", testUserName, "--workspace", "/hello",
				"--logdir", "/logging", "--run", "/bin/veyron", "--dryrun", "--", "one", "two"},
			[]string{"A=B"},
			nil,
			WorkParameters{
				uid:       testUid,
				gid:       testGid,
				workspace: "/hello",
				logDir:    "/logging",
				argv0:     "/bin/veyron",
				argv:      []string{"/bin/veyron", "one", "two"},
				envv:      []string{"A=B"},
				dryrun:    true,
				remove:    false,
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
