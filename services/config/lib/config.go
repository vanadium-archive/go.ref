// A mdns based config service.  We make an mdns group for each config.
// Mdns is probably an over kill for this but why use two when one will do?

// The service provides an eventually consistent map to all nodes on the
// network.  The winning map is the one with the highest version number.
// If a server stores the map in a file, it can also be a source of the
// map

package config

import (
	"fmt"
	"io/ioutil"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"

	"veyron2/verror"
	"veyron2/vlog"

	"code.google.com/p/mdns"
	"code.google.com/p/mdns/go_dns"
)

const maxDNSStringLength = 254

type config struct {
	version uint64
	pairs   map[string]string
}

type configService struct {
	rwlock  sync.RWMutex // protects elements of this structure.
	atomic  sync.RWMutex // ties together setting config below and writing the file.
	mdns    *mdns.MDNS
	service string
	file    string     // file containing config
	current *config    // the highest numbered config
	done    chan bool  // closed to tell children to go away
	change  *sync.Cond // condition variable broadcast to on every config change
	gen     int        // incremented every config change
}

// MDNSConfigService creates a new instance of the config service with the given name.
// If file is non blank, the initial config is read from file and any learned configs are
// stored in it.  Only instances with a file to backup will offer configs to the net.
// All other instances are passive.
func MDNSConfigService(name, file string, loopback bool) (ConfigService, error) {
	x := filepath.Base(file)
	if x == "." {
		x = ""
	}
	mdns, err := mdns.NewMDNS(x, "", "", loopback, false)
	if err != nil {
		vlog.Errorf("mdns startup failed: %s", err)
		return nil, err
	}
	cs := &configService{mdns: mdns, service: name + "-config", file: file, done: make(chan bool)}
	cs.change = sync.NewCond(&cs.rwlock)

	// Read the config file if we have one and offer it to everyone else.
	if cs.current, err = readFile(file); err != nil {
		vlog.Errorf("reading initial config: %s", err)
	}
	cs.Offer()

	// Watch config changes and remember them.
	go cs.watcher()
	return cs, nil
}

// Stop the service.
func (cs *configService) Stop() {
	cs.rwlock.Lock()
	mdns := cs.mdns
	cs.mdns = nil
	c := cs.done
	cs.done = nil
	cs.rwlock.Unlock()

	if c != nil {
		close(c)
	}
	if mdns != nil {
		mdns.Stop()
	}
}

func newConfig() *config {
	return &config{pairs: make(map[string]string)}
}

// parseVersion parses a config version.  The version is a pair of uint32s separated by a '.'.
// If the second is missing, we assume 0.  The first number is for humans.  The
// second is to break ties if machines generate configs.
func parseVersion(s string) (uint64, error) {
	f := strings.SplitN(s, ".", 2)
	v, err := strconv.ParseUint(f[0], 10, 32)
	if err != nil {
		return 0, err
	}
	var r uint64
	if len(f) == 2 {
		r, err = strconv.ParseUint(f[1], 10, 32)
		if err != nil {
			return 0, err
		}
	}
	return (v << 32) | r, nil
}

func serializeVersion(version uint64) string {
	return fmt.Sprintf("%d.%d", version>>32, version&0xffffffff)
}

// parseEntry parse an entry of the form "key : value" and
// add it to the map.  White space before and after "key" and
// value is discarded.
func (c *config) parseEntry(l string) error {
	// Ignore lines with nothing but white space or starting with #
	l = strings.TrimSpace(l)
	if len(l) == 0 || strings.HasPrefix(l, "#") {
		return nil
	}
	// The reset have to be key<white>*:<white>*value
	f := strings.SplitN(l, ":", 2)
	if len(f) != 2 {
		return verror.BadArgf("can't parse %s", l)
	}
	k := strings.TrimSpace(f[0])
	v := strings.TrimSpace(f[1])
	if len(k)+len(v) > maxDNSStringLength {
		return verror.BadArgf("entry %s:%s is too long", k, v)
	}
	c.pairs[k] = v
	if k != "version" {
		return nil
	}
	var err error
	c.version, err = parseVersion(v)
	return err
}

func serializeEntry(k, v string) (string, error) {
	if len(k)+len(v) > maxDNSStringLength {
		return "", verror.BadArgf("entry %s:%s is too long", k, v)
	}
	return k + ":" + v, nil
}

func readFile(file string) (*config, error) {
	if len(file) == 0 {
		return nil, verror.NotFoundf("no file to read")
	}

	// The config has to be small so just read it all in one go.
	b, err := ioutil.ReadFile(file)
	if err != nil {
		return nil, err
	}
	c := newConfig()
	for _, l := range strings.Split(string(b), "\n") {
		if err := c.parseEntry(l); err != nil {
			return nil, verror.BadArgf("file %s: %s", file, err)
		}
	}
	if _, ok := c.pairs["version"]; !ok {
		return nil, verror.BadArgf("file %s: missing legal version", file)
	}
	return c, nil
}

// writeFile is called with the write lock held.
func writeFile(file string, c *config) {
	if len(file) == 0 || c == nil || len(c.pairs) == 0 {
		return
	}
	var s string
	for k, v := range c.pairs {
		e, err := serializeEntry(k, v)
		if err != nil {
			vlog.Errorf("writing %s: %s", file, err)
			return
		}
		s += e + "\n"
	}
	if err := ioutil.WriteFile(file, []byte(s), 0644); err != nil {
		vlog.Errorf("writing %s: %q", file, err)
	}
}

