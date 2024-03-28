package main

import (
	"context"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"strings"
	"time"

	"github.com/rs/zerolog/log"
	"github.com/xssnick/tonutils-go/address"
	"github.com/xssnick/tonutils-go/ton"
)

type PoolInfo struct {
	addr *address.Address

	token0JettonMaster *JettonMasterInfo
	token1JettonMaster *JettonMasterInfo

	token0Address *address.Address
	token1Address *address.Address

	reserve0 *big.Int
	reserve1 *big.Int

	lpFee       int64
	protocolFee int64
	refFee      int64

	collectedToken0ProtocolFee *big.Int
	collectedToken1ProtocolFee *big.Int

	lpJetton      *address.Address
	lpAdminAddr   *address.Address
	lpTotalSupply *big.Int
	lpMintable    bool
	lpOffchainURI string

	symbol      string
	name        string
	description string
	decimals    int
	image       string
}

func (pi *PoolInfo) String() string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("token0Address: %s\n", pi.token0Address.String()))
	sb.WriteString(fmt.Sprintf("token1Address: %s\n", pi.token1Address.String()))
	sb.WriteString(fmt.Sprintf("reserve0: %s\n", pi.reserve0.String()))
	sb.WriteString(fmt.Sprintf("reserve1: %s\n", pi.reserve1.String()))
	sb.WriteString(fmt.Sprintf("lpFee: %d\n", pi.lpFee))
	sb.WriteString(fmt.Sprintf("protocolFee: %d\n", pi.protocolFee))
	sb.WriteString(fmt.Sprintf("refFee: %d\n", pi.refFee))
	sb.WriteString(fmt.Sprintf("collectedToken0ProtocolFee: %s\n", pi.collectedToken0ProtocolFee.String()))
	sb.WriteString(fmt.Sprintf("collectedToken1ProtocolFee: %s\n", pi.collectedToken1ProtocolFee.String()))
	if pi.lpJetton != nil {
		sb.WriteString(fmt.Sprintf("lpJetton: %s\n", pi.lpJetton.String()))
	}

	if pi.lpAdminAddr != nil {
		sb.WriteString(fmt.Sprintf("lpAdminAddr: %s\n", pi.lpAdminAddr.String()))
	}

	if pi.lpTotalSupply != nil {
		sb.WriteString(fmt.Sprintf("lpTotalSupply: %s\n", pi.lpTotalSupply.String()))
	}

	sb.WriteString(fmt.Sprintf("lpMintable: %t\n", pi.lpMintable))
	sb.WriteString(fmt.Sprintf("url: %s\n", pi.lpOffchainURI))
	sb.WriteString(fmt.Sprintf("symbol: %s\n", pi.symbol))
	sb.WriteString(fmt.Sprintf("name: %s\n", pi.name))
	sb.WriteString(fmt.Sprintf("description: %s\n", pi.description))
	sb.WriteString(fmt.Sprintf("decimals: %d\n", pi.decimals))
	sb.WriteString(fmt.Sprintf("image: %s\n", pi.image))

	if pi.token0JettonMaster != nil {
		sb.WriteString("===== token0JettonMaster ===== \n")
		sb.WriteString(pi.token0JettonMaster.String())
	}

	if pi.token1JettonMaster != nil {
		sb.WriteString("===== token1JettonMaster ====== \n")
		sb.WriteString(pi.token1JettonMaster.String())
	}

	return sb.String()
}

func (pi *PoolInfo) FetchLpJettonMasterConfigFromURL() error {
	log.Debug().Msgf("fetching lp jetton master config from %s", pi.lpOffchainURI)
	now := time.Now()
	resp, err := http.Get(pi.lpOffchainURI)
	if err != nil {
		return err
	}

	log.Debug().Msgf("fetching lp info from %s done takes %d ms", pi.lpOffchainURI, time.Since(now).Milliseconds())

	defer resp.Body.Close()
	type Content struct {
		Symbol      string `json:"symbol"`
		Name        string `json:"name"`
		Description string `json:"description"`
		Decimals    int    `json:"decimals"`
		Image       string `json:"image"`
	}

	var content Content
	err = json.NewDecoder(resp.Body).Decode(&content)
	if err != nil {
		return err
	}

	pi.symbol = content.Symbol
	pi.name = content.Name
	pi.description = content.Description
	pi.decimals = content.Decimals
	pi.image = content.Image

	return nil
}

// use regex to match token first with whole as "Token0-Token1 LP"
func (pi *PoolInfo) GetToken0Symbol() string {
	if pi.lpJetton == nil {
		return "unknown"
	}

	return pi.symbol[:strings.Index(pi.symbol, "-")]
}

func (pi *PoolInfo) GetToken1Symbol() string {
	if pi.lpJetton == nil {
		return "unknown"
	}

	startIndex := strings.Index(pi.symbol, "-") + 1
	endIndex := strings.LastIndex(pi.symbol, " ")
	return pi.symbol[startIndex:endIndex]
}

func (pi *PoolInfo) UpdateReserve(api ton.APIClientWrapped) error {
	log.Debug().Msgf("update reserve0 and reserve1: %s", pi.addr.String())
	now := time.Now()
	b, err := api.CurrentMasterchainInfo(context.Background())
	if err != nil {
		return err
	}

	res, err := api.WaitForBlock(b.SeqNo).RunGetMethod(context.Background(), b, pi.addr, "get_pool_data")
	if err != nil {
		return err
	}
	log.Debug().Msgf("update reserve0 and reserve1: %s, take %d ms", pi.addr.String(), time.Since(now).Milliseconds())

	reserve0, err := res.Int(0)
	if err != nil {
		return err
	}
	pi.reserve0 = reserve0

	reserve1, err := res.Int(1)
	if err != nil {
		return err
	}
	pi.reserve1 = reserve1

	log.Debug().Msgf("new reserve0: %s, reserve1: %s", pi.reserve0.String(), pi.reserve1.String())

	return nil
}
