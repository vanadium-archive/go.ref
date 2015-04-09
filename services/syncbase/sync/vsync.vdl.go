// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// This file was auto-generated by the vanadium vdl tool.
// Source: vsync.vdl

package vsync

import (
	// VDL system imports
	"io"
	"v.io/v23"
	"v.io/v23/context"
	"v.io/v23/rpc"
	"v.io/v23/vdl"

	// VDL user imports
	"v.io/v23/security/access"
)

// temporary types
type ObjId string

func (ObjId) __VDLReflect(struct {
	Name string "v.io/syncbase/x/ref/services/syncbase/sync.ObjId"
}) {
}

type Version uint64

func (Version) __VDLReflect(struct {
	Name string "v.io/syncbase/x/ref/services/syncbase/sync.Version"
}) {
}

type GroupId uint64

func (GroupId) __VDLReflect(struct {
	Name string "v.io/syncbase/x/ref/services/syncbase/sync.GroupId"
}) {
}

// DeviceId is the globally unique Id of a device.
type DeviceId string

func (DeviceId) __VDLReflect(struct {
	Name string "v.io/syncbase/x/ref/services/syncbase/sync.DeviceId"
}) {
}

// GenId is the unique Id per generation per device.
type GenId uint64

func (GenId) __VDLReflect(struct {
	Name string "v.io/syncbase/x/ref/services/syncbase/sync.GenId"
}) {
}

// SeqNum is the log sequence number.
type SeqNum uint64

func (SeqNum) __VDLReflect(struct {
	Name string "v.io/syncbase/x/ref/services/syncbase/sync.SeqNum"
}) {
}

// GenVector is the generation vector.
type GenVector map[DeviceId]GenId

func (GenVector) __VDLReflect(struct {
	Name string "v.io/syncbase/x/ref/services/syncbase/sync.GenVector"
}) {
}

// TxId is the unique Id per transaction.
type TxId uint64

func (TxId) __VDLReflect(struct {
	Name string "v.io/syncbase/x/ref/services/syncbase/sync.TxId"
}) {
}

// GroupIdSet is the list of SyncGroup Ids.
type GroupIdSet []GroupId

func (GroupIdSet) __VDLReflect(struct {
	Name string "v.io/syncbase/x/ref/services/syncbase/sync.GroupIdSet"
}) {
}

// LogRec represents a single log record that is exchanged between two
// peers.
//
// It contains log related metadata: DevId is the id of the device
// that created the log record, SyncRootId is the id of a SyncRoot this log
// record was created under, GNum is the Id of the generation that the
// log record is part of, SeqNum is the log sequence number of the log
// record in the generation GNum, and RecType is the type of log
// record.
//
// It also contains information relevant to the updates to an object
// in the store: ObjId is the id of the object that was
// updated. CurVers is the current version number of the
// object. Parents can contain 0, 1 or 2 parent versions that the
// current version is derived from, and Value is the actual value of
// the object mutation.
type LogRec struct {
	// Log related information.
	DevId      DeviceId
	SyncRootId ObjId
	GenNum     GenId
	SeqNum     SeqNum
	RecType    byte
	// Object related information.
	ObjId   ObjId
	CurVers Version
	Parents []Version
	Value   LogValue
}

func (LogRec) __VDLReflect(struct {
	Name string "v.io/syncbase/x/ref/services/syncbase/sync.LogRec"
}) {
}

// LogValue represents an object mutation within a transaction.
type LogValue struct {
	// Mutation is the store mutation representing the change in the object.
	//Mutation raw.Mutation
	// SyncTime is the timestamp of the mutation when it arrives at the Sync server.
	SyncTime int64
	// Delete indicates whether the mutation resulted in the object being
	// deleted from the store.
	Delete bool
	// TxId is the unique Id of the transaction this mutation belongs to.
	TxId TxId
	// TxCount is the number of mutations in the transaction TxId.
	TxCount uint32
}

func (LogValue) __VDLReflect(struct {
	Name string "v.io/syncbase/x/ref/services/syncbase/sync.LogValue"
}) {
}

// DeviceStats contains high-level information on a device participating in
// peer-to-peer synchronization.
type DeviceStats struct {
	DevId      DeviceId            // Device Id.
	LastSync   int64               // Timestamp of last sync from the device.
	GenVectors map[ObjId]GenVector // Generation vectors per SyncRoot.
	IsSelf     bool                // True if the responder is on this device.
}

func (DeviceStats) __VDLReflect(struct {
	Name string "v.io/syncbase/x/ref/services/syncbase/sync.DeviceStats"
}) {
}

// SyncGroupStats contains high-level information on a SyncGroup.
type SyncGroupStats struct {
	Name       string  // Global name of the SyncGroup.
	Id         GroupId // Global Id of the SyncGroup.
	Path       string  // Local store path for the root of the SyncGroup.
	RootObjId  ObjId   // Id of the store object at the root path.
	NumJoiners uint32  // Number of members currently in the SyncGroup.
}

func (SyncGroupStats) __VDLReflect(struct {
	Name string "v.io/syncbase/x/ref/services/syncbase/sync.SyncGroupStats"
}) {
}

// SyncGroupMember contains information on a SyncGroup member.
type SyncGroupMember struct {
	Name     string         // Name of SyncGroup member.
	Id       GroupId        // Global Id of the SyncGroup.
	Metadata JoinerMetaData // Extra member metadata.
}

