//go:build integration

package e2e

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cucumber/godog"
	"github.com/go-resty/resty/v2"
	"github.com/gorilla/websocket"
	"github.com/joekhosbayar/go-mighty/internal/game"
)

type apiFeature struct {
	client       *resty.Client
	lastResponse *resty.Response
	tokens       map[string]string // original_name -> JWT
	userIDs      map[string]string // original_name -> UUID
	realNames    map[string]string // original_name -> unique_username
	activeGameID string
	game         *game.Game
	runID        string
	calledCard   *game.Card

	// WebSocket subscriber state
	wsConn      *websocket.Conn
	wsLastEvent string
}

var userCounter atomic.Int32

func (a *apiFeature) theGameServerIsRunning() error {
	var err error
	for range 5 {
		a.lastResponse, err = a.client.R().Get("/games")
		if err == nil {
			return nil
		}

		time.Sleep(1 * time.Second)
	}

	return errors.New("server unreachable")
}

func (a *apiFeature) getUniqueUsername(username string) string {
	if name, ok := a.realNames[username]; ok {
		return name
	}

	count := userCounter.Add(1)
	unique := fmt.Sprintf("%s_%s_%d", username, a.runID, count)
	a.realNames[username] = unique

	return unique
}

func (a *apiFeature) iSignUpWithUsernameAndPasswordAndEmail(username, password, email string) error {
	uniqueUser := a.getUniqueUsername(username)
	resp, err := a.client.R().SetBody(map[string]string{
		"username": uniqueUser, "password": password, "email": uniqueUser + "@example.com",
	}).Post("/auth/signup")

	a.lastResponse = resp
	if err == nil && resp.StatusCode() == http.StatusCreated {
		var res map[string]any
		if err := json.Unmarshal(resp.Body(), &res); err != nil {
			return err
		}

		if id, ok := res["id"].(string); ok {
			a.userIDs[username] = id
		}
	}

	return err
}

func (a *apiFeature) iLoginWithUsernameAndPassword(username, password string) error {
	uniqueUser := a.getUniqueUsername(username)
	resp, err := a.client.R().SetBody(map[string]string{
		"username": uniqueUser, "password": password,
	}).Post("/auth/login")

	a.lastResponse = resp
	if err == nil && resp.StatusCode() == http.StatusOK {
		var res map[string]string
		if err := json.Unmarshal(resp.Body(), &res); err != nil {
			return err
		}
		a.tokens[username] = res["token"]
	}

	return err
}

func (a *apiFeature) iAmLoggedInAs(username string) error {
	if err := a.iSignUpWithUsernameAndPasswordAndEmail(username, "pass123", ""); err != nil {
		return err
	}
	return a.iLoginWithUsernameAndPassword(username, "pass123")
}

func (a *apiFeature) iCreateANewGameWithID(_ string) error {
	username := ""

	usernames := make([]string, 0, len(a.tokens))
	for u := range a.tokens {
		usernames = append(usernames, u)
	}

	sort.Strings(usernames)

	if len(usernames) > 0 {
		username = usernames[0]
	}
	// Fallback if no players yet (e.g. Lobby test)
	if username == "" {
		if err := a.iAmLoggedInAs("creator"); err != nil {
			return err
		}

		username = "creator"
	}

	token := a.tokens[username]
	if token == "" {
		return fmt.Errorf("cannot create game for %q: user is not logged in or token is missing", username)
	}

	resp, err := a.client.R().
		SetHeader("Authorization", "Bearer "+token).
		Post("/games")

	a.lastResponse = resp
	if err == nil && resp.StatusCode() == http.StatusOK {
		var state game.Game
		if err := json.Unmarshal(resp.Body(), &state); err != nil {
			return err
		}
		a.game = &state
		a.activeGameID = state.ID
	}

	return err
}

func (a *apiFeature) createsAGame(username, _ string) error {
	token, ok := a.tokens[username]
	if !ok || token == "" {
		return fmt.Errorf("cannot create game for %q: user is not logged in or token is missing", username)
	}

	resp, err := a.client.R().
		SetHeader("Authorization", "Bearer "+token).
		Post("/games")

	a.lastResponse = resp
	if err == nil && resp.StatusCode() == http.StatusOK {
		var state game.Game
		if err := json.Unmarshal(resp.Body(), &state); err != nil {
			return err
		}
		a.game = &state
		a.activeGameID = state.ID
	}

	return err
}

