package main

import (
	"bytes"
	"flag"
	"fmt"
	"io"
	"os"
	"os/user"
	"path"
	"time"

	"veyron/services/identity/util"

	"veyron2/rt"
	"veyron2/security"
	"veyron2/vlog"
)

var (
	name      = flag.String("name", "", "Name for the generated identity. By default a name derived from the username of the currently logged-in user and hostname is used")
	blesser   = flag.String("blesser", "", "Path to a file containing the blessor (or - for STDIN)")
	duration  = flag.Duration("duration", 24*time.Hour, "Duration for which a blessing will be valid. Ignored if --blesser is empty")
	interpret = flag.String("interpret", "", "Path to a file containing an identity to interpret (or - for STDIN)")
)

func init() {
	flag.Usage = func() {
		bname := path.Base(os.Args[0])
		fmt.Fprintf(os.Stderr, `%s: Tool to generate Veyron identities

This tool generates veyron private identities (security.PrivateID) and dumps
the generated identity to STDOUT in base64-VOM-encoded format.

Typical usage:
* no flags
  A self-signed identity with a default name of <user>@<hostname> is generated,
  where <user> is the username of the currently logged-in user and <hostname> is
  the host name as reported by the kernel.

* --name NAME
  A self-signed identity for NAME will be generated and dumped to STDOUT

* --name NAME --blesser BLESSER
  BLESSER must be the path to a readable file (or - for STDIN) containing a
  base64-VOM-encoded security.PrivateID that will be used to generate an
  identity with NAME as the blessing name.

* --interpret INTERPRET
  INTERPRET must be the path to a readable file (or - for STDIN) containing a
  base64-VOM-encoded security.PrivateID. This identity will decoded and
  some information will be printed to STDOUT.

For example:
%s --name "foo" | %s --name "bar" --blesser - | %s --interpret -

Full flags:
`, os.Args[0], bname, bname, bname)
		flag.PrintDefaults()
	}
}

func main() {
	// It is currently not possible to regenerate the currently used identity
	// if it becomes corrupt because this tool needs VEYRON_IDENTITY to be "",
	// or point to a valid identity for runtime initialization. Temporarily
	// set the VEYRON_IDENTITY to "" as a workaround.
	// TODO(bprosnitz) Fix this.
	os.Setenv("VEYRON_IDENTITY", "")

	rt.Init()

	if len(*name) == 0 && len(*interpret) == 0 {
		generate(defaultIdentityName())
	}

	if len(*name) > 0 {
		generate(*name)
	}

	if len(*interpret) > 0 {
		id := load(*interpret)
		fmt.Println("Name   : ", id.PublicID())
		fmt.Printf("Go Type: %T\n", id)
		fmt.Println("Key    : <Cannot print the elliptic curve>")
		fmt.Println("      X: ", id.PrivateKey().X)
		fmt.Println("      Y: ", id.PrivateKey().Y)
		fmt.Println("      D: ", id.PrivateKey().D)
		fmt.Println("Any caveats in the identity are not printed")
	}
}

func load(fname string) security.PrivateID {
	if len(fname) == 0 {
		return nil
	}
	var f *os.File
	var err error
	if fname == "-" {
		f = os.Stdin
	} else if f, err = os.Open(fname); err != nil {
		vlog.Fatalf("Failed to open %q: %v", fname, err)
	}
	defer f.Close()
	var buf bytes.Buffer
	if _, err := io.Copy(&buf, f); err != nil {
		vlog.Fatalf("Failed to read %q: %v", fname, err)
	}
	var ret security.PrivateID
	if err := util.Base64VomDecode(buf.String(), &ret); err != nil || ret == nil {
		vlog.Fatalf("Failed to decode %q: %v", fname, err)
	}
	return ret
}

func generate(name string) {
	r := rt.R()
	output, err := r.NewIdentity(name)
	if err != nil {
		vlog.Fatalf("Runtime.NewIdentity(%q): %v", name, err)
	}
	if len(*blesser) > 0 {
		blesser := load(*blesser)
		blessed, err := blesser.Bless(output.PublicID(), name, *duration, nil)
		if err != nil {
			vlog.Fatalf("%q.Bless failed: %v", blesser, err)
		}
		derived, err := output.Derive(blessed)
		if err != nil {
			vlog.Fatalf("%q.Derive(%q) failed: %v", output, blessed, err)
		}
		output = derived
	}
	str, err := util.Base64VomEncode(output)
	if err != nil {
		vlog.Fatalf("Base64VomEncode(%q) failed: %v", output, err)
	}
	fmt.Println(str)
}

func defaultIdentityName() string {
	var name string
	if user, _ := user.Current(); user != nil && len(user.Username) > 0 {
		name = user.Username
	} else {
		name = "anonymous"
	}
	if host, _ := os.Hostname(); len(host) > 0 {
		name = name + "@" + host
	}
	return name
}
