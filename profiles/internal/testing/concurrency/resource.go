package concurrency

// resourceKey represents an identifier of an abstract resource.
type resourceKey interface{}

// resourceSet represents a set of abstract resources.
type resourceSet map[resourceKey]struct{}

// newResourceSet if the resourceSet factory.
func newResourceSet() resourceSet {
	return make(resourceSet)
}
