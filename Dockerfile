FROM golang:1.26-bookworm AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -ldflags "-s -w -X main.version=${VERSION:-dev}" -o /volund ./cmd/volund

FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=builder /volund /volund

EXPOSE 8080 9090

ENTRYPOINT ["/volund"]
CMD ["serve"]
