package db

import (
	"fmt"
	"github.com/polynetwork/poly/common"
)

type Retry struct {
	Height uint32
	Key    string
}

func (this *Retry) Serialization(sink *common.ZeroCopySink) {
	sink.WriteUint32(this.Height)
	sink.WriteString(this.Key)
}

func (this *Retry) Deserialization(source *common.ZeroCopySource) error {
	height, eof := source.NextUint32()
	if eof {
		return fmt.Errorf("waiting deserialize height error")
	}
	key, eof := source.NextString()
	if eof {
		return fmt.Errorf("waiting deserialize key error")
	}

	this.Height = height
	this.Key = key
	return nil
}

type NeoUtxo struct {
	TxId string
	Index int
}

func (this *NeoUtxo) Serialization(sink *common.ZeroCopySink)  {
	sink.WriteString(this.TxId)
	sink.WriteInt32(int32(this.Index))
}

func (this *NeoUtxo) Deserialization(source *common.ZeroCopySource) error {
	txId, eof := source.NextString()
	if eof {
		return fmt.Errorf("waiting deserialize txid error")
	}

	index, eof := source.NextInt32()
	if eof {
		return fmt.Errorf("waiting deserialize index error")
	}

	this.TxId = txId
	this.Index = int(index)
	return nil
}
