package service

import (
	neoRpc "github.com/joeqian10/neo-gogogo/rpc"
	"github.com/joeqian10/neo-gogogo/wallet"
	"github.com/neo-ngd/Relayer/config"
	rsdk "github.com/polynetwork/poly-go-sdk"
)

// SyncService ...
type SyncService struct {
	relayAccount    *rsdk.Account
	relaySdk        *rsdk.PolySdk
	relaySyncHeight uint32

	neoAccount        *wallet.Account
	neoSdk            *neoRpc.RpcClient
	neoSyncHeight     uint32
	neoNextConsensus  string

	config *config.Config
}

// NewSyncService ...
func NewSyncService(acct *rsdk.Account, relaySdk *rsdk.PolySdk, neoAccount *wallet.Account, neoSdk *neoRpc.RpcClient) *SyncService {
	syncSvr := &SyncService{
		relayAccount: acct,
		relaySdk:     relaySdk,

		neoAccount: neoAccount,
		neoSdk:     neoSdk,
		config:     config.DefConfig,
	}
	return syncSvr
}

// Run ...
func (this *SyncService) Run() {
	//go this.NeoToRelay()
	go this.RelayToNeo()
}


