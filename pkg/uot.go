package socks5

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"sync"
)

// UDP over TCP protocol versions, they are used to set the Dialer.UDPOverTCP.
const (
	// UDPOverTCPV1 is the legacy version of the UDP over TCP protocol.
	// Every packet carries its own destination address.
	UDPOverTCPV1 = 1
	// UDPOverTCPV2 is the current version of the UDP over TCP protocol.
	// The destination address can be sent once in the request header.
	UDPOverTCPV2 = 2
)

// Magic addresses which are used by the client to request a UDP over TCP
// connection from the proxy server.
const (
	magicAddressV1 = "sp.udp-over-tcp.arpa"
	magicAddressV2 = "sp.v2.udp-over-tcp.arpa"
)

// UDP over TCP uses its own address type bytes,
// which are different from the ones of the SOCKS5.
const (
	uotIPv4Address = 0x00
	uotIPv6Address = 0x01
	uotFqdnAddress = 0x02
)

var errPacketTooLarge = errors.New("packet too large")

// magicAddress returns the magic address of the given UDP over TCP version.
func magicAddress(version int) string {
	if version == UDPOverTCPV2 {
		return magicAddressV2
	}
	return magicAddressV1
}

// uotVersionOf returns the UDP over TCP version of the address,
// and reports whether the address is a magic address.
func uotVersionOf(addr *address) (int, bool) {
	if addr == nil || addr.IP != nil {
		return 0, false
	}
	switch addr.Name {
	case magicAddressV1:
		return UDPOverTCPV1, true
	case magicAddressV2:
		return UDPOverTCPV2, true
	default:
		return 0, false
	}
}

// parseAddress parses a "host:port" string into an address.
func parseAddress(addr string) (*address, error) {
	host, port, err := splitHostPort(addr)
	if err != nil {
		return nil, err
	}
	if ip := net.ParseIP(host); ip != nil {
		return &address{IP: ip, Port: port}, nil
	}
	return &address{Name: host, Port: port}, nil
}

// toAddress converts a net.Addr into an address.
func toAddress(addr net.Addr) *address {
	switch a := addr.(type) {
	case nil:
		return nil
	case *address:
		return a
	case *net.UDPAddr:
		return &address{IP: a.IP, Port: a.Port}
	}
	a, err := parseAddress(addr.String())
	if err != nil {
		return nil
	}
	return a
}

// toUDPAddr converts a net.Addr into a *net.UDPAddr, resolving the domain name if needed.
func toUDPAddr(addr net.Addr) (*net.UDPAddr, error) {
	switch a := addr.(type) {
	case *net.UDPAddr:
		return a, nil
	case *address:
		if a.IP != nil {
			return &net.UDPAddr{IP: a.IP, Port: a.Port}, nil
		}
		if a.Name != "" {
			return net.ResolveUDPAddr("udp", a.Address())
		}
	}
	return net.ResolveUDPAddr("udp", addr.String())
}

// readUOTAddr reads an address with the UDP over TCP address format.
func readUOTAddr(r io.Reader) (*address, error) {
	addr := &address{}

	var addrType [1]byte
	if _, err := io.ReadFull(r, addrType[:]); err != nil {
		return nil, err
	}

	switch addrType[0] {
	case uotIPv4Address:
		ip := make(net.IP, net.IPv4len)
		if _, err := io.ReadFull(r, ip); err != nil {
			return nil, err
		}
		addr.IP = ip
	case uotIPv6Address:
		ip := make(net.IP, net.IPv6len)
		if _, err := io.ReadFull(r, ip); err != nil {
			return nil, err
		}
		addr.IP = ip
	case uotFqdnAddress:
		length, err := readByte(r)
		if err != nil {
			return nil, err
		}
		name := make([]byte, length)
		if _, err := io.ReadFull(r, name); err != nil {
			return nil, err
		}
		addr.Name = string(name)
	default:
		return nil, errUnrecognizedAddrType
	}

	var port [2]byte
	if _, err := io.ReadFull(r, port[:]); err != nil {
		return nil, err
	}
	addr.Port = int(binary.BigEndian.Uint16(port[:]))
	return addr, nil
}

