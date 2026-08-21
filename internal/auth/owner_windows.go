//go:build windows

package auth

import "io/fs"

// Windows credential files rely on the user profile ACL rather than on POSIX
// ownership bits, so there is nothing portable to assert here.
func checkOwner(fs.FileInfo) error { return nil }
