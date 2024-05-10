#!/bin/bash
# Build script

set -e

echo "========================================="
echo "VectorHub Build Script"
echo "========================================="

# Colors
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

# Configuration
BINARY_NAME="vectorhub"
BUILD_DIR="bin"
VERSION="${VERSION:-dev}"
COMMIT=$(git rev-parse --short HEAD 2>/dev/null || echo "unknown")
BUILD_TIME=$(date -u '+%Y-%m-%d_%H:%M:%S')

# Create build directory
mkdir -p $BUILD_DIR

# Generate protobuf code
echo -e "${YELLOW}Generating protobuf code...${NC}"
make proto
echo -e "${GREEN}✓ Protobuf code generated${NC}"

# Build flags
LDFLAGS="-X main.Version=$VERSION -X main.Commit=$COMMIT -X main.BuildTime=$BUILD_TIME"

# Build binary
echo -e "${YELLOW}Building $BINARY_NAME...${NC}"
CGO_ENABLED=0 go build -ldflags="$LDFLAGS" -o $BUILD_DIR/$BINARY_NAME cmd/vectorhub/main.go
echo -e "${GREEN}✓ Binary built: $BUILD_DIR/$BINARY_NAME${NC}"

# Show binary info
echo ""
echo "Build Information:"
echo "  Version:    $VERSION"
echo "  Commit:     $COMMIT"
echo "  Build Time: $BUILD_TIME"
echo "  Size:       $(du -h $BUILD_DIR/$BINARY_NAME | cut -f1)"

# Build Docker image
if [ "$1" == "docker" ]; then
    echo ""
    echo -e "${YELLOW}Building Docker image...${NC}"
    docker build -t vectorhub:$VERSION .
    docker tag vectorhub:$VERSION vectorhub:latest
    echo -e "${GREEN}✓ Docker image built: vectorhub:$VERSION${NC}"
fi

echo ""
echo -e "${GREEN}Build complete!${NC}"
