//go:build !windows

package fileutil

import "os"

func replaceFileAtomic(src, dst string) error {
	return os.Rename(src, dst)
}
