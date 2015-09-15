// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package mdns implements mDNS plugin for discovery service.
//
// In order to support discovery of a specific vanadium service, an instance
// is advertised in two ways - one as a vanadium service and the other as a
// subtype of vanadium service.
//
// For example, a vanadium printer service is advertised as
//
//    v23._tcp.local.
//    _<printer_service_uuid>._sub._v23._tcp.local.
//
// Even though an instance is advertised as two services, both PTR records refer
// to the same name.
//
//    _v23._tcp.local.  PTR <instance_uuid>.<printer_service_uuid>._v23._tcp.local.
//    _<printer_service_uuid>._sub._v23._tcp.local.
//                      PTR <instance_uuid>.<printer_service_uuid>._v23._tcp.local.
package mdns

import (
	"encoding/hex"
	"fmt"
	"strings"
	"sync"
	"time"

	"v.io/v23/context"
	"v.io/v23/discovery"

	idiscovery "v.io/x/ref/runtime/internal/discovery"

	"github.com/pborman/uuid"
	mdns "github.com/presotto/go-mdns-sd"
)

const (
	v23ServiceName    = "v23"
	serviceNameSuffix = "._sub._" + v23ServiceName
	// The host name is in the form of '<instance uuid>.<service uuid>._v23._tcp.local.'.
	// The double dots at the end are for bypassing the host name composition in
	// go-mdns-sd package so that we can use the same host name both in the (subtype)
	// service and v23 service announcements.
	hostNameSuffix = "._v23._tcp.local.."

	attrInterface = "__intf"
	attrAddr      = "__addr"
)

type plugin struct {
	mdns      *mdns.MDNS
	adStopper *idiscovery.Trigger

	subscriptionRefreshTime time.Duration
	subscriptionWaitTime    time.Duration
	subscriptionMu          sync.Mutex
	subscription            map[string]subscription // GUARDED_BY(subscriptionMu)
}

type subscription struct {
	count            int
	lastSubscription time.Time
}

func (p *plugin) Advertise(ctx *context.T, ad *idiscovery.Advertisement) error {
	serviceName := ad.ServiceUuid.String() + serviceNameSuffix
	hostName := fmt.Sprintf("%x.%s%s", ad.InstanceUuid, ad.ServiceUuid.String(), hostNameSuffix)
	txt, err := createTXTRecords(ad)
	if err != nil {
		return err
	}

	// Announce the service.
	err = p.mdns.AddService(serviceName, hostName, 0, txt...)
	if err != nil {
		return err
	}
	// Announce it as v23 service as well so that we can discover
	// all v23 services through mDNS.
	err = p.mdns.AddService(v23ServiceName, hostName, 0, txt...)
	if err != nil {
		return err
	}
	stop := func() {
		p.mdns.RemoveService(serviceName, hostName)
		p.mdns.RemoveService(v23ServiceName, hostName)
	}
	p.adStopper.Add(stop, ctx.Done())
	return nil
}

func (p *plugin) Scan(ctx *context.T, serviceUuid uuid.UUID, scanCh chan<- *idiscovery.Advertisement) error {
	var serviceName string
	if len(serviceUuid) == 0 {
		serviceName = v23ServiceName
	} else {
		serviceName = serviceUuid.String() + serviceNameSuffix
	}

	go func() {
		p.subscriptionMu.Lock()
		sub := p.subscription[serviceName]
		sub.count++
		// If we haven't refreshed in a while, do it now.
		if time.Since(sub.lastSubscription) > p.subscriptionRefreshTime {
			p.mdns.SubscribeToService(serviceName)
			// Wait a bit to learn about neighborhood.
			time.Sleep(p.subscriptionWaitTime)
			sub.lastSubscription = time.Now()
		}
		p.subscription[serviceName] = sub
		p.subscriptionMu.Unlock()

		defer func() {
			p.subscriptionMu.Lock()
			sub := p.subscription[serviceName]
			sub.count--
			if sub.count == 0 {
				delete(p.subscription, serviceName)
				p.mdns.UnsubscribeFromService(serviceName)
			} else {
				p.subscription[serviceName] = sub
			}
			p.subscriptionMu.Unlock()
		}()

		// TODO(jhahn): Handle "Lost" case.
		services := p.mdns.ServiceDiscovery(serviceName)
		for _, service := range services {
			ad, err := decodeAdvertisement(service)
			if err != nil {
				ctx.Error(err)
				continue
			}
			select {
			case scanCh <- ad:
			case <-ctx.Done():
				return
			}
		}
	}()
	return nil
}

