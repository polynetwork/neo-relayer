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
	"github.com/polynetwork/neo-relayer/log"
	"github.com/polynetwork/poly/common"
	"github.com/polynetwork/poly/core/types"
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

// GetCurrentNeoChainSyncHeight
func (this *SyncService) GetCurrentNeoChainSyncHeight(relayChainID uint64) (uint64, error) {
	arg := models.NewInvokeFunctionStackArg("Integer", fmt.Sprint(relayChainID))
	response := this.neoSdk.InvokeFunction("0x"+helper.ReverseString(this.config.NeoCCMC), GET_CURRENT_HEIGHT, helper.ZeroScriptHashString, arg)
	if response.HasError() || response.Result.State == "FAULT" {
		return 0, fmt.Errorf("[GetCurrentNeoChainSyncHeight] GetCurrentHeight error: %s", "Engine faulted! " + response.Error.Message)
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
	log.Infof("path: " + helper.BytesToHex(path))

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
		log.Infof("headerPath: " + helper.BytesToHex(headerProofBytes))

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

	headProof := sc.ContractParameter{
		Type:  sc.ByteArray,
		Value: headerProofBytes,
	}
	log.Infof("headProof: " + helper.BytesToHex(headerProofBytes))

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

	// build script
	scriptBuilder := sc.NewScriptBuilder()
	scriptHash := helper.HexToBytes(this.config.NeoCCMC) // hex string to little endian byte[]

	args := []sc.ContractParameter{txProof, txProofHeader, headProof, currentHeader, signList}
	scriptBuilder.MakeInvocationScript(scriptHash, VERIFY_AND_EXECUTE_TX, args)
	script := scriptBuilder.ToArray()
	//log.Infof("script: " + helper.BytesToHex(script))

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
		return fmt.Errorf("[syncProofToNeo] SendRawTransaction error: %s, path(cp1): %s, cp2: %d, syncProofToNeo RawTransactionString: %s",
			response.ErrorResponse.Error.Message, helper.BytesToHex(path), int64(blockHeightReliable), rawTxString)
	}
	log.Infof("[syncProofToNeo] syncProofToNeo txHash is: %s", itx.HashString())
	this.waitForNeoBlock()
	return nil
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
