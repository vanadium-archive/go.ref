// Package security contains utility testing functions related to
// security.
package security

import (
	"io/ioutil"
	"os"
	"strconv"
	"time"

	"veyron/lib/testutil"
	isecurity "veyron/runtimes/google/security"

	"veyron2/security"
)

// NewBlessedIdentity creates a new identity and blesses it using the provided blesser
// under the provided name. This function is meant to be used for testing purposes only,
// it panics if there is an error.
func NewBlessedIdentity(blesser security.PrivateID, name string) security.PrivateID {
	id, err := isecurity.NewPrivateID("test", nil)
	if err != nil {
		panic(err)
	}

	blessedID, err := blesser.Bless(id.PublicID(), name, 5*time.Minute, nil)
	if err != nil {
		panic(err)
	}
	derivedID, err := id.Derive(blessedID)
	if err != nil {
		panic(err)
	}
	return derivedID
}

// SaveACLToFile saves the provided ACL in JSON format to a randomly created
// temporary file, and returns the path to the file. This function is meant
// to be used for testing purposes only, it panics if there is an error. The
// caller must ensure that the created file is removed once it is no longer needed.
func SaveACLToFile(acl security.ACL) string {
	f, err := ioutil.TempFile("", "saved_acl")
	if err != nil {
		panic(err)
	}
	defer f.Close()
	if err := security.SaveACL(f, acl); err != nil {
		defer os.Remove(f.Name())
		panic(err)
	}
	return f.Name()
}

// SaveIdentityToFile saves the provided identity in Base64VOM format
// to a randomly created temporary file, and returns the path to the file.
// This function is meant to be used for testing purposes only, it panics
// if there is an error. The caller must ensure that the created file
// is removed once it is no longer needed.
func SaveIdentityToFile(id security.PrivateID) string {
	f, err := ioutil.TempFile("", strconv.Itoa(testutil.Rand.Int()))
	if err != nil {
		panic(err)
	}
	defer f.Close()
	filePath := f.Name()

	if err := security.SaveIdentity(f, id); err != nil {
		os.Remove(filePath)
		panic(err)
	}
	return filePath
}
