package api

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/brianreynaldgit/mev-staking-tracker/internal/models"

	"github.com/gin-gonic/gin"
)

type API struct {
	mevDetector *models.MEVDetector
}

func NewAPI(alchemyURL, alchemyKey string) *API {
	return &API{
		mevDetector: models.NewMEVDetector(alchemyURL, alchemyKey),
	}
}

// @Summary Get MEV opportunities for a specific block
// @Description Returns detected MEV opportunities in a given block
// @Tags MEV
// @Accept json
// @Produce json
// @Param blockNumber path int true "Block number to analyze"
// @Success 200 {object} models.MEVOpportunitiesResponse
// @Failure 400 {object} models.ErrorResponse
// @Failure 500 {object} models.ErrorResponse
// @Router /mev/block/{blockNumber} [get]
func (a *API) GetBlockMEV(c *gin.Context) {
	blockNumber, err := strconv.Atoi(c.Param("blockNumber"))
	if err != nil {
		c.JSON(http.StatusBadRequest, models.ErrorResponse{
			Error: "Invalid block number",
		})
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()

	opportunities, err := a.mevDetector.CheckMEV(ctx, blockNumber)
	if err != nil {
		c.JSON(http.StatusInternalServerError, models.ErrorResponse{
			Error: fmt.Sprintf("Failed to analyze block: %v", err),
		})
		return
	}

	mevReward := a.mevDetector.CalculateMEVReward(opportunities)

	c.JSON(http.StatusOK, models.MEVOpportunitiesResponse{
		BlockNumber:              blockNumber,
		Opportunities:            opportunities,
		EstimatedValidatorReward: mevReward,
		Timestamp:                time.Now(),
	})
}

// @Summary Get validator's estimated MEV rewards
// @Description Returns estimated MEV rewards for a validator across multiple blocks
// @Tags Validator
// @Accept json
// @Produce json
// @Param validatorIndex path int true "Validator index"
// @Param fromBlock query int false "Starting block number (default: latest - 100)"
// @Param toBlock query int false "Ending block number (default: latest)"
// @Success 200 {object} models.ValidatorMEVResponse
// @Failure 400 {object} models.ErrorResponse
// @Failure 500 {object} models.ErrorResponse
// @Router /validator/{validatorIndex}/mev-rewards [get]
func (a *API) GetValidatorMEVRewards(c *gin.Context) {
	validatorIndex, err := strconv.Atoi(c.Param("validatorIndex"))
	if err != nil {
		c.JSON(http.StatusBadRequest, models.ErrorResponse{
			Error: "Invalid validator index",
		})
		return
	}

	// Get block range from query params or use defaults
	fromBlock := -1
	if fromStr := c.Query("fromBlock"); fromStr != "" {
		fromBlock, err = strconv.Atoi(fromStr)
		if err != nil {
			c.JSON(http.StatusBadRequest, models.ErrorResponse{
				Error: "Invalid fromBlock parameter",
			})
			return
		}
	}

	toBlock := -1
	if toStr := c.Query("toBlock"); toStr != "" {
		toBlock, err = strconv.Atoi(toStr)
		if err != nil {
			c.JSON(http.StatusBadRequest, models.ErrorResponse{
				Error: "Invalid toBlock parameter",
			})
			return
		}
	}

	// If no block range specified, analyze last 100 blocks
	if fromBlock == -1 || toBlock == -1 {
		latestBlock, err := a.getLatestBlockNumber(c.Request.Context())
		if err != nil {
			c.JSON(http.StatusInternalServerError, models.ErrorResponse{
				Error: fmt.Sprintf("Failed to get latest block: %v", err),
			})
			return
		}

		if fromBlock == -1 {
			fromBlock = latestBlock - 100
		}
		if toBlock == -1 {
			toBlock = latestBlock
		}
	}

	if fromBlock > toBlock {
		c.JSON(http.StatusBadRequest, models.ErrorResponse{
			Error: "fromBlock must be less than toBlock",
		})
		return
	}

	// Limit to 1000 blocks max for performance
	if toBlock-fromBlock > 1000 {
		c.JSON(http.StatusBadRequest, models.ErrorResponse{
			Error: "Block range too large (max 1000 blocks)",
		})
		return
	}

	// Analyze blocks in parallel with worker pool
	results := make(chan models.BlockMEVResult)
	errors := make(chan error)
	ctx := c.Request.Context()

	go func() {
		defer close(results)
		defer close(errors)

		sem := make(chan struct{}, 10) // Limit concurrent requests
		for blockNumber := fromBlock; blockNumber <= toBlock; blockNumber++ {
			sem <- struct{}{}
			go func(b int) {
				defer func() { <-sem }()

				select {
				case <-ctx.Done():
					errors <- ctx.Err()
					return
				default:
					opps, err := a.mevDetector.CheckMEV(ctx, b)
					if err != nil {
						errors <- fmt.Errorf("block %d: %w", b, err)
						return
					}

					reward := a.mevDetector.CalculateMEVReward(opps)
					results <- models.BlockMEVResult{
						BlockNumber:     b,
						Opportunities:   opps,
						ValidatorReward: reward,
					}
				}
			}(blockNumber)
		}
	}()

	var (
		totalReward  float64
		blockResults []models.BlockMEVResult
		mevBlocks    int
	)

	for {
		select {
		case <-ctx.Done():
			c.JSON(http.StatusInternalServerError, models.ErrorResponse{
				Error: "Request cancelled",
			})
			return
		case err := <-errors:
			c.JSON(http.StatusInternalServerError, models.ErrorResponse{
				Error: fmt.Sprintf("Error processing blocks: %v", err),
			})
			return
		case result, ok := <-results:
			if !ok {
				// All blocks processed
				c.JSON(http.StatusOK, models.ValidatorMEVResponse{
					ValidatorIndex: validatorIndex,
					FromBlock:      fromBlock,
					ToBlock:        toBlock,
					TotalMEVReward: totalReward,
					MEVBlocks:      mevBlocks,
					TotalBlocks:    toBlock - fromBlock + 1,
					Blocks:         blockResults,
					Timestamp:      time.Now(),
				})
				return
			}

			blockResults = append(blockResults, result)
			totalReward += result.ValidatorReward
			if result.ValidatorReward > 0 {
				mevBlocks++
			}
		}
	}
}

func (a *API) getLatestBlockNumber(ctx context.Context) (int, error) {
	url := fmt.Sprintf("%s/v2/%s", a.mevDetector.AlchemyAPIURL, a.mevDetector.AlchemyAPIKey)
	payload := `{"jsonrpc":"2.0","method":"eth_blockNumber","params":[],"id":1}`

	req, err := http.NewRequestWithContext(ctx, "POST", url, strings.NewReader(payload))
	if err != nil {
		return 0, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := a.mevDetector.HttpClient.Do(req)
	if err != nil {
		return 0, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	var result struct {
		Result string `json:"result"`
		Error  struct {
			Message string `json:"message"`
		} `json:"error"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return 0, fmt.Errorf("failed to decode response: %w", err)
	}

	if result.Error.Message != "" {
		return 0, fmt.Errorf("API error: %s", result.Error.Message)
	}

	blockNumber, err := strconv.ParseInt(result.Result[2:], 16, 64)
	if err != nil {
		return 0, fmt.Errorf("failed to parse block number: %w", err)
	}

	return int(blockNumber), nil
}

// @Summary Simulate MEV rewards for a validator
// @Description Simulates potential MEV rewards for a validator over future blocks
// @Tags Validator
// @Accept json
// @Produce json
// @Param request body models.SimulationRequest true "Simulation parameters"
// @Success 200 {object} models.SimulationResponse
// @Failure 400 {object} models.ErrorResponse
// @Failure 500 {object} models.ErrorResponse
// @Router /simulate [post]
func (a *API) SimulateMEVRewards(c *gin.Context) {
	var req models.SimulationRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, models.ErrorResponse{
			Error: fmt.Sprintf("Invalid request: %v", err),
		})
		return
	}

	if req.ValidatorIndex <= 0 {
		c.JSON(http.StatusBadRequest, models.ErrorResponse{
			Error: "Validator index must be positive",
		})
		return
	}

	if req.BlockCount <= 0 || req.BlockCount > 1000 {
		c.JSON(http.StatusBadRequest, models.ErrorResponse{
			Error: "Block count must be between 1 and 1000",
		})
		return
	}

	ctx := c.Request.Context()
	latestBlock, err := a.getLatestBlockNumber(ctx)
	if err != nil {
		c.JSON(http.StatusInternalServerError, models.ErrorResponse{
			Error: fmt.Sprintf("Failed to get latest block: %v", err),
		})
		return
	}

	// Use historical MEV data to simulate future blocks
	historicalBlocks := 100
	if req.BlockCount < historicalBlocks {
		historicalBlocks = req.BlockCount
	}

	historicalRewards := make([]float64, 0, historicalBlocks)
	for i := 0; i < historicalBlocks; i++ {
		blockNumber := latestBlock - i
		opps, err := a.mevDetector.CheckMEV(ctx, blockNumber)
		if err != nil {
			continue // Skip failed blocks
		}
		reward := a.mevDetector.CalculateMEVReward(opps)
		historicalRewards = append(historicalRewards, reward)
	}

	// Calculate statistics for simulation
	var (
		totalHistoricalReward float64
		mevBlocksCount        int
		maxReward             float64
	)
	for _, reward := range historicalRewards {
		totalHistoricalReward += reward
		if reward > 0 {
			mevBlocksCount++
		}
		if reward > maxReward {
			maxReward = reward
		}
	}

	avgReward := totalHistoricalReward / float64(historicalBlocks)
	mevProbability := float64(mevBlocksCount) / float64(historicalBlocks)

	// Generate simulation results
	var (
		totalSimulatedReward   float64
		simulatedBlocksWithMEV int
		blocks                 []models.SimulatedBlock
	)

	for i := 0; i < req.BlockCount; i++ {
		var reward float64
		hasMEV := rand.Float64() < mevProbability //nolint:gosec

		if hasMEV {
			// Use exponential distribution for MEV rewards
			reward = rand.ExpFloat64() * avgReward //nolint:gosec
			if reward > maxReward*2 {
				reward = maxReward * 2
			}
			totalSimulatedReward += reward
			simulatedBlocksWithMEV++
		}

		blocks = append(blocks, models.SimulatedBlock{
			BlockNumber:     latestBlock + i + 1,
			HasMEV:          hasMEV,
			EstimatedReward: reward,
		})
	}

	c.JSON(http.StatusOK, models.SimulationResponse{
		ValidatorIndex:      req.ValidatorIndex,
		SimulatedBlockCount: req.BlockCount,
		TotalReward:         totalSimulatedReward,
		AverageReward:       totalSimulatedReward / float64(req.BlockCount),
		BlocksWithMEV:       simulatedBlocksWithMEV,
		MEVProbability:      mevProbability,
		Blocks:              blocks,
		Timestamp:           time.Now(),
	})
}
