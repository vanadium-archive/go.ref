package main

import (
	"fmt"
	"time"

	"veyron.io/veyron/veyron/lib/testutil/modules"

	"veyron.io/veyron/veyron2/vlog"
)

type tag int

const (
	helpTag tag = iota
	getTag
	setTag
	printTag
	sleepTag
)

type builtin struct{ tag }

func helpF() modules.T {
	return &builtin{helpTag}
}

func sleepF() modules.T {
	return &builtin{sleepTag}
}

func getF() modules.T {
	return &builtin{getTag}
}

func setF() modules.T {
	return &builtin{setTag}
}

func printF() modules.T {
	return &builtin{printTag}
}

func (b *builtin) Help() string {
	switch b.tag {
	case helpTag:
		return "[command]"
	case sleepTag:
		return `[duration]
	sleep for a time (in go time.Duration format): defaults to 1s`
	case getTag:
		return "[<global variable name>]*"
	case setTag:
		return "[<var>=<val>]+"
	case printTag:
		return "[$<var>]*"
	default:
		return fmt.Sprintf("unrecognised tag for builtin: %d", b.tag)
	}
}

func (*builtin) Daemon() bool { return false }

func (b *builtin) Run(args []string) (modules.Variables, []string, modules.Handle, error) {
	switch b.tag {
	case helpTag:
		return helpCmd(args)
	case sleepTag:
		return sleep(args)
	case getTag:
		return get(args)
	case setTag:
		return set(args)
	case printTag:
		return print(args)
	default:
		return nil, nil, nil, fmt.Errorf("unrecognised tag for builtin: %d",
			b.tag)
	}
}

func helpCmd([]string) (modules.Variables, []string, modules.Handle, error) {
	for k, v := range commands {
		if k == "help" {
			continue
		}
		h := v().Help()
		if len(h) > 0 {
			fmt.Printf("%s %s\n\n", k, h)
		} else {
			fmt.Println(k)
		}
	}
	return nil, nil, nil, nil
}

func sleep(args []string) (modules.Variables, []string, modules.Handle, error) {
	if len(args) == 0 {
		vlog.Infof("Sleeping for %s", time.Second)
		time.Sleep(time.Second)
		return nil, nil, nil, nil
	}
	if d, err := time.ParseDuration(args[0]); err != nil {
		return nil, nil, nil, err
	} else {
		vlog.Infof("Sleeping for %s", d)
		time.Sleep(d)
	}
	return nil, nil, nil, nil
}

func get(args []string) (modules.Variables, []string, modules.Handle, error) {
	var r []string
	if len(args) == 0 {
		for k, v := range globals {
			r = append(r, fmt.Sprintf("\t%q=%q\n", k, v))
		}
	} else {
		for _, a := range args {
			if v, present := globals[a]; present {
				r = append(r, fmt.Sprintf("\t%q=%q\n", a, v))
			} else {
				return nil, nil, nil, fmt.Errorf("unknown variable %q", a)
			}
		}
	}
	return nil, r, nil, nil
}

func set(args []string) (modules.Variables, []string, modules.Handle, error) {
	for _, a := range args {
		globals.UpdateFromString(a)
	}
	return nil, nil, nil, nil
}

func print(args []string) (modules.Variables, []string, modules.Handle, error) {
	var r []string
	for _, a := range args {
		r = append(r, a)
	}
	return nil, r, nil, nil
}
