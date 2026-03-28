module github.com/ai-volund/volund

go 1.26.1

require (
	github.com/ai-volund/volund-proto v0.0.0
	github.com/golang-jwt/jwt/v5 v5.3.1
	github.com/spf13/cobra v1.10.2
	google.golang.org/grpc v1.79.3
)

require (
	github.com/inconshreveable/mousetrap v1.1.0 // indirect
	github.com/spf13/pflag v1.0.9 // indirect
	golang.org/x/net v0.51.0 // indirect
	golang.org/x/sys v0.42.0 // indirect
	golang.org/x/text v0.35.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20251202230838-ff82c1b0f217 // indirect
	google.golang.org/protobuf v1.36.11 // indirect
)

replace github.com/ai-volund/volund-proto => ../volund-proto
