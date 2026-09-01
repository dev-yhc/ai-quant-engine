# Quant Engine (Go)

Gin 기반의 새 Go 워크스페이스입니다. 기존 `quant-engine` 저장소와 독립적으로 구성되어 있으며 AI 서비스는 포함하지 않습니다.

## 모듈 구조

```
apps/
  api/                 # 외부 HTTP 접점; valuation-engine을 gRPC로 호출
  data_collector/      # 외부 시장·거시 데이터 수집기
  valuation-engine/    # 채권 평가용 내부 gRPC 서비스
domains/
  marketdata/          # 공유 시장데이터 도메인: domain / application / adapters
  valuation/           # 채권 평가 모델과 시그널 계산 규칙
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

`GET /health`, `GET /v1/bond-valuations/us-treasury/10-year/theoretical-yield`를 제공합니다.

valuation-engine은 `DATABASE_CONNECTION_URL`로 공유 PostgreSQL에 연결합니다. `VALUATION_ENGINE_GRPC_ADDR`은 엔진의 gRPC 주소(엔진 기본 `:9090`, API 기본 `localhost:9090`)이며, `VALUATION_ENGINE_HTTP_ADDR`은 Gin health endpoint 주소(기본 `:8081`)입니다.

평가 엔진은 `market_data.observations`에서 `DGS10`, `T10YIE`, `DFII10`, `DGS2`, `DGS3MO`, `CPIAUCSL`, `A191RL1Q225SBEA`, `ACM_TERM_PREMIUM`, `HLW_R_STAR` 시계열을 읽습니다. 마지막 두 NY Fed 데이터셋은 data-collector가 원본 파일을 정규화한 뒤 해당 series 이름으로 저장해야 합니다.

## Data collector / Temporal

`data-collector`는 Temporal 워커로 실행됩니다. `market-data-collection`
워크플로우는 FRED와 NY Fed 수집 액티비티를 차례로 실행하며, 각 액티비티는
독립적인 재시도 정책을 가집니다.

`apps/data_collector/.env.example`을 참고해 환경 변수를 설정한 뒤 워커를 실행합니다.

```bash
go run ./apps/data_collector/cmd/data-collector
```

스케줄은 워커와 별도의 명령으로 한 번 등록합니다. `TEMPORAL_SCHEDULE_CRON`,
`TEMPORAL_SCHEDULE_TIME_ZONE`, `TEMPORAL_SCHEDULE_ID`로 실행 시각과 식별자를
정의할 수 있습니다. 기본값은 `Asia/Seoul` 기준 평일 06:00입니다.

```bash
go run ./apps/data_collector/cmd/schedule-data-collector
```

같은 스케줄 ID를 다시 등록하면 Temporal이 중복 생성을 거부합니다. 변경하려면
기존 스케줄을 Temporal CLI/UI에서 삭제 또는 갱신한 뒤 이 명령을 다시 실행하세요.
