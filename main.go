package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	_ "github.com/go-sql-driver/mysql"
	"github.com/joho/godotenv"
)

type Reading struct {
	Value    *float64 `json:"value"`
	SensorID string   `json:"sensor_id"`
}

var db *sql.DB
var apiKey string

var tables = []string{"temperatures", "humidities", "co2s", "smells"}

func authMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-API-Key") != apiKey {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next(w, r)
	}
}

func postReading(table string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var data Reading
		if err := json.NewDecoder(r.Body).Decode(&data); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}
		if data.Value == nil {
			http.Error(w, "value is required", http.StatusBadRequest)
			return
		}
		if data.SensorID == "" {
			http.Error(w, "sensor_id is required", http.StatusBadRequest)
			return
		}
		_, err := db.Exec("INSERT INTO "+table+" (sensor_id, value) VALUES (?, ?)", data.SensorID, data.Value)
		if err != nil {
			log.Printf("db insert error: %v", err)
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusCreated)
	}
}

func metricsHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain")
	for _, table := range tables {
		var ts *float64
		err := db.QueryRow("SELECT UNIX_TIMESTAMP(MAX(recorded_at)) FROM " + table).Scan(&ts)
		if err != nil || ts == nil {
			fmt.Fprintf(w, "sensor_last_received_timestamp{table=%q} 0\n", table)
			continue
		}
		fmt.Fprintf(w, "sensor_last_received_timestamp{table=%q} %g\n", table, *ts)
	}

	// latest co2 value per sensor
	rows, err := db.Query("SELECT sensor_id, value FROM co2s WHERE recorded_at = (SELECT MAX(recorded_at) FROM co2s c2 WHERE c2.sensor_id = co2s.sensor_id)")
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var sensorID string
			var value float64
			if err := rows.Scan(&sensorID, &value); err == nil {
				fmt.Fprintf(w, "sensor_co2{sensor_id=%q} %g\n", sensorID, value)
			}
		}
	}

	// latest smell value per sensor
	rows2, err := db.Query("SELECT sensor_id, value FROM smells WHERE recorded_at = (SELECT MAX(recorded_at) FROM smells s2 WHERE s2.sensor_id = smells.sensor_id)")
	if err == nil {
		defer rows2.Close()
		for rows2.Next() {
			var sensorID string
			var value float64
			if err := rows2.Scan(&sensorID, &value); err == nil {
				fmt.Fprintf(w, "sensor_smell{sensor_id=%q} %g\n", sensorID, value)
			}
		}
	}
}

func main() {
	godotenv.Load()

	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		dsn = "sensor:sensor@tcp(localhost:3306)/sensordb?parseTime=true"
	}

	var err error
	db, err = sql.Open("mysql", dsn)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	if err := db.Ping(); err != nil {
		log.Fatal("cannot connect to db:", err)
	}

	apiKey = os.Getenv("API_KEY")
	if apiKey == "" {
		log.Fatal("API_KEY is required")
	}

	http.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		if err := db.Ping(); err != nil {
			http.Error(w, "db unavailable", http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	})
	http.HandleFunc("/metrics", metricsHandler)
	http.HandleFunc("/temperature", authMiddleware(postReading("temperatures")))
	http.HandleFunc("/humidity", authMiddleware(postReading("humidities")))
	http.HandleFunc("/co2", authMiddleware(postReading("co2s")))
	http.HandleFunc("/smell", authMiddleware(postReading("smells")))

	srv := &http.Server{Addr: ":8080"}
	go func() {
		log.Println("listening on :8080")
		if err := srv.ListenAndServe(); err != http.ErrServerClosed {
			log.Fatal(err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGTERM, syscall.SIGINT)
	<-quit

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		log.Fatal(err)
	}
	log.Println("server shutdown")
}
