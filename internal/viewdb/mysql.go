package viewdb

import (
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
	if err := gdb.AutoMigrate(&NodeModel{}, &EdgeModel{}); err != nil {
		return nil, err
	}
	return gdb, nil
}