func (SyncGroupMember) __VDLReflect(struct {
	Name string "v.io/syncbase/x/ref/services/syncbase/sync.SyncGroupMember"
}) {
}

// A SyncGroupInfo is the conceptual state of a SyncGroup object.
type SyncGroupInfo struct {
	Id         GroupId         // Globally unique SyncGroup Id.
	ServerName string          // Global Vanadium name of SyncGroupServer.
	GroupName  string          // Relative name of group; global name is ServerName/GroupName.
	Config     SyncGroupConfig // Configuration parameters of this SyncGroup.
	ETag       string          // Version Id for concurrency control.
	// A map from joiner names to the associated metaData for devices that
	// have called Join() or Create() and not subsequently called Leave()
	// or had Eject() called on them.  The map returned by the calls below
	// may contain only a subset of joiners if the number is large.
	Joiners map[string]JoinerMetaData
	// Blessings for joiners of this SyncGroup will be self-signed by the
	// SyncGroupServer, and will have names matching
	// JoinerBlessingPrefix/Name/...
	JoinerBlessingPrefix string
}

func (SyncGroupInfo) __VDLReflect(struct {
	Name string "v.io/syncbase/x/ref/services/syncbase/sync.SyncGroupInfo"
}) {
}

// A SyncGroupConfig contains some fields of SyncGroupInfo that
// are passed at create time, but which can be changed later.
type SyncGroupConfig struct {
	Desc        string                // Human readable description.
	Options     map[string]*vdl.Value // Options for future evolution.
	Permissions access.Permissions    // The object's Permissions.
	// Mount tables used to advertise for synchronization.
	// Typically, we will have only one entry.  However, an array allows
	// mount tables to be changed over time.
	MountTables            []string
	BlessingsDurationNanos int64 // Duration of blessings, in nanoseconds. 0 => use server default.
}

func (SyncGroupConfig) __VDLReflect(struct {
	Name string "v.io/syncbase/x/ref/services/syncbase/sync.SyncGroupConfig"
}) {
}

// A JoinerMetaData contains the non-name information stored per joiner.
type JoinerMetaData struct {
	// SyncPriority is a hint to bias the choice of syncing partners.
	// Members of the SyncGroup should choose to synchronize more often
	// with partners with lower values.
	SyncPriority int32
}

func (JoinerMetaData) __VDLReflect(struct {
	Name string "v.io/syncbase/x/ref/services/syncbase/sync.JoinerMetaData"
}) {
}

func init() {
	vdl.Register((*ObjId)(nil))
	vdl.Register((*Version)(nil))
	vdl.Register((*GroupId)(nil))
	vdl.Register((*DeviceId)(nil))
	vdl.Register((*GenId)(nil))
	vdl.Register((*SeqNum)(nil))
	vdl.Register((*GenVector)(nil))
	vdl.Register((*TxId)(nil))
	vdl.Register((*GroupIdSet)(nil))
	vdl.Register((*LogRec)(nil))
	vdl.Register((*LogValue)(nil))
	vdl.Register((*DeviceStats)(nil))
	vdl.Register((*SyncGroupStats)(nil))
	vdl.Register((*SyncGroupMember)(nil))
	vdl.Register((*SyncGroupInfo)(nil))
	vdl.Register((*SyncGroupConfig)(nil))
	vdl.Register((*JoinerMetaData)(nil))
}

// NodeRec type log record adds a new node in the dag.
const NodeRec = byte(0)

// LinkRec type log record adds a new link in the dag.
const LinkRec = byte(1)

// Sync interface has Object name "global/vsync/<devid>/sync".
const SyncSuffix = "sync"

// temporary nil values
const NoObjId = ObjId("")

const NoVersion = Version(0)

const NoGroupId = GroupId(0)

// SyncClientMethods is the client interface
// containing Sync methods.
//
// Sync allows a device to GetDeltas from another device.
type SyncClientMethods interface {
	// GetDeltas returns a device's current generation vector and all
	// the missing log records when compared to the incoming generation vector.
	GetDeltas(ctx *context.T, in map[ObjId]GenVector, sgs map[ObjId]GroupIdSet, clientId DeviceId, opts ...rpc.CallOpt) (SyncGetDeltasClientCall, error)
	// GetObjectHistory returns the mutation history of a store object.
	GetObjectHistory(ctx *context.T, oid ObjId, opts ...rpc.CallOpt) (SyncGetObjectHistoryClientCall, error)
	// GetDeviceStats returns information on devices participating in
	// peer-to-peer synchronization.
	GetDeviceStats(*context.T, ...rpc.CallOpt) (SyncGetDeviceStatsClientCall, error)
	// GetSyncGroupMembers returns information on SyncGroup members.
	// If SyncGroup names are specified, only members of these SyncGroups
	// are returned.  Otherwise, if the slice of names is nil or empty,
	// members of all SyncGroups are returned.
	GetSyncGroupMembers(ctx *context.T, sgNames []string, opts ...rpc.CallOpt) (SyncGetSyncGroupMembersClientCall, error)
	// GetSyncGroupStats returns high-level information on all SyncGroups.
	GetSyncGroupStats(*context.T, ...rpc.CallOpt) (SyncGetSyncGroupStatsClientCall, error)
	// Dump writes to the Sync log internal information used for debugging.
	Dump(*context.T, ...rpc.CallOpt) error
}