// rrToConfig converts a set of TXT rrs to a config.
func rrToConfig(rr *dns.RR_TXT) (*config, error) {
	c := newConfig()
	for _, s := range rr.Txt {
		if err := c.parseEntry(s); err != nil {
			return nil, err
		}
	}
	// Ignore any config with no version.
	if _, ok := c.pairs["version"]; !ok {
		return nil, verror.NotFoundf("missing config version")
	}
	return c, nil
}

func (cs *configService) watchSingle(key string, c chan Pair) {
	ov, err := cs.Get(key)
	oexists := err == nil
	c <- Pair{Key: key, Value: ov, Nonexistant: !oexists}
	gen := 0
	for {
		v, err := cs.Get(key)
		exists := err == nil
		if exists != oexists || v != ov {
			c <- Pair{Key: key, Value: v, Nonexistant: !exists}
			ov, oexists = v, exists
		}
		// Block waiting for a change.
		cs.rwlock.Lock()
		for gen == cs.gen {
			cs.change.Wait()
		}
		gen = cs.gen
		cs.rwlock.Unlock()
	}
}

func (cs *configService) watchAll(c chan Pair) {
	omap := make(map[string]string, 0)
	gen := 0
	for {
		nmap, err := cs.GetAll()
		if err != nil {
			nmap = make(map[string]string, 0)
		}
		// See if any key disappeared.
		for k := range omap {
			if _, ok := nmap[k]; !ok {
				c <- Pair{Key: k, Value: "", Nonexistant: true}
			}
		}
		// See if any value changed or new key appeared.
		for k, v := range nmap {
			ov, ok := omap[k]
			if !ok || v != ov {
				c <- Pair{Key: k, Value: v}
			}
		}
		omap = nmap
		// Block waiting for a change.
		cs.rwlock.Lock()
		for gen == cs.gen {
			cs.change.Wait()
		}
		gen = cs.gen
		cs.rwlock.Unlock()
	}
}

// watcher waits for config changes and remembers them.
// TODO(p): Should we also watch the file for changes?
func (cs *configService) watcher() {
	cs.rwlock.RLock()
	c := cs.mdns.ServiceMemberWatch(cs.service)
	cs.mdns.SubscribeToService(cs.service)
	cs.rwlock.RUnlock()
	for {
		select {
		case si := <-c:
			var config *config
			for _, rr := range si.TxtRRs {
				c, err := rrToConfig(rr)
				if err != nil {
					continue
				}
				if config == nil || c.version > config.version {
					config = c
				}
			}
			if config == nil {
				continue
			}
			// This lock is to synchronize writing and reading of the data structure.
			cs.rwlock.Lock()
			if cs.current != nil && config.version <= cs.current.version {
				cs.rwlock.Unlock()
				continue
			}
			// This lock is to tie setting the data structure and writing the file into an atomic operation.
			cs.atomic.Lock()
			cs.current = config
			cs.gen++
			cs.change.Broadcast()
			cs.rwlock.Unlock()
			writeFile(cs.file, cs.current)
			cs.atomic.Unlock()
			cs.Offer()
		case <-cs.done:
			break
		}
	}
	close(c)
	return
}

// Get returns the value associated with key.
func (cs *configService) Get(key string) (string, error) {
	cs.rwlock.RLock()
	defer cs.rwlock.RUnlock()
	if cs.current == nil {
		return "", verror.NotFoundf("no config")
	}
	if v, ok := cs.current.pairs[key]; !ok {
		return "", verror.NotFoundf("config has no key %q", key)
	} else {
		return v, nil
	}
}

// Watch for changes to a particular key.
func (cs *configService) Watch(key string) chan Pair {
	c := make(chan Pair)
	go cs.watchSingle(key, c)
	return c
}

// Get returns the complete configuration.
func (cs *configService) GetAll() (map[string]string, error) {
	cs.rwlock.RLock()
	defer cs.rwlock.RUnlock()
	if cs.current == nil {
		return nil, verror.NotFoundf("no config found")
	}
	// Copy so caller can't change the map under our feet.
	reply := make(map[string]string)
	for k, v := range cs.current.pairs {
		reply[k] = v
	}
	return reply, nil
}

// WatchAll watches for changes to any key.
func (cs *configService) WatchAll() chan Pair {
	c := make(chan Pair)
	go cs.watchAll(c)
	return c
}

// Offer offers the pairs for the config to other servers.
func (cs *configService) Offer() {
	cs.rwlock.RLock()
	defer cs.rwlock.RUnlock()
	if cs.mdns == nil || cs.current == nil || len(cs.file) == 0 {
		return
	}
	// First sort the keys to get a canonical list for the txt entries.
	keys := make([]string, 0)
	for k := range cs.current.pairs {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	// Convert config to a slice of strings.
	var txt []string
	for _, k := range keys {
		e, err := serializeEntry(k, cs.current.pairs[k])
		if err != nil {
			verror.NotFoundf("offering config: %s", cs.file, err)
			return
		}
		txt = append(txt, e)
	}

	// Send (and keep sending).
	cs.mdns.AddService(cs.service, "", 0, txt...)
}

// Reread the config file and remember it if the version is newer than current.
func (cs *configService) Reread() error {
	cs.rwlock.RLock()
	file := cs.file
	cs.rwlock.RUnlock()
	if len(file) == 0 {
		return nil
	}
	c, err := readFile(file)
	if err != nil {
		return err
	}
	cs.rwlock.Lock()
	if cs.current != nil && c.version <= cs.current.version {
		cs.rwlock.Unlock()
		return nil
	}
	cs.current = c
	cs.gen++
	cs.change.Broadcast()
	cs.rwlock.Unlock()
	cs.Offer()
	return nil
}
