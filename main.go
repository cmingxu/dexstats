package main

import (
	"context"
	"encoding/base64"
	"errors"
	"flag"
	"math/big"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/xssnick/tonutils-go/address"
	"github.com/xssnick/tonutils-go/liteclient"
	"github.com/xssnick/tonutils-go/tl"
	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/ton"
	"github.com/xssnick/tonutils-go/ton/jetton"
	"github.com/xssnick/tonutils-go/ton/nft"
	"github.com/xssnick/tonutils-go/tvm/cell"
)

var (
	logLevel = flag.String("loglevel", "info", "log level")
	display  = flag.String("display", "pretty", "display level")

	port = flag.String("port", "8080", "port")
	host = flag.String("host", "localhost", "host")
)

var (
	opJettonNotify = uint64(0x7362d09c)
	opStonfiSwap   = uint64(0x25938561)
)

var (
	opSwap = tl.CRC("swap")
	opLP   = tl.CRC("provide_lp")
)

var (
	sse *SSEServer
)

var swapProcessedCount atomic.Uint64
var (
	beginAt = time.Now()
)

// stonfi dex address, transaction on this address will be watched
var stonfiAddr *address.Address = address.MustParseAddr("EQB3ncyBUTjZUA5EnFKR5_EnOMI9V1tTEAAPaiU71gc4TiUt")

// use this to cache any jetton master
var jWalletMasterCache = NewJettonWalletJettonMasterAddrCache()

// cache all jetton master metadata
var masterOffChainDataCache = NewJettonMasterOffChainDataCache()

// use this to cache any pool info
var priceCollector *PriceCollector = nil

