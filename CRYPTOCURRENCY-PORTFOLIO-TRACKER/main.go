package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"strconv"
	"sync"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

var (
	db *sql.DB
	wg sync.WaitGroup
)

const (
	coincapCryptoAPI = "https://api.coincap.io/v2/assets"
	retryDelay       = 30 // Delay between checking a token's price
)

type coinCapAsset struct {
	Data []struct {
		ID       string `json:"id"`
		Symbol   string `json:"symbol"`
		PriceUsd string `json:"priceUsd"`
	} `json:"data"`
}

type tokenConfig struct {
	Name      string  `json:"name"`
	Symbol    string  `json:"symbol"`
	Threshold float64 `json:"threshold"`
}

type config struct {
	Tokens []tokenConfig `json:"tokens"`
}

type Portfolio struct {
	ID        int          `json:"id"`
	UserID    int          `json:"user_id"`
	Symbol    string       `json:"symbol"`
	Amount    float64      `json:"amount"`
	CreatedAt time.Time    `json:"created_at"`
	UpdatedAt sql.NullTime `json:"updated_at"`
}

func main() {
	// Open database connection
	var err error
	db, err = sql.Open("sqlite3", "./portfolio.db")
	if err != nil {
		log.Fatal("Error opening database connection:", err)
	}
	defer db.Close()

	// Create table if not exists
	if err := createTable(); err != nil {
		log.Fatal("Error creating table:", err)
	}

	// Load configuration from file
	cfg, err := loadConfig("config.json")
	if err != nil {
		log.Fatal("Error loading configuration:", err)
	}

	for _, token := range cfg.Tokens {
		wg.Add(1)
		go monitorToken(token)
	}

	// Define routes
	http.HandleFunc("/portfolio", handlePortfolio)
	http.HandleFunc("/portfolio/add", handleAddToPortfolio)
	http.HandleFunc("/portfolio/value", handlePortfolioValue)

	// Start server
	fmt.Println("Server listening on port 8080...")
	go func() {
		if err := http.ListenAndServe(":8080", nil); err != nil {
			log.Fatal("HTTP server error:", err)
		}
	}()
	wg.Wait()
}

// createTable creates portfolio table if not exists
func createTable() error {
	createStmt := `
		CREATE TABLE IF NOT EXISTS portfolio (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			user_id INTEGER,
			symbol TEXT,
			amount REAL,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP
		);
	`

	_, err := db.Exec(createStmt)
	return err
}

// monitorToken continuously monitors the price of a token
func monitorToken(token tokenConfig) {
	defer wg.Done()
	for {
		price, err := getCoinCapPrice(token.Symbol)
		if err != nil {
			log.Printf("Error retrieving %s price: %v\n", token.Name, err)
			continue
		}
		if price > token.Threshold {
			msg := fmt.Sprintf("%s price ($%.2f) is above threshold ($%.2f)!", token.Name, price, token.Threshold)
			log.Println(msg)
			// Replace messageBox with appropriate notification mechanism
		}
		time.Sleep(retryDelay * time.Second)
	}
}

// loadConfig loads configuration from a file
func loadConfig(filename string) (*config, error) {
	// Load configuration from file
	data, err := ioutil.ReadFile(filename)
	if err != nil {
		return nil, err
	}

	var cfg config
	err = json.Unmarshal(data, &cfg)
	if err != nil {
		return nil, err
	}

	return &cfg, nil
}

// getCoinCapPrice retrieves the price of a cryptocurrency from the CoinCap API
func getCoinCapPrice(symbol string) (float64, error) {
	resp, err := http.Get(coincapCryptoAPI)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	var assetData coinCapAsset
	err = json.NewDecoder(resp.Body).Decode(&assetData)
	if err != nil {
		return 0, err
	}

	for _, asset := range assetData.Data {
		if asset.Symbol == symbol {
			priceUsd, err := strconv.ParseFloat(asset.PriceUsd, 64)
			if err != nil {
				return 0, err
			}
			return priceUsd, nil
		}
	}

	return 0, fmt.Errorf("price data not found for symbol %s", symbol)
}

// handlePortfolio fetches and displays portfolio data
func handlePortfolio(w http.ResponseWriter, r *http.Request) {
	// Fetch portfolio data from the database
	rows, err := db.Query("SELECT * FROM portfolio")
	if err != nil {
		http.Error(w, "Error fetching portfolio data", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	// Create a slice to store portfolio entries
	var portfolio []Portfolio

	// Iterate over the rows and populate the portfolio slice
	for rows.Next() {
		var p Portfolio
		err := rows.Scan(&p.ID, &p.UserID, &p.Symbol, &p.Amount, &p.CreatedAt, &p.UpdatedAt)
		if err != nil {
			http.Error(w, "Error scanning portfolio data", http.StatusInternalServerError)
			return
		}
		portfolio = append(portfolio, p)
	}

	// Set response header
	w.Header().Set("Content-Type", "application/json")

	// Encode portfolio data as JSON and write it to the response writer
	err = json.NewEncoder(w).Encode(portfolio)
	if err != nil {
		http.Error(w, "Error encoding portfolio data", http.StatusInternalServerError)
		return
	}
}

// handleAddToPortfolio adds cryptocurrency to the portfolio
func handleAddToPortfolio(w http.ResponseWriter, r *http.Request) {
	// Parse the request body to extract cryptocurrency data
	var p Portfolio
	err := json.NewDecoder(r.Body).Decode(&p)
	if err != nil {
		http.Error(w, "Error parsing request body", http.StatusBadRequest)
		return
	}

	// Insert cryptocurrency data into the database
	_, err = db.Exec("INSERT INTO portfolio (user_id, symbol, amount) VALUES (?, ?, ?)", p.UserID, p.Symbol, p.Amount)
	if err != nil {
		http.Error(w, "Error adding cryptocurrency to portfolio", http.StatusInternalServerError)
		return
	}

	// Set response status code to indicate success
	w.WriteHeader(http.StatusCreated)
}

// handlePortfolioValue calculates and displays portfolio value
func handlePortfolioValue(w http.ResponseWriter, r *http.Request) {
	// Fetch portfolio data from the database
	rows, err := db.Query("SELECT symbol, amount FROM portfolio")
	if err != nil {
		http.Error(w, "Error fetching portfolio data", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	// Map to store cryptocurrency amounts
	cryptoAmounts := make(map[string]float64)

	// Iterate over the rows and populate the map
	for rows.Next() {
		var symbol string
		var amount float64
		err := rows.Scan(&symbol, &amount)
		if err != nil {
			http.Error(w, "Error scanning portfolio data", http.StatusInternalServerError)
			return
		}
		cryptoAmounts[symbol] += amount
	}

	// Calculate total portfolio value based on current cryptocurrency prices
	var totalValue float64
	for symbol, amount := range cryptoAmounts {
		price, err := getCoinCapPrice(symbol)
		if err != nil {
			http.Error(w, "Error fetching cryptocurrency price", http.StatusInternalServerError)
			return
		}
		totalValue += price * amount
	}

	// Create a response object
	response := struct {
		TotalValue float64 `json:"total_value"`
	}{
		TotalValue: totalValue,
	}

	// Set response header
	w.Header().Set("Content-Type", "application/json")

	// Encode response object as JSON and write it to the response writer
	err = json.NewEncoder(w).Encode(response)
	if err != nil {
		http.Error(w, "Error encoding response data", http.StatusInternalServerError)
		return
	}
}
