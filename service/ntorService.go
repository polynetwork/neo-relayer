package service

import (
	"fmt"
	"github.com/joeqian10/neo-gogogo/helper"
	"github.com/polynetwork/neo-relayer/log"
	"time"
)

//NeoToRelay ...
func (this *SyncService) NeoToRelay() {
	this.relaySyncHeight = this.config.NeoStartHeight // means the next height to be synced
	if this.relaySyncHeight == 0 {                    // means no block header has been synced
		this.neoNextConsensus = ""
	} else {
		for j:= 0; j<5; j++{
			response := this.neoSdk.GetBlockByIndex(this.relaySyncHeight-1) // get the last synced height
			if response.HasError() {
				log.Errorf("[NeoToRelay] neoSdk.GetBlockByIndex error: %s", response.Error.Message)
			}
			block := response.Result
			if block.Hash == "" {
				if j== 4 {
					log.Errorf("[NeoToRelay] rpc request failed 5 times")
					break
				}
				continue
			}
			this.neoNextConsensus = block.NextConsensus // set the next consensus to the last synced block
			break
		}
	}
	for {
		//get current Neo BlockHeight, 5 times rpc
		var currentNeoHeight uint32
		for j := 0; j < 5; j++ {
			response := this.neoSdk.GetBlockCount()
			if response.HasError() {
				log.Errorf("[NeoToRelay] neoSdk.GetBlockCount error: ", response.Error.Message)
				break
			}
			if response.Result == 0 {
				if j == 4 {
					log.Errorf("[NeoToRelay] rpc request failed 5 times")
					currentNeoHeight = this.relaySyncHeight // prevent infinite loop
					break
				}
				continue
			}
			currentNeoHeight = uint32(response.Result - 1)
			break
		}
		err := this.neoToRelay(this.relaySyncHeight, currentNeoHeight)
		if err != nil {
			log.Errorf("[NeoToRelay] neoToRelay error:", err)
		}
		time.Sleep(time.Duration(this.config.ScanInterval) * time.Second)
	}
}

func (this *SyncService) neoToRelay(m, n uint32) error {
	for i := m; i < n; i++ {
		log.Infof("[neoToRelay] start processing NEO block %d", this.relaySyncHeight)
		// request block from NEO, try rpc request 5 times, if failed, continue
		for j := 0; j < 5; j++ {
			response := this.neoSdk.GetBlockByIndex(i)
			if response.HasError() {
				return fmt.Errorf("[neoToRelay] neoSdk.GetBlockByIndex error: %s", response.Error.Message)
			}
			blk := response.Result
			if blk.Hash == "" {
				if j == 4 {
					log.Errorf("[neoToRelay] rpc request failed 5 times")
					break
				}
				continue
			}

			// sync cross chain transaction
			// check if this block contains cross chain tx
			txs := blk.Tx
			for _, tx := range txs {
				if tx.Type != "InvocationTransaction" {
					continue
				}
				response := this.neoSdk.GetApplicationLog(tx.Txid)
				if response.HasError() {
					return fmt.Errorf("[neoToRelay] neoSdk.GetApplicationLog error: %s", response.Error.Message)
				}

				for _, execution := range response.Result.Executions {
					if execution.VMState == "FAULT" {
						continue
					}
					notifications := execution.Notifications
					for _, notification := range execution.Notifications {
						u, _ := helper.UInt160FromString(notification.Contract)
						// outer loop confirm tx is a cross chain tx
						if helper.BytesToHex(u.Bytes()) == this.config.NeoCCMC {
							if notification.State.Type != "Array" {
								return fmt.Errorf("[neoToRelay] notification.State.Type error: Type is not Array")
							}
							states := notification.State.Value // []models.RpcContractParameter
							if states[0].Value != "43726f7373436861696e4c6f636b4576656e74" { // "CrossChainLockEvent"
								continue
							}
							if len(states) != 6 {
								return fmt.Errorf("[neoToRelay] notification.State.Value error: Wrong length of states")
							}

							if this.config.SpecificContract != "" { // when empty, relay everything
								for index, ntf := range notifications {
									// inner loop check it is for this specific contract
									v, _ := helper.UInt160FromString(ntf.Contract)
									if helper.BytesToHex(v.Bytes()) != this.config.SpecificContract {
										if index < len(notifications)-1 {
											continue
										}
										log.Infof("This cross chain tx is not for this specific contract.")
										goto NEXT
									} else {
										break
									}
								}
							}
							key := states[4].Value // hexstring for storeKey: 0102 + toChainId + toRequestId, like 01020501
							//get relay chain sync height
							currentRelayChainSyncHeight, err := this.GetCurrentRelayChainSyncHeight(this.config.NeoChainID)
							if err != nil {
								return fmt.Errorf("[neoToRelay] GetCurrentMainChainSyncHeight error: %s", err)
							}
							var passed uint32
							if i >= currentRelayChainSyncHeight {
								passed = i
							} else {
								passed = currentRelayChainSyncHeight
							}
							err = this.syncProofToRelay(key, passed)
							if err != nil {
								log.Errorf("--------------------------------------------------")
								log.Errorf("[neoToRelay] syncProofToRelay error: %s", err)
								log.Errorf("neoHeight: %d, neoTxId: %s", i, tx.Txid)
								log.Errorf("--------------------------------------------------")
							}
						}
					NEXT:
					} // notification
				} // execution
			}

			// if block.nextConsensus is changed, sync key header of NEO,
			// but should be done after all cross chain tx in this block are handled for verification purpose.
			if blk.NextConsensus != this.neoNextConsensus {
				log.Infof("[neoToRelay] Syncing Key blockHeader from NEO: %d", blk.Index)
				// Syncing key blockHeader to Relay Chain
				err := this.syncHeaderToRelay(this.relaySyncHeight)
				if err != nil {
					log.Errorf("--------------------------------------------------")
					log.Errorf("[neoToRelay] syncHeaderToRelay error: %s", err)
					log.Errorf("height: %d", i)
					log.Errorf("--------------------------------------------------")
				}
				this.neoNextConsensus = blk.NextConsensus
			}

			this.relaySyncHeight++
			break
		}
	}
	return nil
}

func (this *SyncService) NeoToRelayCheckAndRetry() {
	for {
		err := this.checkDoneTx()
		if err != nil {
			log.Errorf("[NeoToRelayCheckAndRetry] this.checkDoneTx error:%s", err)
		}
		err = this.retryTx()
		if err != nil {
			log.Errorf("[NeoToRelayCheckAndRetry] this.retryTx error:%s", err)
		}
		time.Sleep(time.Duration(this.config.ScanInterval) * time.Second)
	}
}
