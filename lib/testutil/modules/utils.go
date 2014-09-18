package modules

import (
	"fmt"
	"os"
	"sync"

	"veyron.io/veyron/veyron/lib/testutil/blackbox"
)

type childList struct {
	sync.Mutex
	cleaningUp bool
	children   map[Handle]struct{}
}

var (
	children = &childList{children: make(map[Handle]struct{})}
)

func (cl *childList) rm(h Handle) {
	cl.Lock()
	defer cl.Unlock()
	delete(cl.children, h)
}

func (cl *childList) add(h Handle) {
	cl.Lock()
	defer cl.Unlock()
	if cl.cleaningUp {
		return
	}
	cl.children[h] = struct{}{}
}

func bbExitWithError(m string) {
	fmt.Printf("ERROR=%s\n", m)
	os.Exit(1)
}

func bbSpawn(name string, args []string, env []string) (*blackbox.Child, Variables, []string, error) {
	c := blackbox.HelperCommand(nil, name, args...)
	c.Cmd.Env = append(c.Cmd.Env, env...)
	c.Cmd.Start()
	c.Expect("ready")

	v := make(Variables)
	r := []string{}
	for {
		line, err := c.ReadLineFromChild()
		if err != nil {
			c.Cleanup()
			return nil, v, r, err
		}
		key, val := v.UpdateFromString(line)
		if key == "ERROR" {
			c.Cleanup()
			return nil, v, r, fmt.Errorf("child: %s", val)
		}
		switch line {
		case "running":
			return c, v, r, nil
		case "done":
			c.Cleanup()
			return nil, v, r, nil
		default:
			if len(key) == 0 {
				r = append(r, line)
			}
		}
	}
}

type handle struct{ c *blackbox.Child }

func (h *handle) Stop() error {
	h.c.Cleanup()
	children.rm(h)
	return nil
}
