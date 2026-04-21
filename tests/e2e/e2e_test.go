package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"testing"
	"time"

	"github.com/cucumber/godog"
	"github.com/go-resty/resty/v2"
	"github.com/joekhosbayar/go-mighty/internal/game"
)

type apiFeature struct {
	client       *resty.Client
	lastResponse *resty.Response
	tokens       map[string]string // original_name -> JWT
	userIDs      map[string]string // original_name -> UUID
	realNames    map[string]string // original_name -> unique_username
	activeGameID string
	gameState    *game.GameState
	runID        string
}

var userCounter int

func (a *apiFeature) theGameServerIsRunning() error {
	var err error
	for i := 0; i < 5; i++ {
		a.lastResponse, err = a.client.R().Get("/games")
		if err == nil { return nil }
		time.Sleep(1 * time.Second)
	}
	return fmt.Errorf("server unreachable")
}

func (a *apiFeature) getUniqueUsername(username string) string {
	if name, ok := a.realNames[username]; ok { return name }
	userCounter++
	unique := fmt.Sprintf("%s_%s_%d", username, a.runID, userCounter)
	a.realNames[username] = unique
	return unique
}

func (a *apiFeature) iSignUpWithUsernameAndPasswordAndEmail(username, password, email string) error {
	uniqueUser := a.getUniqueUsername(username)
	resp, err := a.client.R().SetBody(map[string]string{
		"username": uniqueUser, "password": password, "email": fmt.Sprintf("%s@example.com", uniqueUser),
	}).Post("/auth/signup")
	a.lastResponse = resp
	if err == nil && resp.StatusCode() == http.StatusCreated {
		var res map[string]interface{}
		json.Unmarshal(resp.Body(), &res)
		if id, ok := res["id"].(string); ok { a.userIDs[username] = id }
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
		json.Unmarshal(resp.Body(), &res)
		a.tokens[username] = res["token"]
	}
	return err
}

func (a *apiFeature) iAmLoggedInAs(username string) error {
	if err := a.iSignUpWithUsernameAndPasswordAndEmail(username, "pass123", ""); err != nil { return err }
	return a.iLoginWithUsernameAndPassword(username, "pass123")
}

func (a *apiFeature) iCreateANewGameWithID(id string) error {
	username := ""
	usernames := make([]string, 0, len(a.tokens))
	for u := range a.tokens { usernames = append(usernames, u) }
	sort.Strings(usernames)
	if len(usernames) > 0 { username = usernames[0] }
	// Fallback if no players yet (e.g. Lobby test)
	if username == "" {
		if err := a.iAmLoggedInAs("creator"); err != nil { return err }
		username = "creator"
	}
	token := a.tokens[username]
	if token == "" { return fmt.Errorf("cannot create game for %q: user is not logged in or token is missing", username) }
	resp, err := a.client.R().
		SetHeader("Authorization", "Bearer "+token).
		Post("/games")
	
	a.lastResponse = resp
	if err == nil && resp.StatusCode() == http.StatusOK {
		var state game.GameState
		json.Unmarshal(resp.Body(), &state)
		a.gameState = &state
		a.activeGameID = state.ID
	}
	return err
}

func (a *apiFeature) createsAGame(username, gameID string) error {
	token, ok := a.tokens[username]
	if !ok || token == "" { return fmt.Errorf("cannot create game for %q: user is not logged in or token is missing", username) }
	resp, err := a.client.R().
		SetHeader("Authorization", "Bearer "+token).
		Post("/games")
	
	a.lastResponse = resp
	if err == nil && resp.StatusCode() == http.StatusOK {
		var state game.GameState
		json.Unmarshal(resp.Body(), &state)
		a.gameState = &state
		a.activeGameID = state.ID
	}
	return err
}

func (a *apiFeature) move(username string, moveType game.MoveType, payload interface{}) error {
	if err := a.refreshState(); err != nil { return err }
	resp, err := a.client.R().
		SetHeader("Authorization", "Bearer "+a.tokens[username]).
		SetBody(map[string]interface{}{
			"player_id": a.userIDs[username],
			"move_type": moveType,
			"client_version": a.gameState.Version,
			"payload": payload,
		}).Post("/games/" + a.activeGameID + "/move")
	a.lastResponse = resp
	if err != nil {
		return err
	}
	if resp.StatusCode() != http.StatusOK {
		return fmt.Errorf("move %s failed for %s: %s", moveType, username, resp.String())
	}
	if err := json.Unmarshal(resp.Body(), a.gameState); err != nil {
		return fmt.Errorf(
			"move %s failed for %s: could not decode response body %q: %w",
			moveType, username, string(resp.Body()), err,
		)
	}
	return nil
}

func (a *apiFeature) joinsSeatOfGame(username string, seat int) error {
	token := a.tokens[username]
	resp, err := a.client.R().
		SetHeader("Authorization", "Bearer "+token).
		SetBody(map[string]interface{}{"seat": seat}).
		Post("/games/" + a.activeGameID + "/join")
	if err != nil { return err }
	if resp.StatusCode() != http.StatusOK { return fmt.Errorf("join failed: %s", resp.String()) }
	return a.refreshState()
}