// SyncClientStub adds universal methods to SyncClientMethods.
type SyncClientStub interface {
	SyncClientMethods
	rpc.UniversalServiceMethods
}

// SyncClient returns a client stub for Sync.
func SyncClient(name string) SyncClientStub {
	return implSyncClientStub{name}
}

type implSyncClientStub struct {
	name string
}

func (c implSyncClientStub) GetDeltas(ctx *context.T, i0 map[ObjId]GenVector, i1 map[ObjId]GroupIdSet, i2 DeviceId, opts ...rpc.CallOpt) (ocall SyncGetDeltasClientCall, err error) {
	var call rpc.ClientCall
	if call, err = v23.GetClient(ctx).StartCall(ctx, c.name, "GetDeltas", []interface{}{i0, i1, i2}, opts...); err != nil {
		return
	}
	ocall = &implSyncGetDeltasClientCall{ClientCall: call}
	return
}

func (c implSyncClientStub) GetObjectHistory(ctx *context.T, i0 ObjId, opts ...rpc.CallOpt) (ocall SyncGetObjectHistoryClientCall, err error) {
	var call rpc.ClientCall
	if call, err = v23.GetClient(ctx).StartCall(ctx, c.name, "GetObjectHistory", []interface{}{i0}, opts...); err != nil {
		return
	}
	ocall = &implSyncGetObjectHistoryClientCall{ClientCall: call}
	return
}

func (c implSyncClientStub) GetDeviceStats(ctx *context.T, opts ...rpc.CallOpt) (ocall SyncGetDeviceStatsClientCall, err error) {
	var call rpc.ClientCall
	if call, err = v23.GetClient(ctx).StartCall(ctx, c.name, "GetDeviceStats", nil, opts...); err != nil {
		return
	}
	ocall = &implSyncGetDeviceStatsClientCall{ClientCall: call}
	return
}

func (c implSyncClientStub) GetSyncGroupMembers(ctx *context.T, i0 []string, opts ...rpc.CallOpt) (ocall SyncGetSyncGroupMembersClientCall, err error) {
	var call rpc.ClientCall
	if call, err = v23.GetClient(ctx).StartCall(ctx, c.name, "GetSyncGroupMembers", []interface{}{i0}, opts...); err != nil {
		return
	}
	ocall = &implSyncGetSyncGroupMembersClientCall{ClientCall: call}
	return
}

func (c implSyncClientStub) GetSyncGroupStats(ctx *context.T, opts ...rpc.CallOpt) (ocall SyncGetSyncGroupStatsClientCall, err error) {
	var call rpc.ClientCall
	if call, err = v23.GetClient(ctx).StartCall(ctx, c.name, "GetSyncGroupStats", nil, opts...); err != nil {
		return
	}
	ocall = &implSyncGetSyncGroupStatsClientCall{ClientCall: call}
	return
}

func (c implSyncClientStub) Dump(ctx *context.T, opts ...rpc.CallOpt) (err error) {
	var call rpc.ClientCall
	if call, err = v23.GetClient(ctx).StartCall(ctx, c.name, "Dump", nil, opts...); err != nil {
		return
	}
	err = call.Finish()
	return
}

// SyncGetDeltasClientStream is the client stream for Sync.GetDeltas.
type SyncGetDeltasClientStream interface {
	// RecvStream returns the receiver side of the Sync.GetDeltas client stream.
	RecvStream() interface {
		// Advance stages an item so that it may be retrieved via Value.  Returns
		// true iff there is an item to retrieve.  Advance must be called before
		// Value is called.  May block if an item is not available.
		Advance() bool
		// Value returns the item that was staged by Advance.  May panic if Advance
		// returned false or was not called.  Never blocks.
		Value() LogRec
		// Err returns any error encountered by Advance.  Never blocks.
		Err() error
	}
}

// SyncGetDeltasClientCall represents the call returned from Sync.GetDeltas.
type SyncGetDeltasClientCall interface {
	SyncGetDeltasClientStream
	// Finish blocks until the server is done, and returns the positional return
	// values for call.
	//
	// Finish returns immediately if the call has been canceled; depending on the
	// timing the output could either be an error signaling cancelation, or the
	// valid positional return values from the server.
	//
	// Calling Finish is mandatory for releasing stream resources, unless the call
	// has been canceled or any of the other methods return an error.  Finish should
	// be called at most once.
	Finish() (map[ObjId]GenVector, error)
}

type implSyncGetDeltasClientCall struct {
	rpc.ClientCall
	valRecv LogRec
	errRecv error
}

func (c *implSyncGetDeltasClientCall) RecvStream() interface {
	Advance() bool
	Value() LogRec
	Err() error
} {
	return implSyncGetDeltasClientCallRecv{c}
}

type implSyncGetDeltasClientCallRecv struct {
	c *implSyncGetDeltasClientCall
}

