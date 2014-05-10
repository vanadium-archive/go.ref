package lib

import (
	"bytes"
	"fmt"
	"io"
	"io/ioutil"
	"sync"

	"veyron/lib/toposort"

	"veyron2/idl/build"
)

// idlManager parses and stores idl files sent by the client
type idlManager struct {
	// Protects env and pkgs.
	sync.Mutex

	// The environment that contains the compiled packages
	env *build.Env

	pkgs map[string]*build.Package
}

// A representation of an idl package
type idlBuildPackageRequest struct {
	// The path to the package. This is usually the fully qualified package name.
	Path string

	// A map from basename to the contents of the IDL file
	IDLFiles map[string]string
}

func newBuildPackage(p idlBuildPackageRequest) *build.BuildPackage {
	baseNames := make([]string, 0, len(p.IDLFiles))
	for bn := range p.IDLFiles {
		baseNames = append(baseNames, bn)
	}
	return &build.BuildPackage{
		Dir:              "/nonexistent/path/" + p.Path,
		Path:             p.Path,
		IDLBaseFileNames: baseNames,
		Open: func(*build.BuildPackage) (map[string]io.ReadCloser, error) {
			return openFiles(p.IDLFiles)
		},
	}

}

func openFiles(files map[string]string) (map[string]io.ReadCloser, error) {
	result := make(map[string]io.ReadCloser, len(files))
	for n, data := range files {
		result[n] = ioutil.NopCloser(bytes.NewBufferString(data))
	}
	return result, nil
}

// maybeInitializeLocked initializes the members of idlManager if they aren't
// already initialized.  Mutex must be held.
func (i *idlManager) maybeInitializeLocked() {
	if i.env == nil {
		i.env = build.NewEnv(100)
		i.pkgs = make(map[string]*build.Package)
	}
}

// LoadPackages compiles the idlBuildPackageRequest passed in.  The order of pkgs does not
// matter.  If any of the idl files import a packages that are not in pkgs, they
// should have been passed into LoadPackages previously.  If the package has
// already been loaded, it will not be recompiled. Returns any error that
// was detected.
func (i *idlManager) loadPackages(pkgs []idlBuildPackageRequest) error {
	i.Lock()
	defer i.Unlock()
	i.maybeInitializeLocked()

	buildPkgs := make(map[string]*build.BuildPackage, len(pkgs))
	sorter := toposort.NewSorter()

	// Here we are building the dependency graph and doing a topological sort on
	// the graph to figure out the right order to load these packages.
	for _, pkg := range pkgs {
		buildPkg := newBuildPackage(pkg)
		idlFiles, err := buildPkg.OpenIDLFiles()
		if err != nil {
			return err
		}
		sorter.AddNode(pkg.Path)
		buildPkgs[pkg.Path] = buildPkg
		var depPkgPaths []string
		buildPkg.Name, depPkgPaths = build.ParsePackageImports(idlFiles, i.env)
		for _, dep := range depPkgPaths {
			sorter.AddEdge(pkg.Path, dep)
		}
	}

	sorted, cycles := sorter.Sort()
	if len(cycles) > 0 {
		cycleStr := toposort.PrintCycles(cycles, toString)
		return fmt.Errorf("cyclic package dependency detected: %v", cycleStr)
	}

	for _, pkgName := range sorted {
		pkgName := pkgName.(string)
		// We skip any packages that have already been loaded.
		if buildPkg := buildPkgs[pkgName]; i.pkgs[pkgName] == nil && buildPkg != nil {
			p := build.CompilePackage(buildPkg, i.env)
			if err := i.env.Errors.ToError(); err != nil {
				// We probably should remove the packages that we managed to successfully
				// compile from the environment, but that's hidden from us.  It's
				// not even clear if that is safe.
				// TODO(bjornick): Deal with this somehow.
				i.env.Errors.Reset()
				return err
			}
			i.pkgs[pkgName] = p
		} else if i.pkgs[pkgName] == nil {
			return fmt.Errorf("unknown package %s", pkgName)
		}
	}
	return nil
}

// interfaceMethods finds and returns all methods for a given interface name in the given package
func (i *idlManager) interfaceMethods(packageName, interfaceName string) ([]*build.Method, error) {
	pkg := i.pkgs[packageName]
	if pkg == nil {
		return nil, fmt.Errorf("unknown package %s", packageName)
	}

	for _, idlFile := range pkg.Files {
		for _, iface := range idlFile.Interfaces {
			if iface.Name == interfaceName {
				return iface.Methods(), nil
			}
		}
	}

	return nil, fmt.Errorf("unknown interface %s", interfaceName)
}

func toString(v interface{}) string {
	return v.(string)
}
