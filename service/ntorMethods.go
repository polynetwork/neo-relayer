package service

import (
	"encoding/hex"
	"fmt"
	"github.com/joeqian10/neo-gogogo/block"
	"github.com/joeqian10/neo-gogogo/helper"
	"github.com/joeqian10/neo-gogogo/helper/io"
	"github.com/polynetwork/neo-relayer/common"
	"github.com/polynetwork/neo-relayer/db"
	"github.com/polynetwork/neo-relayer/log"
	pCommon "github.com/polynetwork/poly/common"
	hsCommon "github.com/polynetwork/poly/native/service/header_sync/common"
	"github.com/polynetwork/poly/native/service/header_sync/neo"
	relayUtils "github.com/polynetwork/poly/native/service/utils"
	"strings"
	"time"
)

// GetCurrentRelayChainSyncHeight :get the synced NEO blockHeight from Relay Chain
func (this *SyncService) GetCurrentRelayChainSyncHeight(neoChainID uint64) (uint32, error) {
	contractAddress := relayUtils.HeaderSyncContractAddress
	neoChainIDBytes := common.GetUint64Bytes(neoChainID)
	key := common.ConcatKey([]byte(hsCommon.CONSENSUS_PEER), neoChainIDBytes)
	value, err := this.relaySdk.ClientMgr.GetStorage(contractAddress.ToHexString(), key)
	if err != nil {
		return 0, fmt.Errorf("getStorage error: %s", err)
	}
	neoConsensusPeer := new(neo.NeoConsensus)
	if err := neoConsensusPeer.Deserialization(pCommon.NewZeroCopySource(value)); err != nil {
		return 0, fmt.Errorf("neoconsensus peer deserialize err: %s", err)
	}

	height := neoConsensusPeer.Height
	height++
	return height, nil
}

//syncHeaderToRelay : Sync NEO block head to Relay Chain
func (this *SyncService) syncHeaderToRelay(height uint32) error {
	chainIDBytes := relayUtils.GetUint64Bytes(this.config.NeoChainID)
	heightBytes := relayUtils.GetUint32Bytes(height)
	v, err := this.relaySdk.GetStorage(relayUtils.HeaderSyncContractAddress.ToHexString(), common.ConcatKey([]byte(hsCommon.HEADER_INDEX), chainIDBytes, heightBytes))
	if len(v) != 0 {
		return nil
	}

	//Get NEO BlockHeader for syncing
	response := this.neoSdk.GetBlockHeaderByIndex(height)
	if response.HasError() {
		return fmt.Errorf("[syncHeaderToRelay] neoSdk.GetBlockByIndex error: %s", response.Error.Message)
	}
	rpcBH := response.Result
	blockHeader, err := block.NewBlockHeaderFromRPC(&rpcBH)
	if err != nil {
		return err
	}
	buff := io.NewBufBinaryWriter()
	blockHeader.Serialize(buff.BinaryWriter)
	header := buff.Bytes()
	log.Infof(helper.BytesToHex(header))

	var txHash pCommon.Uint256
	var txErr error
	//Sending transaction to Relay Chain
	txHash, txErr = this.relaySdk.Native.Hs.SyncBlockHeader(this.config.NeoChainID, this.relayAccount.Address, [][]byte{header}, this.relayAccount)

	if txErr != nil {
		return fmt.Errorf("[syncHeaderToRelay] relaySdk.SyncBlockHeader error: %s, neo header: %s", txErr, helper.BytesToHex(header))
	}
	log.Infof("[syncHeaderToRelay] polyTxHash is: %s", txHash.ToHexString())
	this.waitForRelayBlock()
	return nil
}

//syncProofToRelay : send StateRoot Proof to Relay Chain
func (this *SyncService) syncProofToRelay(key string, height uint32) error {
	retry := &db.Retry{
		Height: height,
		Key:    key,
	}
	sink := pCommon.NewZeroCopySink(nil)
	retry.Serialization(sink)

	//get current state height
	var stateHeight uint32 = 0
	for stateHeight < height {
		res := this.neoSdk.GetStateHeight()
		if res.HasError() {
			this.db.PutRetry(sink.Bytes())
			return fmt.Errorf("[syncProofToRelay] neoSdk.GetStateHeight error: %s", res.Error.Message)
		}
		stateHeight = res.Result.StateHeight
	}

	// get state root
	res2 := this.neoSdk.GetStateRootByIndex(height)
	if res2.HasError() {
		this.db.PutRetry(sink.Bytes())
		return fmt.Errorf("[syncProofToRelay] neoSdk.GetStateRootByIndex error: %s", res2.Error.Message)
	}
	stateRoot := res2.Result.StateRoot
	buff := io.NewBufBinaryWriter()
	stateRoot.Serialize(buff.BinaryWriter)
	crossChainMsg := buff.Bytes()
	//fmt.Printf("stateroot: %v", stateRoot)

	// get proof
	res3 := this.neoSdk.GetProof(stateRoot.StateRoot, "0x"+helper.ReverseString(this.config.NeoCCMC), key)
	if res3.HasError() {
		return fmt.Errorf("[syncProofToRelay] neoSdk.GetProof error: %s", res3.Error.Message)
	}
	proof, err := hex.DecodeString(res3.CrosschainProof.Proof)
	if err != nil {
		return fmt.Errorf("[syncProofToRelay] decode proof error: %s", err)
	}
	//log.Info(stateRoot.StateRoot, "0x"+helper.ReverseString(this.config.NeoCCMC), key)

	//sending SyncProof transaction to Relay Chain
	txHash, err := this.relaySdk.Native.Ccm.ImportOuterTransfer(this.config.NeoChainID, nil, height, proof, this.relayAccount.Address[:], crossChainMsg, this.relayAccount)
	if err != nil {
		if strings.Contains(err.Error(), "chooseUtxos, current utxo is not enough") {
			log.Infof("[syncProofToRelay] invokeNativeContract error: %s", err)

			err = this.db.PutRetry(sink.Bytes())
			if err != nil {
				return fmt.Errorf("[syncProofToRelay] this.db.PutRetry error: %s", err)
			}
			log.Infof("[syncProofToRelay] put tx into retry db, height %d, key %s", height, key)
			return nil
		} else if strings.Contains(err.Error(), "checkDoneTx, tx already done") {
			return fmt.Errorf("[syncProofToRelay] invokeNativeContract error: %s", err)
		} else {
			return fmt.Errorf("[syncProofToRelay] invokeNativeContract error: %s, crossChainMsg: %s, proof: %s", err, helper.BytesToHex(crossChainMsg), helper.BytesToHex(proof))
		}
	}

	err = this.db.PutCheck(txHash.ToHexString(), sink.Bytes())
	if err != nil {
		return fmt.Errorf("[syncProofToRelay] this.db.PutCheck error: %s", err)
	}

	log.Infof("[syncProofToRelay] polyTxHash is: %s", txHash.ToHexString())
	return nil
}

