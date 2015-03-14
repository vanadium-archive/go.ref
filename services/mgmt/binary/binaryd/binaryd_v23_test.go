package main_test

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"os/exec"
	"strings"

	"v.io/v23/naming"
	"v.io/x/ref/test/testutil"
	"v.io/x/ref/test/v23tests"
)

//go:generate v23 test generate

func checkFileType(i *v23tests.T, file, typeString string) {
	var catOut bytes.Buffer
	catCmd := exec.Command("cat", file+".__info")
	catCmd.Stdout = &catOut
	catCmd.Stderr = &catOut
	if err := catCmd.Run(); err != nil {
		i.Fatalf("%q failed: %v\n%v", strings.Join(catCmd.Args, " "), err, catOut.String())
	}
	if got, want := strings.TrimSpace(catOut.String()), typeString; got != want {
		i.Fatalf("unexpect file type: got %v, want %v", got, want)
	}
}

func readFileOrDie(i *v23tests.T, path string) []byte {
	result, err := ioutil.ReadFile(path)
	if err != nil {
		i.Fatalf("ReadFile(%q) failed: %v", path, err)
	}
	return result
}

func compareFiles(i *v23tests.T, f1, f2 string) {
	if !bytes.Equal(readFileOrDie(i, f1), readFileOrDie(i, f2)) {
		i.Fatalf("the contents of %s and %s differ when they should not", f1, f2)
	}
}

func deleteFile(i *v23tests.T, clientBin *v23tests.Binary, name, suffix string) {
	clientBin.Start("delete", naming.Join(name, suffix)).WaitOrDie(os.Stdout, os.Stderr)
}

func downloadFile(i *v23tests.T, clientBin *v23tests.Binary, expectError bool, name, path, suffix string) {
	args := []string{"download", naming.Join(name, suffix), path}
	err := clientBin.Start(args...).Wait(os.Stdout, os.Stderr)
	if expectError && err == nil {
		i.Fatalf("%s %v: did not fail when it should", clientBin.Path(), args)
	}
	if !expectError && err != nil {
		i.Fatalf("%s %v: failed: %v", clientBin.Path(), args, err)
	}
}

func downloadURL(i *v23tests.T, path, rootURL, suffix string) {
	url := fmt.Sprintf("http://%v/%v", rootURL, suffix)
	resp, err := http.Get(url)
	if err != nil {
		i.Fatalf("Get(%q) failed: %v", url, err)
	}
	output, err := ioutil.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		i.Fatalf("ReadAll() failed: %v", err)
	}
	if err = ioutil.WriteFile(path, output, 0600); err != nil {
		i.Fatalf("WriteFile() failed: %v", err)
	}
}

func rootURL(i *v23tests.T, clientBin *v23tests.Binary, name string) string {
	return strings.TrimSpace(clientBin.Start("url", name).Output())
}

func uploadFile(i *v23tests.T, clientBin *v23tests.Binary, name, path, suffix string) {
	clientBin.Start("upload", naming.Join(name, suffix), path).WaitOrDie(os.Stdout, os.Stderr)
}

func binaryWithCredentials(i *v23tests.T, extension, pkgpath string) *v23tests.Binary {
	creds, err := i.Shell().NewChildCredentials(extension)
	if err != nil {
		i.Fatalf("NewCustomCredentials (for %q) failed: %v", pkgpath, err)
	}
	b := i.BuildV23Pkg(pkgpath)
	return b.WithStartOpts(b.StartOpts().WithCustomCredentials(creds))
}

func V23TestBinaryRepositoryIntegration(i *v23tests.T) {
	v23tests.RunRootMT(i, "--veyron.tcp.address=127.0.0.1:0")

	// Build the required binaries.
	// The client must run as a "delegate" of the server in order to pass
	// the default authorization checks on the server.
	var (
		binaryRepoBin = binaryWithCredentials(i, "binaryd", "v.io/x/ref/services/mgmt/binary/binaryd")
		clientBin     = binaryWithCredentials(i, "binaryd/client", "v.io/x/ref/cmd/binary")
	)

	// Start the build server.
	binaryRepoName := "test-binary-repository"
	binaryRepoBin.Start(
		"-name="+binaryRepoName,
		"-http=127.0.0.1:0",
		"-veyron.tcp.address=127.0.0.1:0")

	// Upload a random binary file.
	binFile := i.NewTempFile()
	if _, err := binFile.Write(testutil.RandomBytes(16 * 1000 * 1000)); err != nil {
		i.Fatalf("Write() failed: %v", err)
	}
	binSuffix := "test-binary"
	uploadFile(i, clientBin, binaryRepoName, binFile.Name(), binSuffix)

	// Upload a compressed version of the binary file.
	tarFile := binFile.Name() + ".tar.gz"
	var tarOut bytes.Buffer
	tarCmd := exec.Command("tar", "zcvf", tarFile, binFile.Name())
	tarCmd.Stdout = &tarOut
	tarCmd.Stderr = &tarOut
	if err := tarCmd.Run(); err != nil {
		i.Fatalf("%q failed: %v\n%v", strings.Join(tarCmd.Args, " "), err, tarOut.String())
	}
	defer os.Remove(tarFile)
	tarSuffix := "test-compressed-file"
	uploadFile(i, clientBin, binaryRepoName, tarFile, tarSuffix)

	// Download the binary file and check that it matches the
	// original one and that it has the right file type.
	downloadedBinFile := binFile.Name() + "-downloaded"
	defer os.Remove(downloadedBinFile)
	downloadFile(i, clientBin, false, binaryRepoName, downloadedBinFile, binSuffix)
	compareFiles(i, binFile.Name(), downloadedBinFile)
	checkFileType(i, downloadedBinFile, `{"Type":"application/octet-stream","Encoding":""}`)

	// Download the compressed version of the binary file and
	// check that it matches the original one and that it has the
	// right file type.
	downloadedTarFile := binFile.Name() + "-downloaded.tar.gz"
	defer os.Remove(downloadedTarFile)
	downloadFile(i, clientBin, false, binaryRepoName, downloadedTarFile, tarSuffix)
	compareFiles(i, tarFile, downloadedTarFile)
	checkFileType(i, downloadedTarFile, `{"Type":"application/x-tar","Encoding":"gzip"}`)

	// Fetch the root URL of the HTTP server used by the binary
	// repository to serve URLs.
	root := rootURL(i, clientBin, binaryRepoName)

	// Download the binary file using the HTTP protocol and check
	// that it matches the original one.
	downloadedBinFileURL := binFile.Name() + "-downloaded-url"
	defer os.Remove(downloadedBinFileURL)
	downloadURL(i, downloadedBinFileURL, root, binSuffix)
	compareFiles(i, downloadedBinFile, downloadedBinFileURL)

	// Download the compressed version of the binary file using
	// the HTTP protocol and check that it matches the original
	// one.
	downloadedTarFileURL := binFile.Name() + "-downloaded-url.tar.gz"
	defer os.Remove(downloadedTarFileURL)
	downloadURL(i, downloadedTarFileURL, root, tarSuffix)
	compareFiles(i, downloadedTarFile, downloadedTarFileURL)

	// Delete the files.
	deleteFile(i, clientBin, binaryRepoName, binSuffix)
	deleteFile(i, clientBin, binaryRepoName, tarSuffix)

	// Check the files no longer exist.
	downloadFile(i, clientBin, true, binaryRepoName, downloadedBinFile, binSuffix)
	downloadFile(i, clientBin, true, binaryRepoName, downloadedTarFile, tarSuffix)
}
