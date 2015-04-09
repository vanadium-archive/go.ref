// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package vsync

// Package vsync provides veyron sync daemon utility functions. Sync
// daemon serves incoming GetDeltas requests and contacts other peers
// to get deltas from them. When it receives a GetDeltas request, the
// incoming generation vector is diffed with the local generation
// vector, and missing generations are sent back. When it receives
// log records in response to a GetDeltas request, it replays those
// log records to get in sync with the sender.
import (
	"fmt"
	"math/rand"
	"strings"
	"sync"
	"time"

	"v.io/v23/context"
	"v.io/v23/naming"
	"v.io/v23/rpc"
	"v.io/v23/security"
	"v.io/x/lib/vlog"
	"v.io/x/ref/lib/stats"
)

var (
	rng *rand.Rand
)

// Names of Sync stats entries.
const (
	statsKvdbPath       = "vsync/kvdb-path"
	statsDevId          = "vsync/dev-id"
	statsNumDagObj      = "vsync/num-dag-objects"
	statsNumDagNode     = "vsync/num-dag-nodes"
	statsNumDagTx       = "vsync/num-dag-tx"
	statsNumDagPrivNode = "vsync/num-dag-privnodes"
	statsNumSyncGroup   = "vsync/num-syncgroups"
	statsNumMember      = "vsync/num-members"
)

// syncd contains the metadata for the sync daemon.
type syncd struct {
	// Pointers to metadata structures.
	log    *iLog
	devtab *devTable
	dag    *dag
	sgtab  *syncGroupTable

	// Filesystem path to store metadata.
	kvdbPath string

	// Local device id.
	id DeviceId
	// Handle to the server to publish names to mount tables during syncgroup create/join.
	server rpc.Server
	// Map to track the set of names already published for this sync service.
	names map[string]struct{}
	// Relative name of the sync service to be advertised to the syncgroup server.
	// This is built off the local device id.
	relName string

	// Authorizer for sync service.
	auth security.Authorizer

	// RWlock to concurrently access log and device table data structures.
	lock sync.RWMutex

	// Mutex to serialize syncgroup operations and an going
	// initiation round. If both "lock" and "sgOp" need to be
	// acquired, sgOp must be acquired first.
	sgOp sync.Mutex

	// Channel used by Sync responders to notify the SyncGroup refresher
	// to fetch from SyncGroupServer updated info for some SyncGroups.
	// sgRefresh chan *refreshRequest

	// State to coordinate shutting down all spawned goroutines.
	pending sync.WaitGroup
	closed  chan struct{}

	// Handlers for goroutine procedures.
	// hdlGC        *syncGC
	hdlWatcher   *syncWatcher
	hdlInitiator *syncInitiator

	// The context used for background activities.
	ctx *context.T
}

func (o ObjId) String() string {
	return string(o)
}

func (o ObjId) IsValid() bool {
	return o != NoObjId
}

func init() {
	rng = rand.New(rand.NewSource(time.Now().UTC().UnixNano()))
}

func NewVersion() Version {
	for {
		if v := Version(rng.Int63()); v != NoVersion {
			return v
		}
	}
}

// NewSyncd creates a new syncd instance.
//
// Syncd concurrency: syncd initializes three goroutines at
// startup. The "watcher" thread is responsible for watching the store
// for changes to its objects. The "initiator" thread is responsible
// for periodically checking the neighborhood and contacting a peer to
// obtain changes from that peer. The "gc" thread is responsible for
// periodically checking if any log records and dag state can be
// pruned. All these 3 threads perform write operations to the data
// structures, and synchronize by acquiring a write lock on s.lock. In
// addition, when syncd receives an incoming RPC, it responds to the
// request by acquiring a read lock on s.lock. Thus, at any instant in
// time, either one of the watcher, initiator or gc threads is active,
// or any number of responders can be active, serving incoming
// requests. Fairness between these threads follows from
// sync.RWMutex. The spec says that the writers cannot be starved by
// the readers but it does not guarantee FIFO. We may have to revisit
// this in the future.
func NewSyncd(devid string, syncTick time.Duration, server rpc.Server, ctx *context.T) *syncd {
	return newSyncdCore(devid, syncTick, server, ctx)
}

