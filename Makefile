test:
	go run gotest.tools/gotestsum@v1.13.0 -- -count=1 ./...

grpc:
	protoc --go_out=. --go_opt=paths=source_relative \
    --go-grpc_out=. --go-grpc_opt=paths=source_relative \
    logsproxy/v1/logs.proto



