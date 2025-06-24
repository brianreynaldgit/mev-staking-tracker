package models

import (
	"context"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"strings"
	"time"
)

type ErrorResponse struct {
	Error string `json:"error"`
}

type MEVOpportunitiesResponse struct {
	BlockNumber              int              `json:"blockNumber"`
	Opportunities            []MEVOpportunity `json:"opportunities"`
	EstimatedValidatorReward float64          `json:"estimatedValidatorReward"`
	Timestamp                time.Time        `json:"timestamp"`
}

type ValidatorMEVResponse struct {
	ValidatorIndex int              `json:"validatorIndex"`
	FromBlock      int              `json:"fromBlock"`
	ToBlock        int              `json:"toBlock"`
	TotalMEVReward float64          `json:"totalMEVReward"`
	MEVBlocks      int              `json:"mevBlocks"`
	TotalBlocks    int              `json:"totalBlocks"`
	Blocks         []BlockMEVResult `json:"blocks"`
	Timestamp      time.Time        `json:"timestamp"`
}

type BlockMEVResult struct {
	BlockNumber     int              `json:"blockNumber"`
	Opportunities   []MEVOpportunity `json:"opportunities"`
	ValidatorReward float64          `json:"validatorReward"`
}

type SimulationRequest struct {
	ValidatorIndex int `json:"validatorIndex" binding:"required"`
	BlockCount     int `json:"blockCount" binding:"required"`
}

type SimulationResponse struct {
	ValidatorIndex      int              `json:"validatorIndex"`
	SimulatedBlockCount int              `json:"simulatedBlockCount"`
	TotalReward         float64          `json:"totalReward"`
	AverageReward       float64          `json:"averageReward"`
	BlocksWithMEV       int              `json:"blocksWithMEV"`
	MEVProbability      float64          `json:"mevProbability"`
	Blocks              []SimulatedBlock `json:"blocks"`
	Timestamp           time.Time        `json:"timestamp"`
}

type SimulatedBlock struct {
	BlockNumber     int     `json:"blockNumber"`
	HasMEV          bool    `json:"hasMEV"`
	EstimatedReward float64 `json:"estimatedReward"`
}

// Block represents an Ethereum block with transactions
type Block struct {
	Number       string        `json:"number"`
	Transactions []Transaction `json:"transactions"`
	Timestamp    string        `json:"timestamp"`
}

// Transaction represents an Ethereum transaction
type Transaction struct {
	Hash     string `json:"hash"`
	From     string `json:"from"`
	To       string `json:"to"`
	Value    string `json:"value"`
	GasPrice string `json:"gasPrice"`
	GasUsed  string `json:"gasUsed"`
	Input    string `json:"input"`
}

// MEVOpportunity represents a detected MEV opportunity
type MEVOpportunity struct {
	Type         string        `json:"type"` // "arbitrage", "liquidations", "sandwich"
	Profit       float64       `json:"profit"`
	Transactions []Transaction `json:"transactions"`
	BlockNumber  int           `json:"blockNumber"`
}

// MEVDetector handles MEV detection logic
type MEVDetector struct {
	AlchemyAPIURL string
	AlchemyAPIKey string
	HttpClient    *http.Client
	KnownMEVBots  map[string]bool // Known MEV bot addresses
}

// NewMEVDetector creates a new MEV detector instance
func NewMEVDetector(alchemyURL, alchemyKey string) *MEVDetector {
	return &MEVDetector{
		AlchemyAPIURL: alchemyURL,
		AlchemyAPIKey: alchemyKey,
		HttpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
		KnownMEVBots: map[string]bool{
			"0x0000000000007f150bd6f54c40a34d7c3d5e9f56": true, // Flashbots builder
			// Add more known MEV bot addresses
		},
	}
}

