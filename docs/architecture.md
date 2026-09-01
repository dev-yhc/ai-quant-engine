# Architecture

`apps/*`는 실행 진입점과 의존성 조립을 담당한다. `apps/api`는 외부 HTTP 접점이며, 가치평가 요청은 로컬 구현을 직접 호출하지 않고 `valuation-engine`의 내부 gRPC API로 전달한다. 유스케이스는 공유 도메인 모듈의 `application`에, 순수 규칙과 비즈니스 모델은 공유 도메인 모듈의 `domain`에 둔다. 외부 시스템 연결이 생기면 해당 도메인의 `adapters`에 구현을 두고, 필요할 때만 application 패키지에 작은 포트를 정의한다.

아직 사용하지 않는 계층, 인터페이스, 저장소, 데이터베이스 스키마는 미리 만들지 않는다. 구체적인 유스케이스가 실제로 필요해졌을 때 그 유스케이스가 요구하는 최소 구조만 추가한다.

이 저장소는 하나의 공유 도메인 모델을 사용한다. 따라서 앱의 `internal/domain`에 도메인 모델을 복제하지 않는다. 예를 들어 `apps/data_collector`가 수집하는 `Observation`과 `ResearchDataset`은 `domains/marketdata/domain`에 두며, 이후 저장·분석·valuation 앱이 같은 타입을 사용한다. 앱 내부에는 adapter 구현, 설정, 실행 조립만 둔다.

## 서비스 경계와 데이터 흐름

```
외부 소비자 ──HTTP──> apps/api ──gRPC──> apps/valuation-engine
                                           │
data-collector ──수집/적재──> shared PostgreSQL ──┘
```

- `data-collector`는 시장·거시 데이터를 수집하는 데이터 생산자다. 수집·정규화된 데이터는 `valuation-engine`과 공유하는 PostgreSQL을 통해 전달한다. 현재 호스트는 Supabase이며, 두 서비스는 같은 `DATABASE_CONNECTION_URL`을 사용한다.
- `valuation-engine`은 외부 비공개 채권 평가 서비스다. Gin은 운영 상태 확인용 `GET /health`만 제공하며, 업무 호출은 gRPC `valuation.v1.BondEvaluationService`로 받는다.
- `api`만 외부 HTTP 계약을 소유한다. `GET /v1/bond-valuations/us-treasury/10-year/theoretical-yield`는 gRPC 결과를 HTTP JSON으로 변환한다.
- 공유 DB의 변경 이력은 `supabase/migrations/`에서 관리한다. baseline은 `market_data.observations`와 `market_data.research_datasets`만 만들며 Supabase 전용 기능을 사용하지 않는다. 적용 방법은 `supabase/README.md`를 따른다.
- 미국채 10년물 이론금리 계산 로직과 이에 필요한 PostgreSQL 조회 구조는 TODO다. 계산 입력이 확정되기 전까지 사용하지 않는 저장소나 스키마를 만들지 않는다.

## 채권 평가 계약

gRPC 메서드는 `CalculateUSTreasury10YearTheoreticalYield`이다. 내부 계산이 구현되기 전에는 gRPC `Unimplemented`를 반환하며, API는 이를 HTTP `501 Not Implemented`로 변환한다.

이는 Gradle 멀티 모듈에 대응하는 Go workspace + 개별 `go.mod` 구조다. 모듈 경계는 빌드 도구가 강제하고, 의존성 방향은 `apps -> application/adapters -> domain`으로 유지한다. 서비스 간 의존성은 gRPC 계약을 통해서만 둔다.
