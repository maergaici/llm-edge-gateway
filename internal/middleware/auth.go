package middleware

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/golang-jwt/jwt/v5"
	"github.com/redis/go-redis/v9"
)

type AuthCtxKey string

const (
	TenantIDKey AuthCtxKey = "tenant_id"
	UserRoleKey AuthCtxKey = "user_role"
)

var jwtSecret = []byte("my-super-secret-key")

func AuthMiddleware(rdb *redis.Client, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthz" || r.URL.Path == "/readyz" || r.URL.Path == "/metrics" {
			next.ServeHTTP(w, r)
			return
		}
		authHeader := r.Header.Get("Authorization")
		if authHeader == "" || !strings.HasPrefix(authHeader, "Bearer ") {
			http.Error(w, "Unauthorized: Missing or invalid token", http.StatusUnauthorized)
			return
		}
		tokenString := strings.TrimSpace(strings.TrimPrefix(authHeader, "Bearer "))
		//fmt.Printf("--- 网关真实拿到的 Token: [%s] ---\n", tokenString)
		token, err := jwt.Parse(tokenString, func(token *jwt.Token) (interface{}, error) {
			if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
				return nil, fmt.Errorf("Unexpected signing method: %v", token.Header["alg"])
			}
			return jwtSecret, nil
		})
		if err != nil || !token.Valid {
			fmt.Printf("JWT Parse Error: %v\n", err)
			http.Error(w, "Unauthorized: Invalid token", http.StatusUnauthorized)
			return
		}
		var tenantID string
		if claims, ok := token.Claims.(jwt.MapClaims); ok {
			if tid, exists := claims["tenant_id"]; exists {
				tenantID = tid.(string)
			}
		}
		if tenantID == "" {
			http.Error(w, "Unauthorized:Missing tenant_id in token", http.StatusUnauthorized)
			return
		}
		redisKey := fmt.Sprintf("token:%s:%s", tenantID, tokenString)
		val, err := rdb.Get(r.Context(), redisKey).Result()
		if err == redis.Nil || val != "active" {
			http.Error(w, "Unauthorized:Token is not active", http.StatusUnauthorized)
			return
		}
		ctx := context.WithValue(r.Context(), TenantIDKey, tenantID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func GetTenantID(ctx context.Context) string {
	if tid, ok := ctx.Value(TenantIDKey).(string); ok {
		return tid
	}
	return ""
}
