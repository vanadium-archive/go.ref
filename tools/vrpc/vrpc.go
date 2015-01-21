// The following enables go generate to generate the doc.go file.
//go:generate go run $VANADIUM_ROOT/release/go/src/v.io/lib/cmdline/testdata/gendoc.go .

package main

import (
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"v.io/core/veyron2"
	"v.io/core/veyron2/context"
	"v.io/core/veyron2/ipc/reserved"
	"v.io/core/veyron2/rt"
	"v.io/core/veyron2/vdl"
	"v.io/core/veyron2/vdl/build"
	"v.io/core/veyron2/vdl/codegen/vdlgen"
	"v.io/core/veyron2/vdl/compile"
	"v.io/core/veyron2/vdl/vdlroot/src/signature"
	"v.io/lib/cmdline"

	_ "v.io/core/veyron/profiles"
)

var runtime veyron2.Runtime

func main() {
	var err error
	runtime, err = rt.New()
	if err != nil {
		panic(err)
	}
	exitCode := cmdVRPC.Main()
	runtime.Cleanup()
	os.Exit(exitCode)
}

var cmdVRPC = &cmdline.Command{
	Name:  "vrpc",
	Short: "Vanadium Remote Procedure Call tool",
	Long: `
The vrpc tool provides command-line access to Vanadium servers via Remote
Procedure Call.
`,
	// TODO(toddw): Add cmdServe, which will take an interface as input, and set
	// up a server capable of handling the given methods.  When a request is
	// received, it'll allow the user to respond via stdin.
	Children: []*cmdline.Command{cmdSignature, cmdCall, cmdIdentify},
}

const serverDesc = `
<server> identifies a Vanadium server.  It can either be the object address of
the server, or an object name that will be resolved to an end-point.
`

var cmdSignature = &cmdline.Command{
	Run:   runSignature,
	Name:  "signature",
	Short: "Describe the interfaces of a Vanadium server",
	Long: `
Signature connects to the Vanadium server identified by <server>.

If no [method] is provided, returns all interfaces implemented by the server.

If a [method] is provided, returns the signature of just that method.
`,
	ArgsName: "<server> [method]",
	ArgsLong: serverDesc + `
[method] is the optional server method name.
`,
}

var cmdCall = &cmdline.Command{
	Run:   runCall,
	Name:  "call",
	Short: "Call a method of a Vanadium server",
	Long: `
Call connects to the Vanadium server identified by <server> and calls the
<method> with the given positional [args...], returning results on stdout.

TODO(toddw): stdin is read for streaming arguments sent to the server.  An EOF
on stdin (e.g. via ^D) causes the send stream to be closed.

Regardless of whether the call is streaming, the main goroutine blocks for
streaming and positional results received from the server.

All input arguments (both positional and streaming) are specified as VDL
expressions, with commas separating multiple expressions.  Positional arguments
may also be specified as separate command-line arguments.  Streaming arguments
may also be specified as separate newline-terminated expressions.

The method signature is always retrieved from the server as a first step.  This
makes it easier to input complex typed arguments, since the top-level type for
each argument is implicit and doesn't need to be specified.
`,
	ArgsName: "<server> <method> [args...]",
	ArgsLong: serverDesc + `
<method> is the server method to call.

[args...] are the positional input arguments, specified as VDL expressions.
`,
}

var cmdIdentify = &cmdline.Command{
	Run:   runIdentify,
	Name:  "identify",
	Short: "Reveal blessings presented by a Vanadium server",
	Long: `
Identify connects to the Vanadium server identified by <server> and dumps out
the blessings presented by that server (and the subset of those that are
considered valid by the principal running this tool) to standard output.
`,
	ArgsName: "<server>",
	ArgsLong: serverDesc,
}

func runSignature(cmd *cmdline.Command, args []string) error {
	// Error-check args.
	var server, method string
	switch len(args) {
	case 1:
		server = args[0]
	case 2:
		server, method = args[0], args[1]
	default:
		return cmd.UsageErrorf("wrong number of arguments")
	}
	// Get the interface or method signature, and pretty-print.  We print the
	// named types after the signatures, to aid in readability.
	ctx, cancel := context.WithTimeout(runtime.NewContext(), time.Minute)
	defer cancel()
	var types signature.NamedTypes
	if method != "" {
		methodSig, err := reserved.MethodSignature(ctx, server, method)
		if err != nil {
			return fmt.Errorf("MethodSignature failed: %v", err)
		}
		methodSig.Print(cmd.Stdout(), &types)
		fmt.Fprintln(cmd.Stdout())
		types.Print(cmd.Stdout())
		return nil
	}
	ifacesSig, err := reserved.Signature(ctx, server)
	if err != nil {
		return fmt.Errorf("Signature failed: %v", err)
	}
	for i, iface := range ifacesSig {
		if i > 0 {
			fmt.Fprintln(cmd.Stdout())
		}
		iface.Print(cmd.Stdout(), &types)
		fmt.Fprintln(cmd.Stdout())
	}
	types.Print(cmd.Stdout())
	return nil
}

