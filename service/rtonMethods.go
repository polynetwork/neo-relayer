package service

import (
	"bytes"
	"crypto/elliptic"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/btcsuite/btcd/btcec"
	"github.com/joeqian10/neo-gogogo/helper"
	"github.com/joeqian10/neo-gogogo/rpc/models"
	"github.com/joeqian10/neo-gogogo/sc"
	"github.com/joeqian10/neo-gogogo/tx"
	"github.com/ontio/ontology-crypto/ec"
	"github.com/ontio/ontology-crypto/keypair"
	"github.com/ontio/ontology-crypto/signature"
	"github.com/ontio/ontology-crypto/sm2"
	"github.com/polynetwork/neo-relayer/db"
	"github.com/polynetwork/neo-relayer/log"
	"github.com/polynetwork/poly/common"
	"github.com/polynetwork/poly/core/types"
	"sort"
	"strconv"
	"strings"
	"time"

	vconfig "github.com/polynetwork/poly/consensus/vbft/config"
)

const (
	VERIFY_AND_EXECUTE_TX = "VerifyAndExecuteTx"
	GET_CURRENT_HEIGHT    = "currentSyncHeight"
	CHANGE_BOOK_KEEPER    = "ChangeBookKeeper"
	SYNC_BLOCK_HEADER     = "SyncBlockHeader"
)

// GetCurrentNeoChainSyncHeight
func (this *SyncService) GetCurrentNeoChainSyncHeight(relayChainID uint64) (uint64, error) {
	arg := models.NewInvokeFunctionStackArg("Integer", fmt.Sprint(relayChainID))
	response := this.neoSdk.InvokeFunction("0x"+helper.ReverseString(this.config.NeoCCMC), GET_CURRENT_HEIGHT, helper.ZeroScriptHashString, arg)
	if response.HasError() || response.Result.State == "FAULT" {
		return 0, fmt.Errorf("[GetCurrentNeoChainSyncHeight] GetCurrentHeight error: %s", "Engine faulted! "+response.Error.Message)
	}

	var height uint64
	var e error
	var b []byte
	s := response.Result.Stack
	if s != nil && len(s) != 0 {
		b = helper.HexToBytes(s[0].Value)
	}
	if len(b) == 0 {
		height = 0
	} else {
		height = helper.BytesToUInt64(b)
		if e != nil {
			return 0, fmt.Errorf("[GetCurrentNeoChainSyncHeight], ParseVarInt error: %s", e)
		}
		height++ // means the next block header needs to be synced
	}
	return height, nil
}

func (this *SyncService) changeBookKeeper(block *types.Block) error {
	headerBytes := block.Header.GetMessage()
	// raw header
	cp1 := sc.ContractParameter{
		Type:  sc.ByteArray,
		Value: headerBytes,
	}
	log.Infof("raw header: %s", helper.BytesToHex(headerBytes))

	// public keys
	bs := []byte{}
	blkInfo := &vconfig.VbftBlockInfo{}
	_ = json.Unmarshal(block.Header.ConsensusPayload, blkInfo) // already checked before
	if blkInfo.NewChainConfig != nil {
		var bookkeepers []keypair.PublicKey
		for _, peer := range blkInfo.NewChainConfig.Peers {
			keyBytes, _ := hex.DecodeString(peer.ID)
			key, _ := keypair.DeserializePublicKey(keyBytes) // compressed
			bookkeepers = append(bookkeepers, key)
		}
		bookkeepers = keypair.SortPublicKeys(bookkeepers)
		for _, key := range bookkeepers {
			uncompressed := getRelayUncompressedKey(key)
			bs = append(bs, uncompressed...)
		}
	}
	cp2 := sc.ContractParameter{
		Type:  sc.ByteArray,
		Value: bs,
	}
	log.Infof("pub keys: %s", helper.BytesToHex(bs))

	// signatures
	bs2 := []byte{}
	for _, sig := range block.Header.SigData {
		newSig, _ := signature.ConvertToEthCompatible(sig) // convert to eth
		bs2 = append(bs2, newSig...)
	}
	cp3 := sc.ContractParameter{
		Type:  sc.ByteArray,
		Value: bs2,
	}
	log.Infof("signature: %s", helper.BytesToHex(bs2))

	// build script
	sb := sc.NewScriptBuilder()
	scriptHash := helper.HexToBytes(this.config.NeoCCMC) // hex string to little endian byte[]
	sb.MakeInvocationScript(scriptHash, CHANGE_BOOK_KEEPER, []sc.ContractParameter{cp1, cp2, cp3})

	script := sb.ToArray()

	tb := tx.NewTransactionBuilder(this.config.NeoJsonRpcUrl)
	from, err := helper.AddressToScriptHash(this.neoAccount.Address)
	// create an InvocationTransaction
	sysFee := helper.Fixed8FromFloat64(this.config.NeoSysFee)
	netFee := helper.Fixed8FromFloat64(this.config.NeoNetFee)
	itx, err := tb.MakeInvocationTransaction(script, from, nil, from, sysFee, netFee)
	if err != nil {
		return fmt.Errorf("[changeBookKeeper] tb.MakeInvocationTransaction error: %s", err)
	}
	// sign transaction
	err = tx.AddSignature(itx, this.neoAccount.KeyPair)
	if err != nil {
		return fmt.Errorf("[changeBookKeeper] tx.AddSignature error: %s", err)
	}

	rawTxString := itx.RawTransactionString()
	log.Infof(rawTxString)
	// send the raw transaction
	response := this.neoSdk.SendRawTransaction(rawTxString)
	if response.HasError() {
		return fmt.Errorf("[changeBookKeeper] SendRawTransaction error: %s, "+
			"unsigned header hex string: %s, "+
			"public keys hex string: %s, "+
			"signatures hex string: %s"+
			"script hex string: %s, "+
			"changeBookKeeper RawTransactionString: %s",
			response.ErrorResponse.Error.Message,
			helper.BytesToHex(headerBytes),
			helper.BytesToHex(bs),
			helper.BytesToHex(bs2),
			helper.BytesToHex(script),
			rawTxString)
	}

	log.Infof("[changeBookKeeper] txHash is: %s", itx.HashString())
	this.waitForNeoBlock()
	return nil
}