// newSyncdCore is the internal function that creates the Syncd
// structure and initilizes its thread (goroutines).  It takes a
// Veyron Store parameter to separate the core of Syncd setup from the
// external dependency on Veyron Store.
func newSyncdCore(devid string, syncTick time.Duration,
	server rpc.Server, ctx *context.T) *syncd {
	// TODO(kash): Get this to compile
	return nil
	// s := &syncd{ctx: ctx}

	// storePath := "/tmp"

	// // If no authorizer is specified, allow access to all Principals.
	// if s.auth = vflag.NewAuthorizerOrDie(); s.auth == nil {
	// 	s.auth = allowEveryone{}
	// }

	// // Bootstrap my own DeviceId.
	// s.id = DeviceId(devid)

	// // Cache the server to publish names if required.
	// s.server = server
	// s.relName = devIDToName(s.id)
	// s.names = make(map[string]struct{})

	// var err error
	// // Log init.
	// if s.log, err = openILog(storePath+"/vsync_ilog.db", s); err != nil {
	// 	vlog.Fatalf("newSyncd: openILog failed: err %v", err)
	// }

	// // DevTable init.
	// if s.devtab, err = openDevTable(storePath+"/vsync_dtab.db", s); err != nil {
	// 	vlog.Fatalf("newSyncd: openDevTable failed: err %v", err)
	// }

	// // Dag init.
	// if s.dag, err = openDAG(storePath + "/vsync_dag.db"); err != nil {
	// 	vlog.Fatalf("newSyncd: OpenDag failed: err %v", err)
	// }

	// // SyncGroupTable init.
	// if s.sgtab, err = openSyncGroupTable(storePath + "/vsync_sgtab.db"); err != nil {
	// 	vlog.Fatalf("newSyncd: openSyncGroupTable failed: err %v", err)
	// }

	// // Channel for responders to notify the SyncGroup refresher.
	// // Give the channel some buffering to ease a burst of responders
	// // while the refresher is still finishing a previous request.
	// // s.sgRefresh = make(chan *refreshRequest, 4096)

	// // Channel to propagate close event to all threads.
	// s.closed = make(chan struct{})

	// s.pending.Add(2)

	// // Get deltas every peerSyncInterval.
	// s.hdlInitiator = newInitiator(s, syncTick)
	// go s.hdlInitiator.contactPeers()

	// // // Garbage collect every garbageCollectInterval.
	// // s.hdlGC = newGC(s)
	// // go s.hdlGC.garbageCollect()

	// // Start a watcher thread that will get updates from local store.
	// s.hdlWatcher = newWatcher(s)
	// go s.hdlWatcher.watchStore()

	// // Start a thread to refresh SyncGroup info from SyncGroupServer.
	// // go s.syncGroupRefresher()

	// // Server stats.
	// stats.NewString(statsKvdbPath).Set(s.kvdbPath)
	// stats.NewString(statsDevId).Set(string(s.id))

	// return s
}

// devIDToName converts a device ID to its relative name.
func devIDToName(id DeviceId) string {
	return naming.Join([]string{"global", "vsync", string(id)}...)
}

// nameToDevId extracts the DeviceId from a name.
func (s *syncd) nameToDevId(name string) DeviceId {
	parts := strings.Split(name, "/")
	return DeviceId(parts[len(parts)-1])
}

// Close cleans up syncd state.
func (s *syncd) Close() {
	close(s.closed)
	s.pending.Wait()

	stats.Delete(statsKvdbPath)
	stats.Delete(statsDevId)

	// TODO(hpucha): close without flushing.
}

