# Official Mighty Scoring Engine — Design

**Date:** 2026-07-19
**Status:** Approved design, ready for implementation planning
**Scope:** `go-mighty` scoring engine (`internal/game/rules.go`), its tests, `rules.md`, and frontend score display.

## Problem

`CalculateFinalScore` (`internal/game/rules.go:787`) implements a "UCLA Campus Standard"
that diverges from the official Mighty regulations in four structural ways:

1. **Scores on tricks won, not scoring cards captured.** It counts `tricksWon` (0–10 tricks);
   the regulation scores on `P` = the team's captured **scoring cards** (0–20), computed as
   `20 − (opponents' scoring cards)`. Captured point cards are already tracked in `Player.Points`
   (including the declarer's kitty discards at `rules.go:590`).
2. **Wrong formulas.** Uses `contract*10 + overtricks*5` / `−contract*10 − …` instead of the
   official success/failure formulas.
3. **Wrong doublings.** Doubles on `bid == 10` (max bid) instead of on a **run** (all 20 cards);
   omits the **back-run** double entirely.
4. **Not zero-sum.** Records declarer = full, partner = half, opponents = 0. The regulation is
   strictly zero-sum across all five players.

## Bid mapping

The UI bids on a 3–10 scale, which maps to the official 13–20 scoring-card scale:
**`target = bid + 10`** (bid 3 → 13 cards … bid 10 → 20 cards). `P` is the raw captured-card
count (0–20) and is **never** shifted.

## Scoring rules (authoritative)

Let `bid` be on the 3–10 scale, `P` the team's captured scoring cards, `target = bid + 10`.

- **Success** (`P ≥ target`): `S = 2×(bid − 3) + (P − target)`
- **Failure** (`P < target`):  `S = target − P`
- `S == 0` → no payments (bid 3 taking exactly 13 points).

**Doublings** — each multiplies `S` by 2, stacking multiplicatively (×4, ×8, …):

| Double | Condition |
|---|---|
| Run | Team captured all 20 scoring cards (`P == 20`) |
| Back-run | Opponents captured ≥ 11 scoring cards (`P ≤ 9`) |
| No-trump | `Contract.IsNoTrump` |
| No-friend | `IsNoFriend` (openly announced solo only) |

Secret-solo contracts (the called card is in the declarer's own hand or the kitty) do **not**
receive the no-friend double.

## The single distribution rule (guarantees zero-sum)

- **Team** = declarer, plus partner **iff** `friendSeat() ≥ 0 && friendSeat() ≠ declarer`.
- **Opponents** = every other seat.
- `P` = Σ `len(Player.Points)` over team seats. (All 20 point cards are always distributed:
  trick points go to winners; the declarer's 3 kitty discards land in the declarer's `Points`.)
- **On success:** each opponent pays `S`; partner (if present) collects `S`; declarer collects
  the remainder `opp_count × S − partner_share`.
- **On failure:** every sign flips.

This yields the regulation payouts automatically:
- With partner (3 opponents): declarer **2S**, partner **S**, each opponent **−S**.
- Alone / secret-solo (4 opponents): declarer **4S**, each opponent **−S**.

The only difference between announced-alone and secret-solo is the no-friend ×2 multiplier on `S`.

## Verification against the 7 regulation examples

| Bid | Trump | Alone | P | S (with doublings) | Declarer / Partner / each Opp |
|---|---|---|---|---|---|
| 5 | ♦ | no | 16 | 5 | +10 / +5 / −5 |
| 5 | ♦ | no | 13 (fail) | 2 | −4 / −2 / +2 |
| 6 | NT | no | 18 | 8×2 = 16 | +32 / +16 / −16 |
| 6 | NT | no | 13 (fail) | 3×2 = 6 | −12 / −6 / +6 |
| 7 | ♥ | no | 20 (run) | 11×2 = 22 | +44 / +22 / −22 |
| 6 | NT | yes | 17 | 7×2×2 = 28 | +112 / — / −28 |
| 6 | NT | yes | 15 (fail) | 1×2×2 = 4 | −16 / — / +4 |

All match the regulation.

## Approach A — implementation shape

Rewrite `CalculateFinalScore` to return a **full per-seat map** `map[int]int` (seat → signed
score, summing to zero), computed via the distribution rule above. Integer math throughout
(no `float64`, no half-splits, no ±800 cap — the cap is dropped).

- **Signature change:** `CalculateFinalScore()` returns `map[int]int` (seat → signed round score).
- **Call site** (`rules.go:683–701`): after the 10th trick, write the returned per-seat map
  straight into `g.Scores` keyed by player ID (all five seats populated; sums to zero).
- Keep everything in the `game` package; no new package.

## Testing

Table-driven Go tests written **first** (TDD), covering:
- The 7 regulation examples above (exact declarer/partner/opponent values).
- Edge cases: exact-`target` zero-payment (bid 3, P 13); back-run; secret-solo (called own/kitty
  card, no no-friend double); announced-alone failure; combined doublings (NT + no-friend + back-run).
- Zero-sum invariant: every result's five seat values sum to 0.

Update existing scoring assertions in `internal/game/rules_test.go` and
`internal/game/rules_friend_test.go` to the official values.

## Docs

Replace the "Scoring (UCLA Campus Standard)" section of `docs/rules.md` with the official
regulation (formulas, doublings, bid→card mapping, zero-sum distribution).

## Frontend

`view.ts:106` already reads `game.scores?.[p.id]` per player, so populating all five seats flows
through automatically. Remaining work is display polish only: render negative round scores with a
sign and appropriate color in `ScoreBoard.tsx`. Verify `types.ts` `roundScore` allows negatives
(plain `number` — no change expected).

## Out of scope

- Bidding, trick resolution, friend-calling, and power logic are unchanged.
- No new scoring package extraction (approach C was declined).
