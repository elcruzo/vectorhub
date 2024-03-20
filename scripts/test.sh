#!/bin/bash
# Test runner script

set -e

echo "========================================="
echo "VectorHub Test Suite"
echo "========================================="

# Colors
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
NC='\033[0m'

TEST_TYPE="${1:-all}"

run_unit_tests() {
    echo -e "${YELLOW}Running unit tests...${NC}"
    go test -v -race -coverprofile=coverage.out ./internal/... ./pkg/...
    echo -e "${GREEN}✓ Unit tests passed${NC}"
}

run_integration_tests() {
    echo -e "${YELLOW}Running integration tests...${NC}"
    echo -e "${YELLOW}Note: VectorHub server must be running on localhost:50051${NC}"
    
    # Check if server is available
    if ! nc -z localhost 50051 2>/dev/null; then
        echo -e "${RED}Error: VectorHub server is not running on localhost:50051${NC}"
        echo "Start the server with: docker-compose up -d"
        exit 1
    fi
    
    go test -v -race ./test/integration/...
    echo -e "${GREEN}✓ Integration tests passed${NC}"
}

run_benchmark_tests() {
    echo -e "${YELLOW}Running benchmark tests...${NC}"
    echo -e "${YELLOW}Note: VectorHub server must be running on localhost:50051${NC}"
    
    if ! nc -z localhost 50051 2>/dev/null; then
        echo -e "${RED}Error: VectorHub server is not running on localhost:50051${NC}"
        echo "Start the server with: docker-compose up -d"
        exit 1
    fi
    
    go test -bench=. -benchmem ./test/benchmark/...
    echo -e "${GREEN}✓ Benchmark tests completed${NC}"
}

show_coverage() {
    echo -e "${YELLOW}Generating coverage report...${NC}"
    go tool cover -html=coverage.out -o coverage.html
    echo -e "${GREEN}✓ Coverage report generated: coverage.html${NC}"
    
    # Show summary
    go tool cover -func=coverage.out | grep total
}

case "$TEST_TYPE" in
    all)
        run_unit_tests
        echo ""
        run_integration_tests
        echo ""
        show_coverage
        ;;
    unit)
        run_unit_tests
        show_coverage
        ;;
    integration)
        run_integration_tests
        ;;
    benchmark)
        run_benchmark_tests
        ;;
    coverage)
        run_unit_tests
        show_coverage
        ;;
    *)
        echo "Usage: $0 [all|unit|integration|benchmark|coverage]"
        echo ""
        echo "  all          Run all tests (default)"
        echo "  unit         Run unit tests only"
        echo "  integration  Run integration tests only"
        echo "  benchmark    Run benchmark tests only"
        echo "  coverage     Run unit tests and show coverage"
        exit 1
        ;;
esac

echo ""
echo -e "${GREEN}========================================="
echo "All tests completed successfully!"
echo "=========================================${NC}"
