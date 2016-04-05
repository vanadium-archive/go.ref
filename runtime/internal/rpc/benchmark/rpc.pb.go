// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// +build protobuf

package benchmark

import proto "github.com/golang/protobuf/proto"
import fmt "fmt"
import math "math"

// Reference imports to suppress errors if they are not otherwise used.
var _ = proto.Marshal
var _ = fmt.Errorf
var _ = math.Inf

// This is a compile-time assertion to ensure that this generated file
// is compatible with the proto package it is being compiled against.
const _ = proto.ProtoPackageIsVersion1

type TimeTime struct {
	Seconds          *int64 `protobuf:"varint,1,req,name=seconds" json:"seconds,omitempty"`
	Nanos            *int32 `protobuf:"varint,2,req,name=nanos" json:"nanos,omitempty"`
	XXX_unrecognized []byte `json:"-"`
}

func (m *TimeTime) Reset()                    { *m = TimeTime{} }
func (m *TimeTime) String() string            { return proto.CompactTextString(m) }
func (*TimeTime) ProtoMessage()               {}
func (*TimeTime) Descriptor() ([]byte, []int) { return fileDescriptor0, []int{0} }

func (m *TimeTime) GetSeconds() int64 {
	if m != nil && m.Seconds != nil {
		return *m.Seconds
	}
	return 0
}

func (m *TimeTime) GetNanos() int32 {
	if m != nil && m.Nanos != nil {
		return *m.Nanos
	}
	return 0
}

type VtraceAnnotation struct {
	When             *TimeTime `protobuf:"bytes,1,req,name=when" json:"when,omitempty"`
	Msg              *string   `protobuf:"bytes,2,req,name=msg" json:"msg,omitempty"`
	XXX_unrecognized []byte    `json:"-"`
}

func (m *VtraceAnnotation) Reset()                    { *m = VtraceAnnotation{} }
func (m *VtraceAnnotation) String() string            { return proto.CompactTextString(m) }
func (*VtraceAnnotation) ProtoMessage()               {}
func (*VtraceAnnotation) Descriptor() ([]byte, []int) { return fileDescriptor0, []int{1} }

func (m *VtraceAnnotation) GetWhen() *TimeTime {
	if m != nil {
		return m.When
	}
	return nil
}

func (m *VtraceAnnotation) GetMsg() string {
	if m != nil && m.Msg != nil {
		return *m.Msg
	}
	return ""
}

type VtraceSpanRecord struct {
	Id               []byte              `protobuf:"bytes,1,req,name=id" json:"id,omitempty"`
	Parent           []byte              `protobuf:"bytes,2,req,name=parent" json:"parent,omitempty"`
	Name             *string             `protobuf:"bytes,3,req,name=name" json:"name,omitempty"`
	Start            *TimeTime           `protobuf:"bytes,4,req,name=start" json:"start,omitempty"`
	End              *TimeTime           `protobuf:"bytes,5,req,name=end" json:"end,omitempty"`
	Annotations      []*VtraceAnnotation `protobuf:"bytes,6,rep,name=annotations" json:"annotations,omitempty"`
	XXX_unrecognized []byte              `json:"-"`
}

func (m *VtraceSpanRecord) Reset()                    { *m = VtraceSpanRecord{} }
func (m *VtraceSpanRecord) String() string            { return proto.CompactTextString(m) }
func (*VtraceSpanRecord) ProtoMessage()               {}
func (*VtraceSpanRecord) Descriptor() ([]byte, []int) { return fileDescriptor0, []int{2} }

func (m *VtraceSpanRecord) GetId() []byte {
	if m != nil {
		return m.Id
	}
	return nil
}

func (m *VtraceSpanRecord) GetParent() []byte {
	if m != nil {
		return m.Parent
	}
	return nil
}

func (m *VtraceSpanRecord) GetName() string {
	if m != nil && m.Name != nil {
		return *m.Name
	}
	return ""
}

func (m *VtraceSpanRecord) GetStart() *TimeTime {
	if m != nil {
		return m.Start
	}
	return nil
}

func (m *VtraceSpanRecord) GetEnd() *TimeTime {
	if m != nil {
		return m.End
	}
	return nil
}

func (m *VtraceSpanRecord) GetAnnotations() []*VtraceAnnotation {
	if m != nil {
		return m.Annotations
	}
	return nil
}