// syncHeaderToNeo
func (this *SyncService) syncHeaderToNeo(height uint32) error {
	block, err := this.relaySdk.GetBlockByHeight(height)
	if err != nil {
		return fmt.Errorf("[syncHeaderToNeo] GetBlockByHeight error: %s", err)
	}

	sink := common.NewZeroCopySink(nil)
	block.Header.Serialization(sink)
	// raw header
	cp1 := sc.ContractParameter{
		Type:  sc.ByteArray,
		Value: sink.Bytes(),
	}

	// public keys
	bs1 := []byte{}
	cp2 := sc.ContractParameter{
		Type:  sc.ByteArray,
		Value: bs1,
	}

	// signatures
	bs2 := []byte{}
	for _, sig := range block.Header.SigData {
		newSig, _ := signature.ConvertToEthCompatible(sig) // convert to eth
		bs2 = append(bs2, newSig...)
	}
	cp3 := sc.ContractParameter{
		Type:  sc.ByteArray,
		Value: bs2,
	}

	// build script
	sb := sc.NewScriptBuilder()
	scriptHash := helper.HexToBytes(this.config.NeoCCMC) // hex string to little endian byte[]
	sb.MakeInvocationScript(scriptHash, SYNC_BLOCK_HEADER, []sc.ContractParameter{cp1, cp2, cp3})

	script := sb.ToArray()

	tb := tx.NewTransactionBuilder(this.config.NeoJsonRpcUrl)
	from, err := helper.AddressToScriptHash(this.neoAccount.Address)
	// create an InvocationTransaction
	sysFee := helper.Fixed8FromFloat64(this.config.NeoSysFee)
	netFee := helper.Fixed8FromFloat64(this.config.NeoNetFee)
	itx, err := tb.MakeInvocationTransaction(script, from, nil, from, sysFee, netFee)
	if err != nil {
		return fmt.Errorf("[syncHeaderToNeo] tb.MakeInvocationTransaction error: %s", err)
	}

	// sign transaction
	err = tx.AddSignature(itx, this.neoAccount.KeyPair)
	if err != nil {
		return fmt.Errorf("[syncHeaderToNeo] tx.AddSignature error: %s", err)
	}

	rawTxString := itx.RawTransactionString()

	// send the raw transaction
	response := this.neoSdk.SendRawTransaction(rawTxString)
	if response.HasError() {
		return fmt.Errorf("[syncHeaderToNeo] SendRawTransaction error: %s, "+
			"unsigned header hex string: %s, "+
			"public keys hex string: %s, "+
			"signatures hex string: %s"+
			"script hex string: %s, "+
			"syncHeaderToNeo RawTransactionString: %s",
			response.ErrorResponse.Error.Message,
			helper.BytesToHex(sink.Bytes()),
			helper.BytesToHex(bs1),
			helper.BytesToHex(bs2),
			helper.BytesToHex(script),
			rawTxString)
	}

	log.Infof("[syncHeaderToNeo] txHash is: %s", itx.HashString())
	this.waitForNeoBlock()
	return nil
}

