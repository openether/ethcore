package eth

import (
	"bytes"
	"errors"
	"fmt"
	"math/big"
	"strconv"
	"sync"
	"time"

	"github.com/openether/ethcore/accounts"
	"github.com/openether/ethcore/common"
	"github.com/openether/ethcore/common/compiler"
	"github.com/openether/ethcore/common/httpclient"
	"github.com/openether/ethcore/common/registrar/ethreg"
	"github.com/openether/ethcore/core"
	"github.com/openether/ethcore/core/types"
	"github.com/openether/ethcore/eth/downloader"
	"github.com/openether/ethcore/eth/filters"
	"github.com/openether/ethcore/ethdb"
	"github.com/openether/ethcore/event"
	"github.com/openether/ethcore/logger"
	"github.com/openether/ethcore/logger/glog"
	"github.com/openether/ethcore/node"
	"github.com/openether/ethcore/p2p"
	"github.com/openether/ethcore/rlp"
	"github.com/openether/ethcore/rpc"
)

const (
	epochLength    = 30000
	ethashRevision = 23

	autoDAGcheckInterval = 10 * time.Hour
	autoDAGepochHeight   = epochLength / 2
)

type Config struct {
	ChainConfig *core.ChainConfig // chain configuration

	NetworkId int // Network ID to use for selecting peers to connect to
	Genesis   *core.GenesisDump
	SyncMode  downloader.SyncMode // Enables the state download based fast synchronisation algorithm
	MaxPeers  int

	BlockChainVersion  int
	SkipBcVersionCheck bool // e.g. blockchain export
	DatabaseCache      int
	DatabaseHandles    int

	NatSpec   bool
	DocRoot   string
	AutoDAG   bool
	PowTest   bool
	PowShared bool

	AccountManager *accounts.Manager
	Etherbase      common.Address
	GasPrice       *big.Int
	SolcPath       string

	UseAddrTxIndex bool

	GpoMinGasPrice          *big.Int
	GpoMaxGasPrice          *big.Int
	GpoFullBlockRatio       int
	GpobaseStepDown         int
	GpobaseStepUp           int
	GpobaseCorrectionFactor int

	TestGenesisBlock *types.Block   // Genesis block to seed the chain database with (testing only!)
	TestGenesisState ethdb.Database // Genesis state to seed the database with (testing only!)
}

type Ethereum struct {
	config      *Config
	chainConfig *core.ChainConfig
	// Channel for shutting down the ethereum
	shutdownChan chan bool

	// DB interfaces
	chainDb   ethdb.Database // Block chain database
	dappDb    ethdb.Database // Dapp database
	indexesDb ethdb.Database // Indexes database (optional -- eg. add-tx indexes)

	// Handlers
	txPool          *core.TxPool
	txMu            sync.Mutex
	blockchain      *core.BlockChain
	accountManager  *accounts.Manager
	//pow             *Ethash
	protocolManager *ProtocolManager
	SolcPath        string
	solc            *compiler.Solidity
	gpo             *GasPriceOracle

	GpoMinGasPrice          *big.Int
	GpoMaxGasPrice          *big.Int
	GpoFullBlockRatio       int
	GpobaseStepDown         int
	GpobaseStepUp           int
	GpobaseCorrectionFactor int

	httpclient *httpclient.HTTPClient

	eventMux *event.TypeMux

	NatSpec       bool
	netVersionId  int
	netRPCService *PublicNetAPI
}