type VtraceTraceRecord struct {
	Id               []byte              `protobuf:"bytes,1,req,name=id" json:"id,omitempty"`
	SpanRecord       []*VtraceSpanRecord `protobuf:"bytes,2,rep,name=spanRecord" json:"spanRecord,omitempty"`
	XXX_unrecognized []byte              `json:"-"`
}

func (m *VtraceTraceRecord) Reset()                    { *m = VtraceTraceRecord{} }
func (m *VtraceTraceRecord) String() string            { return proto.CompactTextString(m) }
func (*VtraceTraceRecord) ProtoMessage()               {}
func (*VtraceTraceRecord) Descriptor() ([]byte, []int) { return fileDescriptor0, []int{3} }

func (m *VtraceTraceRecord) GetId() []byte {
	if m != nil {
		return m.Id
	}
	return nil
}

func (m *VtraceTraceRecord) GetSpanRecord() []*VtraceSpanRecord {
	if m != nil {
		return m.SpanRecord
	}
	return nil
}

type VtraceResponse struct {
	TraceFlags       *int32             `protobuf:"varint,1,req,name=traceFlags" json:"traceFlags,omitempty"`
	Trace            *VtraceTraceRecord `protobuf:"bytes,2,req,name=trace" json:"trace,omitempty"`
	XXX_unrecognized []byte             `json:"-"`
}

func (m *VtraceResponse) Reset()                    { *m = VtraceResponse{} }
func (m *VtraceResponse) String() string            { return proto.CompactTextString(m) }
func (*VtraceResponse) ProtoMessage()               {}
func (*VtraceResponse) Descriptor() ([]byte, []int) { return fileDescriptor0, []int{4} }

func (m *VtraceResponse) GetTraceFlags() int32 {
	if m != nil && m.TraceFlags != nil {
		return *m.TraceFlags
	}
	return 0
}

func (m *VtraceResponse) GetTrace() *VtraceTraceRecord {
	if m != nil {
		return m.Trace
	}
	return nil
}

type RpcResponse struct {
	Error            *string         `protobuf:"bytes,1,opt,name=error" json:"error,omitempty"`
	EndStreamResults *bool           `protobuf:"varint,2,req,name=endStreamResults" json:"endStreamResults,omitempty"`
	NumPosResults    *uint64         `protobuf:"varint,3,req,name=numPosResults" json:"numPosResults,omitempty"`
	TraceResponse    *VtraceResponse `protobuf:"bytes,4,req,name=traceResponse" json:"traceResponse,omitempty"`
	AckBlessings     *bool           `protobuf:"varint,5,req,name=ackBlessings" json:"ackBlessings,omitempty"`
	XXX_unrecognized []byte          `json:"-"`
}

func (m *RpcResponse) Reset()                    { *m = RpcResponse{} }
func (m *RpcResponse) String() string            { return proto.CompactTextString(m) }
func (*RpcResponse) ProtoMessage()               {}
func (*RpcResponse) Descriptor() ([]byte, []int) { return fileDescriptor0, []int{5} }

func (m *RpcResponse) GetError() string {
	if m != nil && m.Error != nil {
		return *m.Error
	}
	return ""
}

func (m *RpcResponse) GetEndStreamResults() bool {
	if m != nil && m.EndStreamResults != nil {
		return *m.EndStreamResults
	}
	return false
}

func (m *RpcResponse) GetNumPosResults() uint64 {
	if m != nil && m.NumPosResults != nil {
		return *m.NumPosResults
	}
	return 0
}

func (m *RpcResponse) GetTraceResponse() *VtraceResponse {
	if m != nil {
		return m.TraceResponse
	}
	return nil
}

func (m *RpcResponse) GetAckBlessings() bool {
	if m != nil && m.AckBlessings != nil {
		return *m.AckBlessings
	}
	return false
}

type TimeDuration struct {
	Seconds          *int64 `protobuf:"varint,1,req,name=seconds" json:"seconds,omitempty"`
	Nanos            *int64 `protobuf:"varint,2,req,name=nanos" json:"nanos,omitempty"`
	XXX_unrecognized []byte `json:"-"`
}

func (m *TimeDuration) Reset()                    { *m = TimeDuration{} }
func (m *TimeDuration) String() string            { return proto.CompactTextString(m) }
func (*TimeDuration) ProtoMessage()               {}
func (*TimeDuration) Descriptor() ([]byte, []int) { return fileDescriptor0, []int{6} }

