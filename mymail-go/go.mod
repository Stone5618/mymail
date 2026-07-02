module github.com/mymail/mymail-go

go 1.22

require (
	github.com/gin-gonic/gin v1.10.0
	github.com/golang-jwt/jwt/v5 v5.2.1
	github.com/golang-migrate/migrate/v4 v4.17.1
	github.com/google/uuid v1.6.0
	github.com/jmoiron/sqlx v1.4.0
	github.com/prometheus/client_golang v1.20.4
	github.com/spf13/viper v1.19.0
	github.com/stretchr/testify v1.9.0
	go.opentelemetry.io/otel v1.30.0
	go.opentelemetry.io/contrib/instrumentation/github.com/gin-gonic/gin/otelgin v0.54.0
	golang.org/x/crypto v0.27.0
	modernc.org/sqlite v1.34.1
)
