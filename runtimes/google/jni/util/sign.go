package util

import (
	"bytes"
	"strings"
)

type Sign string

const (
	// VoidSign denotes a signature of a Java void type.
	VoidSign Sign = "V"
	// ByteSign denotes a signature of a Java byte type.
	ByteSign Sign = "B"
	// BoolSign denotes a signature of a Java boolean type.
	BoolSign Sign = "Z"
	// CharSign denotes a signature of a Java char type.
	CharSign Sign = "C"
	// ShortSign denotes a signature of a Java short type.
	ShortSign Sign = "S"
	// IntSign denotes a signature of a Java int type.
	IntSign Sign = "I"
	// LongSign denotes a signature of a Java long type.
	LongSign Sign = "J"
	// FloatSign denotes a signature of a Java float type.
	FloatSign Sign = "F"
	// DoubleSign denotes a signature of a Java double type.
	DoubleSign Sign = "D"
)

var (
	// StringSign denotes a signature of a Java String type.
	StringSign = ClassSign("java.lang.String")
	// ObjectSign denotes a signature of a Java Object type.
	ObjectSign = ClassSign("java.lang.Object")
)

// ArraySign returns the array signature, given the underlying array type.
func ArraySign(sign Sign) Sign {
	return "[" + sign
}

// ClassSign returns the signature of the specified Java class.
// The class should be specified in java "java.lang.String" style.
func ClassSign(className string) Sign {
	return Sign("L" + strings.Replace(className, ".", "/", -1) + ";")
}

// FuncSign returns the signature of the specified java function.
func FuncSign(argSigns []Sign, retSign Sign) Sign {
	var buf bytes.Buffer
	buf.WriteRune('(')
	for _, sign := range argSigns {
		buf.WriteString(string(sign))
	}
	buf.WriteRune(')')
	buf.WriteString(string(retSign))
	return Sign(buf.String())
}