func (m *TimeDuration) GetSeconds() int64 {
	if m != nil && m.Seconds != nil {
		return *m.Seconds
	}
	return 0
}

func (m *TimeDuration) GetNanos() int64 {
	if m != nil && m.Nanos != nil {
		return *m.Nanos
	}
	return 0
}

type TimeWireDeadline struct {
	FromNow          *TimeDuration `protobuf:"bytes,1,req,name=fromNow" json:"fromNow,omitempty"`
	NoDeadline       *bool         `protobuf:"varint,2,req,name=noDeadline" json:"noDeadline,omitempty"`
	XXX_unrecognized []byte        `json:"-"`
}

func (m *TimeWireDeadline) Reset()                    { *m = TimeWireDeadline{} }
func (m *TimeWireDeadline) String() string            { return proto.CompactTextString(m) }
func (*TimeWireDeadline) ProtoMessage()               {}
func (*TimeWireDeadline) Descriptor() ([]byte, []int) { return fileDescriptor0, []int{7} }

func (m *TimeWireDeadline) GetFromNow() *TimeDuration {
	if m != nil {
		return m.FromNow
	}
	return nil
}

func (m *TimeWireDeadline) GetNoDeadline() bool {
	if m != nil && m.NoDeadline != nil {
		return *m.NoDeadline
	}
	return false
}

type Signature struct {
	Purpose          []byte  `protobuf:"bytes,1,req,name=purpose" json:"purpose,omitempty"`
	Hash             *string `protobuf:"bytes,2,req,name=hash" json:"hash,omitempty"`
	R                []byte  `protobuf:"bytes,3,req,name=r" json:"r,omitempty"`
	S                []byte  `protobuf:"bytes,4,req,name=s" json:"s,omitempty"`
	XXX_unrecognized []byte  `json:"-"`
}

func (m *Signature) Reset()                    { *m = Signature{} }
func (m *Signature) String() string            { return proto.CompactTextString(m) }
func (*Signature) ProtoMessage()               {}
func (*Signature) Descriptor() ([]byte, []int) { return fileDescriptor0, []int{8} }

func (m *Signature) GetPurpose() []byte {
	if m != nil {
		return m.Purpose
	}
	return nil
}

func (m *Signature) GetHash() string {
	if m != nil && m.Hash != nil {
		return *m.Hash
	}
	return ""
}

func (m *Signature) GetR() []byte {
	if m != nil {
		return m.R
	}
	return nil
}

func (m *Signature) GetS() []byte {
	if m != nil {
		return m.S
	}
	return nil
}

type Caveat struct {
	Id               []byte `protobuf:"bytes,1,req,name=id" json:"id,omitempty"`
	ParamVom         []byte `protobuf:"bytes,2,req,name=paramVom" json:"paramVom,omitempty"`
	XXX_unrecognized []byte `json:"-"`
}

func (m *Caveat) Reset()                    { *m = Caveat{} }
func (m *Caveat) String() string            { return proto.CompactTextString(m) }
func (*Caveat) ProtoMessage()               {}
func (*Caveat) Descriptor() ([]byte, []int) { return fileDescriptor0, []int{9} }

func (m *Caveat) GetId() []byte {
	if m != nil {
		return m.Id
	}
	return nil
}

func (m *Caveat) GetParamVom() []byte {
	if m != nil {
		return m.ParamVom
	}
	return nil
}

type Certificate struct {
	Extension        *string    `protobuf:"bytes,1,req,name=extension" json:"extension,omitempty"`
	PublicKey        []byte     `protobuf:"bytes,2,req,name=publicKey" json:"publicKey,omitempty"`
	Caveats          []*Caveat  `protobuf:"bytes,3,rep,name=caveats" json:"caveats,omitempty"`
	Signature        *Signature `protobuf:"bytes,4,req,name=signature" json:"signature,omitempty"`
	XXX_unrecognized []byte     `json:"-"`
}

func (m *Certificate) Reset()                    { *m = Certificate{} }
func (m *Certificate) String() string            { return proto.CompactTextString(m) }
func (*Certificate) ProtoMessage()               {}
func (*Certificate) Descriptor() ([]byte, []int) { return fileDescriptor0, []int{10} }

func (m *Certificate) GetExtension() string {
	if m != nil && m.Extension != nil {
		return *m.Extension
	}
	return ""
}

