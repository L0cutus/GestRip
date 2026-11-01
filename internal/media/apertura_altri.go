//go:build !linux

package media

import "os"

// ApriInLettura fuori da Linux: un'apertura normale (Lstat e SameFile restano).
func ApriInLettura(p string) (*os.File, error) { return os.Open(p) }