func (this *SyncService) syncProofToNeo(key string, txHeight, lastSynced uint32) error {
	blockHeightReliable := lastSynced + 1
	// get the proof of the cross chain tx
	crossStateProof, err := this.relaySdk.ClientMgr.GetCrossStatesProof(txHeight, key)
	if err != nil {
		return fmt.Errorf("[syncProofToNeo] GetCrossStatesProof error: %s", err)
	}
	path, err := hex.DecodeString(crossStateProof.AuditPath)
	if err != nil {
		return fmt.Errorf("[syncProofToNeo] DecodeString error: %s", err)
	}
	txProof := sc.ContractParameter{
		Type:  sc.ByteArray,
		Value: path,
	}
	log.Infof("txProof: " + helper.BytesToHex(path))

	// get the next block header since it has the stateroot for the cross chain tx
	blockHeightToBeVerified := txHeight + 1
	headerToBeVerified, err := this.relaySdk.GetHeaderByHeight(blockHeightToBeVerified)
	if err != nil {
		return fmt.Errorf("[syncProofToNeo] GetHeaderByHeight error: %s", err)
	}
	txProofHeader := sc.ContractParameter{
		Type:  sc.ByteArray,
		Value: headerToBeVerified.GetMessage(),
	}
	log.Infof("txProofHeader: " + helper.BytesToHex(headerToBeVerified.GetMessage()))

	// check constraints
	if this.config.SpecificContract != "" { // if empty, relay everything
		stateRootValue, err := MerkleProve(path, headerToBeVerified.CrossStateRoot.ToArray())
		if err != nil {
			return fmt.Errorf("[syncProofToNeo] MerkleProve error: %s", err)
		}
		toMerkleValue, err := DeserializeMerkleValue(stateRootValue)

		if err != nil {
			return fmt.Errorf("[syncProofToNeo] DeserializeMerkleValue error: %s", err)
		}
		if helper.BytesToHex(toMerkleValue.TxParam.ToContract) != this.config.SpecificContract {
			log.Infof(helper.BytesToHex(toMerkleValue.TxParam.ToContract))
			log.Infof("This cross chain tx is not for this specific contract.")
			return nil
		}
	}

	var headerProofBytes []byte
	var currentHeaderBytes []byte
	var signListBytes []byte
	if txHeight >= lastSynced {
		// cross chain tx is in current epoch, no need for headerProof and currentHeader
		headerProofBytes = []byte{}
		currentHeaderBytes = []byte{}
		// the signList should be the signature of the header at txHeight + 1
		signListBytes = []byte{}
		for _, sig := range headerToBeVerified.SigData {
			newSig, _ := signature.ConvertToEthCompatible(sig) // convert to eth
			signListBytes = append(signListBytes, newSig...)
		}
	} else {
		// txHeight < lastSynced, so blockHeightToBeVerified < blockHeightReliable
		// get the merkle proof of the block containing the stateroot
		merkleProof, err := this.relaySdk.GetMerkleProof(blockHeightToBeVerified, blockHeightReliable)
		if err != nil {
			return fmt.Errorf("[syncProofToNeo] GetMerkleProof error: %s", err)
		}
		headerProofBytes, err = hex.DecodeString(merkleProof.AuditPath)
		if err != nil {
			return fmt.Errorf("[syncProofToNeo] merkleProof DecodeString error: %s", err)
		}

		// get the raw current header
		headerReliable, err := this.relaySdk.GetHeaderByHeight(blockHeightReliable)
		if err != nil {
			return fmt.Errorf("[syncProofToNeo] GetHeaderByHeight error: %s", err)
		}
		currentHeaderBytes = headerReliable.GetMessage()

		// get the sign list of the current header
		signListBytes = []byte{}
		for _, sig := range headerReliable.SigData {
			newSig, _ := signature.ConvertToEthCompatible(sig) // convert to eth
			signListBytes = append(signListBytes, newSig...)
		}
	}

	headerProof := sc.ContractParameter{
		Type:  sc.ByteArray,
		Value: headerProofBytes,
	}
	log.Infof("headerProof: " + helper.BytesToHex(headerProofBytes))

	currentHeader := sc.ContractParameter{
		Type:  sc.ByteArray,
		Value: currentHeaderBytes,
	}
	log.Infof("currentHeader: " + helper.BytesToHex(currentHeaderBytes))

	signList := sc.ContractParameter{
		Type:  sc.ByteArray,
		Value: signListBytes,
	}
	log.Infof("signList: " + helper.BytesToHex(signListBytes))

	stateRootValue, err := MerkleProve(path, headerToBeVerified.CrossStateRoot.ToArray())
	if err != nil {
		return fmt.Errorf("[syncProofToNeo] MerkleProve error: %s", err)
	}
	toMerkleValue, err := DeserializeMerkleValue(stateRootValue)
	if err != nil {
		return fmt.Errorf("[syncProofToNeo] DeserializeMerkleValue error: %s", err)
	}
	log.Infof("fromChainId: " + strconv.Itoa(int(toMerkleValue.FromChainID)))
	log.Infof("polyTxHash: " + helper.BytesToHex(toMerkleValue.TxHash))
	log.Infof("fromContract: " + helper.BytesToHex(toMerkleValue.TxParam.FromContract))
	log.Infof("toChainId: " + strconv.Itoa(int(toMerkleValue.TxParam.ToChainID)))
	log.Infof("sourceTxHash: " + helper.BytesToHex(toMerkleValue.TxParam.TxHash))
	log.Infof("toContract: " + helper.BytesToHex(toMerkleValue.TxParam.ToContract))
	log.Infof("method: " + helper.BytesToHex(toMerkleValue.TxParam.Method))
	//log.Infof("TxParamArgs: " + helper.BytesToHex(toMerkleValue.TxParam.Args))
	toAssetHash, toAddress, amount, err := DeserializeArgs(toMerkleValue.TxParam.Args)
	if err != nil {
		return fmt.Errorf("[syncProofToNeo] DeserializeArgs error: %s", err)
	}
	log.Infof("toAssetHash: " + helper.BytesToHex(toAssetHash))
	log.Infof("toAddress: " + helper.BytesToHex(toAddress))
	log.Infof("amount: " + amount.String())

	// build script
	scriptBuilder := sc.NewScriptBuilder()
	scriptHash := helper.HexToBytes(this.config.NeoCCMC) // hex string to little endian byte[]

	args := []sc.ContractParameter{txProof, txProofHeader, headerProof, currentHeader, signList}
	scriptBuilder.MakeInvocationScript(scriptHash, VERIFY_AND_EXECUTE_TX, args)
	script := scriptBuilder.ToArray()
	log.Infof("script: " + helper.BytesToHex(script))

	//tb := tx.NewTransactionBuilder(this.config.NeoJsonRpcUrl)
	from, err := helper.AddressToScriptHash(this.neoAccount.Address)
	log.Infof("from: " + helper.BytesToHex(from.Bytes())) // little endian

	retry := &db.Retry{
		Height: txHeight,
		Key:    key,
	}
	sink := common.NewZeroCopySink(nil)
	retry.Serialization(sink)

	// create an InvocationTransaction
	sysFee := helper.Fixed8FromFloat64(this.config.NeoSysFee)
	netFee := helper.Fixed8FromFloat64(this.config.NeoNetFee)
	itx, err := this.MakeInvocationTransaction(script, from, nil, from, sysFee, netFee)

	////---------------------------------------
	//if itx.Gas.Equal(helper.Zero) {
	//	log.Infof("tx already done, height %d, key %s ", txHeight, key)
	//	return nil
	//}
	////----------------------------------------

	if err != nil {
		if strings.Contains(err.Error(), "not enough balance in address") {
			// utxo is not enough, put into NeoRetry
			err = this.db.PutNeoRetry(sink.Bytes())
			if err != nil {
				return fmt.Errorf("[syncProofToNeo] this.db.PutNeoRetry error: %s", err)
			}
			log.Infof("[syncProofToNeo] put tx into retry db, height %d, key %s, db key %s", txHeight, key, helper.BytesToHex(sink.Bytes()))
			return nil
		}
		return fmt.Errorf("[syncProofToNeo] tb.MakeInvocationTransaction error: %s", err)
	}

	// sign transaction
	err = tx.AddSignature(itx, this.neoAccount.KeyPair)
	if err != nil {
		return fmt.Errorf("[syncProofToNeo] tx.AddSignature error: %s", err)
	}

	rawTxString := itx.RawTransactionString()

	// send the raw transaction
	response := this.neoSdk.SendRawTransaction(rawTxString)
	if response.HasError() {
		err = this.db.PutNeoRetry(sink.Bytes())
		if err != nil {
			return fmt.Errorf("[syncProofToRelay] this.db.PutNeoRetry error: %s", err)
		}
		log.Errorf("[syncProofToNeo] put tx into retry db, height %d, key %s, db key %s", txHeight, key, helper.BytesToHex(sink.Bytes()))
		return fmt.Errorf("[syncProofToNeo] SendRawTransaction error: %s, path(cp1): %s, cp2: %d, syncProofToNeo RawTransactionString: %s",
			response.ErrorResponse.Error.Message, helper.BytesToHex(path), int64(blockHeightReliable), rawTxString)
	}
	log.Infof("[syncProofToNeo] syncProofToNeo txHash is: %s", itx.HashString())
	// mark utxo
	for _, unspent := range itx.Inputs {
		neoUtxo := db.NeoUtxo{
			TxId:  unspent.PrevHash.String(),
			Index: int(unspent.PrevIndex),
		}
		sink := common.NewZeroCopySink(nil)
		neoUtxo.Serialization(sink)
		err := this.db.PutUtxo(sink.Bytes(), true)
		if err != nil {
			return err
		}
	}
	//this.waitForNeoBlock()
	return nil
}

