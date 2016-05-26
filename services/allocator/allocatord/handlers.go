// Copyright 2016 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"v.io/v23/security"
)

func handleHome(ss *serverState, rs *requestState) error {
	ctx := ss.ctx
	instances, err := list(ctx, rs.email)
	if err != nil {
		return fmt.Errorf("list error: %v", err)
	}
	type instanceArg struct {
		Name,
		NameRoot,
		DestroyURL,
		DebugURL,
		DashboardURL string
		BlessingPatterns []string
		CreationTime     time.Time
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
		if len(instance.blessingNames) == 0 {
			// TODO(rthellend): Remove when all instances have the new
			// creatorInfo annotations.
			ctx.Info("blessingNames missing from creatorInfo")
			for _, b := range ss.args.baseBlessingNames {
				bName := strings.Join([]string{b, instance.name}, security.ChainSeparator)
				instance.blessingNames = append(instance.blessingNames, bName)
			}
		}

		tmplArgs.Instances = append(tmplArgs.Instances, instanceArg{
			Name:             instance.mountName,
			NameRoot:         nameRoot(ctx),
			CreationTime:     instance.creationTime,
			BlessingPatterns: instance.blessingNames,
			DestroyURL:       makeURL(ctx, routeDestroy, params{paramName: instance.mountName, paramCSRF: rs.csrfToken}),
			DashboardURL:     makeURL(ctx, routeDashboard, params{paramDashboardName: relativeMountName(instance.mountName), paramCSRF: rs.csrfToken}),
			DebugURL:         makeURL(ctx, routeDebug+"/", params{paramName: instance.mountName, paramCSRF: rs.csrfToken}),
		})
	}
	if err := ss.args.assets.executeTemplate(rs.w, homeTmpl, tmplArgs); err != nil {
		return fmt.Errorf("failed to render home template: %v", err)
	}
	return nil
}

func handleCreate(ss *serverState, rs *requestState) error {
	ctx := ss.ctx
	name, err := create(ctx, rs.email, ss.args.baseBlessings, ss.args.baseBlessingNames)
	if err != nil {
		return fmt.Errorf("create failed: %v", err)
	}
	redirectTo := makeURL(ctx, routeHome, params{paramMessage: "created " + name, paramCSRF: rs.csrfToken})
	http.Redirect(rs.w, rs.r, redirectTo, http.StatusFound)
	return nil
}

func handleDestroy(ss *serverState, rs *requestState) error {
	ctx := ss.ctx
	name := rs.r.FormValue(paramName)
	if err := destroy(ctx, rs.email, name); err != nil {
		return fmt.Errorf("destroy failed: %v", err)
	}
	redirectTo := makeURL(ctx, routeHome, params{paramMessage: "destroyed " + name, paramCSRF: rs.csrfToken})
	http.Redirect(rs.w, rs.r, redirectTo, http.StatusFound)
	return nil
}

func handleDebug(ss *serverState, rs *requestState) error {
	instance := rs.r.FormValue(paramName)
	if instance == "" {
		return fmt.Errorf("parameter %q required for instance name", paramDashboardName)
	}
	if err := checkOwner(ss.ctx, rs.email, kubeNameFromMountName(instance)); err != nil {
		return err
	}
	http.StripPrefix(routeDebug, ss.args.debugBrowserServeMux).ServeHTTP(rs.w, rs.r)
	return nil
}
