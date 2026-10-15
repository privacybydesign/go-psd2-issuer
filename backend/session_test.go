package main

import (
	"testing"
	"time"

	"psd2-issuer/credentials"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func testSessionStorage(t *testing.T, storage SessionStorage) {
	t.Helper()
	_, err := storage.Get("missing")
	require.ErrorIs(t, err, ErrSessionNotFound)

	session := NewSession(time.Now())
	session.Credentials = []credentials.Credential{{Key: "income", Attributes: map[string]string{"salary": "3000"}}}
	require.NoError(t, storage.Put(session))

	got, err := storage.Get(session.Id)
	require.NoError(t, err)
	require.Equal(t, session.State, got.State)
	require.Equal(t, "3000", got.Credentials[0].Attributes["salary"])

	require.NoError(t, storage.Delete(session.Id))
	_, err = storage.Get(session.Id)
	require.ErrorIs(t, err, ErrSessionNotFound)
}

func TestInMemorySessionStorage(t *testing.T) {
	testSessionStorage(t, NewInMemorySessionStorage())
}

func TestInMemorySessionStorageExpires(t *testing.T) {
	storage := NewInMemorySessionStorage()
	now := time.Now()
	storage.now = func() time.Time { return now }
	session := NewSession(now)
	require.NoError(t, storage.Put(session))

	now = now.Add(SessionTTL + time.Second)
	_, err := storage.Get(session.Id)
	require.ErrorIs(t, err, ErrSessionNotFound)
}

func TestRedisSessionStorage(t *testing.T) {
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	storage := NewRedisSessionStorage(client, "test")
	testSessionStorage(t, storage)

	session := NewSession(time.Now())
	require.NoError(t, storage.Put(session))
	require.True(t, mr.Exists("test:session:"+session.Id))
	mr.FastForward(SessionTTL + time.Second)
	_, err := storage.Get(session.Id)
	require.ErrorIs(t, err, ErrSessionNotFound)
}

func TestNewSessionIdsAreUnique(t *testing.T) {
	a, b := NewSession(time.Now()), NewSession(time.Now())
	require.NotEqual(t, a.Id, b.Id)
	require.NotEqual(t, a.Id, a.State)
	require.GreaterOrEqual(t, len(a.Id), 26)
}
