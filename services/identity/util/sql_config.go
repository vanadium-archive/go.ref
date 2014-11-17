package util

import (
	"database/sql"
	"fmt"
	_ "github.com/go-sql-driver/mysql"
)

func DBFromConfigDatabase(database string) (*sql.DB, error) {
	db, err := sql.Open("mysql", database+"?parseTime=true")
	if err != nil {
		return nil, fmt.Errorf("failed to create database with database(%v): %v", database, err)
	}
	if err := db.Ping(); err != nil {
		return nil, err
	}
	return db, nil
}
