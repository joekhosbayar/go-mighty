package infra

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

// IMPLEMENT PING, SET, GET STUBS
type fakeRedis struct {
	pingErr error
	getVal  string
	getErr  error
	setErr  error
}

func (f *fakeRedis) Ping(ctx context.Context) *redis.StatusCmd {
	cmd := redis.NewStatusCmd(ctx)
	if f.pingErr != nil {
		cmd.SetErr(f.pingErr)
	} else {
		cmd.SetVal("PONG")
	}
	return cmd
}

func (f *fakeRedis) Set(
	ctx context.Context,
	key string,
	value interface{},
	expiration time.Duration,
) *redis.StatusCmd {
	cmd := redis.NewStatusCmd(ctx)
	if f.setErr != nil {
		cmd.SetErr(f.setErr)
	}
	return cmd
}

func (f *fakeRedis) Get(ctx context.Context, key string) *redis.StringCmd {
	cmd := redis.NewStringCmd(ctx)
	if f.getErr != nil {
		cmd.SetErr(f.getErr)
	} else {
		cmd.SetVal(f.getVal)
	}
	return cmd
}

// BEGIN UNIT TESTS HERE

func TestPingSuccess(t *testing.T) {
	fakeClient := &fakeRedis{}
	client := Redis{
		client: fakeClient,
	}

	pong, err := client.Ping(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if pong != "PONG" {
		t.Fatal("pong != PONG")
	}
}

func TestPingFailure(t *testing.T) {
	expectedErr := errors.New("connection refused")
	fakeClient := &fakeRedis{
		pingErr: expectedErr,
	}

	client := Redis{
		client: fakeClient,
	}

	_, err := client.Ping(context.Background())
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	if err.Error() != expectedErr.Error() {
		t.Fatalf("expected %v, got %v", expectedErr, err)
	}

}

func TestSetSuccess(t *testing.T) {
	fakeClient := &fakeRedis{}
	client := Redis{
		client: fakeClient,
	}
	err := client.Set(context.Background(), "key", "value", time.Second)
	if err != nil {
		t.Fatal(err)
	}
}

func TestSetFailure(t *testing.T) {
	expectedErr := errors.New("connection refused")
	fakeClient := &fakeRedis{
		setErr: expectedErr,
	}
	client := Redis{
		client: fakeClient,
	}
	err := client.Set(context.Background(), "key", "value", time.Second)

	if err == nil {
		t.Fatal("expected error, got nil")
	}

	if err.Error() != expectedErr.Error() {
		t.Fatalf("expected %v, got %v", expectedErr, err)
	}
}

func TestGetSuccess(t *testing.T) {
	fakeClient := &fakeRedis{}
	client := Redis{
		client: fakeClient,
	}

	_, err := client.Get(context.Background(), "key")
	if err != nil {
		t.Fatal(err)
	}
}

func TestGetFailure(t *testing.T) {
	expectedErr := errors.New("connection refused")
	fakeClient := &fakeRedis{
		getErr: expectedErr,
	}
	client := Redis{
		client: fakeClient,
	}
	_, err := client.Get(context.Background(), "key")

	if err == nil {
		t.Fatal("expected error, got nil")
	}

	if err.Error() != expectedErr.Error() {
		t.Fatalf("expected %v, got %v", expectedErr, err)
	}
}
