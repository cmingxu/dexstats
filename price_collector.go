package main

import (
	"context"
	"errors"
	"math/big"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
	"github.com/xssnick/tonutils-go/address"
	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/ton"
)

var (
	tonSymbol  = "pTON"
	usdtSymbol = "jUSDT"
)

var (
	interval = time.Second * 10
)

var (
	tonJUSDPoolAddr = address.MustParseAddr("EQAKleHU6-eGDQUfi4YXMNve4UQP0RGAIRkU4AiRRlgDUbaM")
)

var (
	tonPrice = big.NewInt(0)
	usdPrice = big.NewInt(1000_000)
)

type Currency struct {
	decimal int
	value   *big.Int
}

type PriceCollector struct {
	api ton.APIClientWrapped

	poolInfoMap map[string]*PoolInfo
	mutex       sync.Mutex

	tonUsdPoolInfo *PoolInfo
	priceMap       map[string]Currency
}

func NewPriceCollector(api ton.APIClientWrapped) *PriceCollector {
	pc := &PriceCollector{
		api: api,
	}

	pc.poolInfoMap = make(map[string]*PoolInfo)
	pc.mutex = sync.Mutex{}
	pc.priceMap = make(map[string]Currency)

	pc.tonUsdPoolInfo = &PoolInfo{
		addr:     tonJUSDPoolAddr,
		reserve0: big.NewInt(1),
		reserve1: big.NewInt(1),
		symbol:   "jUSDT-pTON LP",
	}
	return pc
}

func (pic *PriceCollector) SymbolPriceFriendly(symbol string) string {
	c, err := pic.SymbolPrice(symbol)
	if err != nil {
		return "N/A"
	}

	return c.String()
}

func (pic *PriceCollector) SymbolPrice(symbol string) (tlb.Coins, error) {
	price, ok := pic.priceMap[symbol]
	if !ok {
		return tlb.Coins{}, errors.New("price not found")
	}
	return tlb.FromNano(price.value, price.decimal)
}

func (pic *PriceCollector) GetItem(key string) *PoolInfo {
	pic.mutex.Lock()
	defer pic.mutex.Unlock()

	return pic.poolInfoMap[key]
}

func (pic *PriceCollector) SetItem(key string, pi *PoolInfo) {
	pic.mutex.Lock()
	defer pic.mutex.Unlock()

	pic.poolInfoMap[key] = pi
	pic.updatePriceMap()
}

func (pic *PriceCollector) DeleteItem(key string) {
	pic.mutex.Lock()
	defer pic.mutex.Unlock()

	delete(pic.poolInfoMap, key)
}

func (pic *PriceCollector) TonPrice() *big.Int {
	return tonPrice
}

func (pic *PriceCollector) displayPriceMap() {
	for k, v := range pic.priceMap {
		coin, _ := tlb.FromNano(v.value, v.decimal)
		log.Debug().Msgf("%s: %s", k, coin.String())
	}
}

func (pic *PriceCollector) PeriodicallyGetTONUSDPool() {
	pic.getTONUSDPoolData(pic.api)
	pic.updateBasePrice()

	ticker := time.NewTicker(interval)
	for {
		select {
		case <-ticker.C:
			pic.getTONUSDPoolData(pic.api)
			pic.updateBasePrice()
			pic.updatePriceMap()

			log.Debug().Msgf("latest ton price %s", tlb.FromNanoTON(pic.TonPrice()))
		}
	}
}

func (pic *PriceCollector) getTONUSDPoolData(api ton.APIClientWrapped) error {
	log.Debug().Msgf("get ton USDT LP pool data %s", tonJUSDPoolAddr.String())

	now := time.Now()
	b, err := api.CurrentMasterchainInfo(context.Background())
	if err != nil {
		return err
	}

	res, err := api.WaitForBlock(b.SeqNo).RunGetMethod(context.Background(), b, tonJUSDPoolAddr, "get_pool_data")
	if err != nil {
		return err
	}
	log.Debug().Msgf("get_pool_data for ton USDT LP pool took %s", time.Since(now))

	reserve0 := res.MustInt(0)
	pic.tonUsdPoolInfo.reserve0 = reserve0

	reserve1 := res.MustInt(1)
	pic.tonUsdPoolInfo.reserve1 = reserve1

	token0AddrSlice, err := res.Slice(2)
	if err != nil {
		return err
	}
	pic.tonUsdPoolInfo.token0Address = token0AddrSlice.MustLoadAddr()

	token1AddrSlice, err := res.Slice(3)
	if err != nil {
		return err
	}
	pic.tonUsdPoolInfo.token1Address = token1AddrSlice.MustLoadAddr()
	pic.tonUsdPoolInfo.lpFee = res.MustInt(4).Int64()
	pic.tonUsdPoolInfo.protocolFee = res.MustInt(5).Int64()
	pic.tonUsdPoolInfo.refFee = res.MustInt(6).Int64()

	_, _ = res.Slice(7)
	pic.tonUsdPoolInfo.collectedToken0ProtocolFee = res.MustInt(8)
	pic.tonUsdPoolInfo.collectedToken1ProtocolFee = res.MustInt(9)

	return nil
}

func (pic *PriceCollector) updateBasePrice() {
	if pic.tonUsdPoolInfo != nil {
		log.Debug().Msgf("update ton price")
		log.Debug().Msgf("reserve0 %s", pic.tonUsdPoolInfo.reserve0.String())
		log.Debug().Msgf("reserve1 %s", pic.tonUsdPoolInfo.reserve1.String())

		tonPrice = calculatePriceWithReserveP1(pic.tonUsdPoolInfo.reserve0, pic.tonUsdPoolInfo.reserve1, 6, 9, usdPrice)
		pic.priceMap[tonSymbol] = Currency{
			decimal: 9,
			value:   tonPrice,
		}

		pic.priceMap[usdtSymbol] = Currency{
			decimal: 6,
			value:   usdPrice,
		}

		log.Debug().Msgf("new ton price %s", tlb.FromNanoTON(tonPrice).String())

	}
}

