# Relayer

A relayer between Poly and NEO.

## Build From Source

### Prerequisites

- [Golang](https://golang.org/doc/install) version 1.14 or later

### Build

```shell
git clone https://github.com/polynetwork/neo-relayer.git
cd neo-relayer
go build -o neo-relayer main.go
```

After successfully building the source code, you should see the executable program `neo-relayer`.

## Run

Before running the relayer, you need to create a wallet file of PolyNetwork.
Then you need to register the account as a Relayer of the Poly net and let the consensus nodes approve your registration.
Finally, you can start relaying transactions between Poly and Neo.

Before running, you need feed the configuration file `config.json`.

```json
{
  "RelayJsonRpcUrl": "http://40.115.182.238:20336",                 // poly node rpc port
  "RelayChainID": 0,                                                // poly chain id
  "WalletFile": "./poly_test.dat",                                  // poly chain wallet file
  "NeoWalletFile": "neo_test.json",                                 // neo chain wallet file
  "NeoJsonRpcUrl": "http://seed10.ngd.network:20332",               // neo node rpc port
  "NeoChainID": 5,                                                  // neo chain id
  "NeoCCMC": "07946635d87e4120164835391e33a114135b69e1",            // neo ccmc script hash in little endian
  "SpecificContract": "19cd39b09acc059ef6cc92bf2aff80baae2533d2",   // the specific contract you want to monitor, eg. lock proxy, if empty, everything will be relayed
  "NeoSysFee": 2,                                                   // extra system fee for neo chain
  "NeoNetFee": 0.02,                                                // extra network fee for neo chain
  "ScanInterval": 2,                                                // interval for scanning chains
  "RetryInterval": 2,                                               // interval for retrying sending tx to poly
  "DBPath": "boltdb",                                               // path for bolt db
  "NeoSyncHeight": 284956,                                          // start scanning height of poly
  "RelaySyncHeight": 4790618                                        // start scanning height of neo
}
```

Now, you can start neo-relayer using the following command:

```shell
./neo-relayer --neopwd pwd  --relaypwd pwd
```

Flag `neopwd` is the password for your neo wallet and `relaypwd` is the password for your Poly wallet.
The relayer will generate logs under `./Logs` and you can check relayer status by view log file.
