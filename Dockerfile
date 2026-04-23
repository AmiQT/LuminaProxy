# Langkah 1: Phase "Builder"
FROM golang:1.26-alpine AS builder

# Pasang UPX untuk compress binary Go ke tahap ekstrem
RUN apk add --no-cache upx

WORKDIR /app

# Copy dependencies & download
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Compile dengan stripping symbols (-s -w)
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -a -installsuffix cgo -o lumina-proxy .

# Kompres binary menggunakan UPX (Ini 'secret sauce' nak dapat 10-15MB)
RUN upx --best --lzma lumina-proxy

# Langkah 2: Phase "Certs" (Untuk ambil sijil SSL/TLS)
FROM alpine:latest AS certs
RUN apk --no-cache add ca-certificates

# Langkah 3: Phase "Runner" (ULTRA RINGAN - 0 MB base)
FROM scratch

# Salin sijil keselamatan dari stage certs
COPY --from=certs /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/

# Salin binary yang dah dipicit (compressed)
COPY --from=builder /app/lumina-proxy /lumina-proxy

# Buka laluan port default
EXPOSE 8080

# Jalankan proxy
ENTRYPOINT ["/lumina-proxy"]
