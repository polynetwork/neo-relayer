package service

type ToMerkleValue struct {
	TxHash      []byte // poly chain tx hash
	FromChainID uint64
	TxParam     *CrossChainTxParameter
}

type CrossChainTxParameter struct {
	TxHash       []byte // source chain tx hash
	CrossChainID []byte
	FromContract []byte

	ToChainID  uint64
	ToContract []byte
	Method     []byte
	Args       []byte
}

func DeserializeArgs(source []byte) ([]byte, []byte, uint64, error) {
	offset := 0
	var err error
	assetHash, offset, err := ReadVarBytes(source, offset)
	if err != nil {
		return nil, nil, 0, err
	}

	toAddress, offset, err := ReadVarBytes(source, offset)
	if err != nil {
		return nil, nil, 0, err
	}

	toAmount, offset, err := ReadUInt255(source, offset)
	if err != nil {
		return nil, nil, 0, err
	}

	return assetHash, toAddress, toAmount.Uint64(), nil
}

func DeserializeMerkleValue(source []byte) (*ToMerkleValue, error) {
	result := ToMerkleValue{}
	offset := 0
	var err error
	// get TxHash
	result.TxHash, offset, err = ReadVarBytes(source, offset)
	if err != nil {
		return nil, err
	}
	// get FromChainID
	result.FromChainID, offset, err = ReadVarUInt64(source, offset)
	if err != nil {
		return nil, err
	}
	// get CrossChainTxParameter
	result.TxParam, err = DeserializeCrossChainTxParameter(source, offset)
	if err != nil {
		return nil, err
	}

	return &result, nil
}

func DeserializeCrossChainTxParameter(source []byte, offset int) (*CrossChainTxParameter, error) {
	result := CrossChainTxParameter{}
	var err error
	// get TxHash
	result.TxHash, offset, err = ReadVarBytes(source, offset)
	if err != nil {
		return nil, err
	}
	// get CrossChainID
	result.CrossChainID, offset, err = ReadVarBytes(source, offset)
	if err != nil {
		return nil, err
	}
	// get FromContract
	result.FromContract, offset, err = ReadVarBytes(source, offset)
	if err != nil {
		return nil, err
	}
	// get ToChainID
	result.ToChainID, offset, err = ReadVarUInt64(source, offset)
	if err != nil {
		return nil, err
	}
	// get ToContract
	result.ToContract, offset, err = ReadVarBytes(source, offset)
	if err != nil {
		return nil, err
	}
	// get Method
	result.Method, offset, err = ReadVarBytes(source, offset)
	if err != nil {
		return nil, err
	}
	// get Params
	result.Args, offset, err = ReadVarBytes(source, offset)
	if err != nil {
		return nil, err
	}

	return &result, nil
}
