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
	sb.WriteString(fmt.Sprintf("%s %s %s %s for %s %s at TX %s [%s] ",
		"",
		//	s(sa.srcWallet),
		sa.Action(),
		h(sa.token0Coins),
		sa.Token0Symbol(),
		h(sa.token1Coins),
		sa.Token1Symbol(),
		base64.StdEncoding.EncodeToString(sa.hash), sa.Missing()))

	if sa.pool != nil {
		sb.WriteString(fmt.Sprintf("with pool %s (reserve: %s[$ %s]/%s[$ %s]) ",
			sa.pool.symbol, h(sa.pool.reserve0),
			priceCollector.SymbolPriceFriendly(sa.pool.token0JettonMaster.symbol),
			h(sa.pool.reserve1),
			priceCollector.SymbolPriceFriendly(sa.pool.token1JettonMaster.symbol),
		))
	}

	return sb.String()
}

func (sa *SwapAction) LongPretty() string {
	return fmt.Sprintf("%s %s %s %s to %s %s with pool %s(reserve: %s/%s) at TX %s ",
		s(sa.srcWallet), sa.Action(), h(sa.token0Coins), sa.Token0Symbol(),
		h(sa.token1Coins), sa.Token1Symbol(), s(sa.pool.lpJetton), h(sa.pool.reserve0), h(sa.pool.reserve1),
		base64.StdEncoding.EncodeToString(sa.hash))
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
