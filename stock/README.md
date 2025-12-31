# StockExporrter

A Prometheus exporter for stock prices using Polygon.io API.

## Features

- Fetches stock prices from Polygon.io using bid/ask quotes
- Exposes metrics in Prometheus format on `/metrics` endpoint
- Configurable stock symbols via environment variable

## Usage

### Environment Variables (Required)

- `SYMBOLS`: Comma-separated list of stock symbols (e.g., "QCOM,NVDA,INTC")
- `POLYGON_API_KEY`: Polygon.io API key

### Running Locally

**Git Bash / Linux / macOS:**
```bash
export POLYGON_API_KEY="g0########################"
export SYMBOLS="####,####,####"
go run main.go
```

Or as a one-liner:
```bash
POLYGON_API_KEY="g0########################" SYMBOLS="####,####,####" go run main.go
```

**Windows PowerShell:**
```powershell
$env:POLYGON_API_KEY="g0########################"
$env:SYMBOLS="####,####,####"
go run main.go
```

**Windows Command Prompt (cmd.exe):**
```cmd
set POLYGON_API_KEY=g0########################
set SYMBOLS=####,####,####
go run main.go
```

The server will start on port 8080. Access metrics at:
```
http://localhost:8080/metrics
```

### Running with Docker

**Build the Docker image:**
```bash
docker build -t stockexporter .
```

**Run the container:**
```bash
docker run -p 8080:8080 \
  -e POLYGON_API_KEY="g0########################" \
  -e SYMBOLS="####,####,####" \
  stockexporter
```

**Or as a one-liner:**
```bash
docker run -p 8080:8080 -e POLYGON_API_KEY="g0########################" -e SYMBOLS="####,####,####" stockexporter
```

### Metrics

The exporter exposes the following metric:
- `stock_price{symbol="SYMBOL"}` - MID price (calculated as (bid + ask) / 2) for the given symbol