func (c implSyncGetDeltasClientCallRecv) Advance() bool {
	c.c.valRecv = LogRec{}
	c.c.errRecv = c.c.Recv(&c.c.valRecv)
	return c.c.errRecv == nil
}
func (c implSyncGetDeltasClientCallRecv) Value() LogRec {
	return c.c.valRecv
}
func (c implSyncGetDeltasClientCallRecv) Err() error {
	if c.c.errRecv == io.EOF {
		return nil
	}
	return c.c.errRecv
}
func (c *implSyncGetDeltasClientCall) Finish() (o0 map[ObjId]GenVector, err error) {
	err = c.ClientCall.Finish(&o0)
	return
}

// SyncGetObjectHistoryClientStream is the client stream for Sync.GetObjectHistory.
type SyncGetObjectHistoryClientStream interface {
	// RecvStream returns the receiver side of the Sync.GetObjectHistory client stream.
	RecvStream() interface {
		// Advance stages an item so that it may be retrieved via Value.  Returns
		// true iff there is an item to retrieve.  Advance must be called before
		// Value is called.  May block if an item is not available.
		Advance() bool
		// Value returns the item that was staged by Advance.  May panic if Advance
		// returned false or was not called.  Never blocks.
		Value() LogRec
		// Err returns any error encountered by Advance.  Never blocks.
		Err() error
	}
}

// SyncGetObjectHistoryClientCall represents the call returned from Sync.GetObjectHistory.
type SyncGetObjectHistoryClientCall interface {
	SyncGetObjectHistoryClientStream
	// Finish blocks until the server is done, and returns the positional return
	// values for call.
	//
	// Finish returns immediately if the call has been canceled; depending on the
	// timing the output could either be an error signaling cancelation, or the
	// valid positional return values from the server.
	//
	// Calling Finish is mandatory for releasing stream resources, unless the call
	// has been canceled or any of the other methods return an error.  Finish should
	// be called at most once.
	Finish() (Version, error)
}

type implSyncGetObjectHistoryClientCall struct {
	rpc.ClientCall
	valRecv LogRec
	errRecv error
}

func (c *implSyncGetObjectHistoryClientCall) RecvStream() interface {
	Advance() bool
	Value() LogRec
	Err() error
} {
	return implSyncGetObjectHistoryClientCallRecv{c}
}

type implSyncGetObjectHistoryClientCallRecv struct {
	c *implSyncGetObjectHistoryClientCall
}

func (c implSyncGetObjectHistoryClientCallRecv) Advance() bool {
	c.c.valRecv = LogRec{}
	c.c.errRecv = c.c.Recv(&c.c.valRecv)
	return c.c.errRecv == nil
}
func (c implSyncGetObjectHistoryClientCallRecv) Value() LogRec {
	return c.c.valRecv
}
func (c implSyncGetObjectHistoryClientCallRecv) Err() error {
	if c.c.errRecv == io.EOF {
		return nil
	}
	return c.c.errRecv
}
func (c *implSyncGetObjectHistoryClientCall) Finish() (o0 Version, err error) {
	err = c.ClientCall.Finish(&o0)
	return
}

// SyncGetDeviceStatsClientStream is the client stream for Sync.GetDeviceStats.
type SyncGetDeviceStatsClientStream interface {
	// RecvStream returns the receiver side of the Sync.GetDeviceStats client stream.
	RecvStream() interface {
		// Advance stages an item so that it may be retrieved via Value.  Returns
		// true iff there is an item to retrieve.  Advance must be called before
		// Value is called.  May block if an item is not available.
		Advance() bool
		// Value returns the item that was staged by Advance.  May panic if Advance
		// returned false or was not called.  Never blocks.
		Value() DeviceStats
		// Err returns any error encountered by Advance.  Never blocks.
		Err() error
	}
}

// SyncGetDeviceStatsClientCall represents the call returned from Sync.GetDeviceStats.
type SyncGetDeviceStatsClientCall interface {
	SyncGetDeviceStatsClientStream
	// Finish blocks until the server is done, and returns the positional return
	// values for call.
	//
	// Finish returns immediately if the call has been canceled; depending on the
	// timing the output could either be an error signaling cancelation, or the
	// valid positional return values from the server.
	//
	// Calling Finish is mandatory for releasing stream resources, unless the call
	// has been canceled or any of the other methods return an error.  Finish should
	// be called at most once.
	Finish() error
}

type implSyncGetDeviceStatsClientCall struct {
	rpc.ClientCall
	valRecv DeviceStats
	errRecv error
}

func (c *implSyncGetDeviceStatsClientCall) RecvStream() interface {
	Advance() bool
	Value() DeviceStats
	Err() error
} {
	return implSyncGetDeviceStatsClientCallRecv{c}
}

type implSyncGetDeviceStatsClientCallRecv struct {
	c *implSyncGetDeviceStatsClientCall
}

func (c implSyncGetDeviceStatsClientCallRecv) Advance() bool {
	c.c.valRecv = DeviceStats{}
	c.c.errRecv = c.c.Recv(&c.c.valRecv)
	return c.c.errRecv == nil
}
func (c implSyncGetDeviceStatsClientCallRecv) Value() DeviceStats {
	return c.c.valRecv
}
func (c implSyncGetDeviceStatsClientCallRecv) Err() error {
	if c.c.errRecv == io.EOF {
		return nil
	}
	return c.c.errRecv
}
func (c *implSyncGetDeviceStatsClientCall) Finish() (err error) {
	err = c.ClientCall.Finish()
	return
}