func main() {
	flag.Parse()

	zerolog.SetGlobalLevel(zerolog.InfoLevel)
	zerolog.TimeFieldFormat = time.RFC3339
	switch *logLevel {
	case "debug":
		zerolog.SetGlobalLevel(zerolog.DebugLevel)
	case "info":
		zerolog.SetGlobalLevel(zerolog.InfoLevel)
	case "warn":
		zerolog.SetGlobalLevel(zerolog.WarnLevel)
	}

	log.Logger = zerolog.New(zerolog.NewConsoleWriter()).With().Timestamp().Logger()

	sse = NewServer()
	go func() {
		log.Info().Msgf("start sse server on %s:%s", *host, *port)
		http.ListenAndServe(*host+":"+*port, sse)
	}()

	client := liteclient.NewConnectionPool()
	cfg, err := liteclient.GetConfigFromUrl(context.Background(), "https://ton.org/global.config.json")
	panicErr(err)

	// connect to mainnet lite servers
	err = client.AddConnectionsFromConfig(context.Background(), cfg)
	if err != nil {
		panic(err)
	}

	api := ton.NewAPIClient(client, ton.ProofCheckPolicyFast).WithRetry()
	api.SetTrustedBlockFromConfig(cfg)

	b, err := api.CurrentMasterchainInfo(context.Background())
	if err != nil {
		panic(err)
	}
	log.Debug().Msgf("current masterchain block: %d", b.SeqNo)

	acc, err := api.GetAccount(context.Background(), b, stonfiAddr)
	if err != nil {
		panic(err)
	}
	log.Debug().Msgf("account state: %t", acc.IsActive)

	// swap action outputs
	swapActionsCh := make(chan *SwapAction)
	go func() {
		for swapAction := range swapActionsCh {
			if swapAction == nil {
				log.Error().Err(nil).Msg("failed to read swap action")
			}

			if swapAction != nil {
				if *display == "pretty" {
					log.Info().Msgf("%s", swapAction.Pretty())
				}

				if *display == "longpretty" {
					log.Info().Msgf("%s", swapAction.LongPretty())
				}

				if *display == "verbose" {
					log.Info().Msgf("%s", swapAction.String())
				}

				sse.Notifier <- []byte(swapAction.CSV())
			}
		}
	}()

	go func() {
		for {
			time.Sleep(10 * time.Second)
			log.Debug().Msgf("processed %d transactions, took %s, rate %.3f", swapProcessedCount.Load(), time.Since(beginAt),
				float32(swapProcessedCount.Load())/float32(time.Since(beginAt).Seconds()))
		}
	}()

	priceCollector = NewPriceCollector(api)
	go priceCollector.PeriodicallyGetTONUSDPool()

	transactions := make(chan *tlb.Transaction)
	lastProcessedLT := acc.LastTxLT
	go api.SubscribeOnTransactions(context.Background(), stonfiAddr, lastProcessedLT, transactions)

	// listen for new transactions from channel
	for tx := range transactions {
		log.Debug().Msgf("new transaction: %s", base64.StdEncoding.EncodeToString(tx.Hash))
		inslice := tx.IO.In.AsInternal().Payload().BeginParse()

		if tx.IO.Out == nil {
			log.Debug().Msgf("transaction out is nil")
			continue
		}

		outMsgs, err := tx.IO.Out.ToSlice()
		if err != nil {
			log.Debug().Msgf("transaction out is not slice")
			continue
		}

		if len(outMsgs) != 1 {
			log.Debug().Msgf("transaction out is not 1")
			continue
		}

		inOp, err := inslice.LoadUInt(32)
		if err != nil {
			log.Debug().Err(err).Msgf("failed to load in op")
			continue
		}

		outslice := outMsgs[0].AsInternal().Payload().BeginParse()
		outOp, err := outslice.LoadUInt(32)
		if err != nil {
			log.Debug().Err(err).Msgf("failed to load out op")
			continue
		}

		if opJettonNotify != inOp || opStonfiSwap != outOp {
			log.Debug().Msgf("transaction op is not jetton notify or stonfi swap inOp: %X, outOp: %X, skip", inOp, outOp)
			continue
		}

		go func(innerTx *tlb.Transaction, inslice *cell.Slice, outslice *cell.Slice, poolAddr *address.Address) {
			swapAction, err := buildSwapAction(api, innerTx, inslice, outslice, poolAddr)
			if err != nil {
				log.Error().Err(err).Msg("failed to build swap action")
				return
			}

			log.Debug().Msg(strings.Repeat("-", 80))
			log.Debug().Msgf("swap action: %+v", swapAction)
			log.Debug().Msg(strings.Repeat("-", 80))

			swapProcessedCount.Add(1)
			swapActionsCh <- swapAction
		}(tx, inslice.Copy(), outslice.Copy(),
			outMsgs[0].AsInternal().DstAddr)

		// update last processed lt and save it in db
		lastProcessedLT = tx.LT
	}
}

