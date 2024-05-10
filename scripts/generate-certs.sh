#!/bin/bash
# Generate self-signed certificates for development/testing
# DO NOT use these certificates in production!

set -e

CERT_DIR="${1:-./certs}"
DOMAIN="${2:-localhost}"
DAYS="${3:-365}"

echo "Generating self-signed TLS certificates..."
echo "Certificate directory: $CERT_DIR"
echo "Domain: $DOMAIN"
echo "Valid for: $DAYS days"

# Create certificate directory
mkdir -p "$CERT_DIR"

# Generate private key
openssl genrsa -out "$CERT_DIR/server.key" 2048

# Generate certificate signing request
openssl req -new -key "$CERT_DIR/server.key" -out "$CERT_DIR/server.csr" \
  -subj "/C=US/ST=State/L=City/O=VectorHub/OU=Development/CN=$DOMAIN"

# Generate self-signed certificate
openssl x509 -req -days "$DAYS" \
  -in "$CERT_DIR/server.csr" \
  -signkey "$CERT_DIR/server.key" \
  -out "$CERT_DIR/server.crt" \
  -extfile <(printf "subjectAltName=DNS:$DOMAIN,DNS:localhost,IP:127.0.0.1")

# Generate CA certificate (for client verification)
openssl req -new -x509 -days "$DAYS" \
  -key "$CERT_DIR/server.key" \
  -out "$CERT_DIR/ca.crt" \
  -subj "/C=US/ST=State/L=City/O=VectorHub/OU=Development/CN=CA"

# Set permissions
chmod 600 "$CERT_DIR/server.key"
chmod 644 "$CERT_DIR/server.crt" "$CERT_DIR/ca.crt"

# Clean up CSR
rm -f "$CERT_DIR/server.csr"

echo "✓ Certificates generated successfully in $CERT_DIR"
echo ""
echo "Files created:"
echo "  - $CERT_DIR/server.key (private key)"
echo "  - $CERT_DIR/server.crt (certificate)"
echo "  - $CERT_DIR/ca.crt (CA certificate)"
echo ""
echo "To use these certificates, set in your config or environment:"
echo "  VECTORHUB_SERVER__TLS_ENABLED=true"
echo "  VECTORHUB_SERVER__TLS_CERT_FILE=$CERT_DIR/server.crt"
echo "  VECTORHUB_SERVER__TLS_KEY_FILE=$CERT_DIR/server.key"
echo ""
echo "⚠️  WARNING: These are self-signed certificates for development only!"
echo "    For production, use certificates from a trusted Certificate Authority."
