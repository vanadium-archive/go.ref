// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"crypto/tls"
	"crypto/x509"
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"

	"github.com/go-sql-driver/mysql"
)

// Flag controlling auditing and revocation of Blessing operations.
var sqlConf = flag.String("sqlconfig", "", `Path to file containing a json object of the following form:
   {
    "dataSourceName": "[username[:password]@][protocol[(address)]]/dbname", (the connection string required by go-sql-driver)
    "tlsServerName": "serverName", (the domain name of the sql server for ssl)
    "rootCertPath": "/path/server-ca.pem", (the root certificate of the sql server for ssl)
    "clientCertPath": "/path/client-cert.pem", (the client certificate for ssl)
    "clientKeyPath": "/path/client-key.pem" (the client private key for ssl)
   }`)

// The key used by both go-sql-driver and tls for ssl.
const tlsRegisteredKey = "identitydTLS"

// sqlConfig holds the fields needed to connected to a sql instance and the fields
// needed to encrypt the information sent over the wire.
type sqlConfig struct {
	// DataSourceName is the connection string required by go-sql-driver: "[username[:password]@][protocol[(address)]]/dbname".
	DataSourceName string `json:dataSourceName`
	// RootCertPath is the root certificate of the sql server for ssl.
	RootCertPath string `json:rootCertPath`
	// TLSServerName is the domain name of the sql server for ssl.
	TLSServerName string `json:tlsServerName`
	// ClientCertPath is the client certificate for ssl.
	ClientCertPath string `json:clientCertPath`
	// ClientKeyPath is the client private key for ssl.
	ClientKeyPath string `json:clientKeyPath`
}

func dbFromConfigFile(file string) (*sql.DB, error) {
	config, err := readConfigFromFile(file)
	if err != nil {
		return nil, fmt.Errorf("failed to read sql config from %v: %v", file, err)
	}
	if err := registerTLSConfig(config); err != nil {
		return nil, fmt.Errorf("failed to register sql tls config %#v: %v", config, err)
	}
	db, err := sql.Open("mysql", config.DataSourceName+"?parseTime=true&tls="+tlsRegisteredKey)
	if err != nil {
		return nil, fmt.Errorf("failed to create database with database(%v): %v", config.DataSourceName, err)
	}
	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("db initial ping failed: %v", err)
	}
	return db, nil
}

func readConfigFromFile(file string) (*sqlConfig, error) {
	configJSON, err := ioutil.ReadFile(file)
	if err != nil {
		return nil, err
	}
	var config sqlConfig
	err = json.Unmarshal(configJSON, &config)
	return &config, err
}

// registerTLSConfig sets up the connection to the sql instance to require ssl encryption.
// For more information see https://cloud.google.com/sql/docs/instances#ssl.
func registerTLSConfig(config *sqlConfig) error {
	rootCertPool := x509.NewCertPool()
	pem, err := ioutil.ReadFile(config.RootCertPath)
	if err != nil {
		return err
	}
	if ok := rootCertPool.AppendCertsFromPEM(pem); !ok {
		return fmt.Errorf("failed to append PEM to cert pool")
	}
	certs, err := tls.LoadX509KeyPair(config.ClientCertPath, config.ClientKeyPath)
	if err != nil {
		return err
	}
	clientCert := []tls.Certificate{certs}
	return mysql.RegisterTLSConfig(tlsRegisteredKey, &tls.Config{
		RootCAs:      rootCertPool,
		Certificates: clientCert,
		ServerName:   config.TLSServerName,
		ClientAuth:   tls.RequireAndVerifyClientCert,
	})
}
