package sysinit

import (
	"os"
	"text/template"
	"time"
)

// ServiceDescription is a generic service description that represents the
// common configuration details for specific systems.
type ServiceDescription struct {
	Service     string                          // The name of the Service
	Description string                          // A description of the Service
	Environment map[string]string               // Environment variables needed by the service
	Binary      string                          // The binary to be run
	Command     []string                        // The script/binary and command line options to use to start/stop the binary
	User        string                          // The username this service is to run as
	Setup       func(*ServiceDescription) error // Optional function to run before install.
	SetupParams map[string]string               // Params for the Setup function.
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
