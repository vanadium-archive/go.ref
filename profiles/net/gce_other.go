// +build !linux

package net

import (
	"veyron2"
	"veyron2/config"
)

func handleGCE(veyron2.Runtime, *config.Publisher) bool {
	return false
}
