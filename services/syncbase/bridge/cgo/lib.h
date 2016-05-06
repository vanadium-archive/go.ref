// Copyright 2016 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

#ifndef V23_SYNCBASE_LIB_H_
#define V23_SYNCBASE_LIB_H_

#include <stdbool.h>
#include <stdint.h>

// TODO(sadovsky): Add types and functions for watch and sync.

////////////////////////////////////////
// Generic types

// string
typedef struct {
  char* p;
  int n;
} XString;

// []byte
typedef struct {
  uint8_t* p;
  int n;
} XBytes;

// []string
typedef struct {
  XString* p;
  int n;
} XStrings;

////////////////////////////////////////
// Vanadium-specific types

// verror.E
typedef struct {
  XString id;
  unsigned int actionCode;
  XString msg;
  XString stack;
} XVError;

// access.Permissions
// TODO(sadovsky): Decide how to represent perms.
typedef struct {
  XString json;
} XPermissions;

////////////////////////////////////////
// Syncbase-specific types

// syncbase.Id
typedef struct {
  XString blessing;
  XString name;
} XId;

// []syncbase.Id
typedef struct {
  XId* p;
  int n;
} XIds;

// syncbase.BatchOptions
typedef struct {
  XString hint;
  bool readOnly;
} XBatchOptions;

// syncbase.KeyValue
typedef struct {
  XString key;
  XBytes value;
} XKeyValue;

// syncbase.SyncgroupSpec
typedef struct {
  XString description;
  XPermissions perms;
  XIds collections;
  XStrings mountTables;
  bool isPrivate;
} XSyncgroupSpec;

// syncbase.SyncgroupMemberInfo
typedef struct {
  uint8_t syncPriority;
  uint8_t blobDevType;
} XSyncgroupMemberInfo;

// map[string]syncbase.SyncgroupMemberInfo
typedef struct {
  XString* keys;
  XSyncgroupMemberInfo* values;
  int n;
} XSyncgroupMemberInfoMap;

////////////////////////////////////////
// Functions

// Callbacks are represented as struct {XHandle, f(XHandle, ...)} to allow for
// currying RefMap handles to Swift closures.
// https://forums.developer.apple.com/message/15725#15725

typedef int XHandle;

typedef struct {
  XHandle hOnKeyValue;
  XHandle hOnDone;
  void (*onKeyValue)(XHandle hOnKeyValue, XKeyValue);
  void (*onDone)(XHandle hOnKeyValue, XHandle hOnDone, XVError);
} XCollectionScanCallbacks;

#endif  // V23_SYNCBASE_LIB_H_
