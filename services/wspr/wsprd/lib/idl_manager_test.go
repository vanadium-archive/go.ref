package lib

import (
	"testing"

	"veyron2/idl/build"
)

const fooIdl1 = `
package foo
type A struct {
	B int32
	C int32
}

type S interface {
	Func1(I A) (int32, error)
}
`

const fooIdl2 = `
package foo

type S2 interface {
	Func2(I int32) error
}
`

const barIdl = `
package bar

import (
	"wspr/idl_manager/foo"
)

type S3 interface {
	Func(I int32) (foo.A, error)
}
`

func verifyFooPackage(compiledPkg *build.Package, t *testing.T) {
	if compiledPkg == nil {
		t.Error("foo compiled package not found")
	}

	if compiledPkg.ResolveType("A") == nil {
		t.Error("failed resolve 'A'")
	}

	if compiledPkg.ResolveType("S") == nil {
		t.Error("failed resolve 'S'")
	}

	if compiledPkg.ResolveType("S2") == nil {
		t.Error("failed resolve 'S2'")
	}
}

func TestSimpleLoadPackage(t *testing.T) {
	pkg := idlBuildPackageRequest{
		Path: "wspr/idl_manager/foo",
		IDLFiles: map[string]string{
			"bar.idl": fooIdl1,
			"foo.idl": fooIdl2,
		},
	}
	var i idlManager
	if err := i.loadPackages([]idlBuildPackageRequest{pkg}); err != nil {
		t.Errorf("load packages should have succeed: %v", err)
	}

	verifyFooPackage(i.pkgs["wspr/idl_manager/foo"], t)
}

func verifyBarPackage(compiledPkg *build.Package, t *testing.T) {
	if compiledPkg == nil {
		t.Error("compiled bar package not found")
	}

	if compiledPkg.ResolveType("S3") == nil {
		t.Error("failed resolve 'S3'")
	}
}

func TestLoadMultiplePackages(t *testing.T) {
	fooPkg := idlBuildPackageRequest{
		Path: "wspr/idl_manager/foo",
		IDLFiles: map[string]string{
			"foo.idl1": fooIdl1,
			"foo.idl2": fooIdl2,
		},
	}

	barPkg := idlBuildPackageRequest{
		Path: "wspr/idl_manager/bar",
		IDLFiles: map[string]string{
			"bar.idl": barIdl,
		},
	}
	var i idlManager
	if err := i.loadPackages([]idlBuildPackageRequest{barPkg, fooPkg}); err != nil {
		t.Errorf("load packages should have succeed: %v", err)
	}
	verifyFooPackage(i.pkgs["wspr/idl_manager/foo"], t)
	verifyBarPackage(i.pkgs["wspr/idl_manager/bar"], t)
}

func TestMultipleCallsToLoadPackageShareState(t *testing.T) {
	fooPkg := idlBuildPackageRequest{
		Path: "wspr/idl_manager/foo",
		IDLFiles: map[string]string{
			"foo.idl1": fooIdl1,
			"foo.idl2": fooIdl2,
		},
	}

	var i idlManager
	if err := i.loadPackages([]idlBuildPackageRequest{fooPkg}); err != nil {
		t.Errorf("load packages should have succeed: %v", err)
	}
	verifyFooPackage(i.pkgs["wspr/idl_manager/foo"], t)

	barPkg := idlBuildPackageRequest{
		Path: "wspr/idl_manager/bar",
		IDLFiles: map[string]string{
			"bar.idl": barIdl,
		},
	}

	if err := i.loadPackages([]idlBuildPackageRequest{barPkg}); err != nil {
		t.Errorf("load packages should have succeed: %v", err)
	}
	verifyBarPackage(i.pkgs["wspr/idl_manager/bar"], t)
}

func TestFailsWhenDependenciesNotProvided(t *testing.T) {
	barPkg := idlBuildPackageRequest{
		Path: "wspr/idl_manager/bar",
		IDLFiles: map[string]string{
			"bar.idl": barIdl,
		},
	}
	var i idlManager
	if err := i.loadPackages([]idlBuildPackageRequest{barPkg}); err == nil {
		t.Errorf("load packages should have failed")
	}
}
