package db
// db not used
import (
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"github.com/boltdb/bolt"
	"github.com/joeqian10/neo-gogogo/helper"
	"github.com/polynetwork/neo-relayer/log"
	"path"
	"strings"
	"sync"
)

const MAX_NUM = 1000

var (
	BKTCheck = []byte("Check")
	BKTRetry = []byte("Retry")
	BKTHeight = []byte("Height")

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

func (w *BoltDB) PutPolyHeight(height uint32) error {
	w.rwLock.Lock()
	defer w.rwLock.Unlock()

	raw := make([]byte, 4)
	binary.LittleEndian.PutUint32(raw, height)
	return w.db.Update(func(tx *bolt.Tx) error {
		bucket := tx.Bucket(BKTHeight)
		err := bucket.Put([]byte("poly"), raw)
		if err != nil {
			return err
		}

		return nil
	})
}

func (w *BoltDB) GetPolyHeight() uint32 {
	w.rwLock.RLock()
	defer w.rwLock.RUnlock()

	var height uint32
	_ = w.db.View(func(tx *bolt.Tx) error {
		bucket := tx.Bucket(BKTHeight)
		raw := bucket.Get([]byte("poly"))
		if len(raw) == 0 {
			height = 0
			return nil
		}
		height = binary.LittleEndian.Uint32(raw)
		return nil
	})

	return height
}

func (w *BoltDB) PutNeoHeight(height uint32) error {
	w.rwLock.Lock()
	defer w.rwLock.Unlock()

	raw := make([]byte, 4)
	binary.LittleEndian.PutUint32(raw, height)
	return w.db.Update(func(tx *bolt.Tx) error {
		bucket := tx.Bucket(BKTHeight)
		err := bucket.Put([]byte("neo"), raw)
		if err != nil {
			return err
		}

		return nil
	})
}

func (w *BoltDB) GetNeoHeight() uint32 {
	w.rwLock.RLock()
	defer w.rwLock.RUnlock()

	var height uint32
	_ = w.db.View(func(tx *bolt.Tx) error {
		bucket := tx.Bucket(BKTHeight)
		raw := bucket.Get([]byte("neo"))
		if len(raw) == 0 {
			height = 0
			return nil
		}
		height = binary.LittleEndian.Uint32(raw)
		return nil
	})

	return height
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

func (w *BoltDB) PutRetry(k []byte) error {
	w.rwLock.Lock()
	defer w.rwLock.Unlock()

	return w.db.Update(func(btx *bolt.Tx) error {
		bucket := btx.Bucket(BKTRetry)
		err := bucket.Put(k, []byte{0x00})
		if err != nil {
			return err
		}

		return nil
	})
}

func (w *BoltDB) DeleteRetry(k []byte) error {
	w.rwLock.Lock()
	defer w.rwLock.Unlock()

	return w.db.Update(func(tx *bolt.Tx) error {
		bucket := tx.Bucket(BKTRetry)
		err := bucket.Delete(k)
		if err != nil {
			return err
		}
		return nil
	})
}

func (w *BoltDB) GetAllCheck() (map[string][]byte, error) {
	w.rwLock.Lock()
	defer w.rwLock.Unlock()

	checkMap := make(map[string][]byte)
	err := w.db.Update(func(tx *bolt.Tx) error {
		bw := tx.Bucket(BKTCheck)
		err := bw.ForEach(func(k, v []byte) error {
			_k := make([]byte, len(k))
			_v := make([]byte, len(v))
			copy(_k, k)
			copy(_v, v)
			checkMap[hex.EncodeToString(_k)] = _v
			if len(checkMap) >= MAX_NUM {
				return fmt.Errorf("max num")
			}
			return nil
		})
		if err != nil {
			log.Errorf("GetAllCheck err: %s", err)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return checkMap, nil
}

func (w *BoltDB) GetAllRetry() ([][]byte, error) {
	w.rwLock.Lock()
	defer w.rwLock.Unlock()

	retryList := make([][]byte, 0)
	err := w.db.Update(func(tx *bolt.Tx) error {
		bw := tx.Bucket(BKTRetry)
		err := bw.ForEach(func(k, _ []byte) error {
			_k := make([]byte, len(k))
			copy(_k, k)
			retryList = append(retryList, _k)
			if len(retryList) >= MAX_NUM {
				return fmt.Errorf("max num")
			}
			return nil
		})
		if err != nil {
			log.Errorf("GetAllRetry err: %s", err)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return retryList, nil
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
