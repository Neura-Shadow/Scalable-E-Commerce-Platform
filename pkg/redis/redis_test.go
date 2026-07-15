package redis

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sort"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	goredis "github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIncrementWithExpirationSetsTTLOnFirstIncrement(t *testing.T) {
	cache, client := newRedisIntegrationClient(t)
	key := redisTestKey(t)
	defer client.Del(context.Background(), key)

	count, err := cache.IncrementWithExpiration(key, 30*time.Second)

	require.NoError(t, err)
	assert.Equal(t, int64(1), count)
	assertPositiveTTL(t, client, key, 30*time.Second)
}

func TestIncrementWithExpirationPreservesExistingTTLWindow(t *testing.T) {
	cache, client := newRedisIntegrationClient(t)
	key := redisTestKey(t)
	defer client.Del(context.Background(), key)

	_, err := cache.IncrementWithExpiration(key, 30*time.Second)
	require.NoError(t, err)
	before, err := client.PTTL(context.Background(), key).Result()
	require.NoError(t, err)
	time.Sleep(25 * time.Millisecond)

	count, err := cache.IncrementWithExpiration(key, 30*time.Second)
	require.NoError(t, err)
	after, err := client.PTTL(context.Background(), key).Result()
	require.NoError(t, err)

	assert.Equal(t, int64(2), count)
	assert.Less(t, after, before, "later increments must not restart the rate-limit window")
}

func TestIncrementWithExpirationRepairsLegacyPermanentCounter(t *testing.T) {
	cache, client := newRedisIntegrationClient(t)
	key := redisTestKey(t)
	defer client.Del(context.Background(), key)
	require.NoError(t, client.Set(context.Background(), key, 7, 0).Err())

	count, err := cache.IncrementWithExpiration(key, 30*time.Second)

	require.NoError(t, err)
	assert.Equal(t, int64(8), count)
	assertPositiveTTL(t, client, key, 30*time.Second)
}

func TestIncrementWithExpirationIsConsistentUnderConcurrency(t *testing.T) {
	cache, client := newRedisIntegrationClient(t)
	key := redisTestKey(t)
	defer client.Del(context.Background(), key)

	const workers = 32
	counts := make([]int64, workers)
	errs := make([]error, workers)
	var wg sync.WaitGroup
	wg.Add(workers)
	for i := 0; i < workers; i++ {
		go func(index int) {
			defer wg.Done()
			counts[index], errs[index] = cache.IncrementWithExpiration(key, time.Minute)
		}(i)
	}
	wg.Wait()

	for _, err := range errs {
		require.NoError(t, err)
	}
	sort.Slice(counts, func(i, j int) bool { return counts[i] < counts[j] })
	for i, count := range counts {
		assert.Equal(t, int64(i+1), count)
	}
	assertPositiveTTL(t, client, key, time.Minute)
}

func TestIncrementWithExpirationDoesNotDependOnSeparateExpireCommand(t *testing.T) {
	cache, client := newRedisIntegrationClient(t)
	key := redisTestKey(t)
	defer client.Del(context.Background(), key)

	client.AddHook(rejectExpireHook{})
	count, err := cache.IncrementWithExpiration(key, 30*time.Second)

	require.NoError(t, err)
	assert.Equal(t, int64(1), count)
	assertPositiveTTL(t, client, key, 30*time.Second)
}

func TestIncrementWithExpirationReturnsRedisFailure(t *testing.T) {
	client := goredis.NewClient(&goredis.Options{
		Addr:         "127.0.0.1:1",
		DialTimeout:  50 * time.Millisecond,
		ReadTimeout:  50 * time.Millisecond,
		WriteTimeout: 50 * time.Millisecond,
		MaxRetries:   0,
	})
	t.Cleanup(func() { _ = client.Close() })
	cache := &redis{cmd: client, client: client}

	count, err := cache.IncrementWithExpiration(redisTestKey(t), time.Minute)

	assert.Error(t, err)
	assert.Zero(t, count)
}