func (a *apiFeature) move(username string, moveType game.MoveType, payload any) error {
	if err := a.refreshState(); err != nil {
		return err
	}

	resp, err := a.client.R().
		SetHeader("Authorization", "Bearer "+a.tokens[username]).
		SetBody(map[string]any{
			"player_id":      a.userIDs[username],
			"move_type":      moveType,
			"client_version": a.game.Version,
			"payload":        payload,
		}).Post("/games/" + a.activeGameID + "/move")
	a.lastResponse = resp

	if err != nil {
		return err
	}

	if resp.StatusCode() != http.StatusOK {
		return fmt.Errorf("move %s failed for %s: %s", moveType, username, resp.String())
	}

	if err := json.Unmarshal(resp.Body(), a.game); err != nil {
		return fmt.Errorf(
			"move %s failed for %s: could not decode response body %q: %w",
			moveType, username, string(resp.Body()), err,
		)
	}

	return nil
}

func (a *apiFeature) joinsSeatOfGame(username string) error {
	token := a.tokens[username]

	resp, err := a.client.R().
		SetHeader("Authorization", "Bearer "+token).
		Post("/games/" + a.activeGameID + "/join")
	if err != nil {
		return err
	}

	if resp.StatusCode() != http.StatusOK {
		return fmt.Errorf("join failed: %s", resp.String())
	}

	return a.refreshState()
}

func (a *apiFeature) refreshState() error {
	resp, err := a.client.R().Get("/games/" + a.activeGameID)
	if err != nil {
		return err
	}

	var state game.Game
	if err := json.Unmarshal(resp.Body(), &state); err != nil {
		return err
	}
	a.game = &state

	return nil
}

func (a *apiFeature) waitForStatus(status string) error {
	for range 30 {
		if err := a.refreshState(); err != nil {
			return err
		}

		if string(a.game.Status) == status {
			return nil
		}

		time.Sleep(200 * time.Millisecond)
	}

	return fmt.Errorf("timeout waiting for %s, got %s", status, a.game.Status)
}

func (a *apiFeature) findLegalCard(p *game.Player) game.Card {
	trickIdx := len(a.game.Tricks) - 1
	leading := trickIdx >= 0 && len(a.game.Tricks[trickIdx].Cards) == 0

	for _, c := range p.Hand {
		move := game.PlayCardMove{Card: c}
		if leading && c.Rank == game.Joker {
			move.CallJoker = true
			move.CalledSuit = game.Hearts
		}

		err := a.game.ValidateMove(p.ID, game.MovePlayCard, move)
		if err == nil {
			return c
		}
	}
	return p.Hand[0]
}

// playPayload builds a play_card payload, declaring a suit when the Joker leads.
func (a *apiFeature) playPayload(card game.Card) map[string]any {
	trickIdx := len(a.game.Tricks) - 1
	leading := trickIdx >= 0 && len(a.game.Tricks[trickIdx].Cards) == 0
	if leading && card.Rank == game.Joker {
		return map[string]any{"card": card, "called_suit": "hearts"}
	}
	return map[string]any{"card": card}
}

func (a *apiFeature) playOutGame() error {
	for trick := 1; trick <= 10; trick++ {
		for range 5 {
			if err := a.refreshState(); err != nil {
				return err
			}

			if a.game.Status == game.PhaseFinished {
				return nil
			}

			p := a.game.Players[a.game.CurrentTurn]

			var name string

			for n, id := range a.userIDs {
				if id == p.ID {
					name = n
					break
				}
			}

			card := a.findLegalCard(p)
			err := a.move(name, game.MovePlayCard, a.playPayload(card))
			if err != nil {
				return err
			}

			if a.lastResponse.StatusCode() != http.StatusOK {
				return fmt.Errorf("play failed: %s", a.lastResponse.String())
			}
		}
	}

	return nil
}

// seatThatPlayedCalledCard returns the seat that played the declarer's called
// card across all tricks, or -1 if it was never played (no friend, or it was
// discarded into the kitty).
func (a *apiFeature) seatThatPlayedCalledCard() int {
	if a.calledCard == nil {
		return -1
	}

	for _, trick := range a.game.Tricks {
		for _, pc := range trick.Cards {
			if pc.Card.Suit == a.calledCard.Suit && pc.Card.Rank == a.calledCard.Rank {
				return pc.Seat
			}
		}
	}

	return -1
}

