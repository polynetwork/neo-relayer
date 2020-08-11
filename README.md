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

```
{
  "RelayJsonRpcUrl": "http://40.115.182.238:40336",             // poly node
  "WalletFile": "./wallet.dat",                                 // poly chain wallet file
  "NeoWalletFile": "./a.json",                                  // neo chain wallet file
  "NeoJsonRpcUrl": "http://47.89.240.111:12332",                // neo node
  "NeoChainID": 4,                                              // neo chain ID
  "NeoCCMC": "7f25d672e8626d2beaa26f2cb40da6b91f40a382",        // neo ccmc script hash in little endian
  "ScanInterval": 2,                                            // interval for scanning chains
  "NeoSyncHeight": 178168,                                      // start scanning height of poly
  "RelaySyncHeight": 1262851                                    // start scanning height of neo
}
```

Now, you can start neo-relayer using the following command: 

```shell
./neo-relayer --neopwd pwd  --relaypwd pwd
```

Flag `neopwd` is the password for your neo wallet and `relaypwd` is the password for your Poly wallet. 
The relayer will generate logs under `./Logs` and you can check relayer status by view log file.