// isSyncClosing returns true if Close() was called i.e. the "closed" channel is closed.
func (s *syncd) isSyncClosing() bool {
	select {
	case <-s.closed:
		return true
	default:
		return false
	}
}

// GetDeltas responds to the incoming request from a client by sending missing generations to the client.
func (s *syncd) GetDeltas(call SyncGetDeltasServerCall, in map[ObjId]GenVector,
	syncGroups map[ObjId]GroupIdSet, clientID DeviceId) (map[ObjId]GenVector, error) {

	vlog.VI(1).Infof("GetDeltas:: Received vector %v from client %s", in, clientID)

	// Handle misconfiguration: the client cannot have the same ID as this node.
	if clientID == s.id {
		vlog.VI(1).Infof("GetDeltas:: impostor alert: client ID %s is the same as mine %s", clientID, s.id)
		return nil, fmt.Errorf("impostor: cannot be %s, for this node is %s", clientID, s.id)
	}

	out := make(map[ObjId]GenVector)
	for sr, gvIn := range in {
		gvOut, gens, gensInfo, err := s.prepareGensToReply(call, sr, syncGroups[sr], gvIn)
		if err != nil {
			vlog.Errorf("GetDeltas:: prepareGensToReply for sr %s failed with err %v",
				sr.String(), err)
			continue
		}

		for pos, v := range gens {
			gen := gensInfo[pos]
			var count uint64
			for i := SeqNum(0); i <= gen.MaxSeqNum; i++ {
				count++
				rec, err := s.getLogRec(v.devID, v.srID, v.genID, i)
				if err != nil {
					vlog.Fatalf("GetDeltas:: Couldn't get log record %s %s %d %d, err %v",
						v.devID, v.srID.String(), v.genID, i, err)
				}
				vlog.VI(1).Infof("Sending log record %v", rec)
				if err := call.SendStream().Send(*rec); err != nil {
					vlog.Errorf("GetDeltas:: Couldn't send stream err: %v", err)
					return nil, err
				}
			}
			if count != gen.Count {
				vlog.Fatalf("GetDeltas:: GenMetadata has incorrect log records for generation %s %s %d %v",
					v.devID, v.srID.String(), v.genID, gen)
			}
		}
		out[sr] = gvOut
	}

	// Notify the SyncGroup Refresher about the SyncGroup IDs associated
	// with the SyncRoots in the incoming request from the client.
	// Note: the initial slice capacity is only a lower-bound.
	// Note: by using the SyncRoots from the incoming "in" map and all
	// all SyncGroup IDs for each SyncRoot, the refresh is eager because
	// it does not filter out SyncRoots and SyncGroup IDs by the Permissions.
	// sgIDs := make([]syncgroup.Id, 0, len(in))
	// for sr := range in {
	//	for _, id := range syncGroups[sr] {
	//		sgIDs = append(sgIDs, id)
	//	}
	// }
	// s.notifySyncGroupRefresher(devIDToName(clientID), sgIDs)

	return out, nil
}

// // updateDeviceInfo updates the remote device's information based on
// // the incoming GetDeltas request.
// func (s *syncd) updateDeviceInfo(ClientID DeviceId, In GenVector) error {
//	s.lock.Lock()
//	defer s.lock.Unlock()

// // Note that the incoming client generation vector cannot be
// // used for garbage collection. We can only garbage collect
// // based on the generations we receive from other
// // devices. Receiving a set of generations assures that all
// // updates branching from those generations are also received
// // and hence generations present on all devices can be
// // GC'ed. This function sends generations to other devices and
// // hence does not use the generation vector for GC.
// //
// // TODO(hpucha): Cache the client's incoming generation vector
// // to assist in tracking missing generations and hence next
// // peer to contact.
//	if !s.devtab.hasDevInfo(ClientID) {
//		if err := s.devtab.addDevice(ClientID); err != nil {
//			return err
//		}
//	}
//	return nil
// }