// SyncGetSyncGroupMembersClientStream is the client stream for Sync.GetSyncGroupMembers.
type SyncGetSyncGroupMembersClientStream interface {
	// RecvStream returns the receiver side of the Sync.GetSyncGroupMembers client stream.
	RecvStream() interface {
		// Advance stages an item so that it may be retrieved via Value.  Returns
		// true iff there is an item to retrieve.  Advance must be called before
		// Value is called.  May block if an item is not available.
		Advance() bool
		// Value returns the item that was staged by Advance.  May panic if Advance
		// returned false or was not called.  Never blocks.
		Value() SyncGroupMember
		// Err returns any error encountered by Advance.  Never blocks.
		Err() error
	}
}

// SyncGetSyncGroupMembersClientCall represents the call returned from Sync.GetSyncGroupMembers.
type SyncGetSyncGroupMembersClientCall interface {
	SyncGetSyncGroupMembersClientStream
	// Finish blocks until the server is done, and returns the positional return
	// values for call.
	//
	// Finish returns immediately if the call has been canceled; depending on the
	// timing the output could either be an error signaling cancelation, or the
	// valid positional return values from the server.
	//
	// Calling Finish is mandatory for releasing stream resources, unless the call
	// has been canceled or any of the other methods return an error.  Finish should
	// be called at most once.
	Finish() error
}

type implSyncGetSyncGroupMembersClientCall struct {
	rpc.ClientCall
	valRecv SyncGroupMember
	errRecv error
}

func (c *implSyncGetSyncGroupMembersClientCall) RecvStream() interface {
	Advance() bool
	Value() SyncGroupMember
	Err() error
} {
	return implSyncGetSyncGroupMembersClientCallRecv{c}
}

type implSyncGetSyncGroupMembersClientCallRecv struct {
	c *implSyncGetSyncGroupMembersClientCall
}

func (c implSyncGetSyncGroupMembersClientCallRecv) Advance() bool {
	c.c.valRecv = SyncGroupMember{}
	c.c.errRecv = c.c.Recv(&c.c.valRecv)
	return c.c.errRecv == nil
}
func (c implSyncGetSyncGroupMembersClientCallRecv) Value() SyncGroupMember {
	return c.c.valRecv
}
func (c implSyncGetSyncGroupMembersClientCallRecv) Err() error {
	if c.c.errRecv == io.EOF {
		return nil
	}
	return c.c.errRecv
}
func (c *implSyncGetSyncGroupMembersClientCall) Finish() (err error) {
	err = c.ClientCall.Finish()
	return
}

// SyncGetSyncGroupStatsClientStream is the client stream for Sync.GetSyncGroupStats.
type SyncGetSyncGroupStatsClientStream interface {
	// RecvStream returns the receiver side of the Sync.GetSyncGroupStats client stream.
	RecvStream() interface {
		// Advance stages an item so that it may be retrieved via Value.  Returns
		// true iff there is an item to retrieve.  Advance must be called before
		// Value is called.  May block if an item is not available.
		Advance() bool
		// Value returns the item that was staged by Advance.  May panic if Advance
		// returned false or was not called.  Never blocks.
		Value() SyncGroupStats
		// Err returns any error encountered by Advance.  Never blocks.
		Err() error
	}
}

// SyncGetSyncGroupStatsClientCall represents the call returned from Sync.GetSyncGroupStats.
type SyncGetSyncGroupStatsClientCall interface {
	SyncGetSyncGroupStatsClientStream
	// Finish blocks until the server is done, and returns the positional return
	// values for call.
	//
	// Finish returns immediately if the call has been canceled; depending on the
	// timing the output could either be an error signaling cancelation, or the
	// valid positional return values from the server.
	//
	// Calling Finish is mandatory for releasing stream resources, unless the call
	// has been canceled or any of the other methods return an error.  Finish should
	// be called at most once.
	Finish() error
}

type implSyncGetSyncGroupStatsClientCall struct {
	rpc.ClientCall
	valRecv SyncGroupStats
	errRecv error
}

func (c *implSyncGetSyncGroupStatsClientCall) RecvStream() interface {
	Advance() bool
	Value() SyncGroupStats
	Err() error
} {
	return implSyncGetSyncGroupStatsClientCallRecv{c}
}

type implSyncGetSyncGroupStatsClientCallRecv struct {
	c *implSyncGetSyncGroupStatsClientCall
}

func (c implSyncGetSyncGroupStatsClientCallRecv) Advance() bool {
	c.c.valRecv = SyncGroupStats{}
	c.c.errRecv = c.c.Recv(&c.c.valRecv)
	return c.c.errRecv == nil
}
func (c implSyncGetSyncGroupStatsClientCallRecv) Value() SyncGroupStats {
	return c.c.valRecv
}
func (c implSyncGetSyncGroupStatsClientCallRecv) Err() error {
	if c.c.errRecv == io.EOF {
		return nil
	}
	return c.c.errRecv
}
func (c *implSyncGetSyncGroupStatsClientCall) Finish() (err error) {
	err = c.ClientCall.Finish()
	return
}