func (this *SyncService) retrySyncProofToNeo(v []byte, lastSynced uint32) error {
	retry := new(db.Retry)
	err := retry.Deserialization(common.NewZeroCopySource(v))
	if err != nil {
		return fmt.Errorf("[retrySyncProofToNeo] retry.Deserialization error: %s", err)
	}
	txHeight := retry.Height
	key := retry.Key

	blockHeightReliable := lastSynced + 1
	// get the proof of the cross chain tx
	crossStateProof, err := this.relaySdk.ClientMgr.GetCrossStatesProof(txHeight, key)
	if err != nil {
		return fmt.Errorf("[retrySyncProofToNeo] GetCrossStatesProof error: %s", err)
	}
	path, err := hex.DecodeString(crossStateProof.AuditPath)
	if err != nil {
		return fmt.Errorf("[retrySyncProofToNeo] DecodeString error: %s", err)
	}
	txProof := sc.ContractParameter{
		Type:  sc.ByteArray,
		Value: path,
	}
	//log.Infof("path: " + helper.BytesToHex(path))

	// get the next block header since it has the stateroot for the cross chain tx
	blockHeightToBeVerified := txHeight + 1
	headerToBeVerified, err := this.relaySdk.GetHeaderByHeight(blockHeightToBeVerified)
	if err != nil {
		return fmt.Errorf("[retrySyncProofToNeo] GetHeaderByHeight error: %s", err)
	}
	txProofHeader := sc.ContractParameter{
		Type:  sc.ByteArray,
		Value: headerToBeVerified.GetMessage(),
	}
	//log.Infof("txProofHeader: " + helper.BytesToHex(headerToBeVerified.GetMessage()))

	var headerProofBytes []byte
	var currentHeaderBytes []byte
	var signListBytes []byte
	if txHeight >= lastSynced {
		// cross chain tx is in current epoch, no need for headerProof and currentHeader
		headerProofBytes = []byte{}
		currentHeaderBytes = []byte{}
		// the signList should be the signature of the header at txHeight + 1
		signListBytes = []byte{}
		for _, sig := range headerToBeVerified.SigData {
			newSig, _ := signature.ConvertToEthCompatible(sig) // convert to eth
			signListBytes = append(signListBytes, newSig...)
		}
	} else {
		// txHeight < lastSynced, so blockHeightToBeVerified < blockHeightReliable
		// get the merkle proof of the block containing the stateroot
		merkleProof, err := this.relaySdk.GetMerkleProof(blockHeightToBeVerified, blockHeightReliable)
		if err != nil {
			return fmt.Errorf("[retrySyncProofToNeo] GetMerkleProof error: %s", err)
		}
		headerProofBytes, err = hex.DecodeString(merkleProof.AuditPath)
		if err != nil {
			return fmt.Errorf("[retrySyncProofToNeo] merkleProof DecodeString error: %s", err)
		}
		log.Infof("headerPath: " + helper.BytesToHex(headerProofBytes))

		// get the raw current header
		headerReliable, err := this.relaySdk.GetHeaderByHeight(blockHeightReliable)
		if err != nil {
			return fmt.Errorf("[retrySyncProofToNeo] GetHeaderByHeight error: %s", err)
		}
		currentHeaderBytes = headerReliable.GetMessage()

		// get the sign list of the current header
		signListBytes = []byte{}
		for _, sig := range headerReliable.SigData {
			newSig, _ := signature.ConvertToEthCompatible(sig) // convert to eth
			signListBytes = append(signListBytes, newSig...)
		}
	}

	headProof := sc.ContractParameter{
		Type:  sc.ByteArray,
		Value: headerProofBytes,
	}
	//log.Infof("headProof: " + helper.BytesToHex(headerProofBytes))

	currentHeader := sc.ContractParameter{
		Type:  sc.ByteArray,
		Value: currentHeaderBytes,
	}
	//log.Infof("currentHeader: " + helper.BytesToHex(currentHeaderBytes))

	signList := sc.ContractParameter{
		Type:  sc.ByteArray,
		Value: signListBytes,
	}
	//log.Infof("signList: " + helper.BytesToHex(signListBytes))

	// build script
	scriptBuilder := sc.NewScriptBuilder()
	scriptHash := helper.HexToBytes(this.config.NeoCCMC) // hex string to little endian byte[]

	args := []sc.ContractParameter{txProof, txProofHeader, headProof, currentHeader, signList}
	scriptBuilder.MakeInvocationScript(scriptHash, VERIFY_AND_EXECUTE_TX, args)
	script := scriptBuilder.ToArray()
	//log.Infof("script: " + helper.BytesToHex(script))

	//tb := tx.NewTransactionBuilder(this.config.NeoJsonRpcUrl)
	from, err := helper.AddressToScriptHash(this.neoAccount.Address)

	// create an InvocationTransaction
	sysFee := helper.Fixed8FromFloat64(this.config.NeoSysFee)
	netFee := helper.Fixed8FromFloat64(this.config.NeoNetFee)
	itx, err := this.MakeInvocationTransaction(script, from, nil, from, sysFee, netFee)

	////---------------------------------------
	//if itx.Gas.Equal(helper.Zero) {
	//	log.Infof("tx already done, height %d, key %s ", txHeight, key)
	//	return nil
	//}
	////----------------------------------------

	if err != nil {
		if strings.Contains(err.Error(), "not enough balance in address") {
			// still the same error, just log and return
			log.Infof("[retrySyncProofToNeo] remain tx in retry db, MakeInvocationTransaction error: %s", err)
			return nil
		} else {
			// not because utxo is not enough, delete from db
			err := this.db.DeleteNeoRetry(v)
			if err != nil {
				return fmt.Errorf("[retrySyncProofToNeo] this.db.DeleteNeoRetry error: %s", err)
			}
			log.Infof("[retrySyncProofToNeo] delete tx from retry db, height %d, key %s, db key %s", txHeight, key, helper.BytesToHex(v))
			return fmt.Errorf("[retrySyncProofToNeo] tb.MakeInvocationTransaction error: %s", err)
		}
	}

	// sign transaction
	err = tx.AddSignature(itx, this.neoAccount.KeyPair)
	if err != nil {
		return fmt.Errorf("[retrySyncProofToNeo] tx.AddSignature error: %s", err)
	}

	rawTxString := itx.RawTransactionString()

	// send the raw transaction
	response := this.neoSdk.SendRawTransaction(rawTxString)
	if response.HasError() {
		if strings.Contains(response.ErrorResponse.Error.Message, "Block or transaction validation failed") {
			log.Infof("[retrySyncProofToNeo] remain tx in retry db, SendRawTransaction: height %d, key %s, db key %s", txHeight, key, helper.BytesToHex(v))
			return nil
		}

		err := this.db.DeleteNeoRetry(v)
		if err != nil {
			return fmt.Errorf("[retrySyncProofToNeo] this.db.DeleteNeoRetry error: %s", err)
		}
		log.Infof("[retrySyncProofToNeo] delete tx from retry db, height %d, key %s, db key %s", txHeight, key, helper.BytesToHex(v))
		return fmt.Errorf("[retrySyncProofToNeo] SendRawTransaction error: %s, path(cp1): %s, cp2: %d, syncProofToNeo RawTransactionString: %s",
			response.ErrorResponse.Error.Message, helper.BytesToHex(path), int64(blockHeightReliable), rawTxString)
	}
	log.Infof("[retrySyncProofToNeo] syncProofToNeo txHash is: %s", itx.HashString())
	// mark utxo
	for _, unspent := range itx.Inputs {
		neoUtxo := db.NeoUtxo{
			TxId:  unspent.PrevHash.String(),
			Index: int(unspent.PrevIndex),
		}
		sink := common.NewZeroCopySink(nil)
		neoUtxo.Serialization(sink)
		err := this.db.PutUtxo(sink.Bytes(), true)
		if err != nil {
			return err
		}
	}
	err = this.db.DeleteNeoRetry(v)
	if err != nil {
		err := this.db.DeleteNeoRetry(v)
		log.Infof("[retrySyncProofToNeo] delete tx from retry db, height %d, key %s, db key %s", txHeight, key, helper.BytesToHex(v))
		return fmt.Errorf("[retrySyncProofToNeo] this.db.DeleteNeoRetry error: %s", err)
	}
	//this.waitForNeoBlock()
	return nil
}

