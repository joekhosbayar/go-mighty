package service

import (
	"context"
	"errors"
	"testing"
	"time"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
	"github.com/joekhosbayar/go-mighty/internal/game"
	"github.com/joekhosbayar/go-mighty/internal/store/postgres"
	redisstore "github.com/joekhosbayar/go-mighty/internal/store/redis"
	"github.com/redis/go-redis/v9"
)

type publishedEvent struct {
	channel string
	payload any
}

type fakeRedisStore struct {
	game            *game.Game
	saved           bool
	savedWith       int64
	acquireErr      error
	publishedEvents []publishedEvent
}

func (f *fakeRedisStore) SaveGame(_ context.Context, g *game.Game, expectedVersion int64) error {
	f.saved = true
	f.savedWith = expectedVersion
	f.game = g

	return nil
}

func (f *fakeRedisStore) LoadGame(_ context.Context, _ string) (*game.Game, error) {
	return f.game, nil
}

func (f *fakeRedisStore) AcquireLock(_ context.Context, _ string) (string, error) {
	if f.acquireErr != nil {
		return "", f.acquireErr
	}

	return "test-token", nil
}

func (f *fakeRedisStore) ReleaseLock(_ context.Context, _, _ string) error {
	return nil
}

func (f *fakeRedisStore) PublishEvent(_ context.Context, channel string, payload any) error {
	f.publishedEvents = append(f.publishedEvents, publishedEvent{
		channel: channel,
		payload: payload,
	})
	return nil
}

func (f *fakeRedisStore) Subscribe(_ context.Context, _ string) *redis.PubSub {
	return nil
}

func TestJoinGameRejoinSameSeatRefreshesConnectionState(t *testing.T) {
	t.Parallel()
	g := game.New("game-1")
	g.Players[0] = &game.Player{
		ID:          "player-1",
		Name:        "Old Name",
		Seat:        0,
		IsConnected: false,
	}
	g.Version = 3
	g.UpdatedAt = time.Now().Add(-time.Minute)
	prevUpdatedAt := g.UpdatedAt

	redisStore := &fakeRedisStore{game: g}
	svc := &Game{redisStore: redisStore}

	updatedGame, err := svc.JoinGame(t.Context(), "game-1", "player-1", "New Name")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if !redisStore.saved {
		t.Fatalf("expected game to be persisted on rejoin")
	}

	if updatedGame.Players[0].Name != "New Name" {
		t.Fatalf("expected player name to be refreshed, got %q", updatedGame.Players[0].Name)
	}

	if !updatedGame.Players[0].IsConnected {
		t.Fatalf("expected player to be marked connected")
	}

	if updatedGame.Version != 4 {
		t.Fatalf("expected version to increment to 4, got %d", updatedGame.Version)
	}

	if !updatedGame.UpdatedAt.After(prevUpdatedAt) {
		t.Fatalf("expected updated_at to be refreshed")
	}
}

func TestProcessMoveReturnsBusyWhenLockContended(t *testing.T) {
	t.Parallel()

	g := game.New("game-busy")
	store := &fakeRedisStore{game: g, acquireErr: redisstore.ErrLockFailed}
	svc := &Game{redisStore: store}

	_, err := svc.ProcessMove(t.Context(), "game-busy", "p1", game.MovePass, nil, 1)
	if !errors.Is(err, ErrGameBusy) {
		t.Fatalf("expected ErrGameBusy, got %v", err)
	}

	if store.saved {
		t.Fatal("no save should happen when the lock is contended")
	}
}

func TestProcessMoveRejectsStaleClientVersion(t *testing.T) {
	t.Parallel()

	g := game.New("game-stale")
	g.Version = 5
	store := &fakeRedisStore{game: g}
	svc := &Game{redisStore: store}

	_, err := svc.ProcessMove(t.Context(), "game-stale", "p1", game.MovePass, nil, 4)
	if !errors.Is(err, redisstore.ErrStaleVersion) {
		t.Fatalf("expected ErrStaleVersion, got %v", err)
	}

	if store.saved {
		t.Fatal("no save should happen on stale version")
	}
}

func TestJoinGameSavesWithLoadedVersionAsCASExpectation(t *testing.T) {
	t.Parallel()

	g := game.New("game-cas")
	g.Version = 7
	g.Players[0] = &game.Player{ID: "p1", Name: "P1", Seat: 0, IsConnected: false}
	store := &fakeRedisStore{game: g}
	svc := &Game{redisStore: store}

	if _, err := svc.JoinGame(t.Context(), "game-cas", "p1", "P1"); err != nil {
		t.Fatalf("join: %v", err)
	}

	if store.savedWith != 7 {
		t.Fatalf("expected CAS expectation 7 (pre-bump version), got %d", store.savedWith)
	}
}

func TestCreateGamePublishesLobbyEvent(t *testing.T) {
	t.Parallel()

	svc, _ := newTestServiceWithConfig(t)
	fakeStore := &fakeRedisStore{}
	svc.redisStore = fakeStore

	cfg := game.GameConfig{NumPlayers: 5}
	g, err := svc.CreateGame(t.Context(), "game-create-lobby", cfg)
	if err != nil {
		t.Fatalf("CreateGame failed: %v", err)
	}

	foundLobbyEvent := false
	for _, evt := range fakeStore.publishedEvents {
		if evt.channel == "lobby_events" {
			m, ok := evt.payload.(map[string]any)
			if ok && m["type"] == "game_created" && m["game"] == g {
				foundLobbyEvent = true
				break
			}
		}
	}

	if !foundLobbyEvent {
		t.Fatalf("expected lobby_events game_created event to be published, got events: %+v", fakeStore.publishedEvents)
	}
}

func TestJoinGamePublishesLobbyEvent(t *testing.T) {
	t.Parallel()

	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	mock.ExpectExec(`INSERT INTO moves`).
		WillReturnResult(sqlmock.NewResult(1, 1))

	g := game.New("game-join-lobby")
	g.Players[0] = &game.Player{ID: "p1", Name: "Player 1", Seat: 0, IsConnected: true}

	fakeStore := &fakeRedisStore{game: g}
	svc := &Game{
		redisStore:    fakeStore,
		postgresStore: postgres.NewStoreWithDB(db),
	}

	_, err = svc.JoinGame(t.Context(), "game-join-lobby", "p2", "Player 2")
	if err != nil {
		t.Fatalf("JoinGame failed: %v", err)
	}

	var lobbyEvent map[string]any
	var gameEvent map[string]any

	for _, evt := range fakeStore.publishedEvents {
		if evt.channel == "lobby_events" {
			if m, ok := evt.payload.(map[string]any); ok {
				lobbyEvent = m
			}
		}
		if evt.channel == "game-join-lobby" {
			if m, ok := evt.payload.(map[string]any); ok {
				gameEvent = m
			}
		}
	}

	if gameEvent == nil || gameEvent["type"] != "player_joined" {
		t.Fatalf("expected player_joined event on game channel, got %+v", gameEvent)
	}

	if lobbyEvent == nil || lobbyEvent["type"] != "game_joined" {
		t.Fatalf("expected game_joined event on lobby_events channel, got %+v", lobbyEvent)
	}

	if lobbyEvent["game_id"] != "game-join-lobby" {
		t.Errorf("expected game_id game-join-lobby, got %v", lobbyEvent["game_id"])
	}

	if lobbyEvent["players_seated"] != 2 {
		t.Errorf("expected players_seated 2, got %v", lobbyEvent["players_seated"])
	}

	if lobbyEvent["max_players"] != 5 {
		t.Errorf("expected max_players 5, got %v", lobbyEvent["max_players"])
	}
}
