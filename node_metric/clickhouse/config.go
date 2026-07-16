package clickhouse

import (
	"errors"
	"strings"
	"time"
)

// Config 保存 ClickHouse 连接池和超时配置。
type Config struct {
	Addresses       []string
	Database        string
	Username        string
	Password        string
	DialTimeout     time.Duration
	QueryTimeout    time.Duration
	MaxOpenConns    int
	MaxIdleConns    int
	ConnMaxLifetime time.Duration
}

// DefaultConfig 返回适合本地开发的连接参数，不包含密码。
func DefaultConfig() Config {
	return Config{
		Addresses:       []string{"127.0.0.1:9000"},
		Database:        "default",
		Username:        "default",
		DialTimeout:     5 * time.Second,
		QueryTimeout:    5 * time.Second,
		MaxOpenConns:    10,
		MaxIdleConns:    5,
		ConnMaxLifetime: 30 * time.Minute,
	}
}

// Validate 在创建连接前拒绝缺失地址、数据库或无效连接池参数。
func (c Config) Validate() error {
	if len(c.Addresses) == 0 {
		return errors.New("clickhouse metric: address is required")
	}
	for _, address := range c.Addresses {
		if strings.TrimSpace(address) == "" {
			return errors.New("clickhouse metric: address must not be empty")
		}
	}
	if strings.TrimSpace(c.Database) == "" {
		return errors.New("clickhouse metric: database is required")
	}
	if c.DialTimeout <= 0 || c.QueryTimeout <= 0 {
		return errors.New("clickhouse metric: timeouts must be positive")
	}
	if c.MaxOpenConns < 1 || c.MaxIdleConns < 0 || c.MaxIdleConns > c.MaxOpenConns {
		return errors.New("clickhouse metric: invalid connection pool limits")
	}
	if c.ConnMaxLifetime <= 0 {
		return errors.New("clickhouse metric: connection lifetime must be positive")
	}
	return nil
}
