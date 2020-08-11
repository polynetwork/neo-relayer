package db
// db not used
import (
	"encoding/hex"
	"fmt"
	"github.com/boltdb/bolt"
	"github.com/joeqian10/neo-gogogo/helper"
	"path"
	"strings"
	"sync"
)

const MAX_NUM = 1000

var (
	BKTCheck = []byte("Check")
	BKTRetry = []byte("Retry")

	BKTHeader = []byte("Header") // bucket header
	BKTHeightList = []byte("HeightList") // bucket header height list
)

type BoltDB struct {
	rwLock *sync.RWMutex
	db *bolt.DB
	filePath string
}

func NewBoltDB(filePath string) (*BoltDB, error) {
	if !strings.Contains(filePath, ".bin") {
		filePath = path.Join(filePath, "bolt.bin")
	}
	w := new(BoltDB)
	db, err := bolt.Open(filePath, 0644, &bolt.Options{InitialMmapSize: 500000})
	if err != nil {
		return nil, err
	}
	w.db = db
	w.rwLock = new(sync.RWMutex)
	w.filePath = filePath

	if err = db.Update(func(btx *bolt.Tx) error {
		_, err := btx.CreateBucketIfNotExists(BKTCheck)
		if err != nil {
			return err
		}

		return nil
	}); err != nil {
		return nil, err
	}

	if err = db.Update(func(btx *bolt.Tx) error {
		_, err := btx.CreateBucketIfNotExists(BKTRetry)
		if err != nil {
			return err
		}

		return nil
	}); err != nil {
		return nil, err
	}

	if err = db.Update(func(btx *bolt.Tx) error {
		_, err := btx.CreateBucketIfNotExists(BKTHeader)
		if err != nil {
			return err
		}

		return nil
	}); err != nil {
		return nil, err
	}

	if err = db.Update(func(btx *bolt.Tx) error {
		_, err := btx.CreateBucketIfNotExists(BKTHeightList)
		if err != nil {
			return err
		}

		return nil
	}); err != nil {
		return nil, err
	}

	return w, nil
}

func (w *BoltDB) PutCheck(txHash string, v []byte) error {
	w.rwLock.Lock()
	defer w.rwLock.Unlock()

	k, err := hex.DecodeString(txHash)
	if err != nil {
		return err
	}
	return w.db.Update(func(btx *bolt.Tx) error {
		bucket := btx.Bucket(BKTCheck)
		err := bucket.Put(k, v)
		if err != nil {
			return err
		}

		return nil
	})
}

func (w *BoltDB) DeleteCheck(txHash string) error {
	w.rwLock.Lock()
	defer w.rwLock.Unlock()

	k, err := hex.DecodeString(txHash)
	if err != nil {
		return err
	}
	return w.db.Update(func(tx *bolt.Tx) error {
		bucket := tx.Bucket(BKTCheck)
		err := bucket.Delete(k)
		if err != nil {
			return err
		}
		return nil
	})
}

func (w *BoltDB) PutHeader(height uint32, rawHeader []byte) error {
	w.rwLock.Lock()
	defer w.rwLock.Unlock()

	key := helper.UInt32ToBytes(height)
	// check if exists
	var v []byte = nil
	err := w.db.View(func(tx *bolt.Tx) error {
		_v := tx.Bucket(BKTHeader).Get(key)
		v = make([]byte, len(_v))
		copy(v, _v)
		return nil
	})
	if err != nil {
		return err
	}
	if v == nil {
		return nil
	}

	return w.db.Update(func(btx *bolt.Tx) error {
		bucket := btx.Bucket(BKTHeader)
		err := bucket.Put(key, rawHeader)
		if err != nil {
			return err
		}

		return nil
	})
}

func (w *BoltDB) GetHeader(height uint32) ([]byte, error) {
	w.rwLock.Lock()
	defer w.rwLock.Unlock()

	key := helper.UInt32ToBytes(height)
	var v []byte = nil
	err := w.db.View(func(tx *bolt.Tx) error {
		_v := tx.Bucket(BKTHeader).Get(key)
		v = make([]byte, len(_v))
		copy(v, _v)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return v, nil
}

func (w *BoltDB) GetHeadersByRange(low uint32, high uint32) (map[uint32][]byte, error) {
	if low > high {
		return nil, fmt.Errorf("invalid parameters")
	}

	heightList, err := w.GetHeightList()
	if err != nil {
		return nil,err
	}
	if heightList == nil {
		return nil, nil
	}

	var i = 0
	var j = len(heightList)-1
	for heightList[i] <= low { // exclude low
		i++
	}
	for heightList[j] > high { // include high
		j--
	}
	if i > j {
		return nil, nil
	}

	w.rwLock.Lock()
	defer w.rwLock.Unlock()

	var headersMap = make(map[uint32][]byte)
	for k := i; k <= j; k++ {
		key := helper.UInt32ToBytes(heightList[k])
		var v []byte = nil
		err := w.db.View(func(tx *bolt.Tx) error {
			_v := tx.Bucket(BKTHeader).Get(key)
			v = make([]byte, len(_v))
			copy(v, _v)
			return nil
		})
		if err != nil {
			return nil, err
		}
		headersMap[heightList[k]] = v
	}

	return headersMap, nil
}

func (w *BoltDB) PutHeightList(heights []uint32) error {
	w.rwLock.Lock()
	defer w.rwLock.Unlock()
	
	var v []byte
	for _, height := range heights {
		v = append(v, helper.UInt32ToBytes(height)...)
	}

	return w.db.Update(func(btx *bolt.Tx) error {
		bucket := btx.Bucket(BKTHeightList)
		err := bucket.Put(BKTHeightList, v)
		if err != nil {
			return err
		}

		return nil
	})
}

func (w *BoltDB) GetHeightList() ([]uint32, error) {
	w.rwLock.Lock()
	defer w.rwLock.Unlock()
	
	var v []byte
	err := w.db.View(func(tx *bolt.Tx) error {
		_v := tx.Bucket(BKTHeightList).Get(BKTHeightList)
		v = make([]byte, len(_v))
		copy(v, _v)
		return nil
	})
	if err != nil {
		return nil, err
	}
	
	results := make([]uint32, len(v)/4)
	for i := 0; i < len(results); i++ {
		results[i] = helper.BytesToUInt32(v[4*i:4*i+4])
	}
	return results, nil
}

func (w *BoltDB) PutValueInHeightList(height uint32) error {
	heightList, err := w.GetHeightList()
	if err != nil {
		return err
	}
	
	newHeightList := make([]uint32, len(heightList)+1)
	// find the position to insert
	i := 0
	for height < heightList[i] {
		i++
	}
	copy(newHeightList[:i], heightList[:i])
	newHeightList[i] = height
	copy(newHeightList[i+1:], heightList[i:])
	
	err = w.PutHeightList(newHeightList)
	if err != nil {
		return err
	}
	return nil
}

func (w *BoltDB) Close() {
	w.rwLock.Lock()
	w.db.Close()
	w.rwLock.Unlock()
}
