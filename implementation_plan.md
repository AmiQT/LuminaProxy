# Implementation Plan: LuminaProxy Evolution 🚀

Rancangan untuk upgrade LuminaProxy daripada simple caching proxy kepada **Enterprise-Grade API Gateway**. Plan ni dibahagi ikut fasa supaya senang abang nak *tackle* satu-satu esok.

## 🎯 Objektif Utama
Meningkatkan *scalability*, *resilience*, dan *observability* sistem untuk kegunaan production berskala besar.

---

## 🛠️ Proposed Changes

### Fasa 1: Load Balancing & High Availability (HA)
Support berbilang upstream servers supaya load tak tertumpu kat satu tempat.
- **[MODIFY] [main.go](file:///c:/Users/noora/Documents/Coding/LuminaProxy/main.go)**
    - Tukar `defaultUpstream` kepada slice string atau struct `Upstream`.
    - Implement **Round Robin** logic untuk diagihkan request.
    - Tambah background runner untuk **Active Health Checks** (ping upstream setiap 5s).

### Fasa 2: Scalability (Redis Integration)
Sediakan option untuk distributed caching supaya banyak instance proxy boleh share data yang sama.
- **[MODIFY] [main.go](file:///c:/Users/noora/Documents/Coding/LuminaProxy/main.go)**
    - Implement Cache Interface (boleh pilih sama ada nak guna `LRU (local)` atau `Redis`).
    - Tambah logic handling connection pool ke Redis.

### Fasa 3: Observability (Prometheus Metrics)
Tukar JSON metrics kepada standard industri supaya boleh buat dashboard lawa.
- **[MODIFY] [main.go](file:///c:/Users/noora/Documents/Coding/LuminaProxy/main.go)**
    - Gunakan library `github.com/prometheus/client_golang`.
    - Expose endpoint `/metrics`.
- **[NEW] [grafana-dashboard.json](file:///c:/Users/noora/Documents/Coding/LuminaProxy/grafana-dashboard.json)**
    - Template untuk visualisasikan traffic, hit rate, dan latency.

### Fasa 4: Resilience (Circuit Breaker)
Elakkan "Cascading Failure" kalau upstream server tengah nazak.
- **[MODIFY] [main.go](file:///c:/Users/noora/Documents/Coding/LuminaProxy/main.go)**
    - Integrasi pattern **Circuit Breaker** (State: Closed, Open, Half-Open).
    - Logic: Kalau failure rate > 50%, "trip" circuit dan serve stale data/error instantly.

---

## ✅ Verification Plan

### Automated Tests
1. **Load Test**: Guna `loadtest.go` yang sedia ada tapi tambahkan *multiple target* logic.
2. **Failure Simulation**: Matikan satu upstream server secara manual dan tengok kalau proxy pandai *auto-failover*.

### Manual Verification
1. Check `/metrics` endpoint guna browser/curl.
2. Monitor log untuk nampak pergerakan *circuit breaker state*.

---

> [!IMPORTANT]
> **User Review Required**:
> Abang nak fokus feature mana dulu esok? Antigravity cadangkan start dengan **Fasa 1 (Load Balancing)** sebab tu *core* untuk mana-mana proxy.
