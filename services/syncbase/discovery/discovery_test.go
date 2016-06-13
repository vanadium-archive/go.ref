// Copyright 2016 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package discovery_test

import (
	"fmt"
	"reflect"
	"testing"
	"time"

	"v.io/v23"
	"v.io/v23/context"
	"v.io/v23/discovery"
	"v.io/v23/security"
	"v.io/v23/security/access"
	wire "v.io/v23/services/syncbase"
	"v.io/v23/syncbase"
	"v.io/v23/verror"
	idiscovery "v.io/x/ref/lib/discovery"
	fdiscovery "v.io/x/ref/lib/discovery/factory"
	"v.io/x/ref/lib/discovery/global"
	"v.io/x/ref/lib/discovery/plugins/mock"
	_ "v.io/x/ref/runtime/factories/roaming"
	syncdis "v.io/x/ref/services/syncbase/discovery"
	tu "v.io/x/ref/services/syncbase/testutil"
	"v.io/x/ref/test"
	"v.io/x/ref/test/testutil"
)

func TestSyncgroupDiscovery(t *testing.T) {
	_, ctx, sName, rootp, cleanup := tu.SetupOrDieCustom("o:app1:client1", "server",
		tu.DefaultPerms(access.AllTypicalTags(), "root:o:app1:client1"))
	defer cleanup()
	d := tu.CreateDatabase(t, ctx, syncbase.NewService(sName), "d")
	collection1 := tu.CreateCollection(t, ctx, d, "c1")
	collection2 := tu.CreateCollection(t, ctx, d, "c2")

	c1Updates, err := scanAs(ctx, rootp, "o:app1:client1")
	if err != nil {
		panic(err)
	}
	c2Updates, err := scanAs(ctx, rootp, "o:app1:client2")
	if err != nil {
		panic(err)
	}

	sgId := d.Syncgroup(ctx, "sg1").Id()
	spec := wire.SyncgroupSpec{
		Description: "test syncgroup sg1",
		Perms:       tu.DefaultPerms(wire.AllSyncgroupTags, "root:server", "root:o:app1:client1"),
		Collections: []wire.Id{collection1.Id()},
	}
	createSyncgroup(t, ctx, d, sgId, spec, verror.ID(""))

	// First update is for syncbase, and not a specific syncgroup.
	u := <-c1Updates
	attrs := u.Advertisement().Attributes
	peer := attrs[wire.DiscoveryAttrPeer]
	if peer == "" || len(attrs) != 1 {
		t.Errorf("Got %v, expected only a peer name.", attrs)
	}
	// Client2 should see the same.
	if err := expect(c2Updates, &discovery.AdId{}, find, discovery.Attributes{wire.DiscoveryAttrPeer: peer}); err != nil {
		t.Error(err)
	}

	sg1Attrs := discovery.Attributes{
		wire.DiscoveryAttrDatabaseName:      "d",
		wire.DiscoveryAttrDatabaseBlessing:  "root:o:app1",
		wire.DiscoveryAttrSyncgroupName:     "sg1",
		wire.DiscoveryAttrSyncgroupBlessing: "root:o:app1:client1",
	}
	sg2Attrs := discovery.Attributes{
		wire.DiscoveryAttrDatabaseName:      "d",
		wire.DiscoveryAttrDatabaseBlessing:  "root:o:app1",
		wire.DiscoveryAttrSyncgroupName:     "sg2",
		wire.DiscoveryAttrSyncgroupBlessing: "root:o:app1:client1",
	}

	// Then we should see an update for the created syncgroup.
	var sg1AdId discovery.AdId
	if err := expect(c1Updates, &sg1AdId, find, sg1Attrs); err != nil {
		t.Error(err)
	}

	// Now update the spec to add client2 to the permissions.
	spec.Perms = tu.DefaultPerms(wire.AllSyncgroupTags, "root:server", "root:o:app1:client1", "root:o:app1:client2")
	if err := d.SyncgroupForId(sgId).SetSpec(ctx, spec, ""); err != nil {
		t.Fatalf("sg.SetSpec failed: %v", err)
	}

	// Client1 should see a lost and a found message.
	if err := expect(c1Updates, &sg1AdId, both, sg1Attrs); err != nil {
		t.Error(err)
	}
	// Client2 should just now see the found message.
	if err := expect(c2Updates, &sg1AdId, find, sg1Attrs); err != nil {
		t.Error(err)
	}

	// Now create a second syncgroup.
	sg2Id := d.Syncgroup(ctx, "sg2").Id()
	spec2 := wire.SyncgroupSpec{
		Description: "test syncgroup sg2",
		Perms:       tu.DefaultPerms(wire.AllSyncgroupTags, "root:server", "root:o:app1:client1", "root:o:app1:client2"),
		Collections: []wire.Id{collection2.Id()},
	}
	createSyncgroup(t, ctx, d, sg2Id, spec2, verror.ID(""))

	// Both clients should see the new syncgroup.
	var sg2AdId discovery.AdId
	if err := expect(c1Updates, &sg2AdId, find, sg2Attrs); err != nil {
		t.Error(err)
	}
	if err := expect(c2Updates, &sg2AdId, find, sg2Attrs); err != nil {
		t.Error(err)
	}

	spec2.Perms = tu.DefaultPerms(wire.AllSyncgroupTags, "root:server", "root:o:app1:client1")
	if err := d.SyncgroupForId(sg2Id).SetSpec(ctx, spec2, ""); err != nil {
		t.Fatalf("sg.SetSpec failed: %v", err)
	}
	if err := expect(c2Updates, &sg2AdId, lose, sg2Attrs); err != nil {
		t.Error(err)
	}
}

