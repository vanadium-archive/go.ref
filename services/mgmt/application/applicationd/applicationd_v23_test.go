package main_test

import (
	"encoding/json"
	"os"
	"strings"

	"v.io/v23/naming"
	"v.io/v23/security"
	"v.io/x/ref/test/v23tests"
)

//go:generate v23 test generate

func helper(i *v23tests.T, clientBin *v23tests.Binary, expectError bool, cmd string, args ...string) string {
	args = append([]string{cmd}, args...)
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

func matchEnvelope(i *v23tests.T, clientBin *v23tests.Binary, expectError bool, name, suffix string) string {
	return helper(i, clientBin, expectError, "match", naming.Join(name, suffix), "test-profile")
}

func putEnvelope(i *v23tests.T, clientBin *v23tests.Binary, name, suffix, envelope string) string {
	return helper(i, clientBin, false, "put", naming.Join(name, suffix), "test-profile", envelope)
}

func removeEnvelope(i *v23tests.T, clientBin *v23tests.Binary, name, suffix string) string {
	return helper(i, clientBin, false, "remove", naming.Join(name, suffix), "test-profile")
}

func binaryWithCredentials(i *v23tests.T, extension, pkgpath string) *v23tests.Binary {
	creds, err := i.Shell().NewChildCredentials(extension)
	if err != nil {
		i.Fatalf("NewCustomCredentials (for %q) failed: %v", pkgpath, err)
	}
	b := i.BuildV23Pkg(pkgpath)
	return b.WithStartOpts(b.StartOpts().WithCustomCredentials(creds))
}

func V23TestApplicationRepository(i *v23tests.T) {
	v23tests.RunRootMT(i, "--veyron.tcp.address=127.0.0.1:0")

	// Start the application repository.
	appRepoName := "test-app-repo"
	binaryWithCredentials(i, "applicationd", "v.io/x/ref/services/mgmt/application/applicationd").Start(
		"-name="+appRepoName,
		"-store="+i.NewTempDir(),
		"-v=2",
		"-veyron.tcp.address=127.0.0.1:0")

	// Build the client binary (must be a delegate of the server to pass
	// the default authorization policy).
	clientBin := binaryWithCredentials(i, "applicationd/client", "v.io/x/ref/cmd/application")

	// Generate publisher blessings
	publisher, err := i.Shell().NewChildCredentials("publisher")
	if err != nil {
		i.Fatal(err)
	}
	sig, err := publisher.Principal().Sign([]byte("binarycontents"))
	if err != nil {
		i.Fatal(err)
	}
	sigJSON, err := json.MarshalIndent(sig, "    ", "  ")
	if err != nil {
		i.Fatal(err)
	}
	pubJSON, err := json.MarshalIndent(security.MarshalBlessings(publisher.Principal().BlessingStore().Default()), "  ", "  ")
	if err != nil {
		i.Fatal(err)
	}

	// Create an application envelope.
	appRepoSuffix := "test-application/v1"
	appEnvelopeFile := i.NewTempFile()
	wantEnvelope := `{
  "Title": "title",
  "Args": null,
  "Binary": {
    "File": "foo",
    "Signature": ` + string(sigJSON) + `
  },
  "Publisher": ` + string(pubJSON) + `,
  "Env": null,
  "Packages": null
}`
	if _, err := appEnvelopeFile.Write([]byte(wantEnvelope)); err != nil {
		i.Fatalf("Write() failed: %v", err)
	}
	putEnvelope(i, clientBin, appRepoName, appRepoSuffix, appEnvelopeFile.Name())

	// Match the application envelope.
	gotEnvelope := matchEnvelope(i, clientBin, false, appRepoName, appRepoSuffix)
	if gotEnvelope != wantEnvelope {
		i.Fatalf("unexpected output: got %v, want %v", gotEnvelope, wantEnvelope)
	}

	// Remove the application envelope.
	removeEnvelope(i, clientBin, appRepoName, appRepoSuffix)

	// Check that the application envelope no longer exists.
	matchEnvelope(i, clientBin, true, appRepoName, appRepoSuffix)
}
