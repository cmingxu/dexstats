package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"math/big"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/rs/zerolog/log"
	"github.com/xssnick/tonutils-go/address"
)

type JettonMasterInfo struct {
	addr *address.Address

	offChainURI string
	totalSupply *big.Int
	mintable    bool
	adminAddr   *address.Address

	symbol      string
	name        string
	description string
	decimals    int
	image       string
}

func (info *JettonMasterInfo) FetchJettonMasterConfigFromURL() error {
	log.Debug().Msgf("fetching info from %s", info.offChainURI)

	var body []byte
	if masterOffChainDataCache.Get(info.offChainURI) != nil {
		body = masterOffChainDataCache.Get(info.offChainURI)
	} else {
		now := time.Now()
		resp, err := http.Get(info.offChainURI)
		if err != nil {
			return err
		}
		log.Debug().Msgf("fetching info from %s done take %d ms", info.offChainURI, time.Since(now).Milliseconds())

		defer resp.Body.Close()
		body, err = ioutil.ReadAll(resp.Body)
		if err != nil {
			return err
		}

		masterOffChainDataCache.Set(info.offChainURI, body)
	}

	type Content struct {
		Symbol      string `json:"symbol"`
		Name        string `json:"name"`
		Description string `json:"description"`
		Decimals    string `json:"decimals"`
		Image       string `json:"image"`
	}

	type ContentIntDecimal struct {
		Symbol      string `json:"symbol"`
		Name        string `json:"name"`
		Description string `json:"description"`
		Decimals    int    `json:"decimals"`
		Image       string `json:"image"`
	}

	var content Content
	err := json.Unmarshal(body, &content)
	if err != nil {
		var ci ContentIntDecimal
		err = json.Unmarshal(body, &ci)
		if err != nil {
			return err
		}
		info.decimals = ci.Decimals
		info.image = ci.Image
		info.symbol = ci.Symbol
		info.name = ci.Name
		info.description = ci.Description
	} else {
		info.symbol = content.Symbol
		info.name = content.Name
		info.description = content.Description
		info.decimals, _ = strconv.Atoi(content.Decimals)
		info.image = content.Image
	}

	return nil
}

func (ji *JettonMasterInfo) String() string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("addr: %s\n", ji.addr.String()))
	sb.WriteString(fmt.Sprintf("offChainURI: %s\n", ji.offChainURI))
	sb.WriteString(fmt.Sprintf("totalSupply: %s\n", ji.totalSupply.String()))
	sb.WriteString(fmt.Sprintf("mintable: %t\n", ji.mintable))
	sb.WriteString(fmt.Sprintf("adminAddr: %s\n", ji.adminAddr.String()))
	sb.WriteString(fmt.Sprintf("symbol: %s\n", ji.symbol))
	sb.WriteString(fmt.Sprintf("name: %s\n", ji.name))
	sb.WriteString(fmt.Sprintf("description: %s\n", ji.description))
	sb.WriteString(fmt.Sprintf("decimals: %d\n", ji.decimals))

	return sb.String()
}

// use to cache jetton addr to jetton master addr cache
// jetton addr are those store in a pool
// jetton master addr are corresponding jetton master, save get_jetton_data request
type JettonWalletJettonMasterAddrCache struct {
	m map[*address.Address]*address.Address
}

func NewJettonWalletJettonMasterAddrCache() *JettonWalletJettonMasterAddrCache {
	return &JettonWalletJettonMasterAddrCache{
		m: make(map[*address.Address]*address.Address),
	}
}

func (c *JettonWalletJettonMasterAddrCache) Get(addr *address.Address) *address.Address {
	return c.m[addr]
}

func (c *JettonWalletJettonMasterAddrCache) Set(addr *address.Address, masterAddr *address.Address) {
	c.m[addr] = masterAddr
}

// use to cache jetton master addr to jetton master offchain data cache
type JettonMasterOffChainDataCache struct {
	m map[string][]byte
}

func NewJettonMasterOffChainDataCache() *JettonMasterOffChainDataCache {
	return &JettonMasterOffChainDataCache{
		m: make(map[string][]byte),
	}
}

func (c *JettonMasterOffChainDataCache) Get(addr string) []byte {
	return c.m[addr]
}

func (c *JettonMasterOffChainDataCache) Set(addr string, data []byte) {
	c.m[addr] = data
}
