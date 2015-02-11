package main_test

import (
	"encoding/json"
	"os"
	"strings"

	libsecurity "v.io/core/veyron/lib/testutil/security"
	"v.io/core/veyron/lib/testutil/v23tests"
	vsecurity "v.io/core/veyron/security"
	"v.io/core/veyron2/naming"
	"v.io/core/veyron2/security"
)

var binPkgs = []string{
	"v.io/core/veyron/services/mgmt/application/applicationd",
	"v.io/core/veyron/tools/application",
}

//go:generate v23 test generate

func helper(i *v23tests.T, clientBin *v23tests.Binary, expectError bool, credentials, cmd string, args ...string) string {
	args = append([]string{"-veyron.credentials=" + credentials, cmd}, args...)
	inv := clientBin.Start(args...)
	out := inv.Output()
	err := inv.Wait(os.Stdout, os.Stderr)
	if err != nil && !expectError {
		i.Fatalf("%s %q failed: %v\n%v", clientBin.Path(), strings.Join(args, " "), err, out)
	}
	if err == nil && expectError {
		i.Fatalf("%s %q did not fail when it should", clientBin.Path(), strings.Join(args, " "))
	}
	return strings.TrimSpace(out)

}

func matchEnvelope(i *v23tests.T, clientBin *v23tests.Binary, expectError bool, credentials, name, suffix string) string {
	return helper(i, clientBin, expectError, credentials, "match", naming.Join(name, suffix), "test-profile")
}

func putEnvelope(i *v23tests.T, clientBin *v23tests.Binary, credentials, name, suffix, envelope string) string {
	return helper(i, clientBin, false, credentials, "put", naming.Join(name, suffix), "test-profile", envelope)
}

func removeEnvelope(i *v23tests.T, clientBin *v23tests.Binary, credentials, name, suffix string) string {
	return helper(i, clientBin, false, credentials, "remove", naming.Join(name, suffix), "test-profile")
}

func V23TestApplicationRepository(i *v23tests.T) {
	v23tests.RunRootMT(i, "--veyron.tcp.address=127.0.0.1:0")

	// TODO(sjr): talk to caprita about the necessity/correctness of these.
	// Generate credentials.
	serverCred, serverPrin := libsecurity.NewCredentials("server")
	defer os.RemoveAll(serverCred)
	clientCred, _ := libsecurity.ForkCredentials(serverPrin, "client")
	defer os.RemoveAll(clientCred)

	// Start the application repository.
	appRepoName := "test-app-repo"
	appRepoStore := i.TempDir()
	args := []string{
		"-name=" + appRepoName,
		"-store=" + appRepoStore,
		"-v=2",
		"-veyron.tcp.address=127.0.0.1:0",
		"-veyron.credentials=" + serverCred,
	}
	i.BuildGoPkg("v.io/core/veyron/services/mgmt/application/applicationd").Start(args...)

	// Build the client binary.
	clientBin := i.BuildGoPkg("v.io/core/veyron/tools/application")

	// Generate publisher blessings
	principal, err := vsecurity.NewPrincipal()
	if err != nil {
		i.Fatal(err)
	}
	blessings, err := principal.BlessSelf("self")
	if err != nil {
		i.Fatal(err)
	}
	sig, err := principal.Sign([]byte("binarycontents"))
	if err != nil {
		i.Fatal(err)
	}
	sigJSON, err := json.MarshalIndent(sig, "  ", "  ")
	if err != nil {
		i.Fatal(err)
	}
	pubJSON, err := json.MarshalIndent(security.MarshalBlessings(blessings), "  ", "  ")
	if err != nil {
		i.Fatal(err)
	}

	// Create an application envelope.
	appRepoSuffix := "test-application/v1"
	appEnvelopeFile := i.TempFile()
	wantEnvelope := `{
  "Title": "title",
  "Args": null,
  "Binary": "foo",
  "Signature": ` + string(sigJSON) + `,
  "Publisher": ` + string(pubJSON) + `,
  "Env": null,
  "Packages": null
}`
	if _, err := appEnvelopeFile.Write([]byte(wantEnvelope)); err != nil {
		i.Fatalf("Write() failed: %v", err)
	}
	putEnvelope(i, clientBin, clientCred, appRepoName, appRepoSuffix, appEnvelopeFile.Name())

	// Match the application envelope.
	gotEnvelope := matchEnvelope(i, clientBin, false, clientCred, appRepoName, appRepoSuffix)
	if gotEnvelope != wantEnvelope {
		i.Fatalf("unexpected output: got %v, want %v", gotEnvelope, wantEnvelope)
	}

	// Remove the application envelope.
	removeEnvelope(i, clientBin, clientCred, appRepoName, appRepoSuffix)

	// Check that the application envelope no longer exists.
	matchEnvelope(i, clientBin, true, clientCred, appRepoName, appRepoSuffix)
}