// SyncServerMethods is the interface a server writer
// implements for Sync.
//
// Sync allows a device to GetDeltas from another device.
type SyncServerMethods interface {
	// GetDeltas returns a device's current generation vector and all
	// the missing log records when compared to the incoming generation vector.
	GetDeltas(call SyncGetDeltasServerCall, in map[ObjId]GenVector, sgs map[ObjId]GroupIdSet, clientId DeviceId) (map[ObjId]GenVector, error)
	// GetObjectHistory returns the mutation history of a store object.
	GetObjectHistory(call SyncGetObjectHistoryServerCall, oid ObjId) (Version, error)
	// GetDeviceStats returns information on devices participating in
	// peer-to-peer synchronization.
	GetDeviceStats(SyncGetDeviceStatsServerCall) error
	// GetSyncGroupMembers returns information on SyncGroup members.
	// If SyncGroup names are specified, only members of these SyncGroups
	// are returned.  Otherwise, if the slice of names is nil or empty,
	// members of all SyncGroups are returned.
	GetSyncGroupMembers(call SyncGetSyncGroupMembersServerCall, sgNames []string) error
	// GetSyncGroupStats returns high-level information on all SyncGroups.
	GetSyncGroupStats(SyncGetSyncGroupStatsServerCall) error
	// Dump writes to the Sync log internal information used for debugging.
	Dump(rpc.ServerCall) error
}

// SyncServerStubMethods is the server interface containing
// Sync methods, as expected by rpc.Server.
// The only difference between this interface and SyncServerMethods
// is the streaming methods.
type SyncServerStubMethods interface {
	// GetDeltas returns a device's current generation vector and all
	// the missing log records when compared to the incoming generation vector.
	GetDeltas(call *SyncGetDeltasServerCallStub, in map[ObjId]GenVector, sgs map[ObjId]GroupIdSet, clientId DeviceId) (map[ObjId]GenVector, error)
	// GetObjectHistory returns the mutation history of a store object.
	GetObjectHistory(call *SyncGetObjectHistoryServerCallStub, oid ObjId) (Version, error)
	// GetDeviceStats returns information on devices participating in
	// peer-to-peer synchronization.
	GetDeviceStats(*SyncGetDeviceStatsServerCallStub) error
	// GetSyncGroupMembers returns information on SyncGroup members.
	// If SyncGroup names are specified, only members of these SyncGroups
	// are returned.  Otherwise, if the slice of names is nil or empty,
	// members of all SyncGroups are returned.
	GetSyncGroupMembers(call *SyncGetSyncGroupMembersServerCallStub, sgNames []string) error
	// GetSyncGroupStats returns high-level information on all SyncGroups.
	GetSyncGroupStats(*SyncGetSyncGroupStatsServerCallStub) error
	// Dump writes to the Sync log internal information used for debugging.
	Dump(rpc.ServerCall) error
}

// SyncServerStub adds universal methods to SyncServerStubMethods.
type SyncServerStub interface {
	SyncServerStubMethods
	// Describe the Sync interfaces.
	Describe__() []rpc.InterfaceDesc
}

// SyncServer returns a server stub for Sync.
// It converts an implementation of SyncServerMethods into
// an object that may be used by rpc.Server.
func SyncServer(impl SyncServerMethods) SyncServerStub {
	stub := implSyncServerStub{
		impl: impl,
	}
	// Initialize GlobState; always check the stub itself first, to handle the
	// case where the user has the Glob method defined in their VDL source.
	if gs := rpc.NewGlobState(stub); gs != nil {
		stub.gs = gs
	} else if gs := rpc.NewGlobState(impl); gs != nil {
		stub.gs = gs
	}
	return stub
}

type implSyncServerStub struct {
	impl SyncServerMethods
	gs   *rpc.GlobState
}

func (s implSyncServerStub) GetDeltas(call *SyncGetDeltasServerCallStub, i0 map[ObjId]GenVector, i1 map[ObjId]GroupIdSet, i2 DeviceId) (map[ObjId]GenVector, error) {
	return s.impl.GetDeltas(call, i0, i1, i2)
}

func (s implSyncServerStub) GetObjectHistory(call *SyncGetObjectHistoryServerCallStub, i0 ObjId) (Version, error) {
	return s.impl.GetObjectHistory(call, i0)
}

func (s implSyncServerStub) GetDeviceStats(call *SyncGetDeviceStatsServerCallStub) error {
	return s.impl.GetDeviceStats(call)
}

func (s implSyncServerStub) GetSyncGroupMembers(call *SyncGetSyncGroupMembersServerCallStub, i0 []string) error {
	return s.impl.GetSyncGroupMembers(call, i0)
}

func (s implSyncServerStub) GetSyncGroupStats(call *SyncGetSyncGroupStatsServerCallStub) error {
	return s.impl.GetSyncGroupStats(call)
}

func (s implSyncServerStub) Dump(call rpc.ServerCall) error {
	return s.impl.Dump(call)
}

func (s implSyncServerStub) Globber() *rpc.GlobState {
	return s.gs
}

func (s implSyncServerStub) Describe__() []rpc.InterfaceDesc {
	return []rpc.InterfaceDesc{SyncDesc}
}

// SyncDesc describes the Sync interface.
var SyncDesc rpc.InterfaceDesc = descSync

