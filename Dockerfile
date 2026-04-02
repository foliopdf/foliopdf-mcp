FROM golang:1.23-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /foliopdf-mcp .

FROM alpine:3.20
RUN apk add --no-cache ca-certificates
COPY --from=build /foliopdf-mcp /usr/local/bin/foliopdf-mcp
ENTRYPOINT ["foliopdf-mcp"]
