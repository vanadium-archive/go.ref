package lib

import "unicode"

func LowercaseFirstCharacter(s string) string {
	for _, r := range s {
		return string(unicode.ToLower(r)) + s[1:]
	}
	return ""
}

func UppercaseFirstCharacter(s string) string {
	for _, r := range s {
		return string(unicode.ToUpper(r)) + s[1:]
	}
	return ""
}