func New(ctx *node.ServiceContext, config *Config) (*Ethereum, error) {
	// Open the chain database and perform any upgrades needed
	chainDb, err := ctx.OpenDatabase("chaindata", config.DatabaseCache, config.DatabaseHandles)
	if err != nil {
		return nil, err
	}
	if err := upgradeChainDatabase(chainDb); err != nil {
		return nil, err
	}
	if err := addMipmapBloomBins(chainDb); err != nil {
		return nil, err
	}

	dappDb, err := ctx.OpenDatabase("dapp", config.DatabaseCache, config.DatabaseHandles)
	if err != nil {
		return nil, err
	}

	glog.V(logger.Info).Infof("Protocol Versions: %v, Network Id: %v, Chain Id: %v", ProtocolVersions, config.NetworkId, config.ChainConfig.GetChainID())
	glog.D(logger.Warn).Infof("Protocol Versions: %v, Network Id: %v, Chain Id: %v", logger.ColorGreen(fmt.Sprintf("%v", ProtocolVersions)), logger.ColorGreen(strconv.Itoa(config.NetworkId)), logger.ColorGreen(func() string {
		cid := config.ChainConfig.GetChainID().String()
		if cid == "0" {
			cid = "not set"
		}
		return cid
	}()))

	// Load up any custom genesis block if requested
	if config.Genesis != nil {
		_, err := core.WriteGenesisBlock(chainDb, config.Genesis)
		if err != nil {
			return nil, err
		}
	}

	// Load up a test setup if directly injected
	if config.TestGenesisState != nil {
		chainDb = config.TestGenesisState
	}
	if config.TestGenesisBlock != nil {
		core.WriteTd(chainDb, config.TestGenesisBlock.Hash(), config.TestGenesisBlock.Difficulty())
		core.WriteBlock(chainDb, config.TestGenesisBlock)
		core.WriteCanonicalHash(chainDb, config.TestGenesisBlock.Hash(), config.TestGenesisBlock.NumberU64())
		core.WriteHeadBlockHash(chainDb, config.TestGenesisBlock.Hash())
	}

	if !config.SkipBcVersionCheck {
		bcVersion := core.GetBlockChainVersion(chainDb)
		if bcVersion != config.BlockChainVersion && bcVersion != 0 {
			return nil, fmt.Errorf("Blockchain DB version mismatch (%d / %d). Run geth upgradedb.\n", bcVersion, config.BlockChainVersion)
		}
		core.WriteBlockChainVersion(chainDb, config.BlockChainVersion)
	}
	glog.V(logger.Info).Infof("Blockchain DB Version: %d", config.BlockChainVersion)

	eth := &Ethereum{
		config:                  config,
		shutdownChan:            make(chan bool),
		chainDb:                 chainDb,
		dappDb:                  dappDb,
		eventMux:                ctx.EventMux,
		accountManager:          config.AccountManager,
		netVersionId:            config.NetworkId,
		NatSpec:                 config.NatSpec,
		SolcPath:                config.SolcPath,
		GpoMinGasPrice:          config.GpoMinGasPrice,
		GpoMaxGasPrice:          config.GpoMaxGasPrice,
		GpoFullBlockRatio:       config.GpoFullBlockRatio,
		GpobaseStepDown:         config.GpobaseStepDown,
		GpobaseStepUp:           config.GpobaseStepUp,
		GpobaseCorrectionFactor: config.GpobaseCorrectionFactor,
		httpclient:              httpclient.New(config.DocRoot),
	}

	// Initialize indexes db if enabled
	// Blockchain will be assigned the db and atx enabled after blockchain is initialized below.
	var indexesDb ethdb.Database
	if config.UseAddrTxIndex {
		// TODO: these are arbitrary numbers I just made up. Optimize?
		// The reason these numbers are different than the atxi-build command is because for "appending" (vs. building)
		// the atxi database should require far fewer resources since application performance is limited primarily by block import (chaindata db).
		ethdb.SetCacheRatio("chaindata", 0.95)
		ethdb.SetHandleRatio("chaindata", 0.95)
		ethdb.SetCacheRatio("indexes", 0.05)
		ethdb.SetHandleRatio("indexes", 0.05)
		indexesDb, err = ctx.OpenDatabase("indexes", config.DatabaseCache, config.DatabaseCache)
		if err != nil {
			return nil, err
		}
		eth.indexesDb = indexesDb
	}

	// load the genesis block or write a new one if no genesis
	// block is present in the database.
	genesis := core.GetBlock(chainDb, core.GetCanonicalHash(chainDb, 0))
	if genesis == nil {
		genesis, err = core.WriteGenesisBlock(chainDb, core.DefaultConfigMainnet.Genesis)
		if err != nil {
			return nil, err
		}
		glog.V(logger.Info).Infof("Successfully wrote default ethereum mainnet genesis block: %s", logger.ColorGreen(genesis.Hash().Hex()))
		glog.D(logger.Warn).Infof("Wrote mainnet genesis block: %s", logger.ColorGreen(genesis.Hash().Hex()))
	}

	// Log genesis block information.
	var genName string
	if fmt.Sprintf("%x", genesis.Hash()) == "0cd786a2425d16f152c658316c423e6ce1181e15c3295826d7c9904cba9ce303" {
		genName = "morden testnet"
	} else if fmt.Sprintf("%x", genesis.Hash()) == "d4e56740f876aef8c010b86a40d5f56745a118d0906a34e69aec8c0db1cb8fa3" {
		genName = "mainnet"
	} else {
		genName = "custom"
	}
	glog.V(logger.Info).Infof("Successfully established %s genesis block: %s", genName, genesis.Hash().Hex())
	glog.D(logger.Warn).Infof("Genesis block: %s (%s)", logger.ColorGreen(genesis.Hash().Hex()), genName)

	if config.ChainConfig == nil {
		return nil, errors.New("missing chain config")
	}

	eth.chainConfig = config.ChainConfig

	eth.blockchain, err = core.NewBlockChain(chainDb, eth.chainConfig, eth.EventMux())
	if err != nil {
		if err == core.ErrNoGenesis {
			return nil, fmt.Errorf(`No chain found. Please initialise a new chain using the "init" subcommand.`)
		}
		return nil, err
	}
	// Configure enabled atxi for blockchain
	if config.UseAddrTxIndex {
		eth.blockchain.SetAtxi(&core.AtxiT{
			Db: eth.indexesDb,
		})
	}

	eth.gpo = NewGasPriceOracle(eth)

	newPool := core.NewTxPool(eth.chainConfig, eth.EventMux(), eth.blockchain.State, eth.blockchain.GasLimit)
	eth.txPool = newPool

	if eth.protocolManager, err = NewProtocolManager(eth.chainConfig, config.SyncMode, uint64(config.NetworkId), eth.eventMux, eth.txPool, eth.blockchain, chainDb); err != nil {
		return nil, err
	}

	return eth, nil
}

