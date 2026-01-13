FROM golang:1.25 AS build

WORKDIR /app

# Copy the Go module files
COPY go.mod .
COPY go.sum .

# Download the Go module dependencies
RUN go mod download

COPY internal internal
COPY logsproxy logsproxy
COPY main.go main.go

# Build the binary statically
RUN CGO_ENABLED=0 GOOS=linux go build -o /logs-proxy ./

FROM alpine:3.21

WORKDIR /app

COPY --from=build /logs-proxy /logs-proxy

ENTRYPOINT ["/logs-proxy"]

