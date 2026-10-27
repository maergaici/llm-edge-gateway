package router

import (
	"log/slog"
	"time"

	"github.com/sony/gobreaker/v2"
)

// NewCircuitBreaker 负责创建一个配置好的熔断器
func NewCircuitBreaker(name string) *gobreaker.CircuitBreaker[*struct{}] {
	return gobreaker.NewCircuitBreaker[*struct{}](gobreaker.Settings{
		Name:    name,
		Timeout: 10 * time.Second,
		ReadyToTrip: func(counts gobreaker.Counts) bool {
			// 连续失败 10 次触发熔断
			return counts.ConsecutiveFailures >= 10
		},
		OnStateChange: func(name string, from, to gobreaker.State) {
			slog.Info("circuit breaker state changed",
				"name", name,
				"from", from.String(),
				"to", to.String(),
			)
		},
	})
}