// descSync hides the desc to keep godoc clean.
var descSync = rpc.InterfaceDesc{
	Name:    "Sync",
	PkgPath: "v.io/syncbase/x/ref/services/syncbase/sync",
	Doc:     "// Sync allows a device to GetDeltas from another device.",
	Methods: []rpc.MethodDesc{
		{
			Name: "GetDeltas",
			Doc:  "// GetDeltas returns a device's current generation vector and all\n// the missing log records when compared to the incoming generation vector.",
			InArgs: []rpc.ArgDesc{
				{"in", ``},       // map[ObjId]GenVector
				{"sgs", ``},      // map[ObjId]GroupIdSet
				{"clientId", ``}, // DeviceId
			},
			OutArgs: []rpc.ArgDesc{
				{"", ``}, // map[ObjId]GenVector
			},
			Tags: []*vdl.Value{vdl.ValueOf(access.Tag("Write"))},
		},
		{
			Name: "GetObjectHistory",
			Doc:  "// GetObjectHistory returns the mutation history of a store object.",
			InArgs: []rpc.ArgDesc{
				{"oid", ``}, // ObjId
			},
			OutArgs: []rpc.ArgDesc{
				{"", ``}, // Version
			},
		},
		{
			Name: "GetDeviceStats",
			Doc:  "// GetDeviceStats returns information on devices participating in\n// peer-to-peer synchronization.",
			Tags: []*vdl.Value{vdl.ValueOf(access.Tag("Admin"))},
		},
		{
			Name: "GetSyncGroupMembers",
			Doc:  "// GetSyncGroupMembers returns information on SyncGroup members.\n// If SyncGroup names are specified, only members of these SyncGroups\n// are returned.  Otherwise, if the slice of names is nil or empty,\n// members of all SyncGroups are returned.",
			InArgs: []rpc.ArgDesc{
				{"sgNames", ``}, // []string
			},
			Tags: []*vdl.Value{vdl.ValueOf(access.Tag("Admin"))},
		},
		{
			Name: "GetSyncGroupStats",
			Doc:  "// GetSyncGroupStats returns high-level information on all SyncGroups.",
			Tags: []*vdl.Value{vdl.ValueOf(access.Tag("Admin"))},
		},
		{
			Name: "Dump",
			Doc:  "// Dump writes to the Sync log internal information used for debugging.",
			Tags: []*vdl.Value{vdl.ValueOf(access.Tag("Admin"))},
		},
	},
}

// SyncGetDeltasServerStream is the server stream for Sync.GetDeltas.
type SyncGetDeltasServerStream interface {
	// SendStream returns the send side of the Sync.GetDeltas server stream.
	SendStream() interface {
		// Send places the item onto the output stream.  Returns errors encountered
		// while sending.  Blocks if there is no buffer space; will unblock when
		// buffer space is available.
		Send(item LogRec) error
	}
}

// SyncGetDeltasServerCall represents the context passed to Sync.GetDeltas.
type SyncGetDeltasServerCall interface {
	rpc.ServerCall
	SyncGetDeltasServerStream
}

// SyncGetDeltasServerCallStub is a wrapper that converts rpc.StreamServerCall into
// a typesafe stub that implements SyncGetDeltasServerCall.
type SyncGetDeltasServerCallStub struct {
	rpc.StreamServerCall
}

// Init initializes SyncGetDeltasServerCallStub from rpc.StreamServerCall.
func (s *SyncGetDeltasServerCallStub) Init(call rpc.StreamServerCall) {
	s.StreamServerCall = call
}

// SendStream returns the send side of the Sync.GetDeltas server stream.
func (s *SyncGetDeltasServerCallStub) SendStream() interface {
	Send(item LogRec) error
} {
	return implSyncGetDeltasServerCallSend{s}
}

type implSyncGetDeltasServerCallSend struct {
	s *SyncGetDeltasServerCallStub
}

func (s implSyncGetDeltasServerCallSend) Send(item LogRec) error {
	return s.s.Send(item)
}

// SyncGetObjectHistoryServerStream is the server stream for Sync.GetObjectHistory.
type SyncGetObjectHistoryServerStream interface {
	// SendStream returns the send side of the Sync.GetObjectHistory server stream.
	SendStream() interface {
		// Send places the item onto the output stream.  Returns errors encountered
		// while sending.  Blocks if there is no buffer space; will unblock when
		// buffer space is available.
		Send(item LogRec) error
	}
}

// SyncGetObjectHistoryServerCall represents the context passed to Sync.GetObjectHistory.
type SyncGetObjectHistoryServerCall interface {
	rpc.ServerCall
	SyncGetObjectHistoryServerStream
}

// SyncGetObjectHistoryServerCallStub is a wrapper that converts rpc.StreamServerCall into
// a typesafe stub that implements SyncGetObjectHistoryServerCall.
type SyncGetObjectHistoryServerCallStub struct {
	rpc.StreamServerCall
}

// Init initializes SyncGetObjectHistoryServerCallStub from rpc.StreamServerCall.
func (s *SyncGetObjectHistoryServerCallStub) Init(call rpc.StreamServerCall) {
	s.StreamServerCall = call
}

// SendStream returns the send side of the Sync.GetObjectHistory server stream.
func (s *SyncGetObjectHistoryServerCallStub) SendStream() interface {
	Send(item LogRec) error
} {
	return implSyncGetObjectHistoryServerCallSend{s}
}

type implSyncGetObjectHistoryServerCallSend struct {
	s *SyncGetObjectHistoryServerCallStub
}

func (s implSyncGetObjectHistoryServerCallSend) Send(item LogRec) error {
	return s.s.Send(item)
}