// GetBlockData retrieves block data from Alchemy
func (d *MEVDetector) GetBlockData(ctx context.Context, blockNumber int) (*Block, error) {
	url := fmt.Sprintf("%s/v2/%s", d.AlchemyAPIURL, d.AlchemyAPIKey)

	payload := fmt.Sprintf(`{
		"jsonrpc":"2.0",
		"method":"eth_getBlockByNumber",
		"params":["0x%x", true],
		"id":1
	}`, blockNumber)

	req, err := http.NewRequestWithContext(ctx, "POST", url, strings.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := d.HttpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	var result struct {
		Result *Block `json:"result"`
		Error  struct {
			Message string `json:"message"`
		} `json:"error"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	if result.Error.Message != "" {
		return nil, fmt.Errorf("API error: %s", result.Error.Message)
	}

	if result.Result == nil {
		return nil, fmt.Errorf("empty block result")
	}

	return result.Result, nil
}

// CheckMEV detects MEV opportunities in a block
func (d *MEVDetector) CheckMEV(ctx context.Context, blockNumber int) ([]MEVOpportunity, error) {
	block, err := d.GetBlockData(ctx, blockNumber)
	if err != nil {
		return nil, fmt.Errorf("failed to get block data: %w", err)
	}

	var opportunities []MEVOpportunity

	// Check for known MEV bots
	botTxs := d.detectKnownBots(block)
	if len(botTxs) > 0 {
		opportunities = append(opportunities, MEVOpportunity{
			Type:         "known_bot",
			Transactions: botTxs,
			BlockNumber:  blockNumber,
		})
	}

	// Check for high-value transactions (potential MEV)
	highValueTxs := d.detectHighValueTransactions(block)
	if len(highValueTxs) > 0 {
		opportunities = append(opportunities, MEVOpportunity{
			Type:         "high_value",
			Transactions: highValueTxs,
			BlockNumber:  blockNumber,
		})
	}

	// Check for complex transactions (potential arbitrage)
	complexTxs := d.detectComplexTransactions(block)
	if len(complexTxs) > 0 {
		opportunities = append(opportunities, MEVOpportunity{
			Type:         "complex",
			Transactions: complexTxs,
			BlockNumber:  blockNumber,
		})
	}

	return opportunities, nil
}

// detectKnownBots finds transactions from known MEV bots
func (d *MEVDetector) detectKnownBots(block *Block) []Transaction {
	var botTxs []Transaction
	for _, tx := range block.Transactions {
		if d.KnownMEVBots[strings.ToLower(tx.From)] {
			botTxs = append(botTxs, tx)
		}
	}
	return botTxs
}

// detectHighValueTransactions finds high ETH value transactions
func (d *MEVDetector) detectHighValueTransactions(block *Block) []Transaction {
	var highValueTxs []Transaction
	for _, tx := range block.Transactions {
		value := new(big.Int)
		value.SetString(tx.Value[2:], 16) // Remove 0x and parse as hex
		ethValue := new(big.Float).Quo(
			new(big.Float).SetInt(value),
			new(big.Float).SetInt(big.NewInt(1e18)),
		)

		if ethValue.Cmp(big.NewFloat(10)) >= 0 { // 10 ETH threshold
			highValueTxs = append(highValueTxs, tx)
		}
	}
	return highValueTxs
}

// detectComplexTransactions finds transactions with complex input data
func (d *MEVDetector) detectComplexTransactions(block *Block) []Transaction {
	var complexTxs []Transaction
	for _, tx := range block.Transactions {
		// Skip simple ETH transfers
		if len(tx.Input) <= 2 || tx.Input == "0x" {
			continue
		}

		// Check for multiple internal calls (indicated by complex input)
		if len(tx.Input) > 1000 { // Arbitrary threshold for "complex"
			complexTxs = append(complexTxs, tx)
		}
	}
	return complexTxs
}

// CalculateMEVReward estimates the MEV reward for validators
func (d *MEVDetector) CalculateMEVReward(opportunities []MEVOpportunity) float64 {
	var total float64
	for _, opp := range opportunities {
		for _, tx := range opp.Transactions {
			gasPrice := new(big.Int)
			gasPrice.SetString(tx.GasPrice[2:], 16) // Remove 0x and parse as hex

			gasUsed := new(big.Int)
			gasUsed.SetString(tx.GasUsed[2:], 16)

			// Calculate tx fee: gasPrice * gasUsed
			fee := new(big.Int).Mul(gasPrice, gasUsed)
			feeEth := new(big.Float).Quo(
				new(big.Float).SetInt(fee),
				new(big.Float).SetInt(big.NewInt(1e18)),
			)

			feeFloat, _ := feeEth.Float64()
			total += feeFloat * 0.1 // Assume validator gets 10% of MEV
		}
	}
	return total
}
