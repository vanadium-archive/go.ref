package lib

import "unicode"

func lowercaseFirstCharacter(s string) string {
	for _, r := range s {
		return string(unicode.ToLower(r)) + s[1:]
	}
	return ""
}

func uppercaseFirstCharacter(s string) string {
	for _, r := range s {
		return string(unicode.ToUpper(r)) + s[1:]
	}
	return ""
}
