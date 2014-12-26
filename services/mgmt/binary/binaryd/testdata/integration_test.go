package integration_test

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"v.io/core/veyron/lib/modules"
	"v.io/core/veyron/lib/testutil"
	"v.io/core/veyron/lib/testutil/integration"
	"v.io/core/veyron/lib/testutil/security"
	_ "v.io/core/veyron/profiles"
	"v.io/core/veyron2/naming"
)

func init() {
	testutil.Init()
}

var binPkgs = []string{
	"v.io/core/veyron/services/mgmt/binary/binaryd",
	"v.io/core/veyron/tools/binary",
}

func checkFileType(t *testing.T, file, typeString string) {
	var catOut bytes.Buffer
	catCmd := exec.Command("cat", file+".__info")
	catCmd.Stdout = &catOut
	catCmd.Stderr = &catOut
	if err := catCmd.Run(); err != nil {
		t.Fatalf("%q failed: %v\n%v", strings.Join(catCmd.Args, " "), err, catOut.String())
	}
	if got, want := strings.TrimSpace(catOut.String()), typeString; got != want {
		t.Fatalf("unexpect file type: got %v, want %v", got, want)
	}
}

func compareFiles(t *testing.T, f1, f2 string) {
	var cmpOut bytes.Buffer
	cmpCmd := exec.Command("cmp", f1, f2)
	cmpCmd.Stdout = &cmpOut
	cmpCmd.Stderr = &cmpOut
	if err := cmpCmd.Run(); err != nil {
		t.Fatalf("%q failed: %v\n%v", strings.Join(cmpCmd.Args, " "), err, cmpOut.String())
	}
}

func deleteFile(t *testing.T, binDir, credentials, mt, name, suffix string) {
	var deleteOut bytes.Buffer
	deleteArgs := []string{
		"-veyron.credentials=" + credentials,
		"-veyron.namespace.root=" + mt,
		"delete", naming.Join(name, suffix),
	}
	deleteCmd := exec.Command(filepath.Join(binDir, "binary"), deleteArgs...)
	deleteCmd.Stdout = &deleteOut
	deleteCmd.Stderr = &deleteOut
	if err := deleteCmd.Run(); err != nil {
		t.Fatalf("%q failed: %v\n%v", strings.Join(deleteCmd.Args, " "), err, deleteOut.String())
	}
}

func downloadFile(t *testing.T, expectError bool, binDir, credentials, mt, name, path, suffix string) {
	var downloadOut bytes.Buffer
	downloadArgs := []string{
		"-veyron.credentials=" + credentials,
		"-veyron.namespace.root=" + mt,
		"download", naming.Join(name, suffix), path,
	}
	downloadCmd := exec.Command(filepath.Join(binDir, "binary"), downloadArgs...)
	downloadCmd.Stdout = &downloadOut
	downloadCmd.Stderr = &downloadOut
	err := downloadCmd.Run()
	if err != nil && !expectError {
		t.Fatalf("%q failed: %v\n%v", strings.Join(downloadCmd.Args, " "), err, downloadOut.String())
	}
	if err == nil && expectError {
		t.Fatalf("%q did not fail when it should", strings.Join(downloadCmd.Args, " "))
	}
}

func downloadURL(t *testing.T, path, rootURL, suffix string) {
	var curlOut bytes.Buffer
	curlCmd := exec.Command("curl", "-f", "-o", path, fmt.Sprintf("%v/%v", rootURL, suffix))
	curlCmd.Stdout = &curlOut
	curlCmd.Stderr = &curlOut
	if err := curlCmd.Run(); err != nil {
		t.Fatalf("%q failed: %v\n%v", strings.Join(curlCmd.Args, " "), err, curlOut.String())
	}
}

func rootURL(t *testing.T, binDir, credentials, mt, name string) string {
	var rootOut bytes.Buffer
	rootArgs := []string{
		"-veyron.credentials=" + credentials,
		"-veyron.namespace.root=" + mt,
		"url", name,
	}
	rootCmd := exec.Command(filepath.Join(binDir, "binary"), rootArgs...)
	rootCmd.Stdout = &rootOut
	rootCmd.Stderr = &rootOut
	if err := rootCmd.Run(); err != nil {
		t.Fatalf("%q failed: %v\n%v", strings.Join(rootCmd.Args, " "), err, rootOut.String())
	}
	return strings.TrimSpace(rootOut.String())
}

func uploadFile(t *testing.T, binDir, credentials, mt, name, path, suffix string) {
	var uploadOut bytes.Buffer
	uploadArgs := []string{
		"-veyron.credentials=" + credentials,
		"-veyron.namespace.root=" + mt,
		"upload", naming.Join(name, suffix), path,
	}
	uploadCmd := exec.Command(filepath.Join(binDir, "binary"), uploadArgs...)
	uploadCmd.Stdout = &uploadOut
	uploadCmd.Stderr = &uploadOut
	if err := uploadCmd.Run(); err != nil {
		t.Fatalf("%q failed: %v\n%v", strings.Join(uploadCmd.Args, " "), err, uploadOut.String())
	}
}

func TestHelperProcess(t *testing.T) {
	modules.DispatchInTest()
}

