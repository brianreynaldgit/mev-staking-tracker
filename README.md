# MEV Staking Tracker

Track Ethereum validator rewards with MEV boost detection.

## Features
- Track validator rewards
- Detect MEV opportunities
- Simulate future rewards
- PostgreSQL analytics

## API Endpoints
- `GET /rewards/:validator` - Get historical rewards for a validator
- `GET /mev-stats` - Get aggregate MEV statistics
- `POST /simulate` - Simulate future rewards (body: `{"validator_index": 123, "block_count": 100}`)

## Running Locally
1. Start services:
```bash
docker-compose up -d