package mounttable

import (
	"errors"

	"veyron2/ipc"
	"veyron2/naming"
	"veyron2/verror"
	"veyron2/vlog"
)

const maxDepth = 32

func convertServersToStrings(servers []mountedServer, suffix string) (ret []string) {
	for _, s := range servers {
		ret = append(ret, naming.Join(s.Server, suffix))
	}
	return
}

func resolveAgainstMountTable(client ipc.Client, names []string) ([]string, error) {
	// Try each server till one answers.
	finalErr := errors.New("no servers to resolve query")
	for _, name := range names {
		name = naming.MakeFixed(name)
		call, err := client.StartCall(name, "ResolveStep", nil, callTimeout)
		if err != nil {
			finalErr = err
			vlog.VI(2).Infof("ResolveStep.StartCall %s failed: %s", name, err)
			continue
		}
		servers := []mountedServer{}
		var suffix string
		ierr := call.Finish(&servers, &suffix, &err)
		if ierr != nil {
			// Internal/system error.
			finalErr = ierr
			vlog.VI(2).Infof("ResolveStep.Finish %s failed: %s", name, ierr)
			continue
		}
		// If any replica says the name doesn't exist, return that fact.
		if err != nil {
			if verror.Equal(naming.ErrNoSuchName, err) || verror.Equal(naming.ErrNoSuchNameRoot, err) {
				return nil, err
			}
			finalErr = err
			vlog.VI(2).Infof("ResolveStep %s failed: %s", name, err)
			continue
		}
		return convertServersToStrings(servers, suffix), nil
	}
	return nil, finalErr
}

func fixed(names []string) bool {
	for _, name := range names {
		if !naming.Fixed(name) {
			return false
		}
	}
	return true
}

func makeFixed(names []string) (ret []string) {
	for _, name := range names {
		ret = append(ret, naming.MakeFixed(name))
	}
	return
}

// Resolve implements veyron2/naming.MountTable.
func (ns *namespace) Resolve(name string) ([]string, error) {
	vlog.VI(2).Infof("Resolve %s", name)
	names := ns.rootName(name)
	if len(names) == 0 {
		return nil, naming.ErrNoMountTable
	}
	// Iterate walking through mount table servers.
	for remaining := maxDepth; remaining > 0; remaining-- {
		vlog.VI(2).Infof("Resolve loop %s", names)
		if fixed(names) {
			return names, nil
		}
		var err error
		curr := names
		if names, err = resolveAgainstMountTable(ns.rt.Client(), names); err != nil {

			// If the name could not be found in the mount table, return an error.
			if verror.Equal(naming.ErrNoSuchNameRoot, err) {
				err = naming.ErrNoSuchName
			}
			if verror.Equal(naming.ErrNoSuchName, err) {
				return nil, err
			}
			// Any other failure (server not found, no ResolveStep
			// method, etc.) are a sign that iterative resolution can
			// stop.
			return makeFixed(curr), nil
		}
	}
	return nil, naming.ErrResolutionDepthExceeded
}

// ResolveToMountTable implements veyron2/naming.MountTable.
func (ns *namespace) ResolveToMountTable(name string) ([]string, error) {
	vlog.VI(2).Infof("ResolveToMountTable %s", name)
	names := ns.rootName(name)
	if len(names) == 0 {
		return nil, naming.ErrNoMountTable
	}
	last := names
	for remaining := maxDepth; remaining > 0; remaining-- {
		vlog.VI(2).Infof("ResolveToMountTable loop %s", names)
		var err error
		curr := names
		if names, err = resolveAgainstMountTable(ns.rt.Client(), names); err != nil {
			if verror.Equal(naming.ErrNoSuchNameRoot, err) {
				return makeFixed(last), nil
			}
			if verror.Equal(naming.ErrNoSuchName, err) {
				return makeFixed(curr), nil
			}
			// Lots of reasons why another can happen.  We are trying to single out "this isn't
			// a mount table".  TODO(p); make this less of a hack, make a specific verror code
			// that means "we are up but don't implement what you are asking for".
			if notAnMT(err) {
				return makeFixed(last), nil
			}
			// TODO(caprita): If the server is unreachable for
			// example, we may still want to return its parent
			// mounttable rather than an error.
			return nil, err
		}
		if fixed(curr) {
			return curr, nil
		}
		last = curr
	}
	return nil, naming.ErrResolutionDepthExceeded
}

func finishUnresolve(call ipc.ClientCall) ([]string, error) {
	var newNames []string
	var unresolveErr error
	if err := call.Finish(&newNames, &unresolveErr); err != nil {
		return nil, err
	}
	return newNames, unresolveErr
}

func unresolveAgainstServer(client ipc.Client, names []string) ([]string, error) {
	finalErr := errors.New("no servers to unresolve")
	for _, name := range names {
		name = naming.MakeFixed(name)
		call, err := client.StartCall(name, "UnresolveStep", nil, callTimeout)
		if err != nil {
			finalErr = err
			vlog.VI(2).Infof("StartCall %q.UnresolveStep() failed: %s", name, err)
			continue

		}
		newNames, err := finishUnresolve(call)
		if err == nil {
			return newNames, nil
		}
		finalErr = err
		vlog.VI(2).Infof("Finish %s failed: %v", name, err)
	}
	return nil, finalErr
}

// TODO(caprita): Unresolve currently picks the first responsive server as it
// goes up the ancestry line.  This means that, if a service has several
// ancestors (each with potentially more than one replica), the first responsive
// replica of the first responsive ancestor is preferred.  In particular,
// other branches are ignored.  We need to figure out a desired policy for
// selecting the right branch (or should we return a representative of all
// branches?).

// Unesolve implements veyron2/naming.MountTable.
func (ns *namespace) Unresolve(name string) ([]string, error) {
	vlog.VI(2).Infof("Unresolve %s", name)
	names, err := ns.Resolve(name)
	if err != nil {
		return nil, err
	}
	for remaining := maxDepth; remaining > 0; remaining-- {
		vlog.VI(2).Infof("Unresolve loop %s", names)
		curr := names
		if names, err = unresolveAgainstServer(ns.rt.Client(), names); err != nil {
			return nil, err
		}
		if len(names) == 0 {
			return curr, nil
		}
	}
	return nil, naming.ErrResolutionDepthExceeded
}