func buildSwapAction(api ton.APIClientWrapped,
	tx *tlb.Transaction,
	in, out *cell.Slice,
	poolAddr *address.Address) (*SwapAction, error) {
	swapAction := new(SwapAction)
	swapAction.now = tx.Now
	swapAction.hash = tx.Hash
	log.Debug().Msgf("goroutine tx: %s", base64.StdEncoding.EncodeToString(tx.Hash))

	inQueryId, err := in.LoadUInt(64)
	if err != nil {
		return nil, err
	}
	log.Debug().Msgf("inQueryId: %X", inQueryId)

	coins, err := in.LoadBigCoins()
	if err != nil {
		return nil, err
	}
	log.Debug().Msgf("coins: %s", coins.String())

	swapAction.token0Coins = coins

	fromUser, err := in.LoadAddr()
	if err != nil {
		return nil, err
	}
	log.Debug().Msgf("fromUser: %s", fromUser.String())

	log.Debug().Msgf("tx: %s, fromUser: %s", base64.StdEncoding.EncodeToString(tx.Hash), fromUser.String())
	swapAction.srcWallet = fromUser
	refDs, err := in.LoadRef()
	if err != nil {
		return nil, err
	}

	transferOp, err := refDs.LoadUInt(32)
	if err != nil {
		return nil, err
	}
	if uint64(opSwap) != transferOp {
		return nil, errors.New("transfer op is not swap, skip")
	}

	tokenWallet1, err := refDs.LoadAddr()
	if err != nil {
		return nil, err
	}

	log.Debug().Msgf("tokenWallet1: %s", tokenWallet1.String())
	swapAction.dstJetton = tokenWallet1

	swapAction.srcJetton = tx.IO.In.AsInternal().SrcAddr
	if info, err := jettonMasterInfoByJettonWallet(api, swapAction.srcJetton); err != nil {
		log.Debug().Err(err).Msg("failed to get jetton master 0")
		return nil, err
	} else {
		log.Debug().Msg("assiging src jetton master")
		swapAction.srcJettonMaster = info
	}

	minOut, err := refDs.LoadCoins()
	if err != nil {
		return nil, err
	}

	log.Debug().Msgf("minOut: %d", minOut)

	swapAction.token1Coins = new(big.Int).SetUint64(minOut)

	toAddress, err := refDs.LoadAddr()
	if err != nil {
		return nil, err
	}

	log.Debug().Msgf("1 toAddress: %s", toAddress.String())

	hasRef, err := refDs.LoadUInt(1)
	if err != nil {
		return nil, err
	}
	log.Debug().Msgf("hasRef: %d", hasRef)

	outQueryId, err := out.LoadUInt(64)
	if err != nil {
		return nil, err
	}
	log.Debug().Msgf("outQueryId: %X", outQueryId)

	toAddress, err = out.LoadAddr()
	if err != nil {
		return nil, err
	}
	log.Debug().Msgf("2 toAddress: %s", toAddress.String())

	senderAddress, err := out.LoadAddr()
	if err != nil {
		return nil, err
	}
	log.Debug().Msgf("senderAddress: %s", senderAddress.String())

	if pi := priceCollector.GetItem(poolAddr.String()); pi != nil {
		swapAction.pool = pi
	} else {
		poolInfo := new(PoolInfo)
		poolInfo.addr = poolAddr
		swapAction.pool = poolInfo

		master := jetton.NewJettonMasterClient(api, poolAddr)
		log.Debug().Msgf("get_jetton_data for LP pool: %s", poolAddr.String())
		now := time.Now()
		data, err := master.GetJettonData(context.Background())
		log.Debug().Msgf("get_jetton_data took %s", time.Since(now))

		if err != nil {
			log.Debug().Err(err).Msg("failed to get jetton data, LP jetton master information missing")
		} else {
			content := data.Content.(*nft.ContentOffchain)
			poolInfo.lpOffchainURI = content.URI
			poolInfo.lpTotalSupply = data.TotalSupply
			poolInfo.lpMintable = data.Mintable
			poolInfo.lpAdminAddr = data.AdminAddr
			poolInfo.lpJetton = poolAddr
			poolInfo.FetchLpJettonMasterConfigFromURL()
		}
	}

	err = populateLPPoolInfo(api, poolAddr, swapAction.pool)
	if err != nil {
		return nil, errors.New("failed to get LP pool info")
	}

	if info, err := jettonMasterInfoByJettonWallet(api, swapAction.pool.token0Address); err != nil {
		log.Debug().Err(err).Msg("failed to get jetton master 0")
	} else {
		log.Debug().Msg("assiging jetton master 0")
		swapAction.pool.token0JettonMaster = info
	}

	if info, err := jettonMasterInfoByJettonWallet(api, swapAction.pool.token1Address); err != nil {
		log.Debug().Err(err).Msg("failed to get jetton master 1")
	} else {
		log.Debug().Msg("assiging jetton master 1")
		swapAction.pool.token1JettonMaster = info
	}

	// update price collector
	priceCollector.SetItem(poolAddr.String(), swapAction.pool)

	return swapAction, nil
}

