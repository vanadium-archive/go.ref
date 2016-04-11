// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Sb is a syncbase general-purpose client and management utility.
// It currently supports syncQL (the syncbase query language).
//
// The 'sh' command connects to a specified database on a syncbase instance,
// creating it if it does not exist if -create-missing is specified.
// The user can then enter the following at the command line:
//     1. dump - to get a dump of the database
//     2. a syncQL select statement - which is executed and results printed to stdout
//     3. a syncQL delete statement - which is executed to delete k/v pairs from a collection
//     4. destroy {db|collection|syncgroup} <identifier> - to destroy a syncbase object
//     5. make-demo - to create demo collections in the database to experiment with, equivalent to -make-demo flag
//     5. exit (or quit) - to exit the program
//
// When the shell is running non-interactively (stdin not connected to a tty),
// errors cause the shell to exit with a non-zero status.
//
// To build client:
//     jiri go install v.io/x/ref/cmd/sb
//
// To run client:
//     $JIRI_ROOT/release/go/bin/sb sh <app_blessing> <db_name>
//
// Sample run (assuming a syncbase service is mounted at '/:8101/syncbase',
// otherwise specify using -service flag):
//     > $JIRI_ROOT/release/go/bin/sb sh -create-missing -make-demo -format=csv demoapp demodb
//     ? select v.Name, v.Address.State from Customers where Type(v) = "Customer";
//     v.Name,v.Address.State
//     John Smith,CA
//     Bat Masterson,IA
//     ? select v.CustId, v.InvoiceNum, v.ShipTo.Zip, v.Amount from Customers where Type(v) = "Invoice" and v.Amount > 100;
//     v.CustId,v.InvoiceNum,v.ShipTo.Zip,v.Amount
//     2,1001,50055,166
//     2,1002,50055,243
//     2,1004,50055,787
//     ? select k, v fro Customers;
//     Error:
//     select k, v fro Customers
//                 ^
//     13: Expected 'from', found fro.
//     ? select k, v from Customers;
//     k,v
//     001,"{Name: ""John Smith"", Id: 1, Active: true, Address: {Street: ""1 Main St."", City: ""Palo Alto"", State: ""CA"", Zip: ""94303""}, Credit: {Agency: Equifax, Report: EquifaxReport: {Rating: 65}}}"
//     001001,"{CustId: 1, InvoiceNum: 1000, Amount: 42, ShipTo: {Street: ""1 Main St."", City: ""Palo Alto"", State: ""CA"", Zip: ""94303""}}"
//     001002,"{CustId: 1, InvoiceNum: 1003, Amount: 7, ShipTo: {Street: ""2 Main St."", City: ""Palo Alto"", State: ""CA"", Zip: ""94303""}}"
//     001003,"{CustId: 1, InvoiceNum: 1005, Amount: 88, ShipTo: {Street: ""3 Main St."", City: ""Palo Alto"", State: ""CA"", Zip: ""94303""}}"
//     002,"{Name: ""Bat Masterson"", Id: 2, Active: true, Address: {Street: ""777 Any St."", City: ""Collins"", State: ""IA"", Zip: ""50055""}, Credit: {Agency: TransUnion, Report: TransUnionReport: {Rating: 80}}}"
//     002001,"{CustId: 2, InvoiceNum: 1001, Amount: 166, ShipTo: {Street: ""777 Any St."", City: ""Collins"", State: ""IA"", Zip: ""50055""}}"
//     002002,"{CustId: 2, InvoiceNum: 1002, Amount: 243, ShipTo: {Street: ""888 Any St."", City: ""Collins"", State: ""IA"", Zip: ""50055""}}"
//     002003,"{CustId: 2, InvoiceNum: 1004, Amount: 787, ShipTo: {Street: ""999 Any St."", City: ""Collins"", State: ""IA"", Zip: ""50055""}}"
//     002004,"{CustId: 2, InvoiceNum: 1006, Amount: 88, ShipTo: {Street: ""101010 Any St."", City: ""Collins"", State: ""IA"", Zip: ""50055""}}"
//     ? delete from Customers where k = "001002";
//     +-------+
//     | Count |
//     +-------+
//     |     1 |
//     +-------+
//     ? exit;
//     >
package main
