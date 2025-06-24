package main

import (
	"log"
	"os"

	"github.com/brianreynaldgit/mev-staking-tracker/configs"
	"github.com/brianreynaldgit/mev-staking-tracker/internal/api"

	"github.com/gin-gonic/gin"
)

func main() {
	// Load configuration from YAML
	configPath := os.Getenv("CONFIG_PATH")
	if configPath == "" {
		configPath = "config.yaml" // <- assume it's in the root directory
	}
	cfg, err := configs.LoadConfig(configPath)
	if err != nil {
		log.Fatalf("Failed to load configuration: %v", err)
	}

	// Create API handler
	apiHandler := api.NewAPI(cfg.Blockchain.AlchemyAPIURL, cfg.Blockchain.AlchemyAPIKey)

	// Set up router
	router := gin.Default()

	// API routes
	apiGroup := router.Group("/api/v1")
	{
		apiGroup.GET("/mev/block/:blockNumber", apiHandler.GetBlockMEV)
		apiGroup.GET("/validator/:validatorIndex/mev-rewards", apiHandler.GetValidatorMEVRewards)
		apiGroup.POST("/simulate", apiHandler.SimulateMEVRewards)
	}

	// Start server
	log.Printf("Starting MEV Staking Tracker API on port %s", cfg.Server.Port)
	if err := router.Run(":" + cfg.Server.Port); err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
}