func populateLPPoolInfo(api ton.APIClientWrapped, poolAddr *address.Address, pool *PoolInfo) error {
	log.Debug().Msgf("get LP pool data %s", poolAddr.String())
	now := time.Now()
	b, err := api.CurrentMasterchainInfo(context.Background())
	if err != nil {
		return err
	}

	res, err := api.WaitForBlock(b.SeqNo).RunGetMethod(context.Background(), b, poolAddr, "get_pool_data")
	if err != nil {
		return err
	}
	log.Debug().Msgf("get_pool_data took %s", time.Since(now))

	reserve0 := res.MustInt(0)
	pool.reserve0 = reserve0

	reserve1 := res.MustInt(1)
	pool.reserve1 = reserve1

	token0AddrSlice, err := res.Slice(2)
	if err != nil {
		return err
	}
	pool.token0Address = token0AddrSlice.MustLoadAddr()

	token1AddrSlice, err := res.Slice(3)
	if err != nil {
		return err
	}
	pool.token1Address = token1AddrSlice.MustLoadAddr()
	pool.lpFee = res.MustInt(4).Int64()
	pool.protocolFee = res.MustInt(5).Int64()
	pool.refFee = res.MustInt(6).Int64()

	_, _ = res.Slice(7)
	pool.collectedToken0ProtocolFee = res.MustInt(8)
	pool.collectedToken1ProtocolFee = res.MustInt(9)

	return nil
}

func jettonMasterInfoByJettonWallet(api ton.APIClientWrapped,
	jettonWallet *address.Address) (*JettonMasterInfo, error) {
	log.Debug().Msgf("get jetton master by jetton wallet addr: %s", jettonWallet.String())

	var jettonMasterAddr *address.Address
	jettonMasterAddr = jWalletMasterCache.Get(jettonWallet)

	if jettonMasterAddr == nil {
		now := time.Now()
		b, err := api.CurrentMasterchainInfo(context.Background())
		if err != nil {
			return nil, err
		}

		res, err := api.WaitForBlock(b.SeqNo).RunGetMethod(context.Background(), b, jettonWallet, "get_wallet_data")
		if err != nil {
			return nil, err
		}
		log.Debug().Msgf("get jetton master by jetton wallet addr: %s, take %d ms", jettonWallet.String(), time.Since(now).Milliseconds())

		_, err = res.Int(0)
		if err != nil {
			return nil, err
		}

		// owner, omit
		_, err = res.Slice(1)
		if err != nil {
			return nil, err
		}

		slice, err := res.Slice(2)
		if err != nil {
			return nil, err
		}

		jettonMasterAddr, err = slice.LoadAddr()
		if err != nil {
			return nil, err
		}

		jWalletMasterCache.Set(jettonWallet, jettonMasterAddr)
	}

	jettonMaster := new(JettonMasterInfo)
	jettonMaster.addr = jettonMasterAddr

	master := jetton.NewJettonMasterClient(api, jettonMasterAddr)
	data, err := master.GetJettonData(context.Background())
	if err != nil {
		return nil, err
	}

	jettonMaster.adminAddr = data.AdminAddr
	jettonMaster.totalSupply = data.TotalSupply
	jettonMaster.mintable = data.Mintable

	switch data.Content.(type) {
	case *nft.ContentOffchain:
		content := data.Content.(*nft.ContentOffchain)
		jettonMaster.offChainURI = content.URI
		err := jettonMaster.FetchJettonMasterConfigFromURL()
		if err != nil {
			log.Error().Err(err).Msgf("failed to fetch lp info from url")
		}
	case *nft.ContentOnchain:
		payload, _ := data.Content.(*nft.ContentOnchain)
		jettonMaster.name = payload.Name
		jettonMaster.description = payload.Description
		jettonMaster.image = payload.Image
		jettonMaster.symbol = payload.GetAttribute("symbol")
		jettonMaster.decimals, _ = strconv.Atoi(payload.GetAttribute("decimals"))

	case *nft.ContentSemichain:
		payload, _ := data.Content.(*nft.ContentSemichain)
		jettonMaster.name = payload.Name
		jettonMaster.description = payload.Description
		jettonMaster.image = payload.Image
		jettonMaster.decimals, _ = strconv.Atoi(payload.GetAttribute("decimals"))

	default:
		return nil, errors.New("unsupported content type")
	}

	return jettonMaster, nil
}