func (this *SyncService) neoRetryTx() error {
	retryList, err := this.db.GetAllNeoRetry()
	if err != nil {
		return fmt.Errorf("[neoRetryTx] this.db.GetAllRetry error: %s", err)
	}
	for _, v := range retryList {
		// get current neo chain sync height, which is the reliable header height
		currentNeoChainSyncHeight, err := this.GetCurrentNeoChainSyncHeight(this.relaySdk.ChainId)
		if err != nil {
			log.Errorf("[neoRetryTx] GetCurrentNeoChainSyncHeight error: ", err)
		}
		err = this.retrySyncProofToNeo(v, uint32(currentNeoChainSyncHeight))
		if err != nil {
			log.Errorf("[neoRetryTx] this.retrySyncProofToNeo error:%s", err)
		}
		time.Sleep(time.Duration(this.config.RetryInterval) * time.Second)
	}

	return nil
}

func (this *SyncService) MakeInvocationTransaction(script []byte, from helper.UInt160, attributes []*tx.TransactionAttribute, changeAddress helper.UInt160, sysFee helper.Fixed8, netFee helper.Fixed8) (*tx.InvocationTransaction, error) {
	if changeAddress.String() == "0000000000000000000000000000000000000000" {
		changeAddress = from
	}
	// use rpc to get gas consumed
	gasConsumed, err := this.GetGasConsumed(script, from.String())
	if err != nil {
		return nil, err
	}
	itx := tx.NewInvocationTransaction(script)
	if attributes != nil {
		itx.Attributes = attributes
	}
	itx.Gas = gasConsumed.Add(sysFee) // add sys fee
	fee := itx.Gas.Add(netFee)        // add net fee
	if itx.Size() > 1024 {
		fee = fee.Add(helper.Fixed8FromFloat64(0.001))
		fee = fee.Add(helper.Fixed8FromFloat64(float64(itx.Size()) * 0.00001))
	}
	// get transaction inputs
	inputs, totalPayGas, err := this.GetTransactionInputs(from, tx.GasToken, fee)
	if err != nil {
		return nil, err
	}
	if totalPayGas.GreaterThan(fee) {
		itx.Outputs = append(itx.Outputs, tx.NewTransactionOutput(tx.GasToken, totalPayGas.Sub(fee), changeAddress))
	}
	itx.Inputs = inputs
	return itx, nil
}

