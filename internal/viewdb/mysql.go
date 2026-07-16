package viewdb

import (
	"errors"
	"fmt"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

// OpenMySQL opens MySQL and auto-migrates required tables.
// DSN example:
// user:pass@tcp(127.0.0.1:3306)/pathroute?charset=utf8mb4&parseTime=True&loc=Local
func OpenMySQL(dsn string) (*gorm.DB, error) {
	if dsn == "" {
		return nil, fmt.Errorf("MYSQL_DSN is empty")
	}
	gdb, err := gorm.Open(mysql.Open(dsn), &gorm.Config{})
	if err != nil {
		return nil, err
	}
	sqlDB, err := gdb.DB()
	if err != nil {
		return nil, fmt.Errorf("getting mysql connection pool: %w", err)
	}
	models := []any{&NodeModel{}, &EdgeModel{}}
	models = append(models, MetricModels()...)
	if err := gdb.AutoMigrate(models...); err != nil {
		// 迁移失败时调用者拿不到 gdb，必须在此处关闭已经创建的连接池，避免启动重试泄漏资源。
		return nil, errors.Join(err, sqlDB.Close())
	}
	return gdb, nil
}
