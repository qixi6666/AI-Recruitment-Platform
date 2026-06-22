package middleware

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/redis/go-redis/v9"

	"recruitment/shared/rpc"
)

const actorKey = "actor"

type Claims struct {
	UserID   uint64 `json:"user_id"`
	Username string `json:"username"`
	Role     string `json:"role"`
	jwt.RegisteredClaims
}

func Sign(secret string, expireHours int64, user *rpc.UserDTO) (string, error) {
	now := time.Now()
	claims := Claims{
		UserID: user.ID, Username: user.Username, Role: user.Role,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   user.Username,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(time.Duration(expireHours) * time.Hour)),
		},
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(secret))
}

func JWT(secret string) gin.HandlerFunc {
	return func(c *gin.Context) {
		auth := c.GetHeader("Authorization")
		if auth == "" || !strings.HasPrefix(auth, "Bearer ") {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "missing bearer token"})
			return
		}
		tokenText := strings.TrimSpace(strings.TrimPrefix(auth, "Bearer "))
		token, err := jwt.ParseWithClaims(tokenText, &Claims{}, func(token *jwt.Token) (any, error) {
			return []byte(secret), nil
		})
		if err != nil || !token.Valid {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid token"})
			return
		}
		claims, ok := token.Claims.(*Claims)
		if !ok || claims.UserID == 0 {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid token claims"})
			return
		}
		c.Set(actorKey, &rpc.Actor{UserID: claims.UserID, Role: claims.Role})
		c.Next()
	}
}

func RequireRole(role string) gin.HandlerFunc {
	return func(c *gin.Context) {
		actor, ok := Actor(c)
		if !ok || actor.Role != role {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "role not allowed"})
			return
		}
		c.Next()
	}
}

func Actor(c *gin.Context) (*rpc.Actor, bool) {
	value, ok := c.Get(actorKey)
	if !ok {
		return nil, false
	}
	actor, ok := value.(*rpc.Actor)
	return actor, ok
}

func DuplicateSubmit(rdb *redis.Client, ttl time.Duration) gin.HandlerFunc {
	if ttl <= 0 {
		ttl = 3 * time.Second
	}
	return func(c *gin.Context) {
		if rdb == nil || c.Request.Method != http.MethodPost {
			c.Next()
			return
		}
		body, err := io.ReadAll(c.Request.Body)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "read request body failed"})
			return
		}
		c.Request.Body = io.NopCloser(bytes.NewReader(body))

		actor, _ := Actor(c)
		actorID := uint64(0)
		if actor != nil {
			actorID = actor.UserID
		}
		route := c.FullPath()
		if route == "" {
			route = c.Request.URL.Path
		}
		sum := sha256.Sum256([]byte(fmt.Sprintf("%d:%s:%s:%s", actorID, c.Request.Method, route, string(body))))
		key := "duplicate_submit:" + hex.EncodeToString(sum[:])
		ok, err := rdb.SetNX(c.Request.Context(), key, "1", ttl).Result()
		if err != nil {
			c.Next()
			return
		}
		if !ok {
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{"error": "duplicate submission"})
			return
		}

		c.Next()
		if c.Writer.Status() >= http.StatusBadRequest {
			_ = rdb.Del(c.Request.Context(), key).Err()
		}
	}
}

func CORS() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Access-Control-Allow-Origin", c.GetHeader("Origin"))
		c.Header("Access-Control-Allow-Credentials", "true")
		c.Header("Access-Control-Allow-Headers", "Authorization, Content-Type")
		c.Header("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
		if c.Request.Method == http.MethodOptions {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}
		c.Next()
	}
}