func (m *Certificate) GetPublicKey() []byte {
	if m != nil {
		return m.PublicKey
	}
	return nil
}

func (m *Certificate) GetCaveats() []*Caveat {
	if m != nil {
		return m.Caveats
	}
	return nil
}

func (m *Certificate) GetSignature() *Signature {
	if m != nil {
		return m.Signature
	}
	return nil
}

type CertificateChain struct {
	Certificates     []*Certificate `protobuf:"bytes,1,rep,name=certificates" json:"certificates,omitempty"`
	XXX_unrecognized []byte         `json:"-"`
}

func (m *CertificateChain) Reset()                    { *m = CertificateChain{} }
func (m *CertificateChain) String() string            { return proto.CompactTextString(m) }
func (*CertificateChain) ProtoMessage()               {}
func (*CertificateChain) Descriptor() ([]byte, []int) { return fileDescriptor0, []int{11} }

func (m *CertificateChain) GetCertificates() []*Certificate {
	if m != nil {
		return m.Certificates
	}
	return nil
}

type SecurityWireBlessings struct {
	CertificateChains []*CertificateChain `protobuf:"bytes,1,rep,name=certificateChains" json:"certificateChains,omitempty"`
	XXX_unrecognized  []byte              `json:"-"`
}

func (m *SecurityWireBlessings) Reset()                    { *m = SecurityWireBlessings{} }
func (m *SecurityWireBlessings) String() string            { return proto.CompactTextString(m) }
func (*SecurityWireBlessings) ProtoMessage()               {}
func (*SecurityWireBlessings) Descriptor() ([]byte, []int) { return fileDescriptor0, []int{12} }

func (m *SecurityWireBlessings) GetCertificateChains() []*CertificateChain {
	if m != nil {
		return m.CertificateChains
	}
	return nil
}

type VtraceRequest struct {
	SpanId           []byte `protobuf:"bytes,1,req,name=spanId" json:"spanId,omitempty"`
	TraceId          []byte `protobuf:"bytes,2,req,name=traceId" json:"traceId,omitempty"`
	Flags            *int32 `protobuf:"varint,3,req,name=flags" json:"flags,omitempty"`
	LogLevel         *int32 `protobuf:"varint,4,req,name=logLevel" json:"logLevel,omitempty"`
	XXX_unrecognized []byte `json:"-"`
}

func (m *VtraceRequest) Reset()                    { *m = VtraceRequest{} }
func (m *VtraceRequest) String() string            { return proto.CompactTextString(m) }
func (*VtraceRequest) ProtoMessage()               {}
func (*VtraceRequest) Descriptor() ([]byte, []int) { return fileDescriptor0, []int{13} }

func (m *VtraceRequest) GetSpanId() []byte {
	if m != nil {
		return m.SpanId
	}
	return nil
}

func (m *VtraceRequest) GetTraceId() []byte {
	if m != nil {
		return m.TraceId
	}
	return nil
}

func (m *VtraceRequest) GetFlags() int32 {
	if m != nil && m.Flags != nil {
		return *m.Flags
	}
	return 0
}

func (m *VtraceRequest) GetLogLevel() int32 {
	if m != nil && m.LogLevel != nil {
		return *m.LogLevel
	}
	return 0
}

type RpcRequest struct {
	Suffix           *string                `protobuf:"bytes,1,req,name=suffix" json:"suffix,omitempty"`
	Method           *string                `protobuf:"bytes,2,req,name=method" json:"method,omitempty"`
	NumPosArgs       *uint64                `protobuf:"varint,3,req,name=numPosArgs" json:"numPosArgs,omitempty"`
	EndStreamArgs    *bool                  `protobuf:"varint,4,req,name=endStreamArgs" json:"endStreamArgs,omitempty"`
	Deadline         *TimeWireDeadline      `protobuf:"bytes,5,req,name=deadline" json:"deadline,omitempty"`
	GrantedBlessings *SecurityWireBlessings `protobuf:"bytes,6,req,name=grantedBlessings" json:"grantedBlessings,omitempty"`
	TraceRequest     *VtraceRequest         `protobuf:"bytes,7,req,name=traceRequest" json:"traceRequest,omitempty"`
	Language         *string                `protobuf:"bytes,8,req,name=language" json:"language,omitempty"`
	XXX_unrecognized []byte                 `json:"-"`
}

