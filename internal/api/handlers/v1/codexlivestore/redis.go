package codexlivestore

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// Redis returns a Redis-backed store. ttl is the default duration applied when
// Put sees a zero ExpiresAt.
func Redis(client *redis.Client, ttl time.Duration) Store {
	if ttl <= 0 {
		ttl = time.Hour
	}
	return &redisStore{client: client, ttl: ttl}
}

type redisStore struct {
	client *redis.Client
	ttl    time.Duration
}

const redisKeyPrefix = "codex:live:session:"

func redisKey(callID string) string {
	return redisKeyPrefix + callID
}

func (r *redisStore) Get(ctx context.Context, callID string) (Session, bool, error) {
	if callID == "" {
		return Session{}, false, ErrInvalidCallID
	}
	data, err := r.client.Get(ctx, redisKey(callID)).Bytes()
	if err == redis.Nil {
		return Session{}, false, nil
	}
	if err != nil {
		return Session{}, false, err
	}
	var sess Session
	if err := json.Unmarshal(data, &sess); err != nil {
		return Session{}, false, fmt.Errorf("decode codex live session: %w", err)
	}
	if isExpired(time.Now(), sess) {
		_ = r.client.Del(ctx, redisKey(callID))
		return Session{}, false, nil
	}
	return sess, true, nil
}

func (r *redisStore) Put(ctx context.Context, sess Session) error {
	if sess.CallID == "" {
		return ErrInvalidCallID
	}
	if sess.ExpiresAt.IsZero() {
		sess.ExpiresAt = time.Now().Add(r.ttl)
	}
	if sess.CreatedAt.IsZero() {
		sess.CreatedAt = time.Now()
	}
	data, err := json.Marshal(sess)
	if err != nil {
		return fmt.Errorf("encode codex live session: %w", err)
	}
	return r.client.Set(ctx, redisKey(sess.CallID), data, time.Until(sess.ExpiresAt)).Err()
}

func (r *redisStore) Delete(ctx context.Context, callID string) error {
	if callID == "" {
		return ErrInvalidCallID
	}
	return r.client.Del(ctx, redisKey(callID)).Err()
}
