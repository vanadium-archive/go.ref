// +build linux,!arm

package platform

// str converts the input byte slice to a string, ignoring everything following
// a null character (including the null character).
func utsStr(c []int8) string {
	ret := make([]byte, 0, len(c))
	for _, v := range c {
		if v == 0 {
			break
		}
		ret = append(ret, byte(v))
	}
	return string(ret)
}