func runCall(cmd *cmdline.Command, args []string) error {
	// Error-check args, and set up argsdata with a comma-separated list of
	// arguments, allowing each individual arg to already be comma-separated.
	//
	// TODO(toddw): Should we just space-separate the args instead?
	if len(args) < 2 {
		return cmd.UsageErrorf("must specify <server> and <method>")
	}
	server, method := args[0], args[1]
	var argsdata string
	for _, arg := range args[2:] {
		arg := strings.TrimSpace(arg)
		if argsdata == "" || strings.HasSuffix(argsdata, ",") || strings.HasPrefix(arg, ",") {
			argsdata += arg
		} else {
			argsdata += "," + arg
		}
	}
	// Get the method signature and parse args.
	ctx, cancel := context.WithTimeout(runtime.NewContext(), time.Minute)
	defer cancel()
	methodSig, err := reserved.MethodSignature(ctx, server, method)
	if err != nil {
		return fmt.Errorf("MethodSignature failed: %v", err)
	}
	inargs, err := parseInArgs(argsdata, methodSig)
	if err != nil {
		// TODO: Print signature and example.
		return err
	}
	// Start the method call.
	call, err := veyron2.GetClient(ctx).StartCall(ctx, server, method, inargs)
	if err != nil {
		return fmt.Errorf("StartCall failed: %v", err)
	}
	// TODO(toddw): Fire off a goroutine to handle streaming inputs.
	// Handle streaming results.
StreamingResultsLoop:
	for {
		var item *vdl.Value
		switch err := call.Recv(&item); {
		case err == io.EOF:
			break StreamingResultsLoop
		case err != nil:
			return fmt.Errorf("call.Recv failed: %v", err)
		}
		fmt.Fprintf(cmd.Stdout(), "<< %v\n", vdlgen.TypedConst(item, "", nil))
	}
	// Finish the method call.
	outargs := make([]*vdl.Value, len(methodSig.OutArgs))
	outptrs := make([]interface{}, len(outargs))
	for i := range outargs {
		outptrs[i] = &outargs[i]
	}
	if err := call.Finish(outptrs...); err != nil {
		return fmt.Errorf("call.Finish failed: %v", err)
	}
	// Handle application errors, reported as a final error out-arg.
	//
	// TODO(toddw): Change call.Finish to report the error directly.
	outlen := len(outargs)
	if outlen > 0 && methodSig.OutArgs[outlen-1].Type == vdl.ErrorType {
		if errarg := outargs[outlen-1]; !errarg.IsNil() {
			return fmt.Errorf(vdlgen.TypedConst(errarg, "", nil))
		}
		outargs = outargs[:outlen-1]
	}
	// Pretty-print results.
	for i, arg := range outargs {
		if i > 0 {
			fmt.Fprint(cmd.Stdout(), " ")
		}
		fmt.Fprint(cmd.Stdout(), vdlgen.TypedConst(arg, "", nil))
	}
	fmt.Fprintln(cmd.Stdout())
	return nil
}

func parseInArgs(argsdata string, methodSig signature.Method) ([]interface{}, error) {
	if len(methodSig.InArgs) == 0 {
		return nil, nil
	}
	var intypes []*vdl.Type
	for _, inarg := range methodSig.InArgs {
		intypes = append(intypes, inarg.Type)
	}
	env := compile.NewEnv(-1)
	inargs := build.BuildExprs(argsdata, intypes, env)
	if err := env.Errors.ToError(); err != nil {
		return nil, fmt.Errorf("can't parse in-args:\n%v", err)
	}
	if got, want := len(inargs), len(methodSig.InArgs); got != want {
		return nil, fmt.Errorf("got %d args, want %d", got, want)
	}
	// Translate []*vdl.Value to []interface, with each item still *vdl.Value.
	var ret []interface{}
	for _, arg := range inargs {
		ret = append(ret, arg)
	}
	return ret, nil
}

func runIdentify(cmd *cmdline.Command, args []string) error {
	if len(args) != 1 {
		return cmd.UsageErrorf("wrong number of arguments")
	}
	server := args[0]
	ctx, cancel := context.WithTimeout(runtime.NewContext(), time.Minute)
	defer cancel()
	// The method name does not matter - only interested in authentication,
	// not in actually making an RPC.
	call, err := veyron2.GetClient(ctx).StartCall(ctx, server, "", nil)
	if err != nil {
		return fmt.Errorf(`client.StartCall(%q, "", nil) failed with %v`, server, err)
	}
	valid, presented := call.RemoteBlessings()
	fmt.Fprintf(cmd.Stdout(), "PRESENTED:  %v\nVALID:      %v\n", presented, valid)
	return nil
}
