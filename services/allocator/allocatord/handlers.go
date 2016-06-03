// Copyright 2016 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"fmt"
	"net/http"

	"v.io/x/ref/services/allocator"
)

func handleHome(ss *serverState, rs *requestState) error {
	ctx := ss.ctx
	instances, err := serverInstances(ctx, rs.email)
	if err != nil {
		return fmt.Errorf("list error: %v", err)
	}
	type instanceArg struct {
		Instance allocator.Instance
		DestroyURL,
		ResetURL,
		DebugURL,
		DashboardURL,
		SuspendURL,
		ResumeURL string
	}
	tmplArgs := struct {
		AssetsPrefix,
		ServerName,
		Email,
		CreateURL,
		Message string
		Instances []instanceArg
	}{
		AssetsPrefix: ss.args.staticAssetsPrefix,
		ServerName:   ss.args.serverName,
		Email:        rs.email,
		CreateURL:    makeURL(ctx, routeCreate, params{paramCSRF: rs.csrfToken}),
		Message:      rs.r.FormValue(paramMessage),
	}
	for _, instance := range instances {
		tmplArgs.Instances = append(tmplArgs.Instances, instanceArg{
			Instance:     instance,
			DestroyURL:   makeURL(ctx, routeDestroy, params{paramInstance: instance.Handle, paramCSRF: rs.csrfToken}),
			ResetURL:     makeURL(ctx, routeReset, params{paramInstance: instance.Handle, paramCSRF: rs.csrfToken}),
			SuspendURL:   makeURL(ctx, routeSuspend, params{paramInstance: instance.Handle, paramCSRF: rs.csrfToken}),
			ResumeURL:    makeURL(ctx, routeResume, params{paramInstance: instance.Handle, paramCSRF: rs.csrfToken}),
			DashboardURL: makeURL(ctx, routeDashboard, params{paramInstance: instance.Handle}),
			DebugURL:     makeURL(ctx, routeDebug+"/", params{paramMountName: instance.MountName}),
		})
	}
	if err := ss.args.assets.executeTemplate(rs.w, homeTmpl, tmplArgs); err != nil {
		return fmt.Errorf("failed to render home template: %v", err)
	}
	return nil
}

func handleCreate(ss *serverState, rs *requestState) error {
	ctx := ss.ctx
	instance, err := create(ctx, rs.email, ss.args.baseBlessings, ss.args.baseBlessingNames)
	if err != nil {
		return fmt.Errorf("create failed: %v", err)
	}
	redirectTo := makeURL(ctx, routeHome, params{paramMessage: "created " + instance})
	http.Redirect(rs.w, rs.r, redirectTo, http.StatusFound)
	return nil
}

func handleDestroy(ss *serverState, rs *requestState) error {
	ctx := ss.ctx
	instance := rs.r.FormValue(paramInstance)
	if instance == "" {
		return fmt.Errorf("parameter %q required for instance name", paramInstance)
	}
	if err := destroy(ctx, rs.email, instance); err != nil {
		return fmt.Errorf("destroy failed: %v", err)
	}
	redirectTo := makeURL(ctx, routeHome, params{paramMessage: "destroyed " + instance})
	http.Redirect(rs.w, rs.r, redirectTo, http.StatusFound)
	return nil
}

func handleSuspend(ss *serverState, rs *requestState) error {
	ctx := ss.ctx
	instance := rs.r.FormValue(paramInstance)
	if instance == "" {
		return fmt.Errorf("parameter %q required for instance name", paramInstance)
	}
	if err := suspend(ctx, rs.email, instance); err != nil {
		return fmt.Errorf("suspend failed: %v", err)
	}
	redirectTo := makeURL(ctx, routeHome, params{paramMessage: "suspended " + instance})
	http.Redirect(rs.w, rs.r, redirectTo, http.StatusFound)
	return nil
}

func handleResume(ss *serverState, rs *requestState) error {
	ctx := ss.ctx
	instance := rs.r.FormValue(paramInstance)
	if instance == "" {
		return fmt.Errorf("parameter %q required for instance name", paramInstance)
	}
	if err := resume(ctx, rs.email, instance); err != nil {
		return fmt.Errorf("resume failed: %v", err)
	}
	redirectTo := makeURL(ctx, routeHome, params{paramMessage: "resumed " + instance})
	http.Redirect(rs.w, rs.r, redirectTo, http.StatusFound)
	return nil
}

func handleReset(ss *serverState, rs *requestState) error {
	ctx := ss.ctx
	instance := rs.r.FormValue(paramInstance)
	if instance == "" {
		return fmt.Errorf("parameter %q required for instance name", paramInstance)
	}
	if err := resetDisk(ctx, rs.email, instance); err != nil {
		return fmt.Errorf("reset failed: %v", err)
	}
	redirectTo := makeURL(ctx, routeHome, params{paramMessage: "resetted " + instance})
	http.Redirect(rs.w, rs.r, redirectTo, http.StatusFound)
	return nil
}

func handleDebug(ss *serverState, rs *requestState, debugBrowserServeMux *http.ServeMux) error {
	ctx := ss.ctx
	mountName := rs.r.FormValue(paramMountName)
	if mountName == "" {
		return fmt.Errorf("parameter %q required for instance mount name", paramMountName)
	}
	if err := checkOwnerOfMountName(ctx, rs.email, mountName); err != nil {
		return err
	}
	http.StripPrefix(routeDebug, debugBrowserServeMux).ServeHTTP(rs.w, rs.r)
	return nil
}
