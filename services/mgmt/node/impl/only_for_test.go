package impl

// This file contains code in the impl package that we only want built for tests
// (it exposes public API methods that we don't want to normally expose).

func (c *callbackState) leaking() bool {
	c.Lock()
	defer c.Unlock()
	return len(c.channels) > 0
}

func (d *dispatcher) Leaking() bool {
	return d.internal.callback.leaking()
}
