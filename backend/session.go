package main

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"psd2-issuer/credentials"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

// Session statuses, in the order a successful flow goes through them.
const (
	// StatusAwaitingConsent: the user was sent to the enablenow AppScreen.
	StatusAwaitingConsent = "awaiting_consent"
	// StatusAwaitingTransactions: a consent was given, enablenow is fetching
	// the transactions from the bank.
	StatusAwaitingTransactions = "awaiting_transactions"
	// StatusCategorizing: a categorization job is running.
	StatusCategorizing = "categorizing"
	// StatusReady: the credentials are computed and can be issued.
	StatusReady = "ready"
	// StatusFailed: the flow failed, see Session.Error.
	StatusFailed = "failed"
	// StatusVerifying: a demo verifier session, see Session.Verification.
	// The issuance flow never enters or leaves it.
	StatusVerifying = "verifying"
)

// SessionTTL bounds how long a session, including the computed credential
// values, is kept.
const SessionTTL = 30 * time.Minute

var ErrSessionNotFound = errors.New("session not found")

type Session struct {
	Id string `json:"id"`
	// State is the anti-CSRF value passed through the AppScreen and checked
	// when the consent id comes back.
	State       string                   `json:"state"`
	Status      string                   `json:"status"`
	Error       string                   `json:"error,omitempty"`
	ConsentId   string                   `json:"consent_id,omitempty"`
	ProviderKey string                   `json:"provider_key,omitempty"`
	JobId       string                   `json:"job_id,omitempty"`
	Credentials []credentials.Credential `json:"credentials,omitempty"`
	CreatedAt   time.Time                `json:"created_at"`
	// StepStartedAt is when the current status was entered, used to time
	// out flows that never complete.
	StepStartedAt time.Time `json:"step_started_at"`
	// LastCheckedAt is when enablenow was last polled for this session.
	LastCheckedAt time.Time `json:"last_checked_at"`
	// TransientErrors counts consecutive failed calls to enablenow.
	TransientErrors int `json:"transient_errors,omitempty"`
	// Verification is set on demo verifier sessions only.
	Verification *Verification `json:"verification,omitempty"`
}

func NewSession(now time.Time) *Session {
	return &Session{
		Id:            rand.Text(),
		State:         rand.Text(),
		Status:        StatusAwaitingConsent,
		CreatedAt:     now,
		StepStartedAt: now,
	}
}

func (s *Session) setStatus(status string, now time.Time) {
	s.Status = status
	s.StepStartedAt = now
	s.TransientErrors = 0
}

func (s *Session) fail(errorCode string, now time.Time) {
	s.setStatus(StatusFailed, now)
	s.Error = errorCode
}

// SessionStorage must be safe for concurrent use.
type SessionStorage interface {
	// Get returns ErrSessionNotFound when there is no (unexpired) session.
	Get(id string) (*Session, error)
	Put(session *Session) error
	Delete(id string) error
}

// ------------------------------------------------------------------------------

type InMemorySessionStorage struct {
	mutex    sync.Mutex
	sessions map[string]inMemorySession
	now      func() time.Time
}

type inMemorySession struct {
	data   []byte
	expiry time.Time
}

func NewInMemorySessionStorage() *InMemorySessionStorage {
	return &InMemorySessionStorage{sessions: map[string]inMemorySession{}, now: time.Now}
}

func (s *InMemorySessionStorage) Get(id string) (*Session, error) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	entry, ok := s.sessions[id]
	if !ok || s.now().After(entry.expiry) {
		delete(s.sessions, id)
		return nil, ErrSessionNotFound
	}
	var session Session
	if err := json.Unmarshal(entry.data, &session); err != nil {
		return nil, err
	}
	return &session, nil
}

func (s *InMemorySessionStorage) Put(session *Session) error {
	data, err := json.Marshal(session)
	if err != nil {
		return err
	}
	s.mutex.Lock()
	defer s.mutex.Unlock()
	// drop expired sessions so the map cannot grow without bound
	now := s.now()
	for id, entry := range s.sessions {
		if now.After(entry.expiry) {
			delete(s.sessions, id)
		}
	}
	s.sessions[session.Id] = inMemorySession{data: data, expiry: session.CreatedAt.Add(SessionTTL)}
	return nil
}

func (s *InMemorySessionStorage) Delete(id string) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	delete(s.sessions, id)
	return nil
}

// ------------------------------------------------------------------------------

type RedisSessionStorage struct {
	client    *redis.Client
	namespace string
}

func NewRedisSessionStorage(client *redis.Client, namespace string) *RedisSessionStorage {
	return &RedisSessionStorage{client: client, namespace: namespace}
}

func (s *RedisSessionStorage) key(id string) string {
	return fmt.Sprintf("%s:session:%s", s.namespace, id)
}

func (s *RedisSessionStorage) Get(id string) (*Session, error) {
	data, err := s.client.Get(context.Background(), s.key(id)).Bytes()
	if errors.Is(err, redis.Nil) {
		return nil, ErrSessionNotFound
	}
	if err != nil {
		return nil, err
	}
	var session Session
	if err := json.Unmarshal(data, &session); err != nil {
		return nil, err
	}
	return &session, nil
}

func (s *RedisSessionStorage) Put(session *Session) error {
	data, err := json.Marshal(session)
	if err != nil {
		return err
	}
	ttl := time.Until(session.CreatedAt.Add(SessionTTL))
	if ttl <= 0 {
		return s.Delete(session.Id)
	}
	return s.client.Set(context.Background(), s.key(session.Id), data, ttl).Err()
}

func (s *RedisSessionStorage) Delete(id string) error {
	return s.client.Del(context.Background(), s.key(id)).Err()
}
