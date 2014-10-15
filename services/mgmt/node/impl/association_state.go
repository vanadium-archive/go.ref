package impl

import (
	"veyron.io/veyron/veyron2/services/mgmt/node"
)

// TODO(rjk): Replace this with disk-backed storage in the node
// manager's directory hierarchy.
type systemNameIdentityAssociation map[string]string

// associatedUname returns a system name from the identity to system
// name association store if one exists for any of the listed
// identities.
func (u systemNameIdentityAssociation) associatedSystemAccount(identityNames []string) (string, bool) {
	systemName := ""
	present := false

	for _, n := range identityNames {
		if systemName, present = u[n]; present {
			break
		}
	}
	return systemName, present
}

func (u systemNameIdentityAssociation) getAssociations() ([]node.Association, error) {
	assocs := make([]node.Association, 0)
	for k, v := range u {
		assocs = append(assocs, node.Association{k, v})
	}
	return assocs, nil
}

func (u systemNameIdentityAssociation) addAssociations(identityNames []string, systemName string) error {
	for _, n := range identityNames {
		u[n] = systemName
	}
	return nil
}

func (u systemNameIdentityAssociation) deleteAssociations(identityNames []string) error {
	for _, n := range identityNames {
		delete(u, n)
	}
	return nil
}

func newSystemNameIdentityAssociation() systemNameIdentityAssociation {
	return make(map[string]string)
}
