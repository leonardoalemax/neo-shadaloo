FROM golang:1.25.2-alpine AS builder
ENV GOTOOLCHAIN=auto

# Instala o git e o swag caso seus docs dependam de geração automática do Swagger
RUN apk add --no-cache git && go install github.com/swaggo/swag/cmd/swag@latest

WORKDIR /app

# Copia e baixa as dependências (cache otimizado)
COPY go.mod go.sum ./
RUN go mod download

# Copia o resto do código (incluindo a pasta docs)
COPY . .

# Executa a geração dos docs (caso use swag), se não usar, o build segue normal
RUN if [ -f "main.go" ] && grep -q "swag" main.go; then swag init; fi

# Compila o binário de forma estática
RUN CGO_ENABLED=0 GOOS=linux go build -o neo-shadaloo .

# --- STAGE DE EXECUÇÃO ---
FROM alpine:3.20
RUN apk add --no-cache ca-certificates

WORKDIR /app

# Copia o binário gerado no estágio anterior
COPY --from=builder /app/neo-shadaloo .

# No Dokploy/Docker, você não precisa fixar as variáveis com 'ENV VAR=$VAR' no Dockerfile.
# Você deve apenas definir o valor padrão para a porta se quiser.
ENV PORT=8080

EXPOSE 8080

CMD ["./neo-shadaloo"]