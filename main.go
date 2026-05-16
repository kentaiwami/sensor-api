package main

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
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
var slackSigningSecret string

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

type BleReading struct {
	Location string `json:"location"`
	RSSI     *int   `json:"rssi"`
}

func postBleRssi(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var data BleReading
	if err := json.NewDecoder(r.Body).Decode(&data); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if data.RSSI == nil {
		http.Error(w, "rssi is required", http.StatusBadRequest)
		return
	}
	if data.Location == "" {
		http.Error(w, "location is required", http.StatusBadRequest)
		return
	}
	_, err := db.Exec("INSERT INTO ble_rssi (location, rssi) VALUES (?, ?)", data.Location, data.RSSI)
	if err != nil {
		log.Printf("db insert error: %v", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusCreated)
}

func slackEventsHandler(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	// 署名検証
	ts := r.Header.Get("X-Slack-Request-Timestamp")
	sig := r.Header.Get("X-Slack-Signature")
	mac := hmac.New(sha256.New, []byte(slackSigningSecret))
	fmt.Fprintf(mac, "v0:%s:%s", ts, body)
	expected := "v0=" + hex.EncodeToString(mac.Sum(nil))
	if !hmac.Equal([]byte(expected), []byte(sig)) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	var payload struct {
		Type      string `json:"type"`
		Challenge string `json:"challenge"`
		Event     struct {
			Type     string `json:"type"`
			Reaction string `json:"reaction"`
			Item     struct {
				Type string `json:"type"`
				TS   string `json:"ts"`
			} `json:"item"`
		} `json:"event"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	// URL verification
	if payload.Type == "url_verification" {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"challenge": payload.Challenge})
		return
	}

	if payload.Event.Type == "reaction_added" && payload.Event.Item.Type == "message" {
		var result string
		switch payload.Event.Reaction {
		case "white_check_mark":
			result = "confirmed"
		case "x":
			result = "false_positive"
		default:
			w.WriteHeader(http.StatusOK)
			return
		}
		db.Exec(
			"UPDATE smell_notifications SET result=? WHERE slack_ts=?",
			result, payload.Event.Item.TS,
		)
	}

	w.WriteHeader(http.StatusOK)
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

	slackSigningSecret = os.Getenv("SLACK_SIGNING_SECRET")
	if slackSigningSecret == "" {
		log.Fatal("SLACK_SIGNING_SECRET is required")
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
	http.HandleFunc("/ble/rssi", authMiddleware(postBleRssi))
	http.HandleFunc("/slack/events", slackEventsHandler)

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