func createTXTRecords(ad *idiscovery.Advertisement) ([]string, error) {
	// Prepare a TXT record with attributes and addresses to announce.
	//
	// TODO(jhahn): Currently, the record size is limited to 2000 bytes in
	// go-mdns-sd package. Think about how to handle a large TXT record size
	// exceeds the limit.
	txt := make([]string, 0, len(ad.Attrs)+len(ad.Addrs)+1)
	txt = append(txt, fmt.Sprintf("%s=%s", attrInterface, ad.InterfaceName))
	for k, v := range ad.Attrs {
		txt = append(txt, fmt.Sprintf("%s=%s", k, v))
	}
	for _, addr := range ad.Addrs {
		txt = append(txt, fmt.Sprintf("%s=%s", attrAddr, addr))
	}
	return txt, nil
}

func decodeAdvertisement(service mdns.ServiceInstance) (*idiscovery.Advertisement, error) {
	// Note that service.Name would be '<instance uuid>.<service uuid>._v23._tcp.local.' for
	// subtype service discovery and ''<instance uuid>.<service uuid>' for v23 service discovery.
	p := strings.SplitN(service.Name, ".", 3)
	if len(p) < 2 {
		return nil, fmt.Errorf("invalid host name: %s", service.Name)
	}
	instanceUuid, err := hex.DecodeString(p[0])
	if err != nil {
		return nil, fmt.Errorf("invalid instance uuid in host name: %s", p[0])
	}
	serviceUuid := uuid.Parse(p[1])
	if len(serviceUuid) == 0 {
		return nil, fmt.Errorf("invalid service uuid in host name: %s", p[1])
	}

	ad := idiscovery.Advertisement{
		ServiceUuid: serviceUuid,
		Service: discovery.Service{
			InstanceUuid: instanceUuid,
			Attrs:        make(discovery.Attributes),
		},
	}
	for _, rr := range service.TxtRRs {
		for _, txt := range rr.Txt {
			kv := strings.SplitN(txt, "=", 2)
			if len(kv) != 2 {
				return nil, fmt.Errorf("invalid txt record: %s", txt)
			}
			switch k, v := kv[0], kv[1]; k {
			case attrInterface:
				ad.InterfaceName = v
			case attrAddr:
				ad.Addrs = append(ad.Addrs, v)
			default:
				ad.Attrs[k] = v
			}
		}
	}
	return &ad, nil
}

func New(host string) (idiscovery.Plugin, error) {
	return newWithLoopback(host, false)
}

func newWithLoopback(host string, loopback bool) (idiscovery.Plugin, error) {
	if len(host) == 0 {
		// go-mdns-sd reannounce the services periodically only when the host name
		// is set. Use a default one if not given.
		host = "v23()"
	}
	m, err := mdns.NewMDNS(host, "", "", loopback, false)
	if err != nil {
		// The name may not have been unique. Try one more time with a unique
		// name. NewMDNS will replace the "()" with "(hardware mac address)".
		if len(host) > 0 && !strings.HasSuffix(host, "()") {
			m, err = mdns.NewMDNS(host+"()", "", "", loopback, false)
		}
		if err != nil {
			return nil, err
		}
	}
	p := plugin{
		mdns:      m,
		adStopper: idiscovery.NewTrigger(),
		// TODO(jhahn): Figure out a good subscription refresh time.
		subscriptionRefreshTime: 10 * time.Second,
		subscription:            make(map[string]subscription),
	}
	if loopback {
		p.subscriptionWaitTime = 5 * time.Millisecond
	} else {
		p.subscriptionWaitTime = 50 * time.Millisecond
	}
	return &p, nil
}
