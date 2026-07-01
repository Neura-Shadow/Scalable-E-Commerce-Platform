package redis

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	goredis "github.com/go-redis/redis/v8"

	"github.com/quangdangfit/gocommon/logger"
)

const (
	Timeout = 1
)

// IRedis interface
//
//go:generate mockery --name=IRedis
type IRedis interface {
	IsConnected() bool
	Get(key string, value interface{}) error
	Exists(ctx context.Context, key string) (bool, error)
	Set(key string, value interface{}) error
	SetWithExpiration(key string, value interface{}, expiration time.Duration) error
	SetNXWithExpiration(key string, value interface{}, expiration time.Duration) (bool, error)
	IncrementWithExpiration(key string, expiration time.Duration) (int64, error)
	XAdd(ctx context.Context, stream string, values map[string]interface{}) (string, error)
	XGroupCreateMkStream(ctx context.Context, stream, group, start string) error
	XReadGroup(ctx context.Context, group, consumer, stream string, count int64, block time.Duration) ([]RedisStreamMessage, error)
	XAck(ctx context.Context, stream, group string, ids ...string) error
	XAutoClaim(ctx context.Context, stream, group, consumer, start string, minIdle time.Duration, count int64) ([]RedisStreamMessage, string, error)
	Remove(keys ...string) error
	Keys(pattern string) ([]string, error)
	RemovePattern(pattern string) error
}

type RedisStreamMessage struct {
	ID     string
	Values map[string]interface{}
}

// Config redis
type Config struct {
	Address  string
	Password string
	Database int
}

type redis struct {
	cmd    goredis.Cmdable
	client *goredis.Client
}

// New Redis interface with config
func New(config Config) IRedis {
	ctx, cancel := context.WithTimeout(context.Background(), Timeout*time.Second)
	defer cancel()

	rdb := goredis.NewClient(&goredis.Options{
		Addr:     config.Address,
		Password: config.Password,
		DB:       config.Database,
	})

	pong, err := rdb.Ping(ctx).Result()
	if err != nil {
		logger.Fatal(pong, err)
		return nil
	}

	return &redis{
		cmd:    rdb,
		client: rdb,
	}
}

func (r *redis) IsConnected() bool {
	ctx, cancel := context.WithTimeout(context.Background(), Timeout*time.Second)
	defer cancel()

	if r.cmd == nil {
		return false
	}

	_, err := r.cmd.Ping(ctx).Result()
	if err != nil {
		return false
	}
	return true
}

func (r *redis) Get(key string, value interface{}) error {
	ctx, cancel := context.WithTimeout(context.Background(), Timeout*time.Second)
	defer cancel()

	strValue, err := r.cmd.Get(ctx, key).Result()
	if err != nil {
		return err
	}

	err = json.Unmarshal([]byte(strValue), value)
	if err != nil {
		return err
	}

	return nil
}

func (r *redis) Exists(ctx context.Context, key string) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, Timeout*time.Second)
	defer cancel()

	count, err := r.cmd.Exists(ctx, key).Result()
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

func (r *redis) SetWithExpiration(key string, value interface{}, expiration time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), Timeout*time.Second)
	defer cancel()

	bData, _ := json.Marshal(value)
	err := r.cmd.Set(ctx, key, bData, expiration).Err()
	if err != nil {
		return err
	}

	return nil
}

func (r *redis) SetNXWithExpiration(key string, value interface{}, expiration time.Duration) (bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), Timeout*time.Second)
	defer cancel()

	bData, _ := json.Marshal(value)
	return r.cmd.SetNX(ctx, key, bData, expiration).Result()
}

func (r *redis) IncrementWithExpiration(key string, expiration time.Duration) (int64, error) {
	ctx, cancel := context.WithTimeout(context.Background(), Timeout*time.Second)
	defer cancel()

	count, err := r.cmd.Incr(ctx, key).Result()
	if err != nil {
		return 0, err
	}
	if count == 1 && expiration > 0 {
		if err := r.cmd.Expire(ctx, key, expiration).Err(); err != nil {
			return 0, err
		}
	}

	return count, nil
}

func (r *redis) XAdd(ctx context.Context, stream string, values map[string]interface{}) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, Timeout*time.Second)
	defer cancel()

	return r.cmd.XAdd(ctx, &goredis.XAddArgs{
		Stream: stream,
		Values: values,
	}).Result()
}

func (r *redis) XGroupCreateMkStream(ctx context.Context, stream, group, start string) error {
	ctx, cancel := context.WithTimeout(ctx, Timeout*time.Second)
	defer cancel()

	return r.cmd.XGroupCreateMkStream(ctx, stream, group, start).Err()
}

