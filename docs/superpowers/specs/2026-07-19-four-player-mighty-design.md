# Four-Player Mighty (Jun/Chulmin variant) — Design

**Date:** 2026-07-19
**Scope:** `go-mighty` engine + `mighty-frontend` client. (Swift client out of scope.)
**Status:** Approved

## Goal

Add support for the four-player Mighty variant alongside the existing five-player
game, and make the recently-landed official scoring engine
(`CalculateFinalScore`) correct for four-player play. A single `GameConfig`
carried on `Game` centralizes every difference between the variants so the rest
of the engine reads config instead of hardcoding `5`, `13`, etc. Five-player
behavior must be byte-for-byte unchanged.

### The four-player variant (rules)

- Remove all four **2s**, all four **4s**, and the two **red 3s** (♥3, ♦3) from
  the 53-card deck, leaving **43 cards**: 10 per player (×4) + 3 in the blind
  (kitty).
- **Minimum bid is 14** scoring cards (2-vs-2 is easier for the declarer's team
  than 2-vs-3). Maximum stays 20.
- Declarer calls a partner as in the five-player game.
- Score is based on the minimum bid **M = 14**.
- **Win (2-vs-2):** winning players receive the score S, losing players pay S;
  winnings are divided equally between declarer and partner.
- **Failure distribution** is configurable (see Scoring).
- **Declarer alone** receives from / pays to all 3 opponents.
- Calling the **Joker as partner** may be allowed or disallowed (configurable).

## Section 1 — GameConfig (the variant abstraction)

```go
type FailDist string

const (
    // declarer -S, partner -S, each opp +S  (mirror of the win case)
    FailEqualSplit    FailDist = "equal_split"
    // declarer -2S, partner 0, each opp +S  (declarer pays both opponents)
    FailDeclarerAlone FailDist = "declarer_alone"
    // declarer -2S (+1 if S odd), partner -S, each opp +ceil(1.5S)
    FailTwoOneSplit   FailDist = "two_one_split"
)

type GameConfig struct {
    NumPlayers        int      // 4 or 5
    AllowJokerPartner bool     // 4p toggle; always effectively true for 5p
    FailDist          FailDist // 4p 2-vs-2 only; ignored for 5p and alone play
}
```

Carried on `Game` as `Config GameConfig`. Helpers:

- `g.numSeats() int` → `Config.NumPlayers`.
- `g.minBidPoints() int` → **5p → 3** (target 13), **4p → 4** (target 14).
  `Points` is the existing 3–10 bid scale; scoring-card `target = Points + 10`.

Config is chosen at **game creation** and is immutable once bidding starts.
Five-player defaults (`NumPlayers: 5`, `AllowJokerPartner: true`) preserve
today's behavior exactly. `FailDist` is only consulted for four-player 2-vs-2
failures.

## Section 2 — Deck & deal (`card.go`)

`NewDeck` / `Deal` become config-aware:

- **4p deck (43 cards):** standard 53 minus all four **2s**, all four **4s**,
  and the two **red 3s** (♥3, ♦3). Every removed card is a non-point card, so
  the **20 scoring cards (A, K, Q, J, 10) are untouched** — P stays out of 20 and
  the scoring scale is unchanged.
- **Deal:** 4p → 4 hands × 10 + 3 kitty (43 total); 5p → unchanged
  (5 × 10 + 3 = 53). The deal signature moves from `[5][]Card` to a slice-based
  return (e.g. `([][]Card, []Card)`) so both variants fit. The `len(d) != 53`
  guard becomes `len(d) != expectedFor(numPlayers)`.
- **Unchanged:** 10 tricks per round (10 cards/player) in **both** variants, so
  every `len(Tricks) == 10`, last-trick, and 3rd-last-trick rule carries over
  untouched. Mighty (♠A), Joker, and no-trump Mighty (♦A) all remain in the 43.

## Section 3 — Player-count generalization (`game.go`, `rules.go`)