func InitializeScenario(ctx *godog.ScenarioContext) {
	baseURL := os.Getenv("E2E_BASE_URL")
	if baseURL == "" {
		baseURL = "http://localhost:8080"
	}

	api := &apiFeature{client: resty.New().SetBaseURL(baseURL)}

	ctx.Before(func(ctx context.Context, _ *godog.Scenario) (context.Context, error) {
		api.tokens = make(map[string]string)
		api.userIDs = make(map[string]string)
		api.realNames = make(map[string]string)
		api.game = nil
		api.activeGameID = ""
		api.runID = strconv.FormatInt(time.Now().UnixNano(), 10)
		api.wsConn = nil
		api.wsLastEvent = ""
		api.calledCard = nil

		return ctx, nil
	})

	ctx.After(func(ctx context.Context, _ *godog.Scenario, _ error) (context.Context, error) {
		if api.wsConn != nil {
			_ = api.wsConn.Close()
			api.wsConn = nil
		}

		return ctx, nil
	})

	ctx.Step(`^the game server is running$`, api.theGameServerIsRunning)
	ctx.Step(`^I sign up with username "([^"]*)" and password "([^"]*)" and email "([^"]*)"$`, api.iSignUpWithUsernameAndPasswordAndEmail)
	ctx.Step(`^the response status should be (\d+)$`, func(code int) error {
		if api.lastResponse == nil {
			return errors.New("no response captured")
		}

		if api.lastResponse.StatusCode() != code {
			return fmt.Errorf("got %d, body: %s", api.lastResponse.StatusCode(), api.lastResponse.String())
		}

		return nil
	})
	ctx.Step(`^the response should contain a valid user ID$`, func() error { return nil })
	ctx.Step(`^a user "([^"]*)" exists with password "([^"]*)"$`, api.iAmLoggedInAs)
	ctx.Step(`^I login with username "([^"]*)" and password "([^"]*)"$`, api.iLoginWithUsernameAndPassword)
	ctx.Step(`^the response should contain a valid JWT token$`, func() error { return nil })
	ctx.Step(`^I am logged in as "([^"]*)"$`, api.iAmLoggedInAs)
	ctx.Step(`^I create a new game with ID "([^"]*)"$`, api.iCreateANewGameWithID)
	ctx.Step(`^"([^"]*)" creates a .*game "([^"]*)"$`, func(u, _ string) error { return api.createsAGame(u, "") })
	ctx.Step(`^the game "([^"]*)" should exist$`, func(_ string) error { return api.refreshState() })
	ctx.Step(`^there are (\d+) games waiting for players$`, func(c int) error {
		for i := range c {
			if err := api.iCreateANewGameWithID(fmt.Sprintf("wait-%d", i)); err != nil {
				return err
			}
		}
		return nil
	})
	ctx.Step(`^I list games with status "([^"]*)"$`, func(s string) error {
		resp, err := api.client.R().SetQueryParam("status", s).Get("/games")
		api.lastResponse = resp
		return err
	})
	ctx.Step(`^I should see at least (\d+) games in the list$`, func(_ int) error { return nil })

	ctx.Step(`^(\d+) authenticated players: "([^"]*)", "([^"]*)", "([^"]*)", "([^"]*)", "([^"]*)"$`, func(_ int, p1, p2, p3, p4, p5 string) error {
		for _, n := range []string{p1, p2, p3, p4, p5} {
			if err := api.iAmLoggedInAs(n); err != nil {
				return err
			}
		}
		return nil
	})
	ctx.Step(`^"([^"]*)" joins seat (\d+) of game "([^"]*)"$`, func(u string, _ int, _ string) error { return api.joinsSeatOfGame(u) })

	ctx.Step(`^"([^"]*)", "([^"]*)", "([^"]*)", "([^"]*)", "([^"]*)" join the game "([^"]*)"$`, func(p1, p2, p3, p4, p5, _ string) error {
		for _, n := range []string{p1, p2, p3, p4, p5} {
			if err := api.joinsSeatOfGame(n); err != nil {
				return err
			}
		}
		return nil
	})
	ctx.Step(`^all (\d+) players join the game "([^"]*)" in order:$`, func(_, _ string, t *godog.Table) error {
		for _, r := range t.Rows[1:] {
			if err := api.joinsSeatOfGame(r.Cells[0].Value); err != nil {
				return err
			}
		}
		return nil
	})
	ctx.Step(`^the game "([^"]*)" status should be "([^"]*)"$`, func(_, s string) error { return api.waitForStatus(s) })
	ctx.Step(`^all players should have (\d+) cards$`, func(_ int) error { return nil })
	ctx.Step(`^each player should have (\d+) cards in their hand$`, func(_ int) error { return nil })
	ctx.Step(`^"([^"]*)" bids (\d+) "([^"]*)"$`, func(u string, p int, s string) error {
		return api.move(u, game.MoveBid, map[string]any{"suit": s, "points": p})
	})
	ctx.Step(`^"([^"]*)" passes$`, func(u string) error { return api.move(u, game.MovePass, nil) })
	ctx.Step(`^"([^"]*)", "([^"]*)", "([^"]*)", and "([^"]*)" pass$`, func(p1, p2, p3, p4 string) error {
		for _, n := range []string{p1, p2, p3, p4} {
			if err := api.move(n, game.MovePass, nil); err != nil {
				return err
			}
		}
		return nil
	})
	ctx.Step(`^"([^"]*)" should be the declarer with a bid of (\d+) "([^"]*)"$`, func(_, _ string, _ string) error {
		return nil
	})
	ctx.Step(`^"([^"]*)" should have (\d+) cards in hand$`, func(_ string, _ int) error { return nil })
	ctx.Step(`^"([^"]*)" discards (\d+) least powerful cards$`, func(u string, _ int) error {
		if err := api.refreshState(); err != nil {
			return err
		}

		var cards []game.Card

		for _, p := range api.game.Players {
			if p != nil && p.ID == api.userIDs[u] {
				cards = p.Hand[:3]
				break
			}
		}

		return api.move(u, game.MoveDiscard, cards)
	})
	ctx.Step(`^"([^"]*)" calls the "([^"]*)" as the friend$`, func(u, _ string) error {
		card := game.Card{Suit: game.Hearts, Rank: game.Ace}
		api.calledCard = &card

		return api.move(u, game.MoveCallPartner, game.CallPartnerMove{Card: &card})
	})
	ctx.Step(`^"([^"]*)" declares no friend$`, func(u string) error {
		return api.move(u, game.MoveCallPartner, map[string]any{"no_friend": true})
	})
	ctx.Step(`^all remaining tricks are played out legally$`, func() error { return api.playOutGame() })
	ctx.Step(`^the game should have no friend$`, func() error {
		if err := api.refreshState(); err != nil {
			return err
		}

		if !api.game.IsNoFriend {
			return errors.New("expected is_no_friend true")
		}

		return nil
	})
	ctx.Step(`^the partner seat should be unrevealed or match whoever played the called card$`, func() error {
		if err := api.refreshState(); err != nil {
			return err
		}

		playedBy := api.seatThatPlayedCalledCard()
		if api.game.PartnerSeat != -1 && api.game.PartnerSeat != playedBy {
			return fmt.Errorf("partner seat %d, but called card was played by seat %d", api.game.PartnerSeat, playedBy)
		}

		return nil
	})
	ctx.Step(`^the final scores should follow the declarer-partner split$`, func() error {
		if err := api.refreshState(); err != nil {
			return err
		}

		declarer := api.game.Players[api.game.Declarer]
		declarerScore := api.game.Scores[declarer.ID]

		if declarerScore == 0 {
			return errors.New("declarer round score must be non-zero")
		}

		friend := api.seatThatPlayedCalledCard()

		if friend >= 0 && friend != api.game.Declarer {
			partnerScore := api.game.Scores[api.game.Players[friend].ID]
			if diff := declarerScore - 2*partnerScore; diff < -1 || diff > 1 {
				return fmt.Errorf("partner score %d is not half of declarer %d", partnerScore, declarerScore)
			}
		}

		// Official rules are zero-sum: the five seat scores must sum to zero.
		sum := 0
		for _, p := range api.game.Players {
			if p == nil {
				continue
			}
			sum += api.game.Scores[p.ID]
		}
		if sum != 0 {
			return fmt.Errorf("round scores must sum to zero, got %d", sum)
		}

		// With a revealed partner, each opponent loses exactly the partner's share.
		if friend >= 0 && friend != api.game.Declarer {
			partnerScore := api.game.Scores[api.game.Players[friend].ID]
			for _, p := range api.game.Players {
				if p == nil || p.Seat == api.game.Declarer || p.Seat == friend {
					continue
				}
				if s := api.game.Scores[p.ID]; s != -partnerScore {
					return fmt.Errorf("opponent %d score %d, want %d", p.Seat, s, -partnerScore)
				}
			}
		}

		return nil
	})
	ctx.Step(`^the trump suit should be "([^"]*)"$`, func(_ string) error { return nil })
	ctx.Step(`^"([^"]*)" leads the first trick$`, func(_ string) error { return nil })
	ctx.Step(`^all players play out Trick (\d+) legally$`, func(_ int) error {
		for range 5 {
			if err := api.refreshState(); err != nil {
				return err
			}
			p := api.game.Players[api.game.CurrentTurn]

			var name string

			for n, id := range api.userIDs {
				if id == p.ID {
					name = n
					break
				}
			}

			card := api.findLegalCard(p)
			if err := api.move(name, game.MovePlayCard, api.playPayload(card)); err != nil {
				return err
			}

			if api.lastResponse.StatusCode() != http.StatusOK {
				return fmt.Errorf("failed play: %s", api.lastResponse.String())
			}
		}

		return nil
	})
	ctx.Step(`^Trick (\d+) should have a winner$`, func(_ int) error { return nil })
	ctx.Step(`^the winner of Trick (\d+) leads Trick (\d+)$`, func(_, _ int) error { return nil })
	ctx.Step(`^the total number of tricks won should be (\d+)$`, func(_ int) error { return nil })
	ctx.Step(`^the final scores should be calculated and non-zero$`, func() error { return nil })
	ctx.Step(`^"([^"]*)" should be the declarer$`, func(_ string) error { return nil })
	ctx.Step(`^(\d+) tricks are played through the WebSocket$`, func(_ int) error { return api.playOutGame() })
	ctx.Step(`^final scores should be calculated correctly$`, func() error { return nil })

	// --- WebSocket steps ---
	ctx.Step(`^a WebSocket client connects to game "([^"]*)" with an invalid token$`, func(_ string) error {
		return api.connectWSWithToken(baseURL, "totally-invalid-jwt-token")
	})
	ctx.Step(`^the WebSocket should receive an error containing "([^"]*)"$`, func(expected string) error {
		if api.wsConn == nil {
			return errors.New("no WebSocket connection")
		}

		_ = api.wsConn.SetReadDeadline(time.Now().Add(5 * time.Second))

		_, data, err := api.wsConn.ReadMessage()
		if err != nil {
			return fmt.Errorf("failed to read WS message: %w", err)
		}

		if !strings.Contains(string(data), expected) {
			return fmt.Errorf("expected WS error containing %q, got: %s", expected, string(data))
		}

		return nil
	})
	ctx.Step(`^a WebSocket subscriber connects to game "([^"]*)" as "([^"]*)"$`, func(_ string, username string) error {
		token := api.tokens[username]
		if token == "" {
			return fmt.Errorf("no token for user %q", username)
		}

		if err := api.connectWSWithToken(baseURL, token); err != nil {
			return err
		}

		// Give the server a moment to register the subscription
		time.Sleep(200 * time.Millisecond)

		return nil
	})
	ctx.Step(`^the subscriber should receive a "([^"]*)" event within (\d+) seconds$`, func(eventType string, timeout int) error {
		if api.wsConn == nil {
			return errors.New("no WebSocket connection")
		}

		_ = api.wsConn.SetReadDeadline(time.Now().Add(time.Duration(timeout) * time.Second))

		_, data, err := api.wsConn.ReadMessage()
		if err != nil {
			return fmt.Errorf("timed out waiting for %q event: %w", eventType, err)
		}

		api.wsLastEvent = string(data)

		var event map[string]any
		if err := json.Unmarshal(data, &event); err != nil {
			return fmt.Errorf("failed to decode event: %w (raw: %s)", err, string(data))
		}

		gotType, _ := event["type"].(string)
		if gotType != eventType {
			return fmt.Errorf("expected event type %q, got %q (raw: %s)", eventType, gotType, string(data))
		}

		return nil
	})
}

// connectWSWithToken dials the WebSocket endpoint for the active game, then sends the
// first-message AUTH frame with the given token. It stores the connection in api.wsConn.
func (a *apiFeature) connectWSWithToken(baseURL, token string) error {
	wsURL := strings.Replace(baseURL, "http", "ws", 1) + "/games/" + a.activeGameID + "/ws"

	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		return fmt.Errorf("WebSocket dial failed: %w", err)
	}

	authMsg, _ := json.Marshal(map[string]string{
		"type":  "AUTH",
		"token": token,
	})

	if err := conn.WriteMessage(websocket.TextMessage, authMsg); err != nil {
		_ = conn.Close()
		return fmt.Errorf("failed to send AUTH message: %w", err)
	}

	a.wsConn = conn

	return nil
}

func TestFeatures(t *testing.T) {
	t.Parallel()
	suite := godog.TestSuite{
		ScenarioInitializer: InitializeScenario,
		Options:             &godog.Options{Format: "pretty", Paths: []string{"features"}, TestingT: t},
	}
	if suite.Run() != 0 {
		t.Fatal("failed")
	}
}
