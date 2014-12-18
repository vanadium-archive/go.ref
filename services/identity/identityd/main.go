// HTTP server that uses OAuth to create security.Blessings objects.
package main

import (
	"database/sql"
	"flag"
	"fmt"
	"io/ioutil"
	"os"
	"strings"
	"time"

	_ "github.com/go-sql-driver/mysql"

	"veyron.io/veyron/veyron2/vlog"

	"veyron.io/veyron/veyron/services/identity/auditor"
	"veyron.io/veyron/veyron/services/identity/blesser"
	"veyron.io/veyron/veyron/services/identity/oauth"
	"veyron.io/veyron/veyron/services/identity/revocation"
	"veyron.io/veyron/veyron/services/identity/server"
)

var (
	// Flag controlling auditing and revocation of Blessing operations.
	sqlConfig = flag.String("sqlconfig", "", "Path to file containing go-sql-driver connection string of the following form: [username[:password]@][protocol[(address)]]/dbname")

	// Configuration for various Google OAuth-based clients.
	googleConfigWeb     = flag.String("google_config_web", "", "Path to JSON-encoded OAuth client configuration for the web application that renders the audit log for blessings provided by this provider.")
	googleConfigChrome  = flag.String("google_config_chrome", "", "Path to the JSON-encoded OAuth client configuration for Chrome browser applications that obtain blessings from this server (via the OAuthBlesser.BlessUsingAccessToken RPC) from this server.")
	googleConfigAndroid = flag.String("google_config_android", "", "Path to the JSON-encoded OAuth client configuration for Android applications that obtain blessings from this server (via the OAuthBlesser.BlessUsingAccessToken RPC) from this server.")
	googleDomain        = flag.String("google_domain", "", "An optional domain name. When set, only email addresses from this domain are allowed to authenticate via Google OAuth")
)

func main() {
	flag.Usage = usage
	flag.Parse()

	var sqlDB *sql.DB
	var err error
	if len(*sqlConfig) > 0 {
		config, err := ioutil.ReadFile(*sqlConfig)
		if err != nil {
			vlog.Fatalf("failed to read sql config from %v", *sqlConfig)
		}
		sqlDB, err = dbFromConfigDatabase(strings.Trim(string(config), "\n"))
		if err != nil {
			vlog.Fatalf("failed to create sqlDB: %v", err)
		}
	}

	googleoauth, err := oauth.NewGoogleOAuth(*googleConfigWeb)
	if err != nil {
		vlog.Fatalf("Failed to setup GoogleOAuth: %v", err)
	}

	auditor, reader, err := auditor.NewSQLBlessingAuditor(sqlDB)
	if err != nil {
		vlog.Fatalf("Failed to create sql auditor from config: %v", err)
	}

	revocationManager, err := revocation.NewRevocationManager(sqlDB)
	if err != nil {
		vlog.Fatalf("Failed to start RevocationManager: %v", err)
	}

	server.NewIdentityServer(googleoauth, auditor, reader, revocationManager, oauthBlesserGoogleParams(revocationManager)).Serve()
}

func usage() {
	fmt.Fprintf(os.Stderr, `%s starts an HTTP server that brokers blessings after authenticating through OAuth.

To generate TLS certificates so the HTTP server can use SSL:
go run $GOROOT/src/pkg/crypto/tls/generate_cert.go --host <IP address>

To use Google as an OAuth provider the --google_config_* flags must be set to point to
the a JSON file obtained after registering the application with the Google Developer Console
at https://cloud.google.com/console

More details on Google OAuth at:
https://developers.google.com/accounts/docs/OAuth2Login

Flags:
`, os.Args[0])
	flag.PrintDefaults()
}

func oauthBlesserGoogleParams(revocationManager revocation.RevocationManager) blesser.GoogleParams {
	googleParams := blesser.GoogleParams{
		BlessingDuration:  365 * 24 * time.Hour,
		DomainRestriction: *googleDomain,
		RevocationManager: revocationManager,
	}
	if clientID, err := getOAuthClientID(*googleConfigChrome); err != nil {
		vlog.Info(err)
	} else {
		googleParams.AccessTokenClients = append(googleParams.AccessTokenClients, blesser.AccessTokenClient{Name: "chrome", ClientID: clientID})
	}
	if clientID, err := getOAuthClientID(*googleConfigAndroid); err != nil {
		vlog.Info(err)
	} else {
		googleParams.AccessTokenClients = append(googleParams.AccessTokenClients, blesser.AccessTokenClient{Name: "android", ClientID: clientID})
	}
	return googleParams
}

func dbFromConfigDatabase(database string) (*sql.DB, error) {
	db, err := sql.Open("mysql", database+"?parseTime=true")
	if err != nil {
		return nil, fmt.Errorf("failed to create database with database(%v): %v", database, err)
	}
	if err := db.Ping(); err != nil {
		return nil, err
	}
	return db, nil
}

func getOAuthClientID(configFile string) (clientID string, err error) {
	f, err := os.Open(configFile)
	if err != nil {
		return "", fmt.Errorf("failed to open %q: %v", configFile, err)
	}
	defer f.Close()
	clientID, err = oauth.ClientIDFromJSON(f)
	if err != nil {
		return "", fmt.Errorf("failed to decode JSON in %q: %v", configFile, err)
	}
	return clientID, nil
}
