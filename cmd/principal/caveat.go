package main

import (
	"fmt"
	"regexp"
	"strings"

	"v.io/v23/security"
	"v.io/v23/vdl"
	"v.io/x/ref/lib/vdl/build"
	"v.io/x/ref/lib/vdl/compile"
)

// caveatsFlag defines a flag.Value for receiving multiple caveat definitions.
type caveatsFlag struct {
	caveatInfos []caveatInfo
}

type caveatInfo struct {
	pkg, expr, params string
}

// Implements flag.Value.Get
func (c caveatsFlag) Get() interface{} {
	return c.caveatInfos
}

// Implements flag.Value.Set
// Set expects s to be of the form:
// caveatExpr=VDLExpressionOfParam
func (c *caveatsFlag) Set(s string) error {
	exprAndParam := strings.SplitN(s, "=", 2)
	if len(exprAndParam) != 2 {
		return fmt.Errorf("incorrect caveat format: %s", s)
	}
	expr, param := exprAndParam[0], exprAndParam[1]

	// If the caveatExpr is of the form "path/to/package".ConstName we
	// need to extract the package to later obtain the imports.
	var pkg string
	pkgre, err := regexp.Compile(`\A"(.*)"\.`)
	if err != nil {
		return fmt.Errorf("failed to parse pkgregex: %v", err)
	}
	match := pkgre.FindStringSubmatch(expr)
	if len(match) == 2 {
		pkg = match[1]
	}

	c.caveatInfos = append(c.caveatInfos, caveatInfo{pkg, expr, param})
	return nil
}

// Implements flag.Value.String
func (c caveatsFlag) String() string {
	return fmt.Sprint(c.caveatInfos)
}

func (c caveatsFlag) usage() string {
	return `"package/path".CaveatName:VDLExpressionParam to attach to this blessing`
}

func (c caveatsFlag) Compile() ([]security.Caveat, error) {
	if len(c.caveatInfos) == 0 {
		return nil, nil
	}
	var caveats []security.Caveat
	env := compile.NewEnv(-1)

	var pkgs []string
	for _, info := range c.caveatInfos {
		if len(info.pkg) > 0 {
			pkgs = append(pkgs, info.pkg)
		}
	}
	if err := buildPackages(pkgs, env); err != nil {
		return nil, err
	}

	for _, info := range c.caveatInfos {
		caveat, err := newCaveat(info, env)
		if err != nil {
			return nil, err
		}
		caveats = append(caveats, caveat)
	}
	return caveats, nil
}

func newCaveat(info caveatInfo, env *compile.Env) (security.Caveat, error) {
	caveatDesc, err := compileCaveatDesc(info.pkg, info.expr, env)
	if err != nil {
		return security.Caveat{}, err
	}
	param, err := compileParams(info.params, caveatDesc.ParamType, env)
	if err != nil {
		return security.Caveat{}, err
	}

	return security.NewCaveat(caveatDesc, param)
}

func compileParams(paramData string, vdlType *vdl.Type, env *compile.Env) (interface{}, error) {
	params := build.BuildExprs(paramData, []*vdl.Type{vdlType}, env)
	if err := env.Errors.ToError(); err != nil {
		return nil, fmt.Errorf("can't parse param data %s:\n%v", paramData, err)
	}

	return params[0], nil
}

func buildPackages(packages []string, env *compile.Env) error {
	pkgs := build.TransitivePackages(packages, build.UnknownPathIsError, build.Opts{}, env.Errors)
	if !env.Errors.IsEmpty() {
		return fmt.Errorf("failed to get transitive packages packages: %s", env.Errors)
	}
	for _, p := range pkgs {
		build.BuildPackage(p, env)
		if !env.Errors.IsEmpty() {
			return fmt.Errorf("failed to build package(%s): %s", p, env.Errors)
		}
	}
	return nil
}

func compileCaveatDesc(pkg, expr string, env *compile.Env) (security.CaveatDescriptor, error) {
	var vdlValue *vdl.Value
	// In the case that the expr is of the form "path/to/package".ConstName we need to get the
	// resolve the const name instead of building the expressions.
	// TODO(suharshs,toddw): We need to fix BuildExprs so that both these cases will work with it.
	// The issue is that BuildExprs returns a dummy file with no imports instead and errors out instead of
	// looking at the imports in the env.
	if len(pkg) > 0 {
		spl := strings.SplitN(expr, "\".", 2)
		constdef := env.ResolvePackage(pkg).ResolveConst(spl[1])
		if constdef == nil {
			return security.CaveatDescriptor{}, fmt.Errorf("failed to find caveat %v", expr)
		}
		vdlValue = constdef.Value
	} else {
		vdlValues := build.BuildExprs(expr, []*vdl.Type{vdl.TypeOf(security.CaveatDescriptor{})}, env)
		if err := env.Errors.ToError(); err != nil {
			return security.CaveatDescriptor{}, fmt.Errorf("can't build caveat desc %s:\n%v", expr, err)
		}
		if len(vdlValues) == 0 {
			return security.CaveatDescriptor{}, fmt.Errorf("no caveat descriptors were built")
		}
		vdlValue = vdlValues[0]
	}

	var desc security.CaveatDescriptor
	if err := vdl.Convert(&desc, vdlValue); err != nil {
		return security.CaveatDescriptor{}, err
	}
	return desc, nil
}
