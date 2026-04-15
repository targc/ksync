package main

import (
	"database/sql"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"

	_ "github.com/lib/pq"
)

func main() {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		log.Fatal("DATABASE_URL is required")
	}

	db, err := sql.Open("postgres", dsn)
	if err != nil {
		log.Fatalf("failed to open db: %v", err)
	}
	defer db.Close()

	if err := db.Ping(); err != nil {
		log.Fatalf("failed to connect: %v", err)
	}

	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version TEXT PRIMARY KEY,
			applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)
	`); err != nil {
		log.Fatalf("failed to create schema_migrations: %v", err)
	}

	files, err := filepath.Glob("migrations/*.sql")
	if err != nil {
		log.Fatalf("failed to list migrations: %v", err)
	}
	sort.Strings(files)

	for _, f := range files {
		version := filepath.Base(f)

		var exists bool
		if err := db.QueryRow("SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE version=$1)", version).Scan(&exists); err != nil {
			log.Fatalf("failed to check migration %s: %v", version, err)
		}
		if exists {
			fmt.Printf("skip  %s\n", version)
			continue
		}

		content, err := os.ReadFile(f)
		if err != nil {
			log.Fatalf("failed to read %s: %v", f, err)
		}

		tx, err := db.Begin()
		if err != nil {
			log.Fatalf("failed to begin tx for %s: %v", version, err)
		}

		if _, err := tx.Exec(string(content)); err != nil {
			tx.Rollback()
			log.Fatalf("failed to apply %s: %v", version, err)
		}

		if _, err := tx.Exec("INSERT INTO schema_migrations(version) VALUES($1)", version); err != nil {
			tx.Rollback()
			log.Fatalf("failed to record %s: %v", version, err)
		}

		if err := tx.Commit(); err != nil {
			log.Fatalf("failed to commit %s: %v", version, err)
		}

		fmt.Printf("apply %s\n", version)
	}

	fmt.Println("done")
}