func scanAs(ctx *context.T, rootp security.Principal, as string) (<-chan discovery.Update, error) {
	idp := testutil.IDProviderFromPrincipal(rootp)
	p := testutil.NewPrincipal()
	if err := idp.Bless(p, as); err != nil {
		return nil, err
	}
	ctx, err := v23.WithPrincipal(ctx, p)
	if err != nil {
		return nil, err
	}
	dis, err := syncdis.NewDiscovery(ctx, "", 0)
	if err != nil {
		return nil, err
	}
	return dis.Scan(ctx, `v.InterfaceName="v.io/x/ref/services/syncbase/server/interfaces/Sync"`)
}

const (
	lose = "lose"
	find = "find"
	both = "both"
)

func expect(ch <-chan discovery.Update, id *discovery.AdId, typ string, want discovery.Attributes) error {
	select {
	case u := <-ch:
		if (u.IsLost() && typ == find) || (!u.IsLost() && typ == lose) {
			return fmt.Errorf("IsLost mismatch.  Got %v, wanted %v", u, typ)
		}
		ad := u.Advertisement()
		got := ad.Attributes
		if id.IsValid() {
			if *id != ad.Id {
				return fmt.Errorf("mismatched id, got %v, want %v", ad.Id, id)
			}
		} else {
			*id = ad.Id
		}
		if !reflect.DeepEqual(got, want) {
			return fmt.Errorf("got %v, want %v", got, want)
		}
		if typ == both {
			typ = lose
			if u.IsLost() {
				typ = find
			}
			return expect(ch, id, typ, want)
		}
		return nil
	case <-time.After(2 * time.Second):
		return fmt.Errorf("timed out")
	}
}

