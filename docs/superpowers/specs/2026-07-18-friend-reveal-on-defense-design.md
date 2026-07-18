# Mystery Friend: Reveal on Defense

**Date:** 2026-07-18
**Status:** Approved
**Scope:** `go-mighty` backend only. No frontend or API-shape changes.

## Problem

In Mighty the declarer calls a "friend" by naming a card; whoever holds it is
the declarer's secret teammate. Today the backend reveals that teammate the
moment they *play* the called card (`internal/game/rules.go:595-596`). That is
too early and does not match the real game, where the friend stays hidden until
they visibly act as the declarer's partner.

The real tell is **defending point cards**: when the friend wins a trick that
contains scoring cards, those cards go to the declarer's team instead of being
captured against it, and that action outs them. If the friend wins a trick with
nothing to defend (no point cards), it stays ambiguous — a plain opponent could
have won the same trick — so no reveal happens.

## Desired behavior

The friend's public identity (`partner_seat`) is revealed **only** at trick
resolution, when all of the following hold:

1. The friend is the trick winner, and
2. the trick is worth defending — it contains at least one point card
   (A, K, Q, J, 10) played by *any* player, **or** the friend won the trick by
   playing the **joker**.

Notes:
- The "point card" set is the game's existing `Card.IsPointCard()` = {A, K, Q,
  J, 10}. The mighty is an Ace, so it is already a point card; the joker (not a
  point card) is the one special case that needs its own clause.
- A point card played by an *opponent* into a trick the friend wins still
  triggers the reveal — the friend is defending it.
- Winning a trick with no point cards and no joker does **not** reveal the
  friend (the ambiguity caveat).
- Playing the called card no longer reveals the friend on its own. (If that
  play also wins a point-card trick, the reveal fires through the rule above,
  not through a special called-card check.)
- Reveal is **monotonic**: once `partner_seat >= 0`, it never reverts.

## Design (Approach A)

### New helper: `g.friendSeat() int`

Returns the seat of the mystery friend — the holder or past-player of
`PartnerCard` — independent of whether they have been revealed.

```go
// friendSeat returns the seat of the mystery friend (holder of the called
// partner card), or -1 when there is no friend or the card is unheld (e.g. the
// declarer discarded it into the kitty before calling). It scans current hands
// and every played trick card, so it is correct at any point after the friend
// is called and needs no stored field — it survives Redis reloads for free.
func (g *Game) friendSeat() int {
    if g.IsNoFriend || g.PartnerCard == nil {
        return -1
    }
    pc := *g.PartnerCard
    for _, p := range g.Players {
        if p == nil {
            continue
        }
        for _, c := range p.Hand {
            if c.Suit == pc.Suit && c.Rank == pc.Rank {
                return p.Seat
            }
        }
    }
    for _, t := range g.Tricks {
        for _, played := range t.Cards {
            if played.Card.Suit == pc.Suit && played.Card.Rank == pc.Rank {
                return played.Seat
            }
        }
    }
    return -1
}
```

`friendSeat()` is the authoritative internal seat used for scoring and reveal
detection. The public `PartnerSeat` field keeps its JSON tag and its meaning
becomes strictly "the revealed seat, or -1 if not yet revealed."

### Reveal detection at trick resolution

Remove the current reveal (`rules.go:595-596`):

```go
// Reveal the mystery friend the moment the called card hits the table.
if g.PartnerCard != nil && card.Suit == g.PartnerCard.Suit && card.Rank == g.PartnerCard.Rank {
    g.PartnerSeat = p.Seat
}
```

In the `MovePlayCard` branch, inside the `len(cards) == 5` block, right after
`winnerSeat, points := g.ResolveTrick(...)` and `g.Tricks[idx].Winner = winnerSeat`,
add:

```go
// Reveal the friend once they defend: they win a trick that either holds a
// scoring card or was taken with the joker. A pointless win stays ambiguous.
if g.PartnerSeat < 0 {
    if fs := g.friendSeat(); fs >= 0 && winnerSeat == fs && trickRevealsFriend(g.Tricks[idx], fs) {
        g.PartnerSeat = fs
    }
}
```

```go
// trickRevealsFriend reports whether winning this trick outs the friend: it
// holds a scoring card to defend, or the friend won it with the joker. (The
// mighty is an Ace, so it already counts as a scoring card.)
func trickRevealsFriend(t Trick, friendSeat int) bool {
    for _, played := range t.Cards {
        if played.Card.IsPointCard() {
            return true
        }
        if played.Seat == friendSeat && played.Card.Rank == Joker {
            return true
        }
    }
    return false
}
```

