// Copyright 2016 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"encoding/base64"
	"encoding/json"
	"io/ioutil"
	"os"
	"os/exec"
	"text/template"

	"v.io/v23/context"
	"v.io/v23/conventions"
	"v.io/v23/rpc"
	"v.io/v23/security"
	"v.io/v23/security/access"
	"v.io/v23/verror"
	"v.io/v23/vom"
)

type allocatorImpl struct{}

// Create creates a new instance of the service. The instance's
// blessings will be an extension of the blessings granted on this RPC.
// It returns the object name of the new instance.
func (i *allocatorImpl) Create(ctx *context.T, call rpc.ServerCall) (string, error) {
	b, _ := security.RemoteBlessingNames(ctx, call.Security())
	ctx.Infof("Create() called by %v", b)

	mName, kName, err := names(ctx, call.Security())
	if err != nil {
		return "", err
	}

	// TODO(rthellend): Add limit on the total number of servers.
	if _, err := vkube("kubectl", "get", "deployment", kName); err == nil {
		return "", verror.New(verror.ErrExist, ctx)
	}

	cfg, cleanup, err := createDeploymentConfig(ctx, kName, mName, call.Security())
	defer cleanup()
	if err != nil {
		return "", err
	}

	vomBlessings, err := vom.Encode(call.GrantedBlessings())
	if err != nil {
		return "", err
	}
	out, err := vkube(
		"start", "-f", cfg,
		"--base-blessings", base64.URLEncoding.EncodeToString(vomBlessings),
		"--wait",
		serverNameFlag,
	)
	if err != nil {
		ctx.Errorf("vkube start failed: %s", string(out))
		return "", verror.New(verror.ErrInternal, ctx, err)
	}
	return mName, nil
}

// Delete deletes the instance with the given name.
func (i *allocatorImpl) Delete(ctx *context.T, call rpc.ServerCall, name string) error {
	b, _ := security.RemoteBlessingNames(ctx, call.Security())
	ctx.Infof("Delete(%q) called by %v", name, b)

	mName, kName, err := names(ctx, call.Security())
	if err != nil {
		return err
	}

	if name != mName {
		return verror.New(verror.ErrNoAccess, ctx)
	}
	if _, err := vkube("kubectl", "get", "deployment", kName); err != nil {
		return verror.New(verror.ErrNoExist, ctx)
	}
	cfg, cleanup, err := createDeploymentConfig(ctx, kName, mName, call.Security())
	defer cleanup()
	if err != nil {
		return err
	}

	out, err := vkube("stop", "-f", cfg)
	if err != nil {
		ctx.Errorf("vkube stop failed: %s", string(out))
		return verror.New(verror.ErrInternal, ctx, err)
	}
	return nil
}

// List returns a list of all the instances owned by the caller.
func (i *allocatorImpl) List(ctx *context.T, call rpc.ServerCall) ([]string, error) {
	b, _ := security.RemoteBlessingNames(ctx, call.Security())
	ctx.Infof("List() called by %v", b)
	mName, kName, err := names(ctx, call.Security())
	if err != nil {
		return nil, err
	}
	if out, err := vkube("kubectl", "get", "deployment", kName); err != nil {
		ctx.Infof("Couldn't find deployment %q: %s", kName, string(out))
		return nil, nil
	}
	return []string{mName}, nil
}

func createDeploymentConfig(ctx *context.T, deploymentName, mountName string, call security.Call) (string, func(), error) {
	cleanup := func() {}
	acl, err := accessList(ctx, call)
	if err != nil {
		return "", cleanup, err
	}
	creatorInfo, err := creatorInfo(ctx, call)
	if err != nil {
		return "", cleanup, err
	}

	t, err := template.ParseFiles(deploymentTemplateFlag)
	if err != nil {
		return "", cleanup, err
	}
	data := struct {
		AccessList  string
		CreatorInfo string
		MountName   string
		Name        string
	}{
		AccessList:  acl,
		CreatorInfo: creatorInfo,
		MountName:   mountName,
		Name:        deploymentName,
	}

	f, err := ioutil.TempFile("", "allocator-deployment-")
	if err != nil {
		return "", cleanup, err
	}
	defer f.Close()
	cleanup = func() { os.Remove(f.Name()) }

	if err := t.Execute(f, data); err != nil {
		return "", cleanup, err
	}
	return f.Name(), cleanup, nil
}

// accessList returns a double encoded JSON access list that can be used in a
// Deployment template that contains something like:
//   "--v23.permissions.literal={\"Admin\": {{.AccessList}} }"
// The access list include the caller of the RPC.
func accessList(ctx *context.T, call security.Call) (string, error) {
	var acl access.AccessList
	b, _ := security.RemoteBlessingNames(ctx, call)
	for _, blessing := range conventions.ParseBlessingNames(b...) {
		acl.In = append(acl.In, blessing.UserPattern())
	}
	j, err := json.Marshal(acl)
	if err != nil {
		ctx.Errorf("json.Marshal(%#v) failed: %v", acl, err)
		return "", err
	}
	// JSON encode again, because the access list is in a JSON template.
	str := string(j)
	j, err = json.Marshal(str)
	if err != nil {
		ctx.Errorf("json.Marshal(%#v) failed: %v", str, err)
		return "", err
	}
	// Remove the quotes.
	return string(j[1 : len(j)-1]), nil
}

// creatorInfo returns a double encoded JSON access list that can be used as
// annotation in a Deployment template, e.g.
//   "annotations": {
//     "v.io/allocatord/creator-info": {{.CreatorInfo}}
//   }
func creatorInfo(ctx *context.T, call security.Call) (string, error) {
	var info struct {
		Blessings []string `json:"blessings"`
		Endpoint  string   `json:"endpoint"`
	}
	info.Blessings, _ = security.RemoteBlessingNames(ctx, call)
	info.Endpoint = call.RemoteEndpoint().String()
	j, err := json.Marshal(info)
	if err != nil {
		ctx.Errorf("json.Marshal(%#v) failed: %v", info, err)
		return "", err
	}
	// JSON encode again, because the annotation is in a JSON template.
	str := string(j)
	j, err = json.Marshal(str)
	if err != nil {
		ctx.Errorf("json.Marshal(%#v) failed: %v", str, err)
		return "", err
	}
	return string(j), nil
}

func vkube(args ...string) ([]byte, error) {
	args = append(
		[]string{
			"--config=" + vkubeCfgFlag,
			"--get-credentials=false",
		},
		args...,
	)
	return exec.Command(vkubeBinFlag, args...).CombinedOutput()
}
