# Quant Engine (Go)

Gin 기반의 새 Go 워크스페이스입니다. 기존 `quant-engine` 저장소와 독립적으로 구성되어 있으며 AI 서비스는 포함하지 않습니다.

## 모듈 구조

```
apps/
  api/                 # 외부 HTTP 접점; valuation-engine을 gRPC로 호출
  data_collector/      # 외부 시장·거시 데이터 수집기
  valuation-engine/    # 채권 평가용 내부 gRPC 서비스
domains/
  trading/             # 공유 주문 도메인: domain / application / adapters
  marketdata/          # 공유 시장데이터 도메인: domain / application / adapters
```

각 도메인은 독립 Go 모듈입니다. 앱은 도메인 모델을 내부에 다시 정의하지 않고 `domains/*`의 공유 타입을 사용합니다.

## 공유 데이터베이스

`data-collector`와 `valuation-engine`은 같은 PostgreSQL 데이터베이스를 사용합니다. 현재 Supabase의 PostgreSQL을 사용하며, 버전 관리되는 스키마 변경은 [`supabase/migrations/`](supabase/migrations/)에 둡니다. Supabase 연결과 배포 절차는 [`supabase/README.md`](supabase/README.md)를 참고하세요.

## 설계 규칙

- `domain`: 엔티티, 값, 검증 규칙. 프레임워크·DB·HTTP 의존 금지.
- `application`: 유스케이스 조합. 전송 방식과 영속성 세부사항을 모름.
- `adapters`: Gin, DB, 메시지 브로커 등 바깥 세계를 변환.
- 인터페이스는 실제로 교체 가능한 외부 의존성(예: 저장소 또는 브로커)이 두 개 이상 필요해질 때만 추가.
- 현재 사용하지 않는 계층, 인터페이스, 저장소, 스키마는 미리 만들지 않음. 실제 유스케이스가 생길 때 최소 단위로 추가.
- 모든 프로세스 진입점은 `apps/*/cmd/*`에 둠.

## 실행

```bash
go work sync
go run ./apps/api/cmd/api
```

`GET /health`, `POST /v1/orders`, `GET /v1/bond-valuations/us-treasury/10-year/theoretical-yield`를 제공합니다.

`VALUATION_ENGINE_GRPC_ADDR`은 엔진의 gRPC 주소(엔진 기본 `:9090`, API 기본 `localhost:9090`)이며, `VALUATION_ENGINE_HTTP_ADDR`은 Gin health endpoint 주소(기본 `:8081`)입니다. 미국채 10년물 이론금리의 내부 계산 로직은 현재 TODO이며, 호출 시 HTTP `501 Not Implemented`를 반환합니다.