### Scoring uses `friendSeat()`, not `PartnerSeat`

Because `PartnerSeat` can now remain -1 for an entire game (a friend who never
defends), scoring must read the true seat. No score *values* change — only the
source of the team attribution.

- `CalculateFinalScore` (`rules.go:740`): compute `fs := g.friendSeat()` once;
  the trick tally becomes `if t.Winner == g.Declarer || t.Winner == fs`.
- `CalculateFinalScore` friend-share guard (`rules.go:787`): `if g.IsNoFriend || fs < 0 { friendScore = 0 }`.
- End-of-game `Scores` assignment in `ApplyMove` (`rules.go:643-644`): assign the
  partner's half-score to `g.friendSeat()` rather than `g.PartnerSeat`.

At game end (trick 10) the called card has necessarily been played, so
`friendSeat()` resolves from the trick history.

## Edge cases

- **No-friend game** (`IsNoFriend`): `friendSeat()` = -1; `partner_seat` never
  set; no reveal. Unchanged.
- **Called card discarded into the kitty** before calling: `friendSeat()` = -1;
  the declarer is effectively friendless (friend share = 0). Preserves today's
  latent behavior.
- **Declarer calls a card in their own hand:** `friendSeat()` returns the
  declarer's seat. This "self-partner" case is a pre-existing oddity; this spec
  preserves current behavior and does not attempt to redefine it.
- **Monotonic reveal:** the `g.PartnerSeat < 0` guard ensures the reveal is set
  at most once and never cleared.

## Testing

New / updated unit tests in `internal/game/rules_friend_test.go`:

- `friendSeat()`: finds the seat when the card is in a hand; finds it after it
  has been played into a trick; returns -1 for a no-friend game and for a called
  card held by nobody.
- Reveal fires when the friend wins a trick containing a point card they played.
- Reveal fires when the friend wins a trick containing a point card an
  **opponent** played (defending it).
- Reveal fires when the friend wins an otherwise-pointless trick with the joker.
- Reveal does **not** fire when the friend wins a trick with no point cards and
  no joker.
- Reveal does **not** fire when a **non-friend** wins a point-card trick.
- Playing the called card into a trick the friend does not win, or into a
  pointless trick they win, does **not** reveal (replaces
  `TestPlayingCalledCardRevealsPartner`, whose current assertion — reveal on
  playing ♦A into an unfinished trick — is now inverted).
- Reveal is monotonic across subsequent tricks.
- Scoring credits an unrevealed friend's won tricks to the declarer team and
  assigns the friend their half-score via `friendSeat()`.
- `TestDeclarerPlayingOwnCalledCardIsSelfPartner` updated to the new reveal
  timing (self-partner seat resolves via `friendSeat()`; reveal still requires a
  qualifying trick win).

E2E (`tests/e2e/features/friend.feature` + `tests/e2e/e2e_test.go`):

- The step `the partner seat should match whoever played the called card`
  (`e2e_test.go:478-494`) asserts the old reveal semantics and can no longer
  hold in general (a friend may finish a game unrevealed). Replace the scenario's
  reveal assertion with one robust to the new rule — assert the declarer/partner
  score split via the friend seat (as the existing
  `the final scores should follow the declarer-partner split` step already
  does), and either drop the played-the-called-card assertion or replace it with
  a crafted trick that deterministically triggers a defense reveal.

Verification: `go test ./...` in `go-mighty` (unit + integration) green.

## Out of scope (known risks / follow-ups)

- **Raw-payload leak / true secrecy.** The backend serializes the full game —
  every hand and the called `partner_card` — to all clients
  (`internal/service/game_service.go:234`, `internal/api/handler.go:326`). The
  friend is therefore deducible from raw API/WebSocket traffic today,
  independent of this change. This feature hides the friend at the game-state /
  UI level (the frontend never renders opponents' hands and reads
  `partner_seat`), which is what makes the mechanic work in play. Cryptographic
  secrecy would require a per-player state-redaction layer and a per-connection
  broadcast — a separate, larger project. Recorded here as a deliberate
  deferral.
- **Self-partner semantics** (declarer calls own card): preserved as-is, not
  redefined.
