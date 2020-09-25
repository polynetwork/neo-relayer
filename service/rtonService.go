package service

import (
	"encoding/json"
	"fmt"
	"github.com/polynetwork/neo-relayer/log"
	vconfig "github.com/polynetwork/poly/consensus/vbft/config"
	autils "github.com/polynetwork/poly/native/service/utils"
	"time"
)

// RelayToNeo sync headers from relay chain to neo
func (this *SyncService) RelayToNeo() {
	this.neoSyncHeight = this.config.NeoSyncHeight
	for {
		currentRelayChainHeight, err := this.relaySdk.GetCurrentBlockHeight()
		if err != nil {
			log.Errorf("[RelayToNeo] GetCurrentBlockHeight error: ", err)
		}
		err = this.relayToNeo(this.neoSyncHeight, currentRelayChainHeight)
		if err != nil {
			log.Errorf("[RelayToNeo] relayToNeo error: ", err)
		}
		time.Sleep(time.Duration(this.config.ScanInterval) * time.Second)
	}
}

func (this *SyncService) relayToNeo(m, n uint32) error {
	for i := m; i < n; i++ {
		log.Infof("[relayToNeo] start parse block %d", i)

		// sync cross chain info
		events, err := this.relaySdk.GetSmartContractEventByBlock(i)
		if err != nil {
			return fmt.Errorf("[relayToNeo] relaySdk.GetSmartContractEventByBlock error:%s", err)
		}
		for _, event := range events {
			for _, notify := range event.Notify {
				states, ok := notify.States.([]interface{})
				if !ok {
					continue
				}
				if notify.ContractAddress != autils.CrossChainManagerContractAddress.ToHexString() { // relay chain CCMC
					continue
				}
				name := states[0].(string)
				if name == "makeProof" {
					toChainID := uint64(states[2].(float64))
					if toChainID == this.config.NeoChainID {
						key := states[5].(string)
						// get current neo chain sync height, which is the reliable header height
						currentNeoChainSyncHeight, err := this.GetCurrentNeoChainSyncHeight(this.relaySdk.ChainId)
						if err != nil {
							log.Errorf("[relayToNeo] GetCurrentNeoChainSyncHeight error: ", err)
						}
						err = this.syncProofToNeo(key, i, uint32(currentNeoChainSyncHeight))
						if err != nil {
							log.Errorf("--------------------------------------------------")
							log.Errorf("[relayToNeo] syncProofToNeo error: %s", err)
							log.Errorf("polyHeight: %d, key: %s", i, key)
							log.Errorf("--------------------------------------------------")
						}
					}
				}
			}
		}

		if this.config.ChangeBookkeeper {
			// sync key header, change book keeper
			// but should be done after all cross chain tx in this block are handled for verification purpose.
			block, err := this.relaySdk.GetBlockByHeight(i)
			if err != nil {
				return fmt.Errorf("[relayToNeo] GetBlockByHeight error: %s", err)
			}
			blkInfo := &vconfig.VbftBlockInfo{}
			if err := json.Unmarshal(block.Header.ConsensusPayload, blkInfo); err != nil {
				return fmt.Errorf("[relayToNeo] unmarshal blockInfo error: %s", err)
			}
			if blkInfo.NewChainConfig != nil {
				this.waitForNeoBlock() // wait for neo block
				err = this.changeBookKeeper(block)
				if err != nil {
					log.Errorf("--------------------------------------------------")
					log.Errorf("[relayToNeo] syncHeaderToNeo error: %s", err)
					log.Errorf("polyHeight: %d", i)
					log.Errorf("--------------------------------------------------")
				}
			}
		}

		this.neoSyncHeight++
	}
	return nil
}

func (this *SyncService) RelayToNeoRetry() {
	for {
		err := this.neoRetryTx()
		if err != nil {
			log.Errorf("[RelayToNeoRetry] this.neoRetryTx error:%s", err)
		}
		time.Sleep(time.Duration(this.config.ScanInterval) * time.Second)
	}
}
