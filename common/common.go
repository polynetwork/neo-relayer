package common

import (
	"encoding/binary"
	"fmt"
	"github.com/polynetwork/poly/common/password"

	rsdk "github.com/polynetwork/poly-go-sdk"
)

func GetAccountByPassword(sdk *rsdk.PolySdk, path, pwdStr string) (*rsdk.Account, bool) {
	wallet, err := sdk.OpenWallet(path)
	if err != nil {
		fmt.Println("open wallet error:", err)
		return nil, false
	}
	var pwd []byte
	if pwdStr != "" {
		pwd = []byte(pwdStr)
	} else {
		pwd, err = password.GetAccountPassword()
		if err != nil {
			fmt.Println("GetAccountPassword error: ", err)
			return nil, false
		}
	}
	user, err := wallet.GetDefaultAccount(pwd)
	if err != nil {
		fmt.Println("GetDefaultAccount error:", err)
		return nil, false
	}
	return user, true
}

func ConcatKey(args ...[]byte) []byte {
	temp := []byte{}
	for _, arg := range args {
		temp = append(temp, arg...)
	}
	return temp
}

func GetUint32Bytes(num uint32) []byte {
	var p [4]byte
	binary.LittleEndian.PutUint32(p[:], num)
	return p[:]
}

func GetBytesUint32(b []byte) uint32 {
	if len(b) != 4 {
		return 0
	}
	return binary.LittleEndian.Uint32(b[:])
}

func GetBytesUint64(b []byte) uint64 {
	if len(b) != 8 {
		return 0
	}
	return binary.LittleEndian.Uint64(b[:])
}

func GetUint64Bytes(num uint64) []byte {
	var p [8]byte
	binary.LittleEndian.PutUint64(p[:], num)
	return p[:]
}
