package clickhouse

import (
	"context"
	"database/sql"
	"fmt"

	clickhousedriver "github.com/ClickHouse/clickhouse-go/v2"
)

// Open 创建并验证 ClickHouse 连接池，调用者负责关闭返回的连接。
func Open(ctx context.Context, config Config) (*sql.DB, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}

	db := clickhousedriver.OpenDB(&clickhousedriver.Options{
		Addr: config.Addresses,
		Auth: clickhousedriver.Auth{
			Database: config.Database,
			Username: config.Username,
			Password: config.Password,
		},
		DialTimeout: config.DialTimeout,
	})
	db.SetMaxOpenConns(config.MaxOpenConns)
	db.SetMaxIdleConns(config.MaxIdleConns)
	db.SetConnMaxLifetime(config.ConnMaxLifetime)

	pingCtx, cancel := context.WithTimeout(ctx, config.QueryTimeout)
	defer cancel()
	if err := db.PingContext(pingCtx); err != nil {
		if closeErr := db.Close(); closeErr != nil {
			return nil, fmt.Errorf("pinging clickhouse: %v; closing connection: %w", err, closeErr)
		}
		return nil, fmt.Errorf("pinging clickhouse: %w", err)
	}
	return db, nil
}
