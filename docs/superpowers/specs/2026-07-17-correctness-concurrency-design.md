# go-mighty Correctness & Concurrency — Design Spec

**Date**: 2026-07-17
**Status**: Approved
**Scope**: First of three hardening sub-projects identified by an adversarial review of the backend after web-frontend integration. This spec fixes game-rule correctness and move-processing concurrency. Compatibility policy: contract may break freely; the web frontend re-captures fixtures and adapts afterward.

Follow-up specs (named, not designed here):
- **Contract & Security**: per-player state redaction, WS membership checks, typed event protocol with redacted state in every event, structured JSON error codes, CORS.
- **Lifecycle & Ops**: disconnect marking, turn/abandon timeouts, Redis TTL + Postgres ledger rehydration, rate limiting, lobby pagination.

## 1. Findings Driving This Spec

Adversarial review findings fixed by this spec (file references as of commit at review time):

- **F1 — Lock result ignored**: `JoinGame` and `ProcessMove` (`internal/service/game_service.go:73,155`) call `AcquireLock` and discard the boolean. When the lock is held, `SetNX` returns `false` with `err == nil` and the caller proceeds anyway; concurrent operations interleave and lose updates.
- **F2 — Unsafe release**: `ReleaseLock` (`internal/store/redis/redis.go:143`) deletes the lock key unconditionally. An operation that outlives the 5s TTL deletes a successor's lock.
- **F3 — Non-atomic version check and save**: `CheckVersion` reads `game:{id}:version`, then `LoadGame` reads state, then `SaveGame` writes both with two independent `SET`s. Nothing prevents interleaving between these steps or state/version drift.
- **F4 — Partner never revealed**: `PartnerSeat` is never assigned anywhere in `internal/game/rules.go`. `call_partner` stores the card; playing the called card does not reveal the partner. `CalculateFinalScore` (rules.go:635) therefore counts only the declarer's tricks — scoring is wrong in every game with a partner.
- **F5 — No-friend unreachable**: `IsNoFriend` is read in scoring but no move can set it. Playing alone for doubled score is impossible.
- **F6 — Joker lead impossible with a called suit**: the design comment (rules.go:503-507) says the Joker leader specifies a suit via `card.suit`, but `HasCard` then fails because the hand holds `{none, Joker}`. Joker can only lead as suit `none`, which makes the trick lawless (no one can be forced to follow).
- **F7 — Dead pass-in-bid branch**: `ApplyMove(MoveBid)` handles `Points == 0` as a pass (rules.go:384-388), but `validateBid` rejects points < 3, so the branch is unreachable; `pass` is a separate move type.
- **F8 — All-pass dead end**: five passes set `PhaseFinished` with no contract and no scores (rules.go:413-417). Clients see a finished game that never happened.

## 2. Concurrency Design

### Lock with ownership token

`RedisStore` interface changes:

```go
AcquireLock(ctx, gameID string) (token string, err error)   // "" when not acquired after retries
ReleaseLock(ctx, gameID, token string) error
SaveGame(ctx, g *game.Game, expectedVersion int64) error     // CAS; see below
```

- `AcquireLock`: `SET game:{id}:lock <token> NX PX 5000` where token is 16 random bytes hex. On `false`, retry with 50ms/100ms/200ms backoff (3 retries). Still unavailable → return `ErrLockFailed`; handlers map it to HTTP 409 (`game busy, retry`) and WS error `"game busy"`.
- `ReleaseLock`: Lua compare-and-delete — delete only if the stored value equals `token`.

### Compare-and-swap save

`SaveGame(g, expectedVersion)` runs one Lua script:

```
if redis.call('GET', versionKey) == expectedVersion (or key missing and expectedVersion == 0)
then SET stateKey; SET versionKey g.Version; return OK
else return CONFLICT
```

Both keys keep the 24h TTL. `expectedVersion` is the version loaded at the start of the operation (before `ApplyMove` bumps it). The standalone `CheckVersion` interface method is removed; the client-supplied `client_version` is compared in the service against the loaded game's version (`g.Version != clientVersion` → `ErrStaleVersion`) before validation. Client-facing semantics are unchanged: a move tagged with a stale version is rejected.

### Service flow (`ProcessMove` and `JoinGame`)

1. `token, err := AcquireLock` — `""` → busy error (409 / WS `"game busy"`).
2. `defer ReleaseLock(token)`.
3. Load game; capture `loaded := g.Version`.
4. (`ProcessMove` only) reject if `clientVersion != loaded`.
5. Validate, apply (version bumps inside apply / join mutation).
6. `SaveGame(g, loaded)` — CAS conflict is a server bug (lock should prevent it) but returns `ErrStaleVersion` defensively.
7. Persist move to Postgres; publish event.

## 3. Friend Mechanic

### call_partner payload

New domain type replacing the bare `Card` payload:

```go
type CallPartnerMove struct {
    Card     *Card `json:"card,omitempty"`
    NoFriend bool  `json:"no_friend,omitempty"`
}
```