func TestGlobalSyncbaseDiscovery(t *testing.T) {
	ctx, shutdown := test.V23InitWithMounttable()
	defer shutdown()
	// Create syncbase discovery service with mock neighborhood discovery.
	p := mock.New()
	df, _ := idiscovery.NewFactory(ctx, p)
	defer df.Shutdown()
	fdiscovery.InjectFactory(df)
	const globalDiscoveryPath = "syncdis"
	gdis, err := global.New(ctx, globalDiscoveryPath)
	if err != nil {
		t.Fatal(err)
	}
	dis, err := syncdis.NewDiscovery(ctx, globalDiscoveryPath, 100*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	// Start the syncbase discovery scan.
	scanCh, err := dis.Scan(ctx, ``)
	if err != nil {
		t.Fatal(err)
	}
	// The ids of all the ads match.
	ad := &discovery.Advertisement{
		Id:            discovery.AdId{1, 2, 3},
		InterfaceName: "v.io/a",
		Addresses:     []string{"/h1:123/x", "/h2:123/y"},
		Attributes:    discovery.Attributes{"a": "v"},
	}
	newad := &discovery.Advertisement{
		Id:            discovery.AdId{1, 2, 3},
		InterfaceName: "v.io/a",
		Addresses:     []string{"/h1:123/x", "/h3:123/z"},
		Attributes:    discovery.Attributes{"a": "v"},
	}
	newadattach := &discovery.Advertisement{
		Id:            discovery.AdId{1, 2, 3},
		InterfaceName: "v.io/a",
		Addresses:     []string{"/h1:123/x", "/h3:123/z"},
		Attributes:    discovery.Attributes{"a": "v"},
		Attachments:   discovery.Attachments{"k": []byte("v")},
	}
	adinfo := &idiscovery.AdInfo{
		Ad:          *ad,
		Hash:        idiscovery.AdHash{1, 2, 3},
		TimestampNs: time.Now().UnixNano(),
	}
	newadattachinfo := &idiscovery.AdInfo{
		Ad:          *newadattach,
		Hash:        idiscovery.AdHash{3, 2, 1},
		TimestampNs: time.Now().UnixNano(),
	}
	// Advertise ad via both services, we should get a found update.
	adCtx, adCancel := context.WithCancel(ctx)
	p.RegisterAd(adinfo)
	adStopped, err := gdis.Advertise(adCtx, ad, nil)
	if err != nil {
		t.Error(err)
	}
	if err := expect(scanCh, &ad.Id, find, ad.Attributes); err != nil {
		t.Error(err)
	}
	// Lost via both services, we should get a lost update.
	p.UnregisterAd(adinfo)
	adCancel()
	<-adStopped
	if err := expect(scanCh, &ad.Id, lose, ad.Attributes); err != nil {
		t.Error(err)
	}
	// Advertise via mock plugin, we should get a found update.
	p.RegisterAd(adinfo)
	if err := expect(scanCh, &ad.Id, find, ad.Attributes); err != nil {
		t.Error(err)
	}
	// Advertise via global discovery with a newer changed ad, we should get a lost ad
	// update and found ad update.
	adCtx, adCancel = context.WithCancel(ctx)
	adStopped, err = gdis.Advertise(adCtx, newad, nil)
	if err != nil {
		t.Error(err)
	}
	if err := expect(scanCh, &newad.Id, both, newad.Attributes); err != nil {
		t.Error(err)
	}
	// If we advertise an ad from neighborhood discovery that is identical to the
	// ad found from global discovery, but has attachments, we should prefer it.
	p.RegisterAd(newadattachinfo)
	if err := expect(scanCh, &newad.Id, both, newad.Attributes); err != nil {
		t.Error(err)
	}
	adCancel()
	<-adStopped
}

func TestListenForInvites(t *testing.T) {
	_, ctx, sName, rootp, cleanup := tu.SetupOrDieCustom(
		"o:app1:client1",
		"server",
		tu.DefaultPerms(access.AllTypicalTags(), "root:o:app1:client1"))
	defer cleanup()
	service := syncbase.NewService(sName)
	d1 := tu.CreateDatabase(t, ctx, service, "d1")
	d2 := tu.CreateDatabase(t, ctx, service, "d2")

	collections := []wire.Id{}
	for _, d := range []syncbase.Database{d1, d2} {
		for _, n := range []string{"c1", "c2"} {
			c := tu.CreateCollection(t, ctx, d, d.Id().Name+n)
			collections = append(collections, c.Id())
		}
	}

	d1invites, err := listenAs(ctx, rootp, "o:app1:client1", d1.Id())
	if err != nil {
		panic(err)
	}
	d2invites, err := listenAs(ctx, rootp, "o:app1:client1", d2.Id())
	if err != nil {
		panic(err)
	}

	if err := advertiseAndFindSyncgroup(t, ctx, d1, collections[0], d1invites); err != nil {
		t.Error(err)
	}
	if err := advertiseAndFindSyncgroup(t, ctx, d2, collections[2], d2invites); err != nil {
		t.Error(err)
	}
	if err := advertiseAndFindSyncgroup(t, ctx, d1, collections[1], d1invites); err != nil {
		t.Error(err)
	}
	if err := advertiseAndFindSyncgroup(t, ctx, d2, collections[3], d2invites); err != nil {
		t.Error(err)
	}
}

func listenAs(ctx *context.T, rootp security.Principal, as string, db wire.Id) (<-chan syncdis.Invite, error) {
	ch := make(chan syncdis.Invite)
	return ch, syncdis.ListenForInvites(tu.NewCtx(ctx, rootp, as), db, ch)
}

func advertiseAndFindSyncgroup(
	t *testing.T,
	ctx *context.T,
	d syncbase.Database,
	collection wire.Id,
	invites <-chan syncdis.Invite) error {
	sgId := wire.Id{Name: collection.Name + "sg", Blessing: collection.Blessing}
	ctx.Infof("creating %v", sgId)
	spec := wire.SyncgroupSpec{
		Description: "test syncgroup",
		Perms:       tu.DefaultPerms(wire.AllSyncgroupTags, "root:server", "root:o:app1:client1"),
		Collections: []wire.Id{collection},
	}
	createSyncgroup(t, ctx, d, sgId, spec, verror.ID(""))

	// We should see an invite for sg1.
	inv := <-invites
	if inv.Syncgroup != sgId {
		return fmt.Errorf("got %v, want %v", inv.Syncgroup, sgId)
	}
	return nil
}

func createSyncgroup(t *testing.T, ctx *context.T, d syncbase.Database, sgId wire.Id, spec wire.SyncgroupSpec, errID verror.ID) syncbase.Syncgroup {
	sg := d.SyncgroupForId(sgId)
	info := wire.SyncgroupMemberInfo{SyncPriority: 8}
	if err := sg.Create(ctx, spec, info); verror.ErrorID(err) != errID {
		tu.Fatalf(t, "Create SG %+v failed: %v", sgId, err)
	}
	return sg
}
