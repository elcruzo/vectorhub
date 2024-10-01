FROM golang:1.23-alpine AS builder

WORKDIR /app

RUN apk add --no-cache git protobuf protobuf-dev

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN go install google.golang.org/protobuf/cmd/protoc-gen-go@latest && \
    go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest

RUN protoc --go_out=. --go_opt=paths=source_relative \
    --go-grpc_out=. --go-grpc_opt=paths=source_relative \
    api/proto/*.proto

RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o vectorhub cmd/vectorhub/main.go

FROM alpine:latest

RUN apk --no-cache add ca-certificates

WORKDIR /root/

COPY --from=builder /app/vectorhub .
COPY --from=builder /app/configs ./configs

EXPOSE 50051 9090

CMD ["./vectorhub", "-config", "configs/config.yaml"]