// prepareGensToReply verifies if the requestor has access to the
// requested syncroot. If allowed, it processes the incoming
// generation vector and returns the metadata of all the missing
// generations between the incoming and the local generation vector
// for that syncroot.
func (s *syncd) prepareGensToReply(call rpc.ServerCall, srid ObjId, sgVec GroupIdSet, In GenVector) (GenVector, []*genOrder, []*genMetadata, error) {
	s.lock.RLock()
	defer s.lock.RUnlock()

	// Respond to the caller if any of the syncgroups
	// corresponding to the syncroot allow access.
	var allowedErr error
	// for _, sg := range sgVec {
	// 	// Check permissions for the syncgroup.
	// 	sgData, err := s.sgtab.getSyncGroupByID(sg)
	// 	if err != nil {
	// 		return GenVector{}, nil, nil, err
	// 	}
	// 	if vlog.V(2) {
	// 		b, _ := ctx.RemoteBlessings().ForContext(ctx)
	// 		vlog.Infof("prepareGensToReply:: matching acl %v, remote names %v", sgData.SrvInfo.Config.Permissions, b)
	// 	}
	// 	// TODO(ashankar): Consider changing TaggedPermissionsAuthorizer so that it doesn't return an error -
	// 	// instead of an error, it just locks down access completely. The error condition is really
	// 	// really rare and doing so will make for shorter code?
	// 	auth, err := access.TaggedPermissionsAuthorizer(sgData.SrvInfo.Config.Permissions, access.TypicalTagType())
	// 	if err != nil {
	// 		vlog.Errorf("unable to create authorizer: %v", err)
	// 		allowedErr = fmt.Errorf("server error: authorization policy misconfigured")
	// 	} else if allowedErr = auth.Authorize(ctx); allowedErr == nil {
	// 		break
	// 	}
	// }
	if allowedErr != nil {
		return GenVector{}, nil, nil, allowedErr
	}

	// // TODO(hpucha): Need only for GC.
	// if err := s.updateDeviceInfo(ClientID, gvIn); err != nil {
	//	vlog.Fatalf("GetDeltas:: updateDeviceInfo failed with err %v", err)
	// }

	// Get local generation vector.
	out, err := s.devtab.getGenVec(s.id, srid)
	if err != nil {
		return GenVector{}, nil, nil, err
	}

	// Diff the two generation vectors.
	gens, err := s.devtab.diffGenVectors(out, In, srid)
	if err != nil {
		return GenVector{}, nil, nil, err
	}

	// Get the metadata for all the generations in the reply.
	gensInfo := make([]*genMetadata, len(gens))
	for pos, v := range gens {
		gen, err := s.log.getGenMetadata(v.devID, v.srID, v.genID)
		if err != nil || gen.Count <= 0 {
			return GenVector{}, nil, nil, err
		}
		gensInfo[pos] = gen
	}

	return out, gens, gensInfo, nil
}

// getLogRec gets the log record for a given generation and lsn.
func (s *syncd) getLogRec(dev DeviceId, srid ObjId, gen GenId, lsn SeqNum) (*LogRec, error) {
	s.lock.RLock()
	defer s.lock.RUnlock()
	return s.log.getLogRec(dev, srid, gen, lsn)
}

// // GetSyncGroupStats gives the client high-level information on all SyncGroups.
// // The streamed information is returned sorted by SyncGroup name.
// func (s *syncd) GetSyncGroupStats(ctx SyncGetSyncGroupStatsContext) error {
// 	// Get a snapshot of the SyncGroup names.
// 	// The names are a copy, OK to unlock here.
// 	s.lock.RLock()
// 	names, err := s.sgtab.getAllSyncGroupNames()
// 	s.lock.RUnlock()

