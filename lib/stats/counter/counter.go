// Package counter implements counters that keeps track of their recent values
// over different periods of time.
// Example:
// c := counter.New()
// c.Incr(n)
// ...
// delta1h := c.Delta1h()
// delta10m := c.Delta10m()
// delta1m := c.Delta1m()
// and:
// rate1h := c.Rate1h()
// rate10m := c.Rate10m()
// rate1m := c.Rate1m()
package counter

import (
	"time"
)

var (
	// Used for testing.
	Now func() time.Time = time.Now
)

const (
	hour       = 0
	tenminutes = 1
	minute     = 2
)

// Counter is a counter that keeps track of its recent values over a given
// period of time, and with a given resolution. Use New() to instantiate.
type Counter struct {
	ts [3]*timeseries
}

// New returns a new Counter.
func New() *Counter {
	now := Now()
	c := &Counter{}
	c.ts[hour] = newTimeSeries(now, time.Hour, time.Minute)
	c.ts[tenminutes] = newTimeSeries(now, 10*time.Minute, 10*time.Second)
	c.ts[minute] = newTimeSeries(now, time.Minute, time.Second)
	return c
}

func (c *Counter) advance() {
	now := Now()
	for _, ts := range c.ts {
		ts.advanceTime(now)
	}
}

// Value returns the current value of the counter.
func (c *Counter) Value() int64 {
	return c.ts[minute].headValue()
}

// Set updates the current value of the counter.
func (c *Counter) Set(value int64) {
	c.advance()
	for _, ts := range c.ts {
		ts.set(value)
	}
}

// Incr increments the current value of the counter by 'delta'.
func (c *Counter) Incr(delta int64) {
	c.advance()
	for _, ts := range c.ts {
		ts.incr(delta)
	}
}

// Delta1h returns the delta for the last hour.
func (c *Counter) Delta1h() int64 {
	c.advance()
	return c.ts[hour].delta()
}

// Delta10m returns the delta for the last 10 minutes.
func (c *Counter) Delta10m() int64 {
	c.advance()
	return c.ts[tenminutes].delta()
}

// Delta1m returns the delta for the last minute.
func (c *Counter) Delta1m() int64 {
	c.advance()
	return c.ts[minute].delta()
}

// Rate1h returns the rate of change of the counter in the last hour.
func (c *Counter) Rate1h() float64 {
	c.advance()
	return c.ts[hour].rate()
}

// Rate10m returns the rate of change of the counter in the last 10 minutes.
func (c *Counter) Rate10m() float64 {
	c.advance()
	return c.ts[tenminutes].rate()
}

// Rate1m returns the rate of change of the counter in the last minute.
func (c *Counter) Rate1m() float64 {
	c.advance()
	return c.ts[minute].rate()
}

// Reset resets the counter to an empty state.
func (c *Counter) Reset() {
	now := Now()
	for _, ts := range c.ts {
		ts.reset(now)
	}
}
