// Command deriveapikeys derives (or creates) the Polymarket L2 API credentials
// for the wallet in TRADER_PRIVATE_KEY by signing the L1 ClobAuth EIP-712
// message and calling the CLOB. Prints export lines for .env.
//
//	TRADER_PRIVATE_KEY=... go run ./tools/deriveapikeys           # derive existing
//	TRADER_PRIVATE_KEY=... go run ./tools/deriveapikeys -create   # create new
//
// NOTE: the CLOB auth endpoint geofences US persons; this will fail from a
// restricted jurisdiction. The signing is correct regardless.
package main

import (
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"math/big"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

const (
	defaultCLOB = "https://clob.polymarket.com"
	chainID     = 137
	authMessage = "This message attests that I control the given wallet"
)

func keccak(b ...[]byte) []byte { return crypto.Keccak256(b...) }
func pad32(b []byte) []byte     { o := make([]byte, 32); copy(o[32-len(b):], b); return o }

// clobAuthDigest builds the EIP-712 digest for the L1 ClobAuth message.
func clobAuthDigest(addr common.Address, timestamp string, nonce int64) []byte {
	domainType := keccak([]byte("EIP712Domain(string name,string version,uint256 chainId)"))
	domainSep := keccak(domainType,
		keccak([]byte("ClobAuthDomain")), keccak([]byte("1")), pad32(big.NewInt(chainID).Bytes()))
	structHash := keccak(
		keccak([]byte("ClobAuth(address address,string timestamp,uint256 nonce,string message)")),
		pad32(addr.Bytes()),
		keccak([]byte(timestamp)),
		pad32(big.NewInt(nonce).Bytes()),
		keccak([]byte(authMessage)),
	)
	return keccak([]byte{0x19, 0x01}, domainSep, structHash)
}

func main() {
	create := flag.Bool("create", false, "create a new API key (default: derive existing)")
	clob := flag.String("clob", defaultCLOB, "CLOB base URL")
	flag.Parse()

	privHex := strings.TrimPrefix(os.Getenv("TRADER_PRIVATE_KEY"), "0x")
	if privHex == "" {
		log.Fatal("TRADER_PRIVATE_KEY is required")
	}
	key, err := crypto.HexToECDSA(privHex)
	if err != nil {
		log.Fatalf("private key: %v", err)
	}
	addr := crypto.PubkeyToAddress(key.PublicKey)
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	const nonce = 0

	sig, err := crypto.Sign(clobAuthDigest(addr, ts, nonce), key)
	if err != nil {
		log.Fatalf("sign: %v", err)
	}
	sig[64] += 27

	method, path := http.MethodGet, "/auth/derive-api-key"
	if *create {
		method, path = http.MethodPost, "/auth/api-key"
	}
	req, _ := http.NewRequest(method, *clob+path, nil)
	req.Header.Set("POLY_ADDRESS", addr.Hex())
	req.Header.Set("POLY_SIGNATURE", "0x"+hex.EncodeToString(sig))
	req.Header.Set("POLY_TIMESTAMP", ts)
	req.Header.Set("POLY_NONCE", strconv.Itoa(nonce))

	resp, err := (&http.Client{Timeout: 15 * time.Second}).Do(req)
	if err != nil {
		log.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		log.Fatalf("CLOB %s %d: %s\n(US persons are geofenced — this is expected from a restricted region)", path, resp.StatusCode, body)
	}

	var creds struct {
		APIKey     string `json:"apiKey"`
		Secret     string `json:"secret"`
		Passphrase string `json:"passphrase"`
	}
	if err := json.Unmarshal(body, &creds); err != nil {
		log.Fatalf("decode: %v (body: %s)", err, body)
	}
	fmt.Printf("# L2 API credentials for %s\n", addr.Hex())
	fmt.Printf("POLYMARKET_API_KEY=%s\n", creds.APIKey)
	fmt.Printf("POLYMARKET_API_SECRET=%s\n", creds.Secret)
	fmt.Printf("POLYMARKET_API_PASSPHRASE=%s\n", creds.Passphrase)
	fmt.Printf("TRADER_FUNDER_ADDRESS=%s\n", addr.Hex())
}