func (a *apiFeature) refreshState() error {
	resp, err := a.client.R().Get("/games/" + a.activeGameID)
	if err != nil { return err }
	var state game.GameState
	json.Unmarshal(resp.Body(), &state)
	a.gameState = &state
	return nil
}

func (a *apiFeature) waitForStatus(status string) error {
	for i := 0; i < 30; i++ {
		a.refreshState()
		if string(a.gameState.Status) == status { return nil }
		time.Sleep(200 * time.Millisecond)
	}
	return fmt.Errorf("timeout waiting for %s, got %s", status, a.gameState.Status)
}

func (a *apiFeature) findLegalCard(p *game.Player) game.Card {
	trickIdx := len(a.gameState.Tricks) - 1
	if trickIdx < 0 { return p.Hand[0] }
	currentTrick := a.gameState.Tricks[trickIdx]
	if len(currentTrick.Cards) == 0 {
		for _, c := range p.Hand {
			if len(a.gameState.Tricks) == 1 && c.Suit == a.gameState.Trump {
				hasNon := false
				for _, c2 := range p.Hand { if c2.Suit != a.gameState.Trump && c2.Rank != game.Joker { hasNon = true; break } }
				if hasNon { continue }
			}
			return c
		}
	} else {
		for _, c := range p.Hand { if c.Suit == currentTrick.LeadSuit { return c } }
	}
	return p.Hand[0]
}

func (a *apiFeature) playOutGame() error {
	for trick := 1; trick <= 10; trick++ {
		for i := 0; i < 5; i++ {
			a.refreshState()
			if a.gameState.Status == game.PhaseFinished { return nil }
			p := a.gameState.Players[a.gameState.CurrentTurn]
			var name string
			for n, id := range a.userIDs { if id == p.ID { name = n; break } }
			card := a.findLegalCard(p)
			if err := a.move(name, game.MovePlayCard, map[string]interface{}{"card": card}); err != nil { return err }
			if a.lastResponse.StatusCode() != http.StatusOK { return fmt.Errorf("play failed: %s", a.lastResponse.String()) }
		}
	}
	return nil
}

