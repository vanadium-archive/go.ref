package auditor

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	"veyron.io/veyron/veyron/security/audit"
	"veyron.io/veyron/veyron2/security/wire"
	"veyron.io/veyron/veyron2/vlog"
	"veyron.io/veyron/veyron2/vom"
)

type fileAuditor struct {
	mu   sync.Mutex
	file *os.File
	enc  *vom.Encoder
}

// NewFileAuditor returns a security.Auditor implementation that synchronously writes
// audit log entries to files on disk.
//
// The file on disk is named with the provided prefix and a timestamp appended to it.
//
// TODO(ashankar,ataly): Should we be using something more query-able, like sqllite or
// something? Do we need integrity protection (NewSigningWriter)?
func NewFileAuditor(prefix string) (audit.Auditor, error) {
	var file *os.File
	for file == nil {
		fname := fmt.Sprintf("%s.%s", prefix, time.Now().Format(time.RFC3339))
		var err error
		if file, err = os.OpenFile(fname, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600); err != nil && !os.IsExist(err) {
			return nil, err
		}
	}
	return &fileAuditor{
		file: file,
		enc:  vom.NewEncoder(file),
	}, nil
}

func (a *fileAuditor) Audit(entry audit.Entry) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if err := a.enc.Encode(entry); err != nil {
		return fmt.Errorf("failed to write audit entry: %v", err)
	}
	// Ensure that any log entries are not lost in case the machine on which this process is running reboots.
	if err := a.file.Sync(); err != nil {
		return fmt.Errorf("failed to commit audit entry to persistent storage: %v", err)
	}
	return nil
}

// ReadAuditLog reads audit entries written using NewFileAuditor and produces them on the returned channel.
//
// The returned channel is closed when all committed entries have been sent on the channel.
// If blessingFilter is non-empty, only audit entries for method invocations matching:
// security.PrivateID.Bless(<*>, blessingFilter, ...)
// will be printed on the channel.
//
// TODO(ashankar): This custom "query" language is another reason why I think of a more sophisticated store.
func ReadAuditLog(prefix, blessingFilter string) (<-chan audit.Entry, error) {
	files, err := filepath.Glob(prefix + "*")
	if err != nil {
		return nil, err
	}
	c := make(chan audit.Entry)
	go sendAuditEvents(c, files, blessingFilter)
	return c, nil
}

func sendAuditEvents(dst chan<- audit.Entry, src []string, blessingFilter string) {
	defer close(dst)
	for _, fname := range src {
		file, err := os.Open(fname)
		if err != nil {
			vlog.VI(3).Infof("Unable to open %q: %v", fname, err)
			continue
		}
		decoder := vom.NewDecoder(file)
	fileloop:
		for {
			var entry audit.Entry
			switch err := decoder.Decode(&entry); err {
			case nil:
				filterAndSend(&entry, blessingFilter, dst)
			case io.EOF:
				break fileloop
			default:
				vlog.Errorf("Corrupt audit log? %q: %v", fname, err)
				break fileloop
			}
		}
		file.Close()
	}
}

func filterAndSend(entry *audit.Entry, filter string, dst chan<- audit.Entry) {
	if err := wire.ValidateBlessingName(filter); err != nil {
		// If the filter is invalid, send all events.
		// This allows for a means to ask for all events without defining a new
		// special character.
		dst <- *entry
		return
	}
	if entry.Method != "Bless" || len(entry.Arguments) < 2 {
		return
	}
	if name, ok := entry.Arguments[1].(string); ok && name == filter {
		dst <- *entry
	}
}
