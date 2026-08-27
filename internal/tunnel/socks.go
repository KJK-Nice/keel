package tunnel

import (
	"encoding/binary"
	"io"
	"net"
	"strconv"
)

func serveSOCKS5(local net.Conn, dial func(network, addr string) (net.Conn, error)) {
	defer local.Close()
	hdr := make([]byte, 2)
	if _, err := io.ReadFull(local, hdr); err != nil {
		return
	}
	if hdr[0] != 5 {
		return
	}
	nmethods := int(hdr[1])
	if nmethods < 1 {
		return
	}
	methods := make([]byte, nmethods)
	if _, err := io.ReadFull(local, methods); err != nil {
		return
	}
	if _, err := local.Write([]byte{5, 0}); err != nil {
		return
	}

	req := make([]byte, 4)
	if _, err := io.ReadFull(local, req); err != nil {
		return
	}
	if req[0] != 5 || req[1] != 1 {
		_, _ = local.Write([]byte{5, 7, 0, 1, 0, 0, 0, 0, 0, 0})
		return
	}
	host, err := readSOCKSAddr(local, req[3])
	if err != nil {
		_, _ = local.Write([]byte{5, 1, 0, 1, 0, 0, 0, 0, 0, 0})
		return
	}
	var pb [2]byte
	if _, err := io.ReadFull(local, pb[:]); err != nil {
		return
	}
	port := int(binary.BigEndian.Uint16(pb[:]))
	remote, err := dial("tcp", net.JoinHostPort(host, strconv.Itoa(port)))
	if err != nil {
		_, _ = local.Write([]byte{5, 5, 0, 1, 0, 0, 0, 0, 0, 0})
		return
	}
	defer remote.Close()
	if _, err := local.Write([]byte{5, 0, 0, 1, 0, 0, 0, 0, 0, 0}); err != nil {
		return
	}
	pipe(local, remote)
}

func readSOCKSAddr(r io.Reader, atyp byte) (string, error) {
	switch atyp {
	case 1:
		ip := make([]byte, 4)
		if _, err := io.ReadFull(r, ip); err != nil {
			return "", err
		}
		return net.IP(ip).String(), nil
	case 3:
		var l [1]byte
		if _, err := io.ReadFull(r, l[:]); err != nil {
			return "", err
		}
		d := make([]byte, l[0])
		if _, err := io.ReadFull(r, d); err != nil {
			return "", err
		}
		return string(d), nil
	case 4:
		ip := make([]byte, 16)
		if _, err := io.ReadFull(r, ip); err != nil {
			return "", err
		}
		return net.IP(ip).String(), nil
	default:
		return "", io.ErrUnexpectedEOF
	}
}