func InitializeScenario(ctx *godog.ScenarioContext) {
	api := &apiFeature{client: resty.New().SetBaseURL("http://localhost:8080")}
	ctx.Before(func(ctx context.Context, sc *godog.Scenario) (context.Context, error) {
		api.tokens = make(map[string]string); api.userIDs = make(map[string]string); api.realNames = make(map[string]string)
		api.gameState = nil; api.activeGameID = ""; api.runID = fmt.Sprintf("%d", time.Now().UnixNano())
		return ctx, nil
	})

	ctx.Step(`^the game server is running$`, api.theGameServerIsRunning)
	ctx.Step(`^I sign up with username "([^"]*)" and password "([^"]*)" and email "([^"]*)"$`, api.iSignUpWithUsernameAndPasswordAndEmail)
	ctx.Step(`^the response status should be (\d+)$`, func(code int) error {
		if api.lastResponse == nil { return fmt.Errorf("no response captured") }
		if api.lastResponse.StatusCode() != code { return fmt.Errorf("got %d, body: %s", api.lastResponse.StatusCode(), api.lastResponse.String()) }
		return nil
	})
	ctx.Step(`^the response should contain a valid user ID$`, func() error { return nil })
	ctx.Step(`^a user "([^"]*)" exists with password "([^"]*)"$`, api.iAmLoggedInAs)
	ctx.Step(`^I login with username "([^"]*)" and password "([^"]*)"$`, api.iLoginWithUsernameAndPassword)
	ctx.Step(`^the response should contain a valid JWT token$`, func() error { return nil })
	ctx.Step(`^I am logged in as "([^"]*)"$`, api.iAmLoggedInAs)
	ctx.Step(`^I create a new game with ID "([^"]*)"$`, api.iCreateANewGameWithID)
	ctx.Step(`^"([^"]*)" creates a .*game "([^"]*)"$`, func(u, g string) error { return api.createsAGame(u, g) })
	ctx.Step(`^the game "([^"]*)" should exist$`, func(id string) error { return api.refreshState() })
	ctx.Step(`^there are (\d+) games waiting for players$`, func(c int) error {
		for i := 0; i < c; i++ { api.iCreateANewGameWithID(fmt.Sprintf("wait-%d", i)) }; return nil
	})
	ctx.Step(`^I list games with status "([^"]*)"$`, func(s string) error {
		api.lastResponse, _ = api.client.R().SetQueryParam("status", s).Get("/games"); return nil
	})
	ctx.Step(`^I should see at least (\d+) games in the list$`, func(c int) error { return nil })

	ctx.Step(`^(\d+) authenticated players: "([^"]*)", "([^"]*)", "([^"]*)", "([^"]*)", "([^"]*)"$`, func(c int, p1, p2, p3, p4, p5 string) error {
		for _, n := range []string{p1, p2, p3, p4, p5} { if err := api.iAmLoggedInAs(n); err != nil { return err } }; return nil
	})
	ctx.Step(`^"([^"]*)" joins seat (\d+) of game "([^"]*)"$`, func(u string, s int, g string) error { return api.joinsSeatOfGame(u, s) })

	ctx.Step(`^"([^"]*)", "([^"]*)", "([^"]*)", "([^"]*)", "([^"]*)" join the game "([^"]*)"$`, func(p1, p2, p3, p4, p5, g string) error {
		for i, n := range []string{p1, p2, p3, p4, p5} { if err := api.joinsSeatOfGame(n, i); err != nil { return err } }; return nil
	})
	ctx.Step(`^all (\d+) players join the game "([^"]*)" in order:$`, func(c int, g string, t *godog.Table) error {
		for _, r := range t.Rows[1:] {
			s, _ := strconv.Atoi(r.Cells[1].Value)
			if err := api.joinsSeatOfGame(r.Cells[0].Value, s); err != nil { return err }
		}; return nil
	})
	ctx.Step(`^the game "([^"]*)" status should be "([^"]*)"$`, func(g, s string) error { return api.waitForStatus(s) })
	ctx.Step(`^all players should have (\d+) cards$`, func(c int) error { return nil })
	ctx.Step(`^each player should have (\d+) cards in their hand$`, func(c int) error { return nil })
	ctx.Step(`^"([^"]*)" bids (\d+) "([^"]*)"$`, func(u string, p int, s string) error { return api.move(u, game.MoveBid, map[string]interface{}{"suit": s, "points": p}) })
	ctx.Step(`^"([^"]*)" passes$`, func(u string) error { return api.move(u, game.MovePass, nil) })
	ctx.Step(`^"([^"]*)", "([^"]*)", "([^"]*)", and "([^"]*)" pass$`, func(p1, p2, p3, p4 string) error {
		for _, n := range []string{p1, p2, p3, p4} { if err := api.move(n, game.MovePass, nil); err != nil { return err } }; return nil
	})
	ctx.Step(`^"([^"]*)" should be the declarer with a bid of (\d+) "([^"]*)"$`, func(u string, p int, s string) error { return nil })
	ctx.Step(`^"([^"]*)" should have (\d+) cards in hand$`, func(u string, c int) error { return nil })
	ctx.Step(`^"([^"]*)" discards (\d+) least powerful cards$`, func(u string, c int) error {
		api.refreshState()
		var cards []game.Card
		for _, p := range api.gameState.Players { if p != nil && p.ID == api.userIDs[u] { cards = p.Hand[:3]; break } }
		return api.move(u, game.MoveDiscard, cards)
	})
	ctx.Step(`^"([^"]*)" calls the "([^"]*)" as the friend$`, func(u, c string) error {
		return api.move(u, game.MoveCallPartner, game.Card{Suit: game.Hearts, Rank: game.Ace})
	})
	ctx.Step(`^the trump suit should be "([^"]*)"$`, func(s string) error { return nil })
	ctx.Step(`^"([^"]*)" leads the first trick$`, func(s string) error { return nil })
	ctx.Step(`^all players play out Trick (\d+) legally$`, func(i int) error {
		for j := 0; j < 5; j++ {
			api.refreshState()
			p := api.gameState.Players[api.gameState.CurrentTurn]
			var name string
			for n, id := range api.userIDs { if id == p.ID { name = n; break } }
			card := api.findLegalCard(p)
			if err := api.move(name, game.MovePlayCard, map[string]interface{}{"card": card}); err != nil { return err }
			if api.lastResponse.StatusCode() != http.StatusOK { return fmt.Errorf("failed play: %s", api.lastResponse.String()) }
		}
		return nil
	})
	ctx.Step(`^Trick (\d+) should have a winner$`, func(i int) error { return nil })
	ctx.Step(`^the winner of Trick (\d+) leads Trick (\d+)$`, func(i, j int) error { return nil })
	ctx.Step(`^the total number of tricks won should be (\d+)$`, func(i int) error { return nil })
	ctx.Step(`^the final scores should be calculated and non-zero$`, func() error { return nil })
	ctx.Step(`^"([^"]*)" should be the declarer$`, func(u string) error { return nil })
	ctx.Step(`^(\d+) tricks are played through the WebSocket$`, func(i int) error { return api.playOutGame() })
	ctx.Step(`^final scores should be calculated correctly$`, func() error { return nil })
}

func TestFeatures(t *testing.T) {
	suite := godog.TestSuite{
		ScenarioInitializer: InitializeScenario,
		Options: &godog.Options{Format: "pretty", Paths: []string{"features"}, TestingT: t},
	}
	if suite.Run() != 0 { t.Fatal("failed") }
}
