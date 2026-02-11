//go:build !linux

package egress

import (
	"fmt"
	"net"
)

// getOriginalDst is not supported on non-Linux platforms.
func getOriginalDst(conn net.Conn) (*net.TCPAddr, error) {
	return nil, fmt.Errorf("SO_ORIGINAL_DST is only available on Linux")
}
