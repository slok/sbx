package egress

import (
	"encoding/binary"
	"fmt"
	"net"
	"syscall"
	"unsafe"
)

// getOriginalDst retrieves the original destination address before nftables DNAT
// using the SO_ORIGINAL_DST socket option. Linux only.
func getOriginalDst(conn net.Conn) (*net.TCPAddr, error) {
	tcpConn, ok := conn.(*net.TCPConn)
	if !ok {
		return nil, fmt.Errorf("not a TCP connection")
	}

	rawConn, err := tcpConn.SyscallConn()
	if err != nil {
		return nil, fmt.Errorf("could not get raw conn: %w", err)
	}

	var addr syscall.RawSockaddrInet4
	var getErr error
	err = rawConn.Control(func(fd uintptr) {
		// SO_ORIGINAL_DST = 80 on Linux.
		const soOriginalDst = 80
		addrLen := uint32(syscall.SizeofSockaddrInet4)
		_, _, errno := syscall.Syscall6(
			syscall.SYS_GETSOCKOPT,
			fd,
			syscall.SOL_IP,
			soOriginalDst,
			uintptr(unsafe.Pointer(&addr)),
			uintptr(unsafe.Pointer(&addrLen)),
			0,
		)
		if errno != 0 {
			getErr = fmt.Errorf("getsockopt SO_ORIGINAL_DST failed: %w", errno)
		}
	})
	if err != nil {
		return nil, err
	}
	if getErr != nil {
		return nil, getErr
	}

	ip := net.IPv4(addr.Addr[0], addr.Addr[1], addr.Addr[2], addr.Addr[3])
	// Port is in network byte order (big-endian) in RawSockaddrInet4.
	portBytes := (*[2]byte)(unsafe.Pointer(&addr.Port))
	port := int(binary.BigEndian.Uint16(portBytes[:]))

	return &net.TCPAddr{IP: ip, Port: port}, nil
}
