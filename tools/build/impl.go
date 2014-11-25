package main

import (
	"fmt"
	"go/build"
	"io/ioutil"
	"os"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"time"

	"veyron.io/lib/cmdline"
	"veyron.io/veyron/veyron2/context"
	vbuild "veyron.io/veyron/veyron2/services/mgmt/build"
)

var (
	flagArch string
	flagOS   string
)

func init() {
	cmdBuild.Flags.StringVar(&flagArch, "arch", goruntime.GOARCH, "Target architecture.")
	cmdBuild.Flags.StringVar(&flagOS, "os", goruntime.GOOS, "Target operating system.")
}

var cmdRoot = &cmdline.Command{
	Name:  "build",
	Short: "Tool for interacting with the veyron build server",
	Long: `
The build tool tool facilitates interaction with the veyron build server.
`,
	Children: []*cmdline.Command{cmdBuild},
}

// root returns a command that represents the root of the veyron tool.
func root() *cmdline.Command {
	return cmdRoot
}

var cmdBuild = &cmdline.Command{
	Run:   runBuild,
	Name:  "build",
	Short: "Build veyron Go packages",
	Long: `
Build veyron Go packages using a remote build server. The command
collects all source code files that are not part of the Go standard
library that the target packages depend on, sends them to a build
server, and receives the built binaries.
`,
	ArgsName: "<name> <packages>",
	ArgsLong: `
<name> is a veyron object name of a build server
<packages> is a list of packages to build, specified as arguments for
each command. The format is similar to the go tool.  In its simplest
form each package is an import path; e.g. "veyron/tools/build". A
package that ends with "..." does a wildcard match against all
packages with that prefix.
`,
}

// TODO(jsimsa): Add support for importing (and remotely building)
// packages from multiple package source root GOPATH directories with
// identical names.
func importPackages(paths []string, pkgMap map[string]*build.Package) error {
	for _, path := range paths {
		recurse := false
		if strings.HasSuffix(path, "...") {
			recurse = true
			path = strings.TrimSuffix(path, "...")
		}
		if _, exists := pkgMap[path]; !exists {
			srcDir, mode := "", build.ImportMode(0)
			pkg, err := build.Import(path, srcDir, mode)
			if err != nil {
				// "C" is a pseudo-package for cgo: http://golang.org/cmd/cgo/
				// Do not attempt recursive imports.
				if pkg.ImportPath == "C" {
					continue
				}
				return fmt.Errorf("Import(%q,%q,%v) failed: %v", path, srcDir, mode, err)
			}
			if pkg.Goroot {
				continue
			}
			pkgMap[path] = pkg
			if err := importPackages(pkg.Imports, pkgMap); err != nil {
				return err
			}
		}
		if recurse {
			pkg := pkgMap[path]
			fis, err := ioutil.ReadDir(pkg.Dir)
			if err != nil {
				return fmt.Errorf("ReadDir(%v) failed: %v", pkg.Dir)
			}
			for _, fi := range fis {
				if fi.IsDir() {
					subPath := filepath.Join(path, fi.Name(), "...")
					if err := importPackages([]string{subPath}, pkgMap); err != nil {
						return err
					}
				}
			}
		}
	}
	return nil
}

func getSources(pkgMap map[string]*build.Package, cancel <-chan struct{}, errchan chan<- error) <-chan vbuild.File {
	sources := make(chan vbuild.File)
	go func() {
		defer close(sources)
		for _, pkg := range pkgMap {
			for _, files := range [][]string{pkg.CFiles, pkg.CgoFiles, pkg.GoFiles, pkg.SFiles} {
				for _, file := range files {
					path := filepath.Join(pkg.Dir, file)
					bytes, err := ioutil.ReadFile(path)
					if err != nil {
						errchan <- fmt.Errorf("ReadFile(%v) failed: %v", path, err)
						return
					}
					select {
					case sources <- vbuild.File{Contents: bytes, Name: filepath.Join(pkg.ImportPath, file)}:
					case <-cancel:
						errchan <- nil
						return
					}
				}
			}
		}
		errchan <- nil
	}()
	return sources
}

func invokeBuild(ctx context.T, name string, sources <-chan vbuild.File, cancel <-chan struct{}, errchan chan<- error) <-chan vbuild.File {
	binaries := make(chan vbuild.File)
	go func() {
		defer close(binaries)
		client := vbuild.BuilderClient(name)
		stream, err := client.Build(ctx, vbuild.Architecture(flagArch), vbuild.OperatingSystem(flagOS))
		if err != nil {
			errchan <- fmt.Errorf("Build() failed: %v", err)
			return
		}
		sender := stream.SendStream()
		for source := range sources {
			if err := sender.Send(source); err != nil {
				stream.Cancel()
				errchan <- fmt.Errorf("Send() failed: %v", err)
				return
			}
		}
		if err := sender.Close(); err != nil {
			errchan <- fmt.Errorf("Close() failed: %v", err)
			return
		}
		iterator := stream.RecvStream()
		for iterator.Advance() {
			// TODO(mattr): This custom cancellation can probably be folded into the
			// cancellation mechanism provided by the context.
			select {
			case binaries <- iterator.Value():
			case <-cancel:
				errchan <- nil
				return
			}
		}
		if err := iterator.Err(); err != nil {
			errchan <- fmt.Errorf("Advance() failed: %v", err)
			return
		}
		if out, err := stream.Finish(); err != nil {
			errchan <- fmt.Errorf("Finish() failed: (%v, %v)", string(out), err)
			return
		}
		errchan <- nil
	}()
	return binaries
}

func saveBinaries(prefix string, binaries <-chan vbuild.File, cancel chan<- struct{}, errchan chan<- error) {
	go func() {
		for binary := range binaries {
			path, perm := filepath.Join(prefix, filepath.Base(binary.Name)), os.FileMode(0755)
			if err := ioutil.WriteFile(path, binary.Contents, perm); err != nil {
				errchan <- fmt.Errorf("WriteFile(%v, %v) failed: %v", path, perm, err)
				return
			}
			fmt.Printf("Generated binary %v\n", path)
		}
		errchan <- nil
	}()
}

// runBuild identifies the source files needed to build the packages
// specified on command-line and then creates a pipeline that
// concurrently 1) reads the source files, 2) sends them to the build
// server and receives binaries from the build server, and 3) writes
// the binaries out to the disk.
func runBuild(command *cmdline.Command, args []string) error {
	name, paths := args[0], args[1:]
	pkgMap := map[string]*build.Package{}
	if err := importPackages(paths, pkgMap); err != nil {
		return err
	}
	cancel, errchan := make(chan struct{}), make(chan error)
	defer close(errchan)

	ctx, ctxCancel := runtime.NewContext().WithTimeout(time.Minute)
	defer ctxCancel()

	// Start all stages of the pipeline.
	sources := getSources(pkgMap, cancel, errchan)
	binaries := invokeBuild(ctx, name, sources, cancel, errchan)
	saveBinaries(os.TempDir(), binaries, cancel, errchan)
	// Wait for all stages of the pipeline to terminate.
	cancelled, errors, numStages := false, []error{}, 3
	for i := 0; i < numStages; i++ {
		if err := <-errchan; err != nil {
			errors = append(errors, err)
			if !cancelled {
				close(cancel)
				cancelled = true
			}
		}
	}
	if len(errors) != 0 {
		return fmt.Errorf("build failed(%v)", errors)
	}
	return nil
}