func (m *RpcRequest) Reset()                    { *m = RpcRequest{} }
func (m *RpcRequest) String() string            { return proto.CompactTextString(m) }
func (*RpcRequest) ProtoMessage()               {}
func (*RpcRequest) Descriptor() ([]byte, []int) { return fileDescriptor0, []int{14} }

func (m *RpcRequest) GetSuffix() string {
	if m != nil && m.Suffix != nil {
		return *m.Suffix
	}
	return ""
}

func (m *RpcRequest) GetMethod() string {
	if m != nil && m.Method != nil {
		return *m.Method
	}
	return ""
}

func (m *RpcRequest) GetNumPosArgs() uint64 {
	if m != nil && m.NumPosArgs != nil {
		return *m.NumPosArgs
	}
	return 0
}

func (m *RpcRequest) GetEndStreamArgs() bool {
	if m != nil && m.EndStreamArgs != nil {
		return *m.EndStreamArgs
	}
	return false
}

func (m *RpcRequest) GetDeadline() *TimeWireDeadline {
	if m != nil {
		return m.Deadline
	}
	return nil
}

func (m *RpcRequest) GetGrantedBlessings() *SecurityWireBlessings {
	if m != nil {
		return m.GrantedBlessings
	}
	return nil
}

func (m *RpcRequest) GetTraceRequest() *VtraceRequest {
	if m != nil {
		return m.TraceRequest
	}
	return nil
}

func (m *RpcRequest) GetLanguage() string {
	if m != nil && m.Language != nil {
		return *m.Language
	}
	return ""
}

func init() {
	proto.RegisterType((*TimeTime)(nil), "benchmark.timeTime")
	proto.RegisterType((*VtraceAnnotation)(nil), "benchmark.vtraceAnnotation")
	proto.RegisterType((*VtraceSpanRecord)(nil), "benchmark.vtraceSpanRecord")
	proto.RegisterType((*VtraceTraceRecord)(nil), "benchmark.vtraceTraceRecord")
	proto.RegisterType((*VtraceResponse)(nil), "benchmark.vtraceResponse")
	proto.RegisterType((*RpcResponse)(nil), "benchmark.rpcResponse")
	proto.RegisterType((*TimeDuration)(nil), "benchmark.timeDuration")
	proto.RegisterType((*TimeWireDeadline)(nil), "benchmark.timeWireDeadline")
	proto.RegisterType((*Signature)(nil), "benchmark.signature")
	proto.RegisterType((*Caveat)(nil), "benchmark.caveat")
	proto.RegisterType((*Certificate)(nil), "benchmark.certificate")
	proto.RegisterType((*CertificateChain)(nil), "benchmark.certificateChain")
	proto.RegisterType((*SecurityWireBlessings)(nil), "benchmark.securityWireBlessings")
	proto.RegisterType((*VtraceRequest)(nil), "benchmark.vtraceRequest")
	proto.RegisterType((*RpcRequest)(nil), "benchmark.rpcRequest")
}