func (this *SyncService) GetTransactionInputs(from helper.UInt160, assetId helper.UInt256, amount helper.Fixed8) ([]*tx.CoinReference, helper.Fixed8, error) {
	if amount.Equal(helper.Zero) {
		return nil, helper.Zero, nil
	}
	unspents, available, err := this.GetBalance(from, assetId)
	if err != nil {
		return nil, helper.Zero, err
	}
	if available.LessThan(amount) {
		return nil, helper.Zero, fmt.Errorf("not enough balance in address: %s", helper.ScriptHashToAddress(from))
	}
	sort.Sort(sort.Reverse(models.UnspentSlice(unspents))) // sort in decreasing order
	var i int = 0
	var a float64 = helper.Fixed8ToFloat64(amount)
	var inputs []*tx.CoinReference = []*tx.CoinReference{}
	var sum helper.Fixed8 = helper.Zero
	for i < len(unspents) && unspents[i].Value <= a {
		a -= unspents[i].Value
		inputs = append(inputs, tx.ToCoinReference(unspents[i]))
		sum = sum.Add(helper.Fixed8FromFloat64(unspents[i].Value))
		i++
	}
	if a == 0 {
		return inputs, sum, nil
	}
	// use the nearest amount
	for i < len(unspents) && unspents[i].Value >= a {
		i++
	}
	inputs = append(inputs, tx.ToCoinReference(unspents[i-1]))
	sum = sum.Add(helper.Fixed8FromFloat64(unspents[i-1].Value))
	return inputs, sum, nil
}

