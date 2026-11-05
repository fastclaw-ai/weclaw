FROM golang:1.25-alpine AS builder

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /usr/local/bin/weclaw .

FROM alpine:3.21

RUN apk add --no-cache ca-certificates tzdata
COPY --from=builder /usr/local/bin/weclaw /usr/local/bin/weclaw

VOLUME /root/.weclaw
ENTRYPOINT ["weclaw"]
CMD ["start"]