// APIs returns the collection of RPC services the ethereum package offers.
// NOTE, some of these services probably need to be moved to somewhere else.
func (s *Ethereum) APIs() []rpc.API {
	return []rpc.API{
		{
			Namespace: "eth",
			Version:   "1.0",
			Service:   NewPublicEthereumAPI(s),
			Public:    true,
		}, {
			Namespace: "eth",
			Version:   "1.0",
			Service:   NewPublicAccountAPI(s.accountManager),
			Public:    true,
		}, {
			Namespace: "personal",
			Version:   "1.0",
			Service:   NewPrivateAccountAPI(s),
			Public:    false,
		}, {
			Namespace: "eth",
			Version:   "1.0",
			Service:   NewPublicBlockChainAPI(s.chainConfig, s.blockchain, s.chainDb, s.gpo, s.eventMux, s.accountManager),
			Public:    true,
		}, {
			Namespace: "eth",
			Version:   "1.0",
			Service:   NewPublicTransactionPoolAPI(s),
			Public:    true,
		}, {
			Namespace: "eth",
			Version:   "1.0",
			Service:   downloader.NewPublicDownloaderAPI(s.protocolManager.downloader, s.eventMux),
			Public:    true,
		}, {
			Namespace: "txpool",
			Version:   "1.0",
			Service:   NewPublicTxPoolAPI(s),
			Public:    true,
		}, {
			Namespace: "eth",
			Version:   "1.0",
			Service:   filters.NewPublicFilterAPI(s.chainDb, s.eventMux),
			Public:    true,
		}, {
			Namespace: "admin",
			Version:   "1.0",
			Service:   NewPrivateAdminAPI(s),
		}, {
			Namespace: "debug",
			Version:   "1.0",
			Service:   NewPublicDebugAPI(s),
			Public:    true,
		}, {
			Namespace: "net",
			Version:   "1.0",
			Service:   s.netRPCService,
			Public:    true,
		}, {
			Namespace: "admin",
			Version:   "1.0",
			Service:   ethreg.NewPrivateRegistarAPI(s.chainConfig, s.blockchain, s.chainDb, s.txPool, s.accountManager),
		}, {
			Namespace: "geth",
			Version:   "1.0",
			Service:   NewPublicGethAPI(s),
			Public:    true,
		},
	}
}

