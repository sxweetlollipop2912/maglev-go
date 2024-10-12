// Package tuple_hash
// The 5-tuple of a packet refers to the source IP, source port,
// destination IP, destination port and IP protocol number.
package tuple_hash

import (
	"encoding/binary"
	"hash/crc32"
	"net"
)

func Hash(srcIP net.IP, srcPort uint16, dstIP net.IP, dstPort uint16, proto uint8) (uint32, error) {
	hash := crc32.NewIEEE()

	if _, err := hash.Write(srcIP.To16()); err != nil {
		return 0, err
	}
	if _, err := hash.Write(dstIP.To16()); err != nil {
		return 0, err
	}

	srcPortBytes := make([]byte, 2)
	binary.BigEndian.PutUint16(srcPortBytes, srcPort)
	if _, err := hash.Write(srcPortBytes); err != nil {
		return 0, err
	}

	destPortBytes := make([]byte, 2)
	binary.BigEndian.PutUint16(destPortBytes, dstPort)
	if _, err := hash.Write(destPortBytes); err != nil {
		return 0, err
	}

	if _, err := hash.Write([]byte{proto}); err != nil {
		return 0, err
	}

	return hash.Sum32(), nil
}