func TestKeysUsesCursorScanInsteadOfBlockingKeysCommand(t *testing.T) {
	cache, client := newRedisIntegrationClient(t)
	prefix := redisTestKey(t)
	keys := []string{prefix + ":1", prefix + ":2"}
	for _, key := range keys {
		require.NoError(t, client.Set(context.Background(), key, "value", time.Minute).Err())
	}
	defer client.Del(context.Background(), keys...)
	client.AddHook(rejectKeysHook{})

	found, err := cache.Keys(prefix + ":*")

	require.NoError(t, err)
	sort.Strings(found)
	assert.Equal(t, keys, found)
}

func TestRemovePatternDeletesMultipleScanBatches(t *testing.T) {
	cache, client := newRedisIntegrationClient(t)
	prefix := redisTestKey(t)
	keys := make([]string, 250)
	for index := range keys {
		keys[index] = fmt.Sprintf("%s:%03d", prefix, index)
	}
	require.NoError(t, client.MSet(context.Background(), stringPairs(keys, "value")...).Err())
	defer client.Del(context.Background(), keys...)
	client.AddHook(rejectKeysHook{})

	require.NoError(t, cache.RemovePattern(prefix+":*"))

	remaining, err := cache.Keys(prefix + ":*")
	require.NoError(t, err)
	assert.Empty(t, remaining)
}

func stringPairs(keys []string, value string) []interface{} {
	pairs := make([]interface{}, 0, len(keys)*2)
	for _, key := range keys {
		pairs = append(pairs, key, value)
	}
	return pairs
}

func newRedisIntegrationClient(t *testing.T) (*redis, *goredis.Client) {
	t.Helper()

	address := os.Getenv("redis_uri")
	if address == "" {
		address = "127.0.0.1:6379"
	}
	database, err := strconv.Atoi(os.Getenv("redis_db"))
	if err != nil {
		database = 0
	}
	client := goredis.NewClient(&goredis.Options{
		Addr:     address,
		Password: os.Getenv("redis_password"),
		DB:       database,
		PoolSize: 128,
	})
	t.Cleanup(func() { _ = client.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := client.Ping(ctx).Err(); err != nil {
		t.Skipf("Redis integration service is unavailable at %s: %v", address, err)
	}

	return &redis{cmd: client, client: client}, client
}

func redisTestKey(t *testing.T) string {
	t.Helper()
	return fmt.Sprintf("test:redis:%s:%s", t.Name(), uuid.NewString())
}

func assertPositiveTTL(t *testing.T, client *goredis.Client, key string, maximum time.Duration) {
	t.Helper()
	ttl, err := client.PTTL(context.Background(), key).Result()
	require.NoError(t, err)
	assert.Greater(t, ttl, time.Duration(0), "counter key must never be permanent")
	assert.LessOrEqual(t, ttl, maximum)
}

type rejectExpireHook struct{}

func (rejectExpireHook) DialHook(next goredis.DialHook) goredis.DialHook {
	return next
}

func (rejectExpireHook) ProcessHook(next goredis.ProcessHook) goredis.ProcessHook {
	return func(ctx context.Context, cmd goredis.Cmder) error {
		if cmd.Name() == "expire" || cmd.Name() == "pexpire" {
			return errors.New("direct expiration commands are rejected by the test")
		}
		return next(ctx, cmd)
	}
}

func (rejectExpireHook) ProcessPipelineHook(next goredis.ProcessPipelineHook) goredis.ProcessPipelineHook {
	return next
}

type rejectKeysHook struct{}

func (rejectKeysHook) DialHook(next goredis.DialHook) goredis.DialHook {
	return next
}

func (rejectKeysHook) ProcessHook(next goredis.ProcessHook) goredis.ProcessHook {
	return func(ctx context.Context, cmd goredis.Cmder) error {
		if cmd.Name() == "keys" {
			return errors.New("blocking KEYS command is rejected by the test")
		}
		return next(ctx, cmd)
	}
}

func (rejectKeysHook) ProcessPipelineHook(next goredis.ProcessPipelineHook) goredis.ProcessPipelineHook {
	return next
}