func (this *SyncService) GetGasConsumed(script []byte, checkWitnessHashes string) (*helper.Fixed8, error) {
	response := this.neoSdk.InvokeScript(helper.BytesToHex(script), checkWitnessHashes)
	if response.HasError() {
		return nil, fmt.Errorf(response.ErrorResponse.Error.Message)
	}
	if response.Result.State == "FAULT" { // CNEO case, use ScriptContainer in contract will cause engine fault
		result := helper.Fixed8FromInt64(0)
		return &result, nil
	}
	gasConsumed, err := helper.Fixed8FromString(response.Result.GasConsumed)
	if err != nil {
		return nil, err
	}
	gas := gasConsumed.Sub(helper.Fixed8FromInt64(50))
	if gas.LessThan(helper.Zero) || gas.Equal(helper.Zero) {
		return &helper.Zero, nil
	} else {
		g := gas.Ceiling()
		return &g, nil
	}
}

func (this *SyncService) GetBalance(account helper.UInt160, assetId helper.UInt256) ([]models.Unspent, helper.Fixed8, error) {
	response := this.neoSdk.GetUnspents(helper.ScriptHashToAddress(account))
	if response.HasError() {
		return nil, helper.Zero, fmt.Errorf(response.ErrorResponse.Error.Message)
	}
	balances := response.Result.Balances
	// check if there is enough balance of this asset in this account
	var unspents []models.Unspent = []models.Unspent{}
	sum := helper.Zero
	for _, balance := range balances {
		if balance.AssetHash == assetId.String() {
			for _, unspent := range balance.Unspents {
				neoUtxo := db.NeoUtxo{
					TxId:  unspent.Txid,
					Index: unspent.N,
				}
				sink := common.NewZeroCopySink(nil)
				neoUtxo.Serialization(sink)
				// check if in db
				isSpent, err := this.db.GetUtxo(sink.Bytes())
				if err != nil {
					if strings.Contains(err.Error(), "value does not exist") {
						// put this utxo into db
						err2 := this.db.PutUtxo(sink.Bytes(), false)
						if err2 != nil {
							return nil, helper.Zero, err2
						}
						unspents = append(unspents, unspent)
						sum = sum.Add(helper.Fixed8FromFloat64(unspent.Value))
					} else {
						return nil, helper.Zero, err
					}
				}
				if isSpent != nil && !*isSpent {
					unspents = append(unspents, unspent)
					sum = sum.Add(helper.Fixed8FromFloat64(unspent.Value))
				}
			}
		}
	}

	return unspents, sum, nil
}

