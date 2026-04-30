package main

import (
	"database/sql"
	"encoding/json"
	"log"
	"net/http"
	"os"

	_ "github.com/go-sql-driver/mysql"
	"github.com/joho/godotenv"
)

type Reading struct {
	Value    *float64 `json:"value"`
	SensorID string   `json:"sensor_id"`
}

var db *sql.DB
var apiKey string

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

	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	http.HandleFunc("/temperature", authMiddleware(postReading("temperatures")))
	http.HandleFunc("/humidity", authMiddleware(postReading("humidities")))
	http.HandleFunc("/co2", authMiddleware(postReading("co2s")))
	log.Println("listening on :8080")
	log.Fatal(http.ListenAndServe(":8080", nil))
}
