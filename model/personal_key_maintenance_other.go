//go:build !linux

package model

import "os"

func ownedByRoot(os.FileInfo) bool { return false }
