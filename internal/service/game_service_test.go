package service

import (
	"context"
	"testing"
	"time"

	"github.com/joekhosbayar/go-mighty/internal/game"
	"github.com/redis/go-redis/v9"
)

type fakeRedisStore struct {
	game  *game.GameState
	saved bool
}

func (f *fakeRedisStore) SaveGame(ctx context.Context, g *game.GameState) error {
	f.saved = true
	f.game = g
	return nil
}

func (f *fakeRedisStore) LoadGame(ctx context.Context, gameID string) (*game.GameState, error) {
	return f.game, nil
}

func (f *fakeRedisStore) AcquireLock(ctx context.Context, gameID string) (bool, error) {
	return true, nil
}

func (f *fakeRedisStore) ReleaseLock(ctx context.Context, gameID string) error {
	return nil
}

func (f *fakeRedisStore) CheckVersion(ctx context.Context, gameID string, clientVersion int64) error {
	return nil
}

func (f *fakeRedisStore) PublishEvent(ctx context.Context, gameID string, event interface{}) error {
	return nil
}

func (f *fakeRedisStore) Subscribe(ctx context.Context, gameID string) *redis.PubSub {
	return nil
}

func TestJoinGameRejoinSameSeatRefreshesConnectionState(t *testing.T) {
	g := game.NewGame("game-1")
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
	svc := &GameService{redisStore: redisStore}

	updatedGame, err := svc.JoinGame(context.Background(), "game-1", "player-1", "New Name")
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
