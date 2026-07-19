# Mighty Game Rules

## Introduction
Mighty is a high-stakes, point-trick card game featuring bidding and a "Mystery Friend" partner mechanic.

## Players and Cards
- **Players**: 5 players (Standard).
- **Deck**: 52 cards + 1 Joker (53 total).
- **Ranks**: A (High) → 2 (Low).
- **Points**: A, K, Q, J, and 10 of any suit are "Point Cards". There are 20 total point cards in the deck.

## Special Cards (The Magic Cards)

### 1. The Mighty
The strongest card in the game. It wins any trick it is played in.
- **Normal**: Ace of Spades (♠A).
- **If Spades are Trump**: Ace of Clubs (♣A).

### 2. The Joker
The second strongest card. It wins any trick unless:
- The Mighty is played in the same trick.
- It is the **First Trick** or the **Last Trick** of the hand (in which case it loses all power).
- The **Joker Caller** was led and called for the Joker.

### 3. The Joker Caller (The Ripper)
A nemesis for the Joker.
- **Normal**: Three of Clubs (♣3).
- **If Clubs are Trump**: Three of Spades (♠3).
- **Rule**: If led, the player can choose to "Call the Joker". The player holding the Joker **MUST** play it, and the Joker loses all power for that trick.
- **Exception**: If the Joker holder also has the Mighty, they can play the Mighty instead to win the trick and save their Joker.

## The Game Flow

### 1. Bidding Phase
Players bid for the contract, specifying a **bid level** and the trump suit (or No-Trump).
A bid is a promise of how many of the 20 scoring cards the declarer's team will capture.
- **Bid range and mapping**: bids are entered on a **3–10** scale. This maps to the
  official Mighty **13–20** scoring-card scale by **`target = bid + 10`**:

  | Bid (our scale) | 3 | 4 | 5 | 6 | 7 | 8 | 9 | 10 |
  |---|---|---|---|---|---|---|---|---|
  | Scoring-card target (`bid + 10`) | 13 | 14 | 15 | 16 | 17 | 18 | 19 | 20 |

  A bid is **not** a number of tricks — there are only 10 tricks in a hand, but 20
  scoring cards. The bid, the target, and the captured count `P` are all measured in
  scoring cards.
- Minimum bid: 3 (target 13).
- No-Trump bids beat suit bids of the same level. Suit bids are ranked: Clubs < Diamonds < Hearts < Spades.
- Bidding ends after 4 consecutive passes. The winner becomes the **Declarer**.

### 2. Exchanging Phase (The Kitty)
- 3 cards are dealt face-down as the "Kitty".
- The Declarer takes the Kitty and then discards 3 cards of their choice back to their score pile.

### 3. Calling the Friend
The Declarer calls out a specific card (e.g., "Ace of Hearts").
- The player holding that card becomes the **Partner**.
- The Partner remains secret until the called card is played.
- **No Friend**: The Declarer can choose to play alone for doubled points.

### 4. Playing Phase
- The Declarer leads the first trick.
- **Rule**: No trump can be led on the first trick unless the player has only trumps.
- **Rule**: The Mighty cannot be played on the first trick unless the player cannot follow the lead suit.
- Players must follow the lead suit if possible.

## Scoring (Official Mighty)

Scores are zero-sum: they add up to zero across all five players. `P` is the number
of the 20 scoring cards the declarer's team captured. Bids are entered on a 3–10
scale, mapping to a scoring-card target of `bid + 10` (13–20).

- **Success** (`P ≥ bid + 10`): `S = 2×(bid − 3) + (P − (bid + 10))`.
- **Failure** (`P < bid + 10`): `S = (bid + 10) − P`.
- **Distribution** — success: declarer **+2S**, partner **+S**, each opponent **−S**;
  failure reverses every sign. Playing alone: declarer **±4S**, each opponent **∓S**.
- **Multipliers** (each ×2, stacking):
    - **Run**: team captured all 20 scoring cards.
    - **Back-run**: opponents captured ≥ 11 scoring cards.
    - **No-Trump**.
    - **No-Friend**: announced solo only (not a secret solo).
- If the bid is the minimum (3) and the team takes exactly 13 points, `S = 0` and
  there is no payment.

`P` counts the scoring cards captured by the declarer's **team** (declarer + revealed
partner). All 20 scoring cards are always accounted for: trick points go to the trick
winner, and the declarer's 3 kitty discards go to the declarer's own pile. A **secret
solo** (the called card is in the declarer's own hand or the kitty, so no partner is
ever revealed) uses the alone distribution but does **not** get the No-Friend double —
that double is only for an openly announced solo.

### Worked examples

Bid values below are on our 3–10 scale; `P` is captured scoring cards.

| Bid | Trump | Alone | P | Result | S (with doublings) | Declarer / Partner / each Opp |
|---|---|---|---|---|---|---|
| 5 | Diamonds | no | 16 | success | `2×(5−3)+(16−15)` = **5** | +10 / +5 / −5 |
| 5 | Diamonds | no | 13 | fail | `15−13` = **2** | −4 / −2 / +2 |
| 6 | No-Trump | no | 18 | success | `8 ×2` = **16** | +32 / +16 / −16 |
| 6 | No-Trump | no | 13 | fail | `3 ×2` = **6** | −12 / −6 / +6 |
| 7 | Hearts | no | 20 | run | `11 ×2` = **22** | +44 / +22 / −22 |
| 6 | No-Trump | yes | 17 | success | `7 ×2 ×2` = **28** | +112 / — / −28 |
| 6 | No-Trump | yes | 15 | fail | `1 ×2 ×2` = **4** | −16 / — / +4 |

### Implementation note

Scoring lives in `CalculateFinalScore` (`internal/game/rules.go`). It returns a
`map[int]int` of seat → signed round score that always sums to zero, computed by one
distribution rule: each opponent pays `S`, the partner (if any) collects `S`, and the
declarer collects the remainder (`oppCount × S − partnerShare`), with every sign
flipped on failure. That single rule yields both the partnered (2S / S / −S×3) and
alone (4S / −S×4) payouts.
