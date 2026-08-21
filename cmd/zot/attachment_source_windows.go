//go:build windows

package main

import "os"

func openAttachmentSource(path string) (*os.File, error) {
	return os.Open(path)
}