func (r *redis) XReadGroup(ctx context.Context, group, consumer, stream string, count int64, block time.Duration) ([]RedisStreamMessage, error) {
	ctx, cancel := context.WithTimeout(ctx, block+Timeout*time.Second)
	defer cancel()

	streams, err := r.cmd.XReadGroup(ctx, &goredis.XReadGroupArgs{
		Group:    group,
		Consumer: consumer,
		Streams:  []string{stream, ">"},
		Count:    count,
		Block:    block,
	}).Result()
	if err != nil {
		if err == goredis.Nil {
			return nil, nil
		}
		return nil, err
	}

	return toRedisStreamMessages(streams), nil
}

func (r *redis) XAck(ctx context.Context, stream, group string, ids ...string) error {
	ctx, cancel := context.WithTimeout(ctx, Timeout*time.Second)
	defer cancel()

	return r.cmd.XAck(ctx, stream, group, ids...).Err()
}

func (r *redis) XAutoClaim(
	ctx context.Context,
	stream string,
	group string,
	consumer string,
	start string,
	minIdle time.Duration,
	count int64,
) ([]RedisStreamMessage, string, error) {
	ctx, cancel := context.WithTimeout(ctx, Timeout*time.Second)
	defer cancel()

	if r.client == nil {
		return nil, "", fmt.Errorf("redis client is required")
	}

	result, err := r.client.Do(
		ctx,
		"XAUTOCLAIM",
		stream,
		group,
		consumer,
		minIdle.Milliseconds(),
		start,
		"COUNT",
		count,
	).Result()
	if err != nil {
		if err == goredis.Nil {
			return nil, start, nil
		}
		return nil, "", err
	}

	messages, nextStart, err := parseXAutoClaimResult(result)
	if err != nil {
		return nil, "", err
	}

	return messages, nextStart, nil
}

func toRedisStreamMessages(streams []goredis.XStream) []RedisStreamMessage {
	messages := make([]RedisStreamMessage, 0)
	for _, stream := range streams {
		for _, message := range stream.Messages {
			messages = append(messages, RedisStreamMessage{
				ID:     message.ID,
				Values: message.Values,
			})
		}
	}
	return messages
}

func parseXAutoClaimResult(result interface{}) ([]RedisStreamMessage, string, error) {
	items, ok := result.([]interface{})
	if !ok || len(items) < 2 {
		return nil, "", fmt.Errorf("unexpected XAUTOCLAIM response")
	}

	nextStart, err := streamValueToString(items[0])
	if err != nil {
		return nil, "", err
	}

	rawMessages, ok := items[1].([]interface{})
	if !ok {
		return nil, "", fmt.Errorf("unexpected XAUTOCLAIM messages response")
	}

	messages := make([]RedisStreamMessage, 0, len(rawMessages))
	for _, rawMessage := range rawMessages {
		pair, ok := rawMessage.([]interface{})
		if !ok || len(pair) != 2 {
			return nil, "", fmt.Errorf("unexpected XAUTOCLAIM message response")
		}

		id, err := streamValueToString(pair[0])
		if err != nil {
			return nil, "", err
		}

		values, err := streamFieldValuesToMap(pair[1])
		if err != nil {
			return nil, "", err
		}
		messages = append(messages, RedisStreamMessage{ID: id, Values: values})
	}

	return messages, nextStart, nil
}

func streamFieldValuesToMap(raw interface{}) (map[string]interface{}, error) {
	fields, ok := raw.([]interface{})
	if !ok || len(fields)%2 != 0 {
		return nil, fmt.Errorf("unexpected stream field values")
	}

	values := make(map[string]interface{}, len(fields)/2)
	for i := 0; i < len(fields); i += 2 {
		key, err := streamValueToString(fields[i])
		if err != nil {
			return nil, err
		}
		values[key] = fields[i+1]
	}
	return values, nil
}

func streamValueToString(value interface{}) (string, error) {
	switch v := value.(type) {
	case string:
		return v, nil
	case []byte:
		return string(v), nil
	default:
		return fmt.Sprint(v), nil
	}
}

func (r *redis) Set(key string, value interface{}) error {
	ctx, cancel := context.WithTimeout(context.Background(), Timeout*time.Second)
	defer cancel()

	bData, _ := json.Marshal(value)
	err := r.cmd.Set(ctx, key, bData, 0).Err()
	if err != nil {
		return err
	}

	return nil
}

func (r *redis) Remove(keys ...string) error {
	ctx, cancel := context.WithTimeout(context.Background(), Timeout*time.Second)
	defer cancel()

	err := r.cmd.Del(ctx, keys...).Err()
	if err != nil {
		return err
	}

	return nil
}

func (r *redis) Keys(pattern string) ([]string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), Timeout*time.Second)
	defer cancel()

	keys, err := r.cmd.Keys(ctx, pattern).Result()
	if err != nil {
		return nil, err
	}

	return keys, nil
}

func (r *redis) RemovePattern(pattern string) error {
	keys, err := r.Keys(pattern)
	if err != nil {
		return err
	}

	if len(keys) == 0 {
		return nil
	}

	err = r.Remove(keys...)
	if err != nil {
		return err
	}

	return nil
}