func (s *Ethereum) ResetWithGenesisBlock(gb *types.Block) {
	s.blockchain.ResetWithGenesisBlock(gb)
}

func (s *Ethereum) AccountManager() *accounts.Manager  { return s.accountManager }
func (s *Ethereum) BlockChain() *core.BlockChain       { return s.blockchain }
func (s *Ethereum) TxPool() *core.TxPool               { return s.txPool }
func (s *Ethereum) EventMux() *event.TypeMux           { return s.eventMux }
func (s *Ethereum) ChainDb() ethdb.Database            { return s.chainDb }
func (s *Ethereum) DappDb() ethdb.Database             { return s.dappDb }
func (s *Ethereum) IsListening() bool                  { return true } // Always listening
func (s *Ethereum) EthVersion() int                    { return int(s.protocolManager.SubProtocols[0].Version) }
func (s *Ethereum) NetVersion() int                    { return s.netVersionId }
func (s *Ethereum) ChainConfig() *core.ChainConfig     { return s.chainConfig }
func (s *Ethereum) Downloader() *downloader.Downloader { return s.protocolManager.downloader }

// Protocols implements node.Service, returning all the currently configured
// network protocols to start.
func (s *Ethereum) Protocols() []p2p.Protocol {
	return s.protocolManager.SubProtocols
}

// Start implements node.Service, starting all internal goroutines needed by the
// Ethereum protocol implementation.
func (s *Ethereum) Start(srvr *p2p.Server) error {
	s.protocolManager.Start(s.config.MaxPeers)
	s.netRPCService = NewPublicNetAPI(srvr, s.NetVersion())
	return nil
}

// Stop implements node.Service, terminating all internal goroutines used by the
// Ethereum protocol.
func (s *Ethereum) Stop() error {
	s.blockchain.Stop()
	s.protocolManager.Stop()
	s.txPool.Stop()
	s.eventMux.Stop()

	s.chainDb.Close()
	s.dappDb.Close()
	close(s.shutdownChan)

	return nil
}

// This function will wait for a shutdown and resumes main thread execution
func (s *Ethereum) WaitForShutdown() {
	<-s.shutdownChan
}

// HTTPClient returns the light http client used for fetching offchain docs
// (natspec, source for verification)
func (self *Ethereum) HTTPClient() *httpclient.HTTPClient {
	return self.httpclient
}

func (self *Ethereum) Solc() (*compiler.Solidity, error) {
	var err error
	if self.solc == nil {
		self.solc, err = compiler.New(self.SolcPath)
	}
	return self.solc, err
}

// set in js console via admin interface or wrapper from cli flags
func (self *Ethereum) SetSolc(solcPath string) (*compiler.Solidity, error) {
	self.SolcPath = solcPath
	self.solc = nil
	return self.Solc()
}

