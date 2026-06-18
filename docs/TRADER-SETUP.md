# Trader setup (live order) — proxy wallet

> ⚠️ **Eligibility:** Polymarket geofences US persons; trading from a restricted
> region is blocked at the API and against their ToS. Only proceed where you are
> permitted to trade. This guide does not help bypass geofencing.

> ⚠️ **Real money.** Use a **dedicated wallet** funded with a small, capped
> amount. The trader is the sole key holder; the private key goes in a k8s
> Secret, never in git.

## Wallet model

Polymarket holds trading funds in a **proxy** controlled by your EOA:

- **signer** = your EOA (holds the private key, signs orders) → `TRADER_PRIVATE_KEY`
- **maker**  = the proxy/Gnosis-Safe that holds USDC → `TRADER_MAKER_ADDRESS`
- **signature type** → `TRADER_SIGNATURE_TYPE`: `1` = Polymarket proxy, `2` = Gnosis Safe.
  (`0` = plain EOA, only if the EOA itself holds funds and approvals — simplest for a test wallet.)

## 1. Fund + allowances (one-time, on Polygon)

In the funds-holding wallet (proxy or EOA):

- Hold **USDC.e** (`0x2791bca1...84174`).
- Approve the exchange to move funds:
  - **USDC.e `approve`** the CTF Exchange (`0xE111180000d2663C0091e4f400237545B87B996B`) — and the
    NegRisk exchange (`0xC5d563A36AE78145C45a50134d48A1215220f80a`) if trading neg-risk markets.
  - **ConditionalTokens `setApprovalForAll`** (CTF `0x4D97DCd97eC945f40cF65F87097ACe5EA0476045`)
    for the same exchange(s) — required to **sell**.

The Polymarket web app sets these approvals for you when you enable trading on a
proxy/Safe (the recommended path). For a plain EOA test wallet you can send the
two approvals directly. CLOB matching is gasless; you only pay gas for these
one-time approvals.

## 2. Derive L2 API credentials

```bash
TRADER_PRIVATE_KEY=0x... make trader-keys          # derive existing
TRADER_PRIVATE_KEY=0x... make trader-keys ARGS=-create   # first time
```

This signs the L1 `ClobAuth` EIP-712 message and prints `POLYMARKET_API_KEY/
SECRET/PASSPHRASE` + `TRADER_FUNDER_ADDRESS` to paste into your Secret. (Geofenced
for US — the signing is correct, but the CLOB call will reject restricted regions.)

## 3. Configure the trader

```
TRADER_PRIVATE_KEY=...            # k8s Secret only
POLYMARKET_API_KEY=...            # from step 2
POLYMARKET_API_SECRET=...
POLYMARKET_API_PASSPHRASE=...
TRADER_FUNDER_ADDRESS=0x...       # signer EOA (from step 2)
TRADER_MAKER_ADDRESS=0x...        # proxy/Safe holding funds
TRADER_SIGNATURE_TYPE=1           # 1 proxy | 2 gnosis | 0 EOA
TRADER_EXCHANGE_ADDRESS=0xE11118... # CTF (default) or NegRisk per market
EXECUTION_MODE=approval           # approval | auto | auto_below:<usd>
MAX_USD_PER_TRADE=25
MAX_USD_PER_DAY=200
MAX_OPEN_EXPOSURE_USD=500
```

## 4. First real order (tiny, verified)

Before trusting it: confirm the live `/order` payload shape (field names,
`metadata`/`builder`, side string) against the CLOB, then place a **~$1** order
on a liquid market from the dedicated wallet, confirm the fill, and verify the
caps + `/halt` behave. The gated integration test:

```bash
RPC_WSS_URL=... go test -tags=integration ./services/trader/...   # (to be added)
```

A failed placement is intentionally **not** auto-retried (no double-place); a
reconciler against the CLOB by order id is Phase 9.
