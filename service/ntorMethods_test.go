package service

import (
	"github.com/joeqian10/neo-gogogo/rpc"
	"github.com/polynetwork/neo-relayer/log"
	"testing"
)

func TestNewSyncService(t *testing.T) {

}

func TestChangeConsensus(t *testing.T) {
	url := "http://seed10.ngd.network:21332"
	client := rpc.NewClient(url)
	currentConsensus := "AbmNLjoW4Sg4SuYweUf9MyuAwwygqD91rb"
	for i:=1061056; i< 1061156; i+=1 {
		log.Infof("current height: %d", i)
		response := client.GetBlockHeaderByIndex(uint32(i))
		blk := response.Result
		if blk.NextConsensus != currentConsensus {

			currentConsensus = blk.NextConsensus
		}
	}
}

func TestChangeConsensus10000(t *testing.T) {
	url := "http://seed10.ngd.network:21332"
	client := rpc.NewClient(url)
	currentConsensus := "AarH7d8Zg92UKVTVNUXR4YDagVj4rEmpFZ"
	for i:=1590000; i< 5000000; i+=10000 {
		log.Infof("current height: %d", i)
		response := client.GetBlockHeaderByIndex(uint32(i))
		blk := response.Result
		if blk.NextConsensus != currentConsensus {
			ChangeConsensus1000(i-10000, i, currentConsensus)

			currentConsensus = blk.NextConsensus
		}
	}
}

func ChangeConsensus1000(from, to int, c string) {
	url := "http://seed10.ngd.network:21332"
	client := rpc.NewClient(url)
	currentConsensus := c
	for i:=from; i< to; i+=1000 {
		log.Infof("current height: %d", i)
		response := client.GetBlockHeaderByIndex(uint32(i))
		blk := response.Result
		if blk.NextConsensus != currentConsensus {
			ChangeConsensus100(i-1000, i, currentConsensus)
			break
		}
	}
}

func ChangeConsensus100(from, to int, c string) {
	url := "http://seed10.ngd.network:21332"
	client := rpc.NewClient(url)
	currentConsensus := c
	for i:=from; i< to; i+=100 {
		log.Infof("current height: %d", i)
		response := client.GetBlockHeaderByIndex(uint32(i))
		blk := response.Result
		if blk.NextConsensus != currentConsensus {
			ChangeConsensus10(i-100, i, currentConsensus)
			break
		}
	}
}

func ChangeConsensus10(from, to int, c string) {
	url := "http://seed10.ngd.network:21332"
	client := rpc.NewClient(url)
	currentConsensus := c
	for i:=from; i< to; i+=10 {
		log.Infof("current height: %d", i)
		response := client.GetBlockHeaderByIndex(uint32(i))
		blk := response.Result
		if blk.NextConsensus != currentConsensus {
			ChangeConsensus1(i-10, i, currentConsensus)
			break
		}
	}
}

func ChangeConsensus1(from, to int, c string) {
	url := "http://seed10.ngd.network:21332"
	client := rpc.NewClient(url)
	currentConsensus := c
	for i:=from; i< to; i+=1 {
		log.Infof("current height: %d", i)
		response := client.GetBlockHeaderByIndex(uint32(i))
		blk := response.Result
		if blk.NextConsensus != currentConsensus {
			log.Infof("---------------------------------------------------------------height: %d", i)
			break
		}
	}
}