// 	if err != nil {
// 		vlog.Errorf("GetSyncGroupStats:: cannot get SyncGroup names: %v", err)
// 		return err
// 	}
// 	if len(names) == 0 {
// 		return nil
// 	}

// 	// Send the SyncGroup stats in the stream in sorted order.
// 	// Skip SyncGroups that got deleted in the meanwhile.
// 	sort.Strings(names)
// 	for _, name := range names {
// 		// The SyncGroup object is a copy, OK to unlock here.
// 		s.lock.RLock()
// 		sg, err := s.sgtab.getSyncGroupByName(name)
// 		s.lock.RUnlock()

// 		if err != nil {
// 			continue // skip deleted SyncGroup
// 		}

// 		stats := SyncGroupStats{
// 			Name:       name,
// 			SGObjId:      sg.SrvInfo.SGObjId,
// 			Path:       sg.LocalPath,
// 			RootObjId:    ObjId(sg.SrvInfo.RootObjId),
// 			NumJoiners: uint32(len(sg.SrvInfo.Joiners)),
// 		}

// 		if err = ctx.SendStream().Send(stats); err != nil {
// 			vlog.Errorf("GetSyncGroupStats:: cannot send stream data: %v", err)
// 			return err
// 		}
// 	}

// 	return nil
// }

// // GetSyncGroupMembers gives the client information on SyncGroup members.
// // If SyncGroup names are specified, only their member information is sent.
// // Otherwise information on members from all SyncGroups is sent.
// func (s *syncd) GetSyncGroupMembers(ctx SyncGetSyncGroupMembersContext, sgNames []string) error {

// 	// If SyncGroup names are given, fetch their IDs.
// 	// Also get a snapshot of the SyncGroup members.
// 	wantedGroupIds := make(map[syncgroup.Id]bool)

// 	s.lock.RLock()
// 	for _, name := range sgNames {
// 		sgid, err := s.sgtab.getSyncGroupID(name)
// 		if err != nil {
// 			s.lock.RUnlock()
// 			vlog.Errorf("GetSyncGroupMembers:: invalid SyncGroup %s: %v", name, err)
// 			return err
// 		}
// 		wantedGroupIds[sgid] = true
// 	}

// 	memberMap, err := s.sgtab.getMembers() // The map is a copy, OK to unlock here.
// 	s.lock.RUnlock()

// 	if err != nil {
// 		vlog.Errorf("GetSyncGroupMembers:: cannot get members: %v", err)
// 		return err
// 	}
// 	if len(memberMap) == 0 {
// 		return nil
// 	}

// 	// Send the member information in the stream in sorted order.
// 	// Skip members that left in the meanwhile.
// 	// If SyncGroup names are given, only return members in these groups.
// 	members := make([]string, 0, len(memberMap))
// 	for member := range memberMap {
// 		members = append(members, member)
// 	}
// 	sort.Strings(members)

// 	for _, member := range members {
// 		// The member info points to in-memory data; cannot unlock
// 		// while accessing that info.
// 		s.lock.RLock()
// 		info, err := s.sgtab.getMemberInfo(member)
// 		if err != nil {
// 			s.lock.RUnlock()
// 			continue // skip member no longer there
// 		}

// 		replyData := make([]SyncGroupMember, 0, len(info.gids))
// 		for sgid, meta := range info.gids {
// 			if len(wantedGroupIds) > 0 && !wantedGroupIds[sgid] {
// 				continue // skip unwanted SyncGroups
// 			}

// 			data := SyncGroupMember{
// 				Name:     member,
// 				SGObjId:    sgid,
// 				Metadata: meta.metaData,
// 			}
// 			replyData = append(replyData, data)
// 		}

// 		s.lock.RUnlock()

// 		for _, data := range replyData {
// 			if err = ctx.SendStream().Send(data); err != nil {
// 				vlog.Errorf("GetSyncGroupMembers:: cannot send stream data: %v", err)
// 				return err
// 			}
// 		}
// 	}