var fileDescriptor0 = []byte{
	// 710 bytes of a gzipped FileDescriptorProto
	0x1f, 0x8b, 0x08, 0x00, 0x00, 0x09, 0x6e, 0x88, 0x02, 0xff, 0x7c, 0x54, 0x4b, 0x73, 0xd3, 0x30,
	0x10, 0x9e, 0xbc, 0x93, 0x8d, 0x53, 0x12, 0xd1, 0x82, 0x99, 0x72, 0x08, 0x3e, 0x40, 0x87, 0x47,
	0x60, 0x7a, 0xe0, 0xc0, 0x89, 0x96, 0x0e, 0x33, 0x1d, 0x18, 0x28, 0x6d, 0x07, 0xce, 0xaa, 0xad,
	0x38, 0x9e, 0xda, 0x92, 0x91, 0xe4, 0x3e, 0x6e, 0x1c, 0xf8, 0x0b, 0xfc, 0x13, 0x7e, 0x20, 0x2b,
	0xf9, 0x11, 0x37, 0x4d, 0x39, 0x24, 0x13, 0xaf, 0xb5, 0x9f, 0xf6, 0x7b, 0x6c, 0x60, 0x20, 0x53,
	0x7f, 0x96, 0x4a, 0xa1, 0x05, 0x19, 0x9c, 0x31, 0xee, 0x2f, 0x12, 0x2a, 0xcf, 0xbd, 0xe7, 0xd0,
	0xd7, 0x51, 0xc2, 0x4e, 0xf1, 0x43, 0xee, 0x41, 0x4f, 0x31, 0x5f, 0xf0, 0x40, 0xb9, 0x8d, 0x69,
	0x73, 0xa7, 0x45, 0x46, 0xd0, 0xe1, 0x94, 0x0b, 0xe5, 0x36, 0xf1, 0xb1, 0xe3, 0xed, 0xc3, 0xf8,
	0x42, 0x4b, 0xea, 0xb3, 0x3d, 0xce, 0x85, 0xa6, 0x3a, 0x12, 0x9c, 0x3c, 0x81, 0xf6, 0xe5, 0x82,
	0x71, 0xdb, 0x30, 0xdc, 0xbd, 0x3f, 0xab, 0x90, 0x67, 0x15, 0xec, 0x10, 0x5a, 0x89, 0x0a, 0x2d,
	0xc6, 0xc0, 0xfb, 0xdb, 0x28, 0x41, 0x4e, 0x52, 0xca, 0x8f, 0xf1, 0x3a, 0x19, 0x10, 0x80, 0x66,
	0x14, 0x58, 0x08, 0x87, 0x6c, 0x40, 0x37, 0xa5, 0x92, 0x71, 0x6d, 0x1b, 0x1c, 0xe2, 0x40, 0x9b,
	0xd3, 0x84, 0xb9, 0x2d, 0xd3, 0x4e, 0x3c, 0xe8, 0x28, 0x4d, 0xa5, 0x76, 0xdb, 0x77, 0xdf, 0x37,
	0x85, 0x16, 0xe3, 0x81, 0xdb, 0xb9, 0xfb, 0xc4, 0x1b, 0x18, 0xd2, 0x8a, 0x82, 0x72, 0xbb, 0xd3,
	0x16, 0x9e, 0xdc, 0xae, 0x9d, 0x5c, 0xa5, 0xe9, 0x1d, 0xc1, 0x24, 0xaf, 0x9d, 0x9a, 0xaf, 0x35,
	0x63, 0xbf, 0x06, 0x50, 0x15, 0x21, 0x1c, 0x7d, 0x3d, 0xe2, 0x92, 0xb3, 0xf7, 0x0d, 0x36, 0xf2,
	0xda, 0x31, 0x53, 0x29, 0x4e, 0xc1, 0x08, 0xe2, 0xd9, 0xc2, 0xc7, 0x98, 0x86, 0xb9, 0x03, 0x1d,
	0xf2, 0x02, 0x3a, 0xb6, 0x66, 0xc5, 0x18, 0xee, 0x3e, 0xbe, 0x85, 0x58, 0x9b, 0xc7, 0xfb, 0xd3,
	0x80, 0x21, 0x9a, 0x5c, 0x01, 0xa2, 0x7d, 0x4c, 0x4a, 0x21, 0x11, 0xab, 0x81, 0xda, 0xb9, 0x30,
	0x46, 0x5d, 0x4e, 0xb4, 0x64, 0x34, 0xc1, 0x33, 0x59, 0xac, 0x73, 0x63, 0xfb, 0x64, 0x0b, 0x46,
	0x3c, 0x4b, 0x8e, 0x84, 0x2a, 0xcb, 0x46, 0xec, 0x36, 0xca, 0x34, 0xba, 0x31, 0x61, 0x21, 0xfa,
	0xa3, 0x5b, 0x43, 0x54, 0x37, 0x6e, 0x82, 0x43, 0xfd, 0xf3, 0xfd, 0x98, 0x29, 0x15, 0x71, 0x24,
	0x61, 0x3c, 0xe8, 0x7b, 0x33, 0x70, 0x8c, 0xf4, 0x07, 0x99, 0xcc, 0x33, 0xf3, 0xff, 0x9c, 0xb5,
	0x50, 0xec, 0xb1, 0x39, 0xff, 0x23, 0x92, 0xec, 0x80, 0xd1, 0x20, 0x8e, 0x38, 0x23, 0x3b, 0xd0,
	0x9b, 0x4b, 0x91, 0x7c, 0x11, 0x97, 0x45, 0xd4, 0x1e, 0xae, 0x18, 0x5b, 0xa1, 0xa3, 0x8c, 0x5c,
	0x94, 0x7d, 0x39, 0x41, 0xef, 0x3d, 0x0c, 0x54, 0x14, 0x72, 0xaa, 0x33, 0x69, 0x63, 0x9e, 0x66,
	0x32, 0x15, 0x48, 0xa8, 0x51, 0x46, 0x6c, 0x41, 0xd5, 0x22, 0x4f, 0x28, 0x19, 0x40, 0x43, 0x5a,
	0x01, 0x1c, 0xf3, 0x53, 0x59, 0xd2, 0x8e, 0xf7, 0x14, 0xba, 0x3e, 0xbd, 0x60, 0x54, 0xdf, 0x70,
	0x7d, 0x0c, 0x7d, 0x0c, 0x2b, 0x4d, 0xbe, 0x8b, 0x24, 0x8f, 0xab, 0xf7, 0x0b, 0x3d, 0xf0, 0x99,
	0xd4, 0xd1, 0x3c, 0xf2, 0xa9, 0x66, 0x64, 0x02, 0x03, 0x76, 0xa5, 0x19, 0x57, 0x38, 0x9a, 0x6d,
	0x1a, 0x98, 0x52, 0x9a, 0x9d, 0xc5, 0x91, 0xff, 0x89, 0x5d, 0x17, 0x21, 0xf7, 0xa0, 0x97, 0xa3,
	0x1b, 0xe9, 0x4d, 0x74, 0x26, 0x35, 0x76, 0xc5, 0xbd, 0xcf, 0x6a, 0x1c, 0x0a, 0x27, 0x36, 0x6b,
	0xa7, 0xaa, 0x77, 0x48, 0x76, 0x5c, 0x9b, 0xe0, 0xc3, 0x82, 0x46, 0x9c, 0xbc, 0x04, 0xa7, 0x56,
	0x33, 0xba, 0x9b, 0x5b, 0x1e, 0xd4, 0x6f, 0x59, 0xbe, 0xf6, 0xbe, 0xc2, 0x16, 0x1a, 0x94, 0xc9,
	0x48, 0x5f, 0x1b, 0x13, 0x2a, 0x3f, 0xc9, 0x5b, 0x98, 0xac, 0x42, 0x97, 0x58, 0xdb, 0xeb, 0xb1,
	0xec, 0x19, 0x0c, 0xfb, 0xa8, 0x4c, 0xca, 0xcf, 0x8c, 0x29, 0x6d, 0xb6, 0xdc, 0xac, 0xcb, 0x61,
	0x29, 0x24, 0x7a, 0x62, 0xdf, 0x1f, 0x06, 0x85, 0x22, 0x18, 0x89, 0xb9, 0xdd, 0x83, 0x96, 0xdd,
	0x03, 0x14, 0x3a, 0x16, 0xe1, 0x67, 0x76, 0xc1, 0x62, 0xcb, 0xbd, 0xe3, 0xfd, 0x6e, 0x02, 0xd8,
	0xb0, 0x2f, 0x01, 0xb3, 0xf9, 0x3c, 0xba, 0x2a, 0x44, 0xc6, 0xe7, 0x84, 0xe9, 0x85, 0x08, 0x0a,
	0x57, 0x4d, 0x2a, 0x6c, 0xc4, 0xf7, 0x64, 0x58, 0xe6, 0x1b, 0x63, 0x5f, 0x2d, 0x84, 0x2d, 0xb7,
	0xed, 0x36, 0xbc, 0x82, 0x7e, 0x50, 0xc6, 0x27, 0xff, 0x13, 0xd9, 0x5e, 0xc9, 0xda, 0x8d, 0x64,
	0xbe, 0x83, 0x71, 0x28, 0x29, 0xd7, 0x2c, 0x58, 0xe6, 0xbe, 0x6b, 0xdb, 0xa6, 0x75, 0x7b, 0xd6,
	0xea, 0x69, 0x36, 0xa3, 0x26, 0x8b, 0xdb, 0xb3, 0x7d, 0xee, 0x9a, 0x05, 0xcb, 0x59, 0x1a, 0x19,
	0x28, 0x0f, 0x33, 0x1a, 0x32, 0xb7, 0x6f, 0x78, 0xfd, 0x0b, 0x00, 0x00, 0xff, 0xff, 0xad, 0xa5,
	0xca, 0xfd, 0xd6, 0x05, 0x00, 0x00,
}
