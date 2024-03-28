package main

import (
	"encoding/base64"
	"fmt"
	"math/big"
	"regexp"
	"strings"

	"github.com/xssnick/tonutils-go/address"
	"github.com/xssnick/tonutils-go/tlb"
)

type BuyOrSell string

const (
	Buy     BuyOrSell = "buy"
	Sell    BuyOrSell = "sell"
	Unknown BuyOrSell = "unknown"
)

type SwapAction struct {
	srcJettonMaster *JettonMasterInfo

	srcWallet *address.Address
	srcJetton *address.Address
	dstJetton *address.Address

	token0Coins *big.Int
	token1Coins *big.Int

	pool *PoolInfo

	now  uint32
	hash []byte
}

func (sa *SwapAction) String() string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Hash: %s\n", base64.StdEncoding.EncodeToString(sa.hash)))
	sb.WriteString(fmt.Sprintf("Action: %s\n", sa.Action()))
	sb.WriteString(fmt.Sprintf("SrcWallet: %s\n", sa.srcWallet.String()))
	sb.WriteString(fmt.Sprintf("SrcJetton: %s\n", sa.srcJetton.String()))
	sb.WriteString(fmt.Sprintf("DstJetton: %s\n", sa.dstJetton.String()))
	sb.WriteString(fmt.Sprintf("InCoins: %s\n", sa.token0Coins.String()))
	sb.WriteString(fmt.Sprintf("OutCoins: %s\n", sa.token1Coins.String()))
	sb.WriteString(fmt.Sprintf("Now: %d\n", sa.now))

	if sa.pool != nil {
		sb.WriteString("=====  pool ==== \n")
		sb.WriteString(sa.pool.String())
	}

	return sb.String()
}

func (sa *SwapAction) Pretty() string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("%s %s %s %s for %s %s at TX %s",
		s(sa.srcWallet),
		sa.Action(),
		h(sa.token0Coins),
		sa.Token0Symbol(),
		h(sa.token1Coins),
		sa.Token1Symbol(),
		base64.StdEncoding.EncodeToString(sa.hash)))

	if sa.pool != nil {
		sb.WriteString(fmt.Sprintf("POOL %s (reserve: %s[$ %s]/%s[$ %s]) ",
			sa.pool.symbol, h(sa.pool.reserve0),
			priceCollector.SymbolPriceFriendly(sa.pool.token0JettonMaster.symbol),
			h(sa.pool.reserve1),
			priceCollector.SymbolPriceFriendly(sa.pool.token1JettonMaster.symbol),
		))
	}

	return sb.String()
}

// a. Name: token name
// b. Symbol: token symbol
// c. Tx Hash
// d. Trader wallet
// e. Marketcap
// f. Token amt swapped
// g. Amnt of Ton swapped
// h. Type: buy or sell
// I. Token price in usd
// j. Ton price in usd
// k. Liquidity reserves
// l. Token balance held by the wallet (of the traded token)
// m. Total supply

func (sa *SwapAction) LongPretty() string {
	p0, err := priceCollector.SymbolPrice(sa.pool.token0JettonMaster.symbol)
	if err != nil {
		p0 = tlb.Coins{}
	}

	t0Market := new(big.Int).Mul(sa.pool.token0JettonMaster.totalSupply, p0.Nano())

	p1, err := priceCollector.SymbolPrice(sa.pool.token1JettonMaster.symbol)
	if err != nil {
		p1 = tlb.Coins{}
	}

	t1Market := new(big.Int).Mul(sa.pool.token1JettonMaster.totalSupply, p1.Nano())

	items := []string{
		sa.pool.token0JettonMaster.name,
		sa.pool.token0JettonMaster.symbol,
		base64.StdEncoding.EncodeToString(sa.hash),
		sa.srcWallet.String(),
		t0Market.String(),
		sa.token0Coins.String(),
		sa.token1Coins.String(),
		string(sa.Action()),
		p0.String(),
		tonPrice.String(),
		sa.pool.reserve0.String(),
		sa.pool.reserve1.String(),
		"", // token balance held by the wallet
		sa.pool.token0JettonMaster.totalSupply.String(),
		sa.pool.token1JettonMaster.totalSupply.String(),
		t1Market.String(),
	}

	return strings.Join(items, ",")
}

func (sa *SwapAction) CSV() string {
	return sa.LongPretty()
}

func (sa *SwapAction) p(sym string) string {
	return "nil"
}

func (sa *SwapAction) Action() BuyOrSell {
	if sa.srcJettonMaster == nil {
		return Unknown
	}

	if sa.pool == nil {
		return Unknown
	}

	if sa.srcJettonMaster.symbol == sa.pool.GetToken0Symbol() {
		return Buy
	} else {
		return Sell
	}
}

func (sa *SwapAction) Token0Symbol() string {
	if sa.pool.token0JettonMaster != nil {
		return sa.pool.token0JettonMaster.symbol
	}

	return "unknown"
}

func (sa *SwapAction) Token0Name() string {
	if sa.pool.token0JettonMaster != nil {
		return sa.pool.token0JettonMaster.name
	}

	return "unknown"
}

func (sa *SwapAction) Token1Symbol() string {
	if sa.pool.token1JettonMaster != nil {
		return sa.pool.token1JettonMaster.symbol
	}

	return "unknown"
}

func (sa *SwapAction) Token1Name() string {
	if sa.pool.token1JettonMaster != nil {
		return sa.pool.token1JettonMaster.name
	}

	return "unknown"
}

func (sa *SwapAction) Missing() string {
	var sb strings.Builder
	if sa.pool == nil {
		sb.WriteString("X")
	} else {
		sb.WriteString("O")
	}

	if sa.pool.token0JettonMaster == nil {
		sb.WriteString("X")
	} else {
		sb.WriteString("O")
	}

	if sa.pool.token1JettonMaster == nil {
		sb.WriteString("X")
	} else {
		sb.WriteString("O")
	}

	return sb.String()
}

func s(addr *address.Address) string {
	if addr == nil {
		return "nil"
	}

	reg := regexp.MustCompile(`^(.{4}).*(.{4})$`)
	return reg.ReplaceAllString(addr.String(), "$1...$2")
}

func ss(addr string) string {
	reg := regexp.MustCompile(`^(.{4}).*(.{4})$`)
	return reg.ReplaceAllString(addr, "$1...$2")
}

func h(v *big.Int) string {
	if v == nil {
		return "nil"
	}

	coins := tlb.MustFromNano(v, 9)
	return coins.String()
}
