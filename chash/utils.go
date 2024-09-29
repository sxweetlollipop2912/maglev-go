package chash

import crc322 "hash/crc32"

func crc32(msg []byte) uint32 {
	return crc322.ChecksumIEEE(msg)
}