func (this *SyncService) waitForNeoBlock() {
	time.Sleep(time.Duration(15) * time.Second)
}

func getRelayUncompressedKey(key keypair.PublicKey) []byte {
	var buff bytes.Buffer
	switch t := key.(type) {
	case *ec.PublicKey:
		switch t.Algorithm {
		case ec.ECDSA:
			// Take P-256 as a special case
			if t.Params().Name == elliptic.P256().Params().Name {
				return ec.EncodePublicKey(t.PublicKey, false)
			}
			buff.WriteByte(byte(0x12))
		case ec.SM2:
			buff.WriteByte(byte(0x13))
		}
		label, err := getCurveLabel(t.Curve.Params().Name)
		if err != nil {
			panic(err)
		}
		buff.WriteByte(label)
		buff.Write(ec.EncodePublicKey(t.PublicKey, false))
	default:
		panic("err")
	}
	return buff.Bytes()
}

func getCurveLabel(name string) (byte, error) {
	switch strings.ToUpper(name) {
	case strings.ToUpper(elliptic.P224().Params().Name):
		return 1, nil
	case strings.ToUpper(elliptic.P256().Params().Name):
		return 2, nil
	case strings.ToUpper(elliptic.P384().Params().Name):
		return 3, nil
	case strings.ToUpper(elliptic.P521().Params().Name):
		return 4, nil
	case strings.ToUpper(sm2.SM2P256V1().Params().Name):
		return 20, nil
	case strings.ToUpper(btcec.S256().Name):
		return 5, nil
	default:
		panic("err")
	}
}
