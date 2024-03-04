package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5/pgxpool"
)

var db *pgxpool.Pool

const date = "2024-01-01T02:34:38.543030Z"

func main() {
	println("Starting...")

	loc, err := time.LoadLocation("America/Sao_Paulo")
	if err != nil {
		fmt.Println("Error loading location:", err)
		return
	}
	time.Local = loc

	ctx := context.Background()

	for i := 0; i < 10; i++ {
		db, err = pgxpool.New(ctx, "user=db password=db host=db port=5432 dbname=db")
		if err == nil {
			break
		} else {
			println("Failed to connect to DB, retrying in 5 seconds")
			time.Sleep(5 * time.Second)
		}
	}
	println("Connected to DB")
	defer db.Close()

	db.Config().MaxConnIdleTime = 10 * time.Minute
	db.Config().MaxConnLifetime = 2 * time.Hour
	db.Config().MaxConns = 50
	db.Config().MinConns = 49
	db.Config().HealthCheckPeriod = 10 * time.Minute

	http.HandleFunc("POST /clientes/{id}/transacoes", handleTransactions())
	http.HandleFunc("GET /clientes/{id}/extrato", handleStatement())

	println("Listening on :8080")
	if err := http.ListenAndServe(":8080", nil); err != nil {
		println("Failed to start server", err)
	}
}

func handleTransactions() http.HandlerFunc {
	type transactionRequest struct {
		Value     int    `json:"valor"`
		Type      string `json:"tipo"`
		Descricao string `json:"descricao"`
	}

	return func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		var tr transactionRequest
		if err := json.NewDecoder(r.Body).Decode(&tr); err != nil {
			w.WriteHeader(http.StatusUnprocessableEntity)
			w.Write([]byte(`{}`))
			return
		}

		if tr.Value < 1 {
			w.WriteHeader(http.StatusUnprocessableEntity)
			w.Write([]byte(`{}`))
			return
		}

		if tr.Type != "d" && tr.Type != "c" {
			w.WriteHeader(http.StatusUnprocessableEntity)
			w.Write([]byte(`{}`))
			return
		}

		descLen := utf8.RuneCountInString(tr.Descricao)
		if descLen < 1 || descLen > 10 {
			w.WriteHeader(http.StatusUnprocessableEntity)
			w.Write([]byte(`{}`))
			return
		}

		customerIDStr := r.PathValue("id")
		customerID, err := strconv.Atoi(customerIDStr)
		if err != nil {
			w.WriteHeader(http.StatusNotFound)
			w.Write([]byte(`{}`))
			return
		}

		if customerID < 1 || customerID > 5 {
			w.WriteHeader(http.StatusNotFound)
			w.Write([]byte(`{}`))
			return
		}

		var newBalance int
		var success bool
		var limit int
		if tr.Type == "c" {
			err = db.QueryRow(r.Context(), "SELECT * FROM credit($1, $2, $3)", customerID, tr.Value, tr.Descricao).Scan(&newBalance, &success, &limit)
		} else {
			err = db.QueryRow(r.Context(), "SELECT * FROM debit($1, $2, $3)", customerID, tr.Value, tr.Descricao).Scan(&newBalance, &success, &limit)
		}

		if err != nil || !success {
			w.WriteHeader(http.StatusUnprocessableEntity)
			w.Write([]byte(`{}`))
			return
		}

		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"limite": ` + fmt.Sprintf("%d", limit) + `, "saldo": ` + fmt.Sprintf("%d", newBalance) + `}`))
	}
}

func handleStatement() http.HandlerFunc {
	type balanceRes struct {
		Total int    `json:"total"`
		Date  string `json:"data_extrato"` // "2024-01-17T02:34:38.543030Z"
		Limit int    `json:"limite"`
	}

	type transactionRes struct {
		Value int    `json:"valor"`
		Type  string `json:"tipo"`
		Desc  string `json:"descricao"`
		Date  string `json:"realizada_em"` // "2024-01-17T02:34:38.543030Z"
	}

	type response struct {
		Balance      balanceRes       `json:"saldo"`
		Transactions []transactionRes `json:"ultimas_transacoes"`
	}

	return func(w http.ResponseWriter, r *http.Request) {
		customerIDStr := r.PathValue("id")
		customerID, err := strconv.Atoi(customerIDStr)
		if err != nil {
			w.WriteHeader(http.StatusNotFound)
			w.Write([]byte(`{}`))
			return
		}

		if customerID < 1 || customerID > 5 {
			w.WriteHeader(http.StatusNotFound)
			w.Write([]byte(`{}`))
			return
		}

		var limit, balance int
		tx, err := db.Begin(r.Context())
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte(`{}`))
			return
		}
		defer tx.Rollback(r.Context())

		tx.QueryRow(r.Context(), "SELECT \"limit\", balance FROM customers WHERE id = $1", customerID).Scan(&limit, &balance)

		rows, err := tx.Query(r.Context(), "SELECT amount, type, description, created_at FROM transactions WHERE customer_id = $1 ORDER BY id DESC LIMIT 10", customerID)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte(`{}`))
			return
		}
		defer rows.Close()

		transactions := make([]transactionRes, 0)
		for rows.Next() {
			var t transactionRes
			var createdAt time.Time
			rows.Scan(&t.Value, &t.Type, &t.Desc, &createdAt)
			t.Date = createdAt.Format(time.RFC3339Nano)
			transactions = append(transactions, t)
		}

		tx.Commit(r.Context())

		b := balanceRes{Total: balance, Date: time.Now().Format(time.RFC3339Nano), Limit: limit}
		resp := response{Balance: b, Transactions: transactions}

		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(resp)
	}

}