// suppose each jetton decimal is 9
func calculatePriceWithReserveP0(reserve0, reserve1 *big.Int, d0, d1 int, price1 *big.Int) *big.Int {
	log.Debug().Msgf("calculate price with reserve0 %s, reserve1 %s, d0 %d, d1 %d, price1 %s", reserve0.String(), reserve1.String(), d0, d1, price1.String())
	t1cap := new(big.Float).Quo(
		// reserve1 * price1
		new(big.Float).Mul(
			new(big.Float).SetInt(reserve1),
			new(big.Float).SetInt(price1),
		),

		// 10 ** (decimals)
		new(big.Float).SetInt(new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(d1)), nil)),
	)

	t0price := new(big.Float).Quo(
		// t1cap * decimal
		new(big.Float).Mul(t1cap, new(big.Float).SetInt(new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(d0)), nil))),
		// reserve0
		new(big.Float).SetInt(reserve0),
	)

	priceInt, _ := t0price.Int(new(big.Int))

	log.Debug().Msgf("calulated new price0 %s", priceInt.String())

	return priceInt
}

// suppose each jetton decimal is 9
func calculatePriceWithReserveP1(reserve0, reserve1 *big.Int, d0, d1 int, price0 *big.Int) *big.Int {
	log.Debug().Msgf("calculate price with reserve0 %s, reserve1 %s, d0 %d, d1 %d, price0 %s", reserve0.String(), reserve1.String(), d0, d1, price0.String())

	r0 := new(big.Float).SetInt(reserve0)
	r1 := new(big.Float).SetInt(reserve1)
	p0 := new(big.Float).SetInt(price0)

	if d0 > d1 {
		r1.Mul(r1, new(big.Float).SetInt(new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(d0-d1)), nil)))
	} else {
		r0.Mul(r0, new(big.Float).SetInt(new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(d1-d0)), nil)))
	}

	t1price := new(big.Float).Quo(
		// reserve0 * price0
		new(big.Float).Mul(r0, p0),
		r1,
	)

	priceInt, _ := t1price.Int(new(big.Int))
	log.Debug().Msgf("calulated new price1 %s", priceInt.String())
	return priceInt

	//t0cap := new(big.Float).Mul(
	//	// reserve0 * price0
	//	new(big.Float).Mul(new(big.Float).SetInt(reserve0), new(big.Float).SetInt(price0)),

	//	// 10 ** (decimals)
	//	new(big.Float).SetInt(new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(d1)), nil)),
	//)

	//fmt.Printf("t0cap %s\n", t0cap.String())
	//fmt.Printf("1110 %s\n", new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(d1)), nil).String())

	//t1price := new(big.Float).Quo(
	//	// t0cap
	//	t0cap,
	//	new(big.Float).Mul(new(big.Float).SetInt(reserve1), new(big.Float).SetInt(new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(d0)), nil))),
	//)

	//fmt.Printf("t1price %s\n", t1price.String())
	//priceInt, _ := t1price.Int(new(big.Int))
	//log.Debug().Msgf("calulated new price1 %s", priceInt.String())

	//return priceInt

}

func (pic *PriceCollector) updatePriceMap() {
	//pic.mutex.Lock()
	//defer pic.mutex.Unlock()

	// shortcut if ton price is missing
	if pic.tonUsdPoolInfo == nil {
		return
	}

	for _, pi := range pic.poolInfoMap {
		if pi.token0JettonMaster == nil || pi.token1JettonMaster == nil {
			continue
		}

		log.Debug().Msgf("update price for %s-%s, current ton price is %s", pi.token0JettonMaster.symbol, pi.token1JettonMaster.symbol, tonPrice.String())

		if pi.token0JettonMaster.symbol == tonSymbol {
			newToken1Price := calculatePriceWithReserveP1(
				pi.reserve0,
				pi.reserve1,
				pi.token0JettonMaster.decimals,
				pi.token1JettonMaster.decimals,
				tonPrice)
			pic.priceMap[pi.token1JettonMaster.symbol] = Currency{
				decimal: pi.token1JettonMaster.decimals,
				value:   newToken1Price,
			}
		}

		if pi.token1JettonMaster.symbol == tonSymbol {
			newToken0Price := calculatePriceWithReserveP0(pi.reserve0,
				pi.reserve1,
				pi.token0JettonMaster.decimals,
				pi.token1JettonMaster.decimals,
				tonPrice)
			pic.priceMap[pi.token0JettonMaster.symbol] = Currency{
				decimal: pi.token0JettonMaster.decimals,
				value:   newToken0Price,
			}
		}

		if pi.token0JettonMaster.symbol == usdtSymbol {
			newToken1Price := calculatePriceWithReserveP1(pi.reserve0,
				pi.reserve1,
				6,
				pi.token1JettonMaster.decimals,
				usdPrice)
			pic.priceMap[pi.token1JettonMaster.symbol] = Currency{
				decimal: pi.token1JettonMaster.decimals,
				value:   newToken1Price,
			}
		}

		if pi.token1JettonMaster.symbol == usdtSymbol {
			newToken0Price := calculatePriceWithReserveP0(pi.reserve0,
				pi.reserve1,
				pi.token0JettonMaster.decimals,
				6,
				usdPrice)
			pic.priceMap[pi.token0JettonMaster.symbol] = Currency{
				decimal: pi.token0JettonMaster.decimals,
				value:   newToken0Price,
			}
		}
	}

	pic.displayPriceMap()
}
