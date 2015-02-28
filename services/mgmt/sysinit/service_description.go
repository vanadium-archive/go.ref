package sysinit

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"text/template"
	"time"
)

const dateFormat = "Jan 2 2006 at 15:04:05 (MST)"

// ServiceDescription is a generic service description that represents the
// common configuration details for specific systems.
type ServiceDescription struct {
	Service     string            // The name of the Service
	Description string            // A description of the Service
	Environment map[string]string // Environment variables needed by the service
	Binary      string            // The binary to be run
	Command     []string          // The script/binary and command line options to use to start/stop the binary
	User        string            // The username this service is to run as
}

// TODO(caprita): Unit test.

// SaveTo serializes the service description object to a file.
func (sd *ServiceDescription) SaveTo(fName string) error {
	jsonSD, err := json.Marshal(sd)
	if err != nil {
		return fmt.Errorf("Marshal(%v) failed: %v", sd, err)
	}
	if err := ioutil.WriteFile(fName, jsonSD, 0600); err != nil {
		return fmt.Errorf("WriteFile(%v) failed: %v", fName, err)
	}
	return nil
}

// LoadFrom de-serializes the service description object from a file created by
// SaveTo.
func (sd *ServiceDescription) LoadFrom(fName string) error {
	if sdBytes, err := ioutil.ReadFile(fName); err != nil {
		return fmt.Errorf("ReadFile(%v) failed: %v", fName, err)
	} else if err := json.Unmarshal(sdBytes, sd); err != nil {
		return fmt.Errorf("Unmarshal(%v) failed: %v", sdBytes, err)
	}
	return nil
}

func (sd *ServiceDescription) writeTemplate(templateContents, file string) error {
	conf, err := template.New(sd.Service + ".template").Parse(templateContents)
	if err != nil {
		return err
	}
	w := os.Stdout
	if len(file) > 0 {
		w, err = os.Create(file)
		if err != nil {
			return err
		}
	}
	type tmp struct {
		*ServiceDescription
		Date string
	}
	data := &tmp{
		ServiceDescription: sd,
		Date:               time.Now().Format(dateFormat),
	}
	return conf.Execute(w, &data)
}