// writeUOTAddr writes an address with the UDP over TCP address format.
func writeUOTAddr(w io.Writer, addr *address) error {
	if addr == nil {
		_, err := w.Write([]byte{uotIPv4Address, 0, 0, 0, 0, 0, 0})
		return err
	}
	if addr.IP != nil {
		if ip4 := addr.IP.To4(); ip4 != nil {
			if _, err := w.Write([]byte{uotIPv4Address}); err != nil {
				return err
			}
			if _, err := w.Write(ip4); err != nil {
				return err
			}
		} else if ip6 := addr.IP.To16(); ip6 != nil {
			if _, err := w.Write([]byte{uotIPv6Address}); err != nil {
				return err
			}
			if _, err := w.Write(ip6); err != nil {
				return err
			}
		} else {
			if _, err := w.Write([]byte{uotIPv4Address, 0, 0, 0, 0}); err != nil {
				return err
			}
		}
	} else if addr.Name != "" {
		if len(addr.Name) > 255 {
			return errStringTooLong
		}
		if _, err := w.Write([]byte{uotFqdnAddress, byte(len(addr.Name))}); err != nil {
			return err
		}
		if _, err := w.Write([]byte(addr.Name)); err != nil {
			return err
		}
	} else {
		if _, err := w.Write([]byte{uotIPv4Address, 0, 0, 0, 0}); err != nil {
			return err
		}
	}

	var port [2]byte
	binary.BigEndian.PutUint16(port[:], uint16(addr.Port))
	_, err := w.Write(port[:])
	return err
}

// readUoTRequest reads the request header of the version 2.
func readUoTRequest(r io.Reader) (bool, *address, error) {
	isConnect, err := readByte(r)
	if err != nil {
		return false, nil, err
	}
	destination, err := readAddr(r)
	if err != nil {
		return false, nil, err
	}
	return isConnect == 1, destination, nil
}

// writeUoTRequest writes the request header of the version 2.
func writeUoTRequest(w io.Writer, isConnect bool, destination *address) error {
	var flag [1]byte
	if isConnect {
		flag[0] = 1
	}
	if _, err := w.Write(flag[:]); err != nil {
		return err
	}
	return writeAddr(w, destination)
}

// UoTConn wraps a connection and turns it into a packet oriented connection
// by the UDP over TCP protocol.
type UoTConn struct {
	net.Conn
	destination *address
	isConnect   bool
	writeMutex  sync.Mutex
	bufWrite    [maxUdpPacket + maxHeaderSize]byte
}

// NewUoTConn returns a UoTConn over the connection.
// When isConnect is true, all the packets are sent to the destination and no
// address is carried by each packet, it only works with the version 2.
func NewUoTConn(conn net.Conn, version int, isConnect bool, destination *address) *UoTConn {
	return &UoTConn{
		Conn:        conn,
		destination: destination,
		isConnect:   isConnect && version == UDPOverTCPV2,
	}
}

// ReadFrom implements the net.PacketConn ReadFrom method.
func (c *UoTConn) ReadFrom(p []byte) (n int, addr net.Addr, err error) {
	destination := c.destination
	if !c.isConnect {
		destination, err = readUOTAddr(c.Conn)
		if err != nil {
			return 0, nil, err
		}
	}

	var length [2]byte
	if _, err = io.ReadFull(c.Conn, length[:]); err != nil {
		return 0, nil, err
	}
	size := int(binary.BigEndian.Uint16(length[:]))
	if size > len(p) {
		return 0, nil, io.ErrShortBuffer
	}
	if _, err = io.ReadFull(c.Conn, p[:size]); err != nil {
		return 0, nil, err
	}
	return size, destination, nil
}

// Read implements the net.Conn Read method.
func (c *UoTConn) Read(b []byte) (int, error) {
	n, _, err := c.ReadFrom(b)
	if err != nil {
		return 0, err
	}
	return n, nil
}

// WriteTo implements the net.PacketConn WriteTo method.
func (c *UoTConn) WriteTo(p []byte, addr net.Addr) (n int, err error) {
	if len(p) > maxUdpPacket {
		return 0, errPacketTooLarge
	}

	c.writeMutex.Lock()
	defer c.writeMutex.Unlock()

	buf := bytes.NewBuffer(c.bufWrite[:0])
	if !c.isConnect {
		destination := toAddress(addr)
		if destination == nil {
			return 0, errUnrecognizedAddrType
		}
		if err = writeUOTAddr(buf, destination); err != nil {
			return 0, err
		}
	}

	var length [2]byte
	binary.BigEndian.PutUint16(length[:], uint16(len(p)))
	buf.Write(length[:])
	buf.Write(p)

	if _, err = c.Conn.Write(buf.Bytes()); err != nil {
		return 0, err
	}
	return len(p), nil
}

// Write implements the net.Conn Write method.
func (c *UoTConn) Write(b []byte) (int, error) {
	if c.destination == nil {
		return 0, errBadHeader
	}
	return c.WriteTo(b, c.destination)
}

// RemoteAddr implements the net.Conn RemoteAddr method.
func (c *UoTConn) RemoteAddr() net.Addr {
	if c.destination != nil {
		return c.destination
	}
	return c.Conn.RemoteAddr()
}
