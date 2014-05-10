package impl

import "strconv"

// counter is a closure used for generating unique identifiers.
func counter() func() string {
	var n int = 0
	return func() string {
		n++
		return strconv.Itoa(n)
	}
}
