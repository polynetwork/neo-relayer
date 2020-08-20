package main

import (
	"fmt"
	"github.com/joeqian10/neo-gogogo/rpc"
	"github.com/joeqian10/neo-gogogo/wallet"
	"github.com/polynetwork/poly/core/types"
	"golang.org/x/crypto/ssh/terminal"
	"os"
	"os/signal"
	"runtime"
	"syscall"
	"time"

	"github.com/polynetwork/neo-relayer/cmd"
	"github.com/polynetwork/neo-relayer/common"
	"github.com/polynetwork/neo-relayer/config"
	"github.com/polynetwork/neo-relayer/log"
	"github.com/polynetwork/neo-relayer/service"

	relaySdk "github.com/polynetwork/poly-go-sdk"
	"github.com/urfave/cli"
)

func setupApp() *cli.App {
	app := cli.NewApp()
	app.Usage = "NEO Relayer"
	app.Action = startSync
	app.Copyright = "Copyright in 2020 The NEO Project"
	app.Flags = []cli.Flag{
		cmd.LogLevelFlag,
		cmd.ConfigPathFlag,
		cmd.NeoPwd,
		cmd.RelayPwd,
	}
	app.Commands = []cli.Command{}
	app.Before = func(context *cli.Context) error {
		runtime.GOMAXPROCS(runtime.NumCPU())
		return nil
	}
	return app
}

func main() {
	if err := setupApp().Run(os.Args); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func startSync(ctx *cli.Context) {
	logLevel := ctx.GlobalInt(cmd.GetFlagName(cmd.LogLevelFlag))
	log.InitLog(logLevel, log.PATH, log.Stdout)
	//log.InitErrorCaseLogger(logLevel, log.ErrorCasePath, log.Stdout)
	configPath := ctx.String(cmd.GetFlagName(cmd.ConfigPathFlag))
	err := config.DefConfig.Init(configPath)
	if err != nil {
		fmt.Println("DefConfig.Init error: ", err)
		return
	}

	neoPwd := ctx.GlobalString(cmd.GetFlagName(cmd.NeoPwd))
	relayPwd := ctx.GlobalString(cmd.GetFlagName(cmd.RelayPwd))

	//create Relay Chain RPC Client
	relaySdk := relaySdk.NewPolySdk()
	err = SetUpPoly(relaySdk, config.DefConfig.RelayJsonRpcUrl)
	if err != nil {
		panic(fmt.Errorf("failed to set up poly: %v", err))
	}

	// Get wallet account from Relay Chain
	account, ok := common.GetAccountByPassword(relaySdk, config.DefConfig.WalletFile, relayPwd)
	if !ok {
		log.Errorf("[NEO Relayer] common.GetAccountByPassword error")
		return
	}

	// create an NEO RPC client
	neoRpcClient := rpc.NewClient(config.DefConfig.NeoJsonRpcUrl)

	// open the NEO wallet
	//neoAccount, err := wallet.NewAccountFromWIF(config.DefConfig.NeoWalletWIF)
	w, err := wallet.NewWalletFromFile(config.DefConfig.NeoWalletFile)
	if err != nil {
		log.Errorf("[NEO Relayer] Failed to open NEO wallet")
		return
	}

	if neoPwd == "" {
		fmt.Println()
		fmt.Printf("Neo Wallet Password:")
		pwd, err := terminal.ReadPassword(int(os.Stdin.Fd()))
		if err != nil {
			log.Errorf("[NEO Relayer] Invalid password entered")
		}
		neoPwd = string(pwd)
		fmt.Println()
	}
	err = w.DecryptAll(neoPwd)
	if err != nil {
		log.Errorf("[NEO Relayer] Failed to decrypt NEO account")
		return
	}
	neoAccount := w.Accounts[0]

	//Start syncing
	syncService := service.NewSyncService(account, relaySdk, neoAccount, neoRpcClient)
	syncService.Run()

	waitToExit()
}

func waitToExit() {
	exit := make(chan bool, 0)
	sc := make(chan os.Signal, 1)
	signal.Notify(sc, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
	go func() {
		for sig := range sc {
			log.Infof("Neo Relayer received exit signal: %v.", sig.String())
			close(exit)
			break
		}
	}()
	<-exit
}

func SetUpPoly(poly *relaySdk.PolySdk, rpcAddr string) error {
	poly.NewRpcClient().SetAddress(rpcAddr)
	c1 := make(chan *types.Header, 1)
	c2 := make(chan error, 1)

	// use another routine to check time out and error
	go func() {
		hdr, err := poly.GetHeaderByHeight(0)
		if err != nil {
			c2 <- err
		}
		c1 <- hdr
	}()

	select {
	case hdr := <- c1:
		poly.SetChainId(hdr.ChainID)
	case err := <- c2:
		return  err
	case <- time.After(time.Second * 5):
		return fmt.Errorf("poly rpc port timeout")
	}

	return nil
}

//func SetUpPoly1(poly *relaySdk.PolySdk, rpcAddr string) error {
//	poly.NewRpcClient().SetAddress(rpcAddr)
//	hdr, err := poly.GetHeaderByHeight(0)
//	if err != nil {
//		return err
//	}
//	poly.SetChainId(hdr.ChainID)
//	return nil
//}