func TestBinaryRepositoryIntegration(t *testing.T) {
	// Build the required binaries.
	binDir, cleanup, err := integration.BuildPkgs(binPkgs)
	if err != nil {
		t.Fatalf("%v", err)
	}
	defer cleanup()

	// Start a root mount table.
	shell, err := modules.NewShell(nil)
	if err != nil {
		t.Fatalf("NewShell() failed: %v", err)
	}
	defer shell.Cleanup(os.Stdin, os.Stderr)
	handle, mt, err := integration.StartRootMT(shell)
	if err != nil {
		t.Fatalf("%v", err)
	}
	defer handle.CloseStdin()

	// Generate credentials.
	serverCred, serverPrin := security.NewCredentials("server")
	defer os.RemoveAll(serverCred)
	clientCred, _ := security.ForkCredentials(serverPrin, "client")
	defer os.RemoveAll(clientCred)

	// Start the build server.
	binaryRepoBin := filepath.Join(binDir, "binaryd")
	binaryRepoName := "test-binary-repository"
	args := []string{
		"-name=" + binaryRepoName,
		"-http=127.0.0.1:0",
		"-veyron.tcp.address=127.0.0.1:0",
		"-veyron.credentials=" + serverCred,
		"-veyron.namespace.root=" + mt,
	}
	serverProcess, err := integration.StartServer(binaryRepoBin, args)
	if err != nil {
		t.Fatalf("%v", err)
	}
	defer serverProcess.Kill()

	// Upload a random binary file.
	binFile, err := ioutil.TempFile("", "")
	if err != nil {
		t.Fatalf("TempFile() failed: %v", err)
	}
	defer binFile.Close()
	defer os.Remove(binFile.Name())
	if _, err := binFile.Write(testutil.RandomBytes(16 * 1000 * 1000)); err != nil {
		t.Fatalf("Write() failed: %v", err)
	}
	binSuffix := "test-binary"
	uploadFile(t, binDir, clientCred, mt, binaryRepoName, binFile.Name(), binSuffix)

	// Upload a compressed version of the binary file.
	tarFile := binFile.Name() + ".tar.gz"
	var tarOut bytes.Buffer
	tarCmd := exec.Command("tar", "zcvf", tarFile, binFile.Name())
	tarCmd.Stdout = &tarOut
	tarCmd.Stderr = &tarOut
	if err := tarCmd.Run(); err != nil {
		t.Fatalf("%q failed: %v\n%v", strings.Join(tarCmd.Args, " "), err, tarOut.String())
	}
	defer os.Remove(tarFile)
	tarSuffix := "test-compressed-file"
	uploadFile(t, binDir, clientCred, mt, binaryRepoName, tarFile, tarSuffix)

	// Download the binary file and check that it matches the
	// original one and that it has the right file type.
	downloadedBinFile := binFile.Name() + "-downloaded"
	defer os.Remove(downloadedBinFile)
	downloadFile(t, false, binDir, clientCred, mt, binaryRepoName, downloadedBinFile, binSuffix)
	compareFiles(t, binFile.Name(), downloadedBinFile)
	checkFileType(t, downloadedBinFile, `{"Type":"application/octet-stream","Encoding":""}`)

	// Download the compressed version of the binary file and
	// check that it matches the original one and that it has the
	// right file type.
	downloadedTarFile := binFile.Name() + "-downloaded.tar.gz"
	defer os.Remove(downloadedTarFile)
	downloadFile(t, false, binDir, clientCred, mt, binaryRepoName, downloadedTarFile, tarSuffix)
	compareFiles(t, tarFile, downloadedTarFile)
	checkFileType(t, downloadedTarFile, `{"Type":"application/x-tar","Encoding":"gzip"}`)

	// Fetch the root URL of the HTTP server used by the binary
	// repository to serve URLs.
	root := rootURL(t, binDir, clientCred, mt, binaryRepoName)

	// Download the binary file using the HTTP protocol and check
	// that it matches the original one.
	downloadedBinFileURL := binFile.Name() + "-downloaded-url"
	defer os.Remove(downloadedBinFileURL)
	downloadURL(t, downloadedBinFileURL, root, binSuffix)
	compareFiles(t, downloadedBinFile, downloadedBinFileURL)

	// Download the compressed version of the binary file using
	// the HTTP protocol and check that it matches the original
	// one.
	downloadedTarFileURL := binFile.Name() + "-downloaded-url.tar.gz"
	defer os.Remove(downloadedTarFileURL)
	downloadURL(t, downloadedTarFileURL, root, tarSuffix)
	compareFiles(t, downloadedTarFile, downloadedTarFileURL)

	// Delete the files.
	deleteFile(t, binDir, clientCred, mt, binaryRepoName, binSuffix)
	deleteFile(t, binDir, clientCred, mt, binaryRepoName, tarSuffix)

	// Check the files no longer exist.
	downloadFile(t, true, binDir, clientCred, mt, binaryRepoName, downloadedBinFile, binSuffix)
	downloadFile(t, true, binDir, clientCred, mt, binaryRepoName, downloadedTarFile, tarSuffix)
}