`ConvertPayload` accepts the new object; a bare `Card` object (has `suit`+`rank`, no `no_friend`) is still converted, easing migration. Validation: exactly one of `Card`/`NoFriend` must be present; `NoFriend` sets `g.IsNoFriend = true`, leaves `PartnerCard` nil. Both paths advance to `playing`.

### Partner reveal

In `ApplyMove(MovePlayCard)`, after adding the card to the trick: if `g.PartnerCard != nil` and the played card equals it, set `g.PartnerSeat = p.Seat`. This includes the declarer playing their own called card (self-partner): `PartnerSeat == Declarer`, no extra multiplier, trick counting unaffected (the `||` in scoring double-counts nothing).

### Called card never played

If the hand ends with `PartnerCard != nil` and `PartnerSeat == -1` (the called card was discarded to the kitty pile or simply never surfaced — with 10 tricks × 5 players every held card is played, so in practice only the kitty case), the declarer scores alone: team tricks = declarer tricks, `friendScore = 0`, and the no-friend ×2 multiplier does **not** apply. `CalculateFinalScore` already produces this behavior once `PartnerSeat` is real; the only change needed is that `friendScore = 0` when `PartnerSeat == -1` in addition to the `IsNoFriend` case.

### Scores map semantics

`Game.Scores` is documented (struct comment + API docs) as **final round scores**: declarer gets the full score, revealed partner half, all other players 0. It is not "card points taken"; clients wanting card points count `Player.Points`.

## 4. Joker Lead

`PlayCardMove` gains a field:

```go
type PlayCardMove struct {
    Card       Card `json:"card"`
    CallJoker  bool `json:"call_joker"`
    CalledSuit Suit `json:"called_suit,omitempty"` // required when leading the Joker
}
```

Validation (`validatePlayCard`): when the played card is the Joker **and** the player is leading the trick, `CalledSuit` must be one of the four real suits — otherwise `invalid move: joker lead requires called_suit`. When not leading, or not the Joker, a non-empty `CalledSuit` is rejected. The hand check matches the Joker as `{none, Joker}` (unchanged — the bug was in the *intended* contract, not `HasCard`).

Apply: when the Joker leads, `Trick.LeadSuit = CalledSuit`, so follow-suit validation works for the rest of the trick. Joker power rules are unchanged (powerless on tricks 1 and 10).

## 5. Bidding Cleanup

- Delete the unreachable `Points == 0` pass branch in `ApplyMove(MoveBid)`.
- All-pass redeal: when `len(PassedPlayers) == 5`, instead of `PhaseFinished`, reset the hand: `Bids`, `CurrentBid`, `PassedPlayers`, `Declarer` cleared, then `Start()` (fresh shuffle/deal, kitty, `PhaseBidding`, first bidder seat 0), version bumped once by the enclosing apply. Dealer rotation stays out of scope (the dealer never rotates today; noted for Lifecycle spec).

## 6. Error Surface

No new error framework in this spec (that is the Contract & Security spec). Two additions only:
- `ErrGameBusy` (`"game busy"`) — returned when the lock cannot be acquired; HTTP 409, WS `ERROR` frame with that text.
- `ErrStaleVersion` keeps its current text and mapping.

## 7. Testing

**Unit (`internal/game`, table-driven):**
- Partner reveal: opponent plays called card mid-hand → `partner_seat` set from that play onward; declarer plays own called card → self-partner; called card in kitty → hand ends with `partner_seat == -1`, declarer-alone scoring without ×2.
- No-friend: `{"no_friend": true}` sets the flag, skips partner, doubles score; payload with both card and no_friend rejected; payload with neither rejected.
- Joker lead: leading Joker without `called_suit` rejected; with `called_suit: "hearts"` → `lead_suit` hearts and followers must follow hearts; `called_suit` on a non-Joker play rejected.
- All-pass: five passes → new 10-card hands, empty bids, `bidding`, version increased.
- Scoring: regression table for declarer+partner trick counting with the reveal in place (win, lose, overtricks, multipliers, 800 cap).

**Integration (`internal/store/redis`, `-tags=integration`, real Redis):**
- 5 goroutines calling `ProcessMove` concurrently for the same game: exactly one succeeds per version; no lost updates (final version == number of successful moves + initial).
- Lock: second acquirer gets `""` while held; token release cannot delete a different token; expired lock re-acquirable.
- CAS save: save with wrong expectedVersion fails and leaves state untouched.

**E2E (`tests/e2e`, Gherkin):** extend the game-flow feature with a friend-reveal scenario (declarer calls a card, holder plays it, final scores split declarer/partner) and a no-friend scenario.

**Cross-repo follow-up (not in this plan):** re-run `npm run capture` in `mighty-frontend` and adapt the client (`call_partner` payload, `called_suit`, redeal handling).

## 8. Out of Scope

State redaction and WS membership (Contract & Security spec), disconnect/timeout/rehydration (Lifecycle & Ops spec), dealer rotation, multi-hand sessions, error-code framework, CORS, docs overhaul beyond the `Scores` comment and the payload shapes changed here.
