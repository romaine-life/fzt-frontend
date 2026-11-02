//go:build !windows

package frontend

import "errors"

// Only Windows PCs hold a device key; elsewhere the token file is the only sign-in.
func openPlatformKey() (deviceSigner, error) {
	return nil, errors.New("device sign-in needs Windows")
}
