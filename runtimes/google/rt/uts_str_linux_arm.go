// +build linux,arm

package rt

// str converts the input byte slice to a string, ignoring everything following
// a null character (including the null character).
func utsStr(c []uint8) string {
	ret := make([]byte, 0, len(c))
	for _, v := range c {
		if v == 0 {
			break
		}
		ret = append(ret, byte(v))
	}
	return string(ret)
}
