#!/bin/bash
# Local development deployment script

set -e

echo "========================================="
echo "VectorHub Local Deployment"
echo "========================================="

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Check prerequisites
echo -e "${YELLOW}Checking prerequisites...${NC}"

if ! command -v docker &> /dev/null; then
    echo -e "${RED}Error: Docker is not installed${NC}"
    exit 1
fi

if ! command -v docker-compose &> /dev/null; then
    echo -e "${RED}Error: docker-compose is not installed${NC}"
    exit 1
fi

echo -e "${GREEN}✓ Prerequisites satisfied${NC}"

# Check if .env exists
if [ ! -f .env ]; then
    echo -e "${YELLOW}Creating .env file from .env.example...${NC}"
    cp .env.example .env
    echo -e "${GREEN}✓ .env file created${NC}"
    echo -e "${YELLOW}Please review and update .env file with your settings${NC}"
fi

# Build the Docker image
echo -e "${YELLOW}Building Docker image...${NC}"
docker-compose build
echo -e "${GREEN}✓ Docker image built${NC}"

# Start services
echo -e "${YELLOW}Starting services...${NC}"
docker-compose up -d
echo -e "${GREEN}✓ Services started${NC}"

# Wait for services to be ready
echo -e "${YELLOW}Waiting for services to be ready...${NC}"
sleep 10

# Check Redis
echo -e "${YELLOW}Checking Redis instances...${NC}"
for i in {0..3}; do
    port=$((6379 + i))
    if docker-compose exec redis-$i redis-cli ping > /dev/null 2>&1; then
        echo -e "${GREEN}✓ Redis-$i is ready (port $port)${NC}"
    else
        echo -e "${RED}✗ Redis-$i is not responding${NC}"
    fi
done

# Check VectorHub
echo -e "${YELLOW}Checking VectorHub service...${NC}"
if curl -s http://localhost:9090/health/live > /dev/null 2>&1; then
    echo -e "${GREEN}✓ VectorHub is running${NC}"
else
    echo -e "${YELLOW}VectorHub may still be starting up...${NC}"
fi

# Display service URLs
echo -e "\n${GREEN}========================================="
echo "Deployment Complete!"
echo "=========================================${NC}"
echo ""
echo "Service URLs:"
echo "  VectorHub gRPC:  localhost:50051"
echo "  VectorHub Metrics: http://localhost:9090/metrics"
echo "  VectorHub Health:  http://localhost:9090/health"
echo "  Prometheus:        http://localhost:9091"
echo "  Grafana:           http://localhost:3000 (admin/admin)"
echo ""
echo "View logs:"
echo "  docker-compose logs -f vectorhub"
echo ""
echo "Stop services:"
echo "  docker-compose down"
echo ""
echo -e "${YELLOW}Note: It may take a minute for all services to be fully ready${NC}"