// 	return nil
// }

// Dump writes to the Sync log internal information used for debugging.
func (s *syncd) Dump(call rpc.ServerCall) error {
	s.lock.RLock()
	defer s.lock.RUnlock()

	vlog.VI(1).Infof("Dump: ==== BEGIN ====")
	s.devtab.dump()
	// s.sgtab.dump()
	s.log.dump()
	s.dag.dump()
	vlog.VI(1).Infof("Dump: ==== END ====")
	return nil
}

// GetDeviceStats gives the client information on devices that have synchronized
// with the server.
func (s *syncd) GetDeviceStats(call SyncGetDeviceStatsServerCall) error {
	// Get a snapshot of the device IDs.
	// The device IDs are copies, OK to unlock here.
	s.lock.RLock()
	myID := s.id
	devIDs, err := s.devtab.getDeviceIds()
	s.lock.RUnlock()

	if err != nil {
		vlog.Errorf("GetDeviceStats:: cannot get device IDs: %v", err)
		return err
	}

	// Send the device stats in the stream.
	for _, id := range devIDs {
		// The device info is a copy, OK to unlock here.
		s.lock.RLock()
		info, err := s.devtab.getDevInfo(id)
		s.lock.RUnlock()

		if err != nil {
			continue // skip unknown devices
		}

		stats := DeviceStats{
			DevId:      id,
			LastSync:   info.Ts.Unix(),
			GenVectors: info.Vectors,
			IsSelf:     id == myID,
		}

		if err = call.SendStream().Send(stats); err != nil {
			vlog.Errorf("GetDeviceStats:: cannot send stream data: %v", err)
			return err
		}
	}

	return nil
}

// GetObjectHistory gives the client the mutation history of a store object.
// It also returns the object version that is at the "head" of the graph (DAG).
func (s *syncd) GetObjectHistory(call SyncGetObjectHistoryServerCall, oid ObjId) (Version, error) {
	// Get the object version numbers in reverse chronological order,
	// starting from the "head" version back through its ancestors.
	// Also get the DAG parents of each object version.
	var versions []Version
	parents := make(map[Version][]Version)

	s.lock.RLock()
	head, err := s.dag.getHead(oid)
	if err != nil {
		// For now ignore objects that do not yet have a "head" version.
		// They are either not part of any SyncGroup, or this Sync server
		// has not finished catching up (Sync or Watch operations).
		vlog.Errorf("GetObjectHistory:: unknown object %v: %v", oid, err)
		s.lock.RUnlock()
		return NoVersion, err
	}

	s.dag.ancestorIter(oid, []Version{head},
		func(oid ObjId, v Version, node *dagNode) error {
			versions = append(versions, v)
			parents[v] = node.Parents
			return nil
		})
	s.lock.RUnlock()

	// Stream the object log records in the given order.
	for _, v := range versions {
		// The log record is a copy, OK to unlock here.
		s.lock.RLock()
		rec, err := s.hdlInitiator.getLogRec(oid, v)
		s.lock.RUnlock()

		if err != nil {
			vlog.Errorf("GetObjectHistory:: cannot get log record for %v:%v: %v", oid, v, err)
			return NoVersion, err
		}

		// To give the client a full DAG, send the parents as seen in
		// the DAG node instead of the ones in the log record.
		// Note: DAG nodes contain the full sets of parents whereas
		// log records may have subsets due to the different types of
		// records: NodeRec-Parents + LinkRec-Parent == DAG-Parents.
		rec.Parents = parents[v]

		if err = call.SendStream().Send(*rec); err != nil {
			vlog.Errorf("GetObjectHistory:: cannot send stream data: %v", err)
			return NoVersion, err
		}
	}

	return head, nil
}

// allowEveryone implements the authorization policy of ... allowing every principal.
type allowEveryone struct{}

func (allowEveryone) Authorize(security.Call) error { return nil }
