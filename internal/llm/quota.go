package llm

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

type TokenQuota struct {
	rdb   *redis.Client
	limit int
	ctx   context.Context
}

// NewTokenQuota 初始化时传入 Redis 客户端和限额
func NewTokenQuota(rdb *redis.Client, limit int) *TokenQuota {
	return &TokenQuota{
		rdb:   rdb,
		limit: limit,
		ctx:   context.Background(),
	}
}

// CheckAndReserve 预扣额度
func (tq *TokenQuota) CheckAndReserve(key string, estTokens int) bool {
	redisKey := fmt.Sprintf("quota:%s", key)

	// 使用 Redis 的 INCRBY 保证并发安全
	val, err := tq.rdb.IncrBy(tq.ctx, redisKey, int64(estTokens)).Result()
	if err != nil {
		fmt.Printf("Redis 预扣失败: %v\n", err)
		return false
	}

	// 如果是第一次创建该租户，设置 1 分钟过期（实现滑动窗口）
	if val == int64(estTokens) {
		tq.rdb.Expire(tq.ctx, redisKey, time.Minute)
	}

	// 如果预扣后超过了限制，说明超额了
	return val <= int64(tq.limit)
}

// AddUsage 追加实际消耗
func (tq *TokenQuota) AddUsage(key string, tokens int) {
	redisKey := fmt.Sprintf("quota:%s", key)
	tq.rdb.IncrBy(tq.ctx, redisKey, int64(tokens))
}

// IsExceeded 检查是否超额
func (tq *TokenQuota) IsExceeded(key string) bool {
	redisKey := fmt.Sprintf("quota:%s", key)
	val, err := tq.rdb.Get(tq.ctx, redisKey).Int()
	if err != nil {
		return false
	}
	return val >= tq.limit
}
