package service

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"github.com/joeqian10/neo-gogogo/crypto"
	"github.com/joeqian10/neo-gogogo/helper"
	"log"
	"math/big"
)

func MerkleProve(path, root []byte) ([]byte, error) {
	offset := 0
	value, offset, err := ReadVarBytes(path, offset)
	log.Printf(helper.BytesToHex(value))
	if err != nil {
		return nil, err
	}
	hash := HashLeaf(value)
	log.Printf(helper.BytesToHex(hash))
	size := (len(path) - offset) / 32
	for i := 0; i < size; i++ {
		var f []byte
		f, offset, err = ReadBytes(path, offset, 1)
		log.Printf(helper.BytesToHex(f))
		if err != nil {
			return nil, err
		}
		var v []byte
		v, offset, err = ReadBytes(path, offset, 32)
		log.Printf(helper.BytesToHex(v))
		if err != nil {
			return nil, err
		}
		if f[0] == 0x00 {
			hash = HashChildren(v, hash)
		} else {
			hash = HashChildren(hash, v)
		}
		log.Printf(helper.BytesToHex(hash))
	}

	if !bytes.Equal(hash, root) {
		return nil, fmt.Errorf("expect root is not equal actual root, expect:%x, actual:%x", hash, root)
	}
	return value, nil
}

func HashChildren(v, hash []byte) []byte {
	prefix := []byte{0x01}
	return crypto.Sha256(append(append(prefix, v...), hash...))
}

func HashLeaf(value []byte) []byte {
	prefix := []byte{0x00}
	return crypto.Sha256(append(prefix, value...))
}

func ReadVarBytes(buffer []byte, offset int) ([]byte, int, error) {
	count, newOffset, err := ReadVarUInt(buffer, offset)
	if err != nil {
		return nil, 0, err
	}
	return ReadBytes(buffer, newOffset, int(count))
}

func ReadBytes(buffer []byte, offset int, count int) ([]byte, int, error) {
	if offset+count > len(buffer) {
		return nil, 0, fmt.Errorf("incorrect offset or count")
	}
	return buffer[offset : offset+count], offset + count, nil
}

func ReadVarUInt(buffer []byte, offset int) (uint64, int, error) {
	res, newOffset, err := ReadBytes(buffer, offset, 1)
	if err != nil {
		return 0, 0, err
	}
	if len(res) != 1 {
		return 0, 0, fmt.Errorf("incorrect lenght being read")
	}
	if res[0] == 0xFD {
		return ReadVarUInt16(buffer, newOffset)
	} else if res[0] == 0xFE {
		return ReadVarUInt32(buffer, newOffset)
	} else if res[0] == 0xFF {
		return ReadVarUInt64(buffer, newOffset)
	} else {
		return uint64(res[0]), newOffset, nil
	}
}

func ReadVarUInt8(buffer []byte, offset int) (uint64, int, error) {
	if offset+1 > len(buffer) {
		return 0, 0, fmt.Errorf("invalid offset")
	}
	u := uint8(buffer[offset : offset+1][0])
	return uint64(u), offset + 1, nil
}

func ReadVarUInt16(buffer []byte, offset int) (uint64, int, error) {
	if offset+2 > len(buffer) {
		return 0, 0, fmt.Errorf("invalid offset")
	}
	return uint64(binary.LittleEndian.Uint16(buffer[offset : offset+2])), offset + 2, nil
}

func ReadVarUInt32(buffer []byte, offset int) (uint64, int, error) {
	if offset+4 > len(buffer) {
		return 0, 0, fmt.Errorf("invalid offset")
	}
	return uint64(binary.LittleEndian.Uint32(buffer[offset : offset+4])), offset + 4, nil
}

func ReadVarUInt64(buffer []byte, offset int) (uint64, int, error) {
	if offset+8 > len(buffer) {
		return 0, 0, fmt.Errorf("invalid offset")
	}
	return binary.LittleEndian.Uint64(buffer[offset : offset+8]), offset + 8, nil
}

func ReadUInt255(buffer []byte, offset int) (*big.Int, int, error) {
	if offset+32 > len(buffer) {
		return nil, 0, fmt.Errorf("invalid offset")
	}
	res := big.Int{}
	return res.SetBytes(buffer[offset : offset+32]), offset + 32, nil
}