// dagFiles(epoch) returns the two alternative DAG filenames (not a path)
// 1) <revision>-<hex(seedhash[8])> 2) full-R<revision>-<hex(seedhash[8])>
func dagFiles(epoch uint64) (string, string) {
	seedHash, _ := GetSeedHash(epoch * epochLength)
	dag := fmt.Sprintf("full-R%d-%x", ethashRevision, seedHash[:8])
	return dag, "full-R" + dag
}

// upgradeChainDatabase ensures that the chain database stores block split into
// separate header and body entries.
func upgradeChainDatabase(db ethdb.Database) error {
	// Short circuit if the head block is stored already as separate header and body
	data, err := db.Get([]byte("LastBlock"))
	if err != nil {
		return nil
	}
	head := common.BytesToHash(data)

	if block := core.GetBlockByHashOld(db, head); block == nil {
		return nil
	}
	// At least some of the database is still the old format, upgrade (skip the head block!)
	glog.V(logger.Info).Info("Old database detected, upgrading...")

	if db, ok := db.(*ethdb.LDBDatabase); ok {
		blockPrefix := []byte("block-hash-")
		for it := db.NewIterator(); it.Next(); {
			// Skip anything other than a combined block
			if !bytes.HasPrefix(it.Key(), blockPrefix) {
				continue
			}
			// Skip the head block (merge last to signal upgrade completion)
			if bytes.HasSuffix(it.Key(), head.Bytes()) {
				continue
			}
			// Load the block, split and serialize (order!)
			block := core.GetBlockByHashOld(db, common.BytesToHash(bytes.TrimPrefix(it.Key(), blockPrefix)))

			if err := core.WriteTd(db, block.Hash(), block.DeprecatedTd()); err != nil {
				return err
			}
			if err := core.WriteBody(db, block.Hash(), block.Body()); err != nil {
				return err
			}
			if err := core.WriteHeader(db, block.Header()); err != nil {
				return err
			}
			if err := db.Delete(it.Key()); err != nil {
				return err
			}
		}
		// Lastly, upgrade the head block, disabling the upgrade mechanism
		current := core.GetBlockByHashOld(db, head)

		if err := core.WriteTd(db, current.Hash(), current.DeprecatedTd()); err != nil {
			return err
		}
		if err := core.WriteBody(db, current.Hash(), current.Body()); err != nil {
			return err
		}
		if err := core.WriteHeader(db, current.Header()); err != nil {
			return err
		}
	}
	return nil
}

func addMipmapBloomBins(db ethdb.Database) (err error) {
	const mipmapVersion uint = 2

	// check if the version is set. We ignore data for now since there's
	// only one version so we can easily ignore it for now
	var data []byte
	data, _ = db.Get([]byte("setting-mipmap-version"))
	if len(data) > 0 {
		var version uint
		if err := rlp.DecodeBytes(data, &version); err == nil && version == mipmapVersion {
			return nil
		}
	}

	defer func() {
		if err == nil {
			var val []byte
			val, err = rlp.EncodeToBytes(mipmapVersion)
			if err == nil {
				err = db.Put([]byte("setting-mipmap-version"), val)
			}
			return
		}
	}()
	latestBlock := core.GetBlock(db, core.GetHeadBlockHash(db))
	if latestBlock == nil { // clean database
		return
	}

	tstart := time.Now()
	glog.V(logger.Info).Infoln("upgrading db log bloom bins")
	for i := uint64(0); i <= latestBlock.NumberU64(); i++ {
		hash := core.GetCanonicalHash(db, i)
		if (hash == common.Hash{}) {
			return fmt.Errorf("chain db corrupted. Could not find block %d.", i)
		}
		err := core.WriteMipmapBloom(db, i, core.GetBlockReceipts(db, hash))
		if err != nil {
			return err
		}
	}
	glog.V(logger.Info).Infoln("upgrade completed in", time.Since(tstart))
	return nil
}