func (this *SyncService) retrySyncProofToRelay(v []byte) error {
	retry := new(db.Retry)
	err := retry.Deserialization(pCommon.NewZeroCopySource(v))
	if err != nil {
		return fmt.Errorf("[retrySyncProofToRelay] retry.Deserialization error: %s", err)
	}

	// get state root
	res2 := this.neoSdk.GetStateRootByIndex(retry.Height)
	if res2.HasError() {
		return fmt.Errorf("[retrySyncProofToRelay] neoSdk.GetStateRootByIndex error: %s", res2.Error.Message)
	}
	stateRoot := res2.Result.StateRoot
	buff := io.NewBufBinaryWriter()
	stateRoot.Serialize(buff.BinaryWriter)
	crossChainMsg := buff.Bytes()

	// get proof
	res3 := this.neoSdk.GetProof(stateRoot.StateRoot, "0x"+helper.ReverseString(this.config.NeoCCMC), retry.Key)
	if res3.HasError() {
		return fmt.Errorf("[retrySyncProofToRelay] neoSdk.GetProof error: %s", res3.Error.Message)
	}
	proof, err := hex.DecodeString(res3.CrosschainProof.Proof)
	if err != nil {
		return fmt.Errorf("[retrySyncProofToRelay] decode proof error: %s", err)
	}

	txHash, err := this.relaySdk.Native.Ccm.ImportOuterTransfer(this.config.NeoChainID, nil, retry.Height, proof, this.relayAccount.Address[:], crossChainMsg, this.relayAccount)
	if err != nil {
		if strings.Contains(err.Error(), "chooseUtxos, current utxo is not enough") {
			log.Infof("[retrySyncProofToRelay] invokeNativeContract error: %s", err)
			return nil
		} else {
			if err := this.db.DeleteRetry(v); err != nil {
				return fmt.Errorf("[retrySyncProofToRelay] this.db.DeleteRetry error: %s", err)
			}
			return fmt.Errorf("[retrySyncProofToRelay] invokeNativeContract error: %s", err)
		}
	}

	err = this.db.PutCheck(txHash.ToHexString(), v)
	if err != nil {
		return fmt.Errorf("[retrySyncProofToRelay] this.db.PutCheck error: %s", err)
	}
	err = this.db.DeleteRetry(v)
	if err != nil {
		return fmt.Errorf("[retrySyncProofToRelay] this.db.DeleteRetry error: %s", err)
	}

	log.Infof("[retrySyncProofToRelay] syncProofToAlia txHash is :", txHash.ToHexString())
	return nil
}

func (this *SyncService) waitForRelayBlock() {
	_, err := this.relaySdk.WaitForGenerateBlock(90*time.Second, 3)
	if err != nil {
		log.Errorf("[waitForRelayBlock] error: %s", err)
	}
}



func (this *SyncService) checkDoneTx() error {
	checkMap, err := this.db.GetAllCheck()
	if err != nil {
		return fmt.Errorf("[checkDoneTx] this.db.GetAllCheck error: %s", err)
	}
	for k, v := range checkMap {
		event, err := this.relaySdk.GetSmartContractEvent(k)
		if err != nil {
			return fmt.Errorf("[checkDoneTx] this.aliaSdk.GetSmartContractEvent error: %s", err)
		}
		if event == nil {
			log.Infof("[checkDoneTx] can not find event of hash %s", k)
			continue
		}
		if event.State != 1 {
			log.Infof("[checkDoneTx] state of tx %s is not success", k)
			err := this.db.PutRetry(v)
			if err != nil {
				log.Errorf("[checkDoneTx] this.db.PutRetry error:%s", err)
			}
		}
		err = this.db.DeleteCheck(k)
		if err != nil {
			log.Errorf("[checkDoneTx] this.db.DeleteCheck error:%s", err)
		}
	}

	return nil
}

func (this *SyncService) retryTx() error {
	retryList, err := this.db.GetAllRetry()
	if err != nil {
		return fmt.Errorf("[retryTx] this.db.GetAllRetry error: %s", err)
	}
	for _, v := range retryList {
		err = this.retrySyncProofToRelay(v)
		if err != nil {
			log.Errorf("[retryTx] this.retrySyncProofToRelay error:%s", err)
		}
		time.Sleep(time.Duration(this.config.RetryInterval) * time.Second)
	}

	return nil
}
