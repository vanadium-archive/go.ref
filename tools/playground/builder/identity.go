// Functions to create and bless identities.

package main

import (
	"os"
	"path"
)

type identity struct {
	Name     string
	Blesser  string
	Duration string
	Files    []string
}

func (id identity) create() error {
	if err := id.generate(); err != nil {
		return err
	}
	if id.Blesser != "" || id.Duration != "" {
		return id.bless()
	}
	return nil
}

func (id identity) generate() error {
	args := []string{"generate"}
	if id.Blesser == "" && id.Duration == "" {
		args = append(args, id.Name)
	}
	return runIdentity(args, path.Join("ids", id.Name))
}

func (id identity) bless() error {
	filename := path.Join("ids", id.Name)
	var blesser string
	if id.Blesser == "" {
		blesser = filename
	} else {
		blesser = path.Join("ids", id.Blesser)
	}
	args := []string{"bless", "--with", blesser}
	if id.Duration != "" {
		args = append(args, "--for", id.Duration)
	}
	args = append(args, filename, id.Name)
	tempfile := filename + ".tmp"
	if err := runIdentity(args, tempfile); err != nil {
		return err
	}
	return os.Rename(tempfile, filename)
}

func createIdentities(ids []identity) error {
	debug("Generating identities")
	if err := os.MkdirAll("ids", 0777); err != nil {
		return err
	}
	for _, id := range ids {
		if err := id.create(); err != nil {
			return err
		}
	}
	return nil
}

func runIdentity(args []string, filename string) error {
	cmd := makeCmd("", false, "identity", args...)
	out, err := os.Create(filename)
	if err != nil {
		return err
	}
	defer out.Close()
	// Note, here we actually overwrite cmd.Stdout (rather than adding a writer to
	// the multiWriter), so only stderr will be streamed back to the client.
	cmd.Stdout = out
	return cmd.Run()
}
