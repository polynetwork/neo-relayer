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
		return fmt.Errorf("Waiting deserialize height error")
	}
	key, eof := source.NextString()
	if eof {
		return fmt.Errorf("Waiting deserialize key error")
	}

	this.Height = height
	this.Key = key
	return nil
}