Keep `Players [5]*Player`; a four-player game occupies seats 0–3 and leaves
seat 4 `nil` (existing loops already `nil`-check players, and the frontend
already models seats as a nullable array). Replace hardcoded `5`:

- `% 5` → `% g.numSeats()` (turn rotation, `advanceToNextBidder`).
- `IsFull`: `count == g.numSeats()`.
- Bidding end conditions: all-but-one-passed `len(PassedPlayers) == g.numSeats()-1`;
  `>= g.numSeats()`; all-passed `== g.numSeats()`.
- Trick complete: `len(Cards) == g.numSeats()`.
- Auto-start in `JoinGame` triggers when `numSeats()` seats are filled.

**Bidding minimum:** `bid.Points` lower bound becomes `g.minBidPoints()` — a
four-player game rejects bids below 14; the upper bound stays 10 (= 20).

**Joker-as-partner:** in `CallPartner` validation, reject a Joker partner card
when `NumPlayers == 4 && !AllowJokerPartner`.

## Section 4 — Scoring engine (`CalculateFinalScore`)

Two changes to the existing official-scoring function. The S magnitude and all
doublings (run `p==20`, back-run `20-p>=11`, no-trump, no-friend) are
**unchanged**.

1. **Generalize M:** success value `2*(Points-3)` → `2*(Points - g.minBidPoints())`.
   `target = Points + 10`; `success = p >= target`. (5p identical; 4p uses
   offset 4, i.e. M = 14.)

2. **Distribution** (result map keyed by seat, always sums to zero):

   - **Win, or any alone (no-partner) game:** existing formula, generalized over
     `numSeats`. In 4p-2-vs-2 this yields declarer +S, partner +S, each opp −S
     (the confirmed equal split). Alone: declarer ±S versus each of 3 opponents.
   - **Failure with partner present (four-player 2-vs-2 only):** branch on
     `Config.FailDist`:
     - `equal_split` → declarer −S, partner −S, each opp +S *(today's generalized
       behavior)*.
     - `declarer_alone` → declarer −2S, partner 0, each opp +S.
     - `two_one_split` → partner −S; each opp +⌈1.5S⌉; declarer pays the
       remainder so the round sums to zero and every amount is an integer:
       - S even → declarer −2S, each opp +1.5S.
       - S odd → declarer −(2S+1), each opp +(3S+1)/2.
   - **Five-player failure path is untouched.**

## Section 5 — Wiring & frontend

**Backend wiring:**

- `CreateGame(ctx, id, cfg GameConfig)` → `New(id, cfg)`.
- Create-game HTTP request gains `num_players` and (for 4p) `allow_joker_partner`
  and `fail_dist`, validated and defaulted (5-player standard when omitted).
- Persisted game JSON (Postgres/Redis) includes `config`.

**Frontend (`mighty-frontend`):**

- Create-game form picks 4 or 5 players and — for 4p — the failure-distribution
  rule and the joker-partner toggle.
- `types.ts` gains `config`; `"/5 seated"` → `"/{numPlayers} seated"`.
- Render N seats; BidPanel minimum derived from config (14 for 4p); ScoreBoard
  iterates N players.
- Any client-side rule mirror in `src/core` (bid minimum, player count) updated
  in parallel with the backend.

## Section 6 — Testing (TDD)

- **card_test:** 4p deck = 43 cards with the exact removed set; 20 point cards
  intact; deal = 4 × 10 + 3.
- **scoring_test:** 4p M = 14 success / overtrick / failure for all three
  `FailDist` rules, including the **odd-S integer edge** (assert each row sums to
  zero); alone win/failure (3 opponents); doublings still apply; **5p regression
  suite unchanged**.
- **rules_test:** 4p rotation / trick completion / bidding-end at 4 seats; bids
  below 14 rejected; joker-partner allowed vs rejected per toggle.
- **frontend:** create-game config, seat rendering, bid-minimum, scoreboard for
  four players.

## Non-goals

- Swift client changes.
- Any change to five-player rules, scoring, or JSON shape beyond the additive
  `config` field.
- Reworking `Players` into a slice (kept as `[5]*Player` with a nil seat 4).
