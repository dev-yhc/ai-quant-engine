# Quant Engine (Go)

Gin 기반의 새 Go 워크스페이스입니다. 기존 `quant-engine` 저장소와 독립적으로 구성되어 있으며 AI 서비스는 포함하지 않습니다.

## 문서

- [Architecture](docs/architecture.md): 서비스 경계, 데이터 흐름 및 설계 원칙
- [Architecture Decision Records (ADR)](docs/adr.md): Google Drive에서 관리하는 주요 설계 결정 원문 링크

## 모듈 구조

```
apps/
  api/                 # 외부 HTTP 접점; valuation-engine을 gRPC로 호출
  alert-dispatcher/    # valuation outbox를 Slack alert로 전송
  data_collector/      # 외부 시장·거시 데이터 수집기
	trading-engine/      # 승인/자동 OrderIntent를 검증하고 Toss 주문으로 전달
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

## Trading engine / Toss 증권 주문

`trading-engine`은 signal-dispatcher의 `SubmitOrderIntent` gRPC 호출만 받으며, valuation DB를 직접 읽지 않습니다. 요청을 `trading.orders`에 먼저 저장한 후 worker가 최종 risk gate를 다시 검사하고 Toss Open API로 주문을 전송합니다. `idempotency_key`는 엔진 DB에서 영구 중복 방지에 사용되고, Toss에는 이 키에서 결정적으로 만든 `clientOrderId`를 사용합니다. Toss의 10분 멱등성 윈도우가 지난 일시 오류는 재주문하지 않고 `UNKNOWN`으로 남겨 수동 확인이 필요합니다.

처음에는 아래처럼 모두 명시적으로 설정하기 전까지 주문이 전송되지 않습니다. `TRADING_EXECUTION_ENABLED`의 기본값은 `false`이며, 자동신호는 별도로 `TRADING_AUTO_EXECUTION_ENABLED=true`가 필요합니다. 승인 주문은 `mode=APPROVED`와 `approval_request_id`가 있어야 합니다.

```bash
export TRADING_DATABASE_CONNECTION_URL="$DATABASE_CONNECTION_URL"
export TOSS_CLIENT_ID='...'
export TOSS_CLIENT_SECRET='...'
export TOSS_ACCOUNT_SEQ=1
export TRADING_EXECUTION_ENABLED=true
export TRADING_AUTO_EXECUTION_ENABLED=false
export TRADING_KILL_SWITCH=false
export TRADING_ALLOWED_STRATEGIES=valuation
export TRADING_ALLOWED_INSTRUMENTS=US:AAPL
export TRADING_MAX_QUANTITY=1
go run ./apps/trading-engine/cmd/trading-engine
```

`SubmitOrderIntent`의 struct payload는 `id`, `signal_event_id`, `approval_request_id`, `strategy`, `instrument` (`US:AAPL` 또는 `KR:005930`), `side`, `order_type`, `quantity` 또는 `order_amount`, `limit_price`, `idempotency_key`, `policy_version`, `mode`, `expires_at` (RFC3339)을 사용합니다.

### Trading book / portfolio alerts

`GET /v1/trading-book`은 Toss의 전체 보유 주식(KR·US)과 KRW/USD 즉시 매수 가능 현금을 조회하고, 현재 USD/KRW 환율로 원화 평가액과 자산별 전체 포트폴리오 비중(`weight`; 0~1)을 반환합니다. Toss의 보유자산 API 범위상 옵션·채권은 이 trading book에 포함되지 않습니다. USD 주식·현금이 모두 없으면 환율은 호출하지 않습니다.

`POST /v1/trading-book/alerts`는 같은 현재 스냅샷을 반환하면서 `trading.portfolio_alert_outbox`에 durable alert를 적재합니다. `alert-dispatcher`가 이 outbox를 polling하여 Slack으로 종목별 원화 평가액과 비중을 보냅니다. 조회 GET은 알림을 만들지 않으므로 화면 갱신이 알림을 중복 발송하지 않습니다. dispatcher는 `DATABASE_CONNECTION_URL`로 valuation outbox에, `TRADING_DATABASE_CONNECTION_URL`(미설정 시 전자와 동일한 값)로 trading outbox에 연결합니다.

> **향후 분리:** 이 HTTP endpoint와 포트폴리오 알림은 현재 운영 편의를 위한 임시 경계입니다. `trading-engine`은 안정적인 주문 검증·실행에만 집중해야 하므로, 조회 트래픽 또는 알림 처리량이 주문 경로에 영향을 주기 전에 portfolio/account 서비스로 분리합니다.

평가 엔진은 `market_data.observations`에서 `DGS10`, `T10YIE`, `DFII10`, `DGS2`, `DGS3MO`, `CPIAUCSL`, `A191RL1Q225SBEA`, `ACM_TERM_PREMIUM`, `HLW_R_STAR` 시계열을 읽습니다. 마지막 두 NY Fed 데이터셋은 data-collector activity가 공식 소스를 정규화해 해당 series 이름으로 upsert합니다. valuation 요청 경로에서는 NY Fed 원본을 다시 내려받거나 파싱하지 않습니다.

## Data collector / Temporal

`data-collector`는 Temporal 워커로 실행됩니다. `market-data-collection`
워크플로우는 FRED와 NY Fed 수집 액티비티를 차례로 실행하며, 각 액티비티는
독립적인 재시도 정책을 가집니다.

NY Fed Markets Data API는 ACM term premium을 제공하지 않습니다. 따라서 ACM은
NY Fed Term Premia 인터랙티브의 공식 CSV에서 `RunDates`와 10년 `TERMYld`를 읽어
`ACM_TERM_PREMIUM`으로 저장합니다. HLW workbook의 미국 Natural Rate는
`HLW_R_STAR`로 저장합니다. 두 시계열은 수집 시점에 한 번만 정규화됩니다.

### Local Temporal

먼저 `apps/data_collector/.env.example`을 복사해 `apps/data_collector/.env`를
만들고 실제 `DATABASE_CONNECTION_URL`, `FRED_API_KEY`를 입력합니다. Docker
Desktop을 실행한 뒤 아래 명령으로 로컬 Temporal Server, Web UI와
`data-collector` 워커를 함께 띄웁니다.
Temporal gRPC는 `localhost:7233`, UI는 <http://localhost:8080>에서 제공됩니다.
워커 컨테이너는 Compose 네트워크의 `temporal:7233`에 자동으로 연결됩니다.

```bash
cp apps/data_collector/.env.example apps/data_collector/.env
docker compose -f docker-compose.temporal.yml up -d
docker compose -f docker-compose.temporal.yml ps
```

호스트에서 워커를 직접 실행하려면 기존처럼 아래 명령을 사용할 수 있습니다.

```bash
go run ./apps/data_collector/cmd/data-collector
```

스케줄은 워커와 별도의 명령으로 한 번 등록합니다. `TEMPORAL_SCHEDULE_CRON`,
`TEMPORAL_SCHEDULE_TIME_ZONE`, `TEMPORAL_SCHEDULE_ID`로 실행 시각과 식별자를
정의할 수 있습니다. 기본값은 `Asia/Seoul` 기준 매일 23:00입니다. 기존 ID의
스케줄이 있으면 등록 명령은 새 cron/time zone으로 갱신합니다.

```bash
docker compose -f docker-compose.temporal.yml --profile tools run --rm data-collector-schedule
```

## Signal alerts

수집 성공 뒤 `valuation-engine`은 US 10년물 valuation을 계산하고 같은
transaction에서 `signal_events`와 `signal_outbox`를 기록합니다. `alert-dispatcher`는
매일 정보성 alert를 만들고, 이전 signal과 달라진 `UNDERVALUED`/`OVERVALUED` signal에는
별도의 거래 승인 요청 alert를 만듭니다. 승인 요청은 주문이 아니며, 이후 별도
trading-engine이 처리할 `approval_request`만 생성합니다.

실제 Slack incoming webhook을 연결한 경우에만 `SLACK_WEBHOOK_URL`을 환경에 추가하고
dispatcher를 시작합니다. 현재 repository의 테스트는 로컬 HTTP mock으로 Slack payload만
검증하며, 테스트나 기본 Compose 실행에서 Slack으로 메시지를 보내지 않습니다.

```bash
docker compose -f docker-compose.temporal.yml --profile alerts up -d alert-dispatcher
```

로컬 Temporal 데이터까지 지우려면 다음 명령을 사용합니다.

```bash
docker compose -f docker-compose.temporal.yml down -v
```