// SyncGetDeviceStatsServerStream is the server stream for Sync.GetDeviceStats.
type SyncGetDeviceStatsServerStream interface {
	// SendStream returns the send side of the Sync.GetDeviceStats server stream.
	SendStream() interface {
		// Send places the item onto the output stream.  Returns errors encountered
		// while sending.  Blocks if there is no buffer space; will unblock when
		// buffer space is available.
		Send(item DeviceStats) error
	}
}

// SyncGetDeviceStatsServerCall represents the context passed to Sync.GetDeviceStats.
type SyncGetDeviceStatsServerCall interface {
	rpc.ServerCall
	SyncGetDeviceStatsServerStream
}

// SyncGetDeviceStatsServerCallStub is a wrapper that converts rpc.StreamServerCall into
// a typesafe stub that implements SyncGetDeviceStatsServerCall.
type SyncGetDeviceStatsServerCallStub struct {
	rpc.StreamServerCall
}

// Init initializes SyncGetDeviceStatsServerCallStub from rpc.StreamServerCall.
func (s *SyncGetDeviceStatsServerCallStub) Init(call rpc.StreamServerCall) {
	s.StreamServerCall = call
}

// SendStream returns the send side of the Sync.GetDeviceStats server stream.
func (s *SyncGetDeviceStatsServerCallStub) SendStream() interface {
	Send(item DeviceStats) error
} {
	return implSyncGetDeviceStatsServerCallSend{s}
}

type implSyncGetDeviceStatsServerCallSend struct {
	s *SyncGetDeviceStatsServerCallStub
}

func (s implSyncGetDeviceStatsServerCallSend) Send(item DeviceStats) error {
	return s.s.Send(item)
}

// SyncGetSyncGroupMembersServerStream is the server stream for Sync.GetSyncGroupMembers.
type SyncGetSyncGroupMembersServerStream interface {
	// SendStream returns the send side of the Sync.GetSyncGroupMembers server stream.
	SendStream() interface {
		// Send places the item onto the output stream.  Returns errors encountered
		// while sending.  Blocks if there is no buffer space; will unblock when
		// buffer space is available.
		Send(item SyncGroupMember) error
	}
}

// SyncGetSyncGroupMembersServerCall represents the context passed to Sync.GetSyncGroupMembers.
type SyncGetSyncGroupMembersServerCall interface {
	rpc.ServerCall
	SyncGetSyncGroupMembersServerStream
}

// SyncGetSyncGroupMembersServerCallStub is a wrapper that converts rpc.StreamServerCall into
// a typesafe stub that implements SyncGetSyncGroupMembersServerCall.
type SyncGetSyncGroupMembersServerCallStub struct {
	rpc.StreamServerCall
}

// Init initializes SyncGetSyncGroupMembersServerCallStub from rpc.StreamServerCall.
func (s *SyncGetSyncGroupMembersServerCallStub) Init(call rpc.StreamServerCall) {
	s.StreamServerCall = call
}

// SendStream returns the send side of the Sync.GetSyncGroupMembers server stream.
func (s *SyncGetSyncGroupMembersServerCallStub) SendStream() interface {
	Send(item SyncGroupMember) error
} {
	return implSyncGetSyncGroupMembersServerCallSend{s}
}

type implSyncGetSyncGroupMembersServerCallSend struct {
	s *SyncGetSyncGroupMembersServerCallStub
}

func (s implSyncGetSyncGroupMembersServerCallSend) Send(item SyncGroupMember) error {
	return s.s.Send(item)
}

// SyncGetSyncGroupStatsServerStream is the server stream for Sync.GetSyncGroupStats.
type SyncGetSyncGroupStatsServerStream interface {
	// SendStream returns the send side of the Sync.GetSyncGroupStats server stream.
	SendStream() interface {
		// Send places the item onto the output stream.  Returns errors encountered
		// while sending.  Blocks if there is no buffer space; will unblock when
		// buffer space is available.
		Send(item SyncGroupStats) error
	}
}

// SyncGetSyncGroupStatsServerCall represents the context passed to Sync.GetSyncGroupStats.
type SyncGetSyncGroupStatsServerCall interface {
	rpc.ServerCall
	SyncGetSyncGroupStatsServerStream
}

// SyncGetSyncGroupStatsServerCallStub is a wrapper that converts rpc.StreamServerCall into
// a typesafe stub that implements SyncGetSyncGroupStatsServerCall.
type SyncGetSyncGroupStatsServerCallStub struct {
	rpc.StreamServerCall
}

// Init initializes SyncGetSyncGroupStatsServerCallStub from rpc.StreamServerCall.
func (s *SyncGetSyncGroupStatsServerCallStub) Init(call rpc.StreamServerCall) {
	s.StreamServerCall = call
}

// SendStream returns the send side of the Sync.GetSyncGroupStats server stream.
func (s *SyncGetSyncGroupStatsServerCallStub) SendStream() interface {
	Send(item SyncGroupStats) error
} {
	return implSyncGetSyncGroupStatsServerCallSend{s}
}

type implSyncGetSyncGroupStatsServerCallSend struct {
	s *SyncGetSyncGroupStatsServerCallStub
}

func (s implSyncGetSyncGroupStatsServerCallSend) Send(item SyncGroupStats) error {
	return s.s.Send(item)
}
