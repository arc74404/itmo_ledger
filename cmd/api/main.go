package main

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"simple-ledger.itmo.ru/internal/data"

	_ "github.com/lib/pq"
)

type config struct {
	port int
	db   struct {
		dsn string
	}
	timeouts struct {
		idle  time.Duration
		read  time.Duration
		write time.Duration
	}
}

type application struct {
	config config
	logger *log.Logger
	models data.Models
	db     *sql.DB
}

func main() {
	var cfg config

	flag.IntVar(&cfg.port, "port", 8080, "API server port")
	flag.StringVar(&cfg.db.dsn, "db-dsn", os.Getenv("DB_DSN"), "PostgreSQL DSN")
	flag.DurationVar(&cfg.timeouts.idle, "idle-timeout", time.Minute, "HTTP idle timeout")
	flag.DurationVar(&cfg.timeouts.read, "read-timeout", 10*time.Second, "HTTP read timeout")
	flag.DurationVar(&cfg.timeouts.write, "write-timeout", 30*time.Second, "HTTP write timeout")
	flag.Parse()

	logger := log.New(os.Stdout, "", log.Ldate|log.Ltime)

	db, err := openDB(cfg)
	if err != nil {
		logger.Fatal(err, nil)
	}
	defer db.Close()

	app := &application{
		config: cfg,
		logger: logger,
		models: data.NewModels(db),
		db:     db,
	}

	srv := &http.Server{
		Addr:         fmt.Sprintf(":%d", cfg.port),
		Handler:      app.routes(),
		IdleTimeout:  cfg.timeouts.idle,
		ReadTimeout:  cfg.timeouts.read,
		WriteTimeout: cfg.timeouts.write,
	}

	logger.Printf("starting server on %s", srv.Addr)
	err = srv.ListenAndServe()
	logger.Fatal(err)
}

func openDB(cfg config) (*sql.DB, error) {

	fmt.Printf("Using DSN: %s\n", cfg.db.dsn)

	if cfg.db.dsn == "" {
		return nil, fmt.Errorf("DB_DSN is empty")
	}

	db, err := sql.Open("postgres", cfg.db.dsn)
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err = db.PingContext(ctx)
	if err != nil {
		return nil, err
	}

	return db, nil
}
