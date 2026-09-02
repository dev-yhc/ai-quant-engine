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
- `valuation-engine`은 `market_data.observations`의 시점별 관측값을 forward-fill로 정렬하되 미래 관측값은 사용하지 않는다.

## 채권 평가 계약

gRPC 메서드는 `CalculateUSTreasury10YearTheoreticalYield`이다. 응답은 `Date`, `Actual`, 세 개의 독립 Anchor, 동일가중 복합 `Anchor`, `RawDistance`, `Bias`, `Delta`, `DistanceStdDev`, `ZScore`, `Signal`을 포함한다. 필수 시계열 또는 정렬 가능한 과거 표본이 부족하면 gRPC `FailedPrecondition`을 반환하며 API는 HTTP `422`로 변환한다.

## Dynamic Valuation Anchor

- Macro Fundamental Anchor: `HLW_R_STAR + T10YIE + ACM_TERM_PREMIUM`.
- Statistical Dynamic Trend Anchor: 실측 `DGS10`에 대한 one-sided local-level Kalman filter. 각 시점의 상태와 분산은 해당 시점까지의 값으로만 갱신한다.
- Regression Multi-factor Anchor: CPI 전년비, GDP 성장률, `DGS2-DGS3MO` 커브 기울기, `DFII10` 실질금리를 설명변수로 사용하는 rolling OLS. 현재 시점 예측에는 이전 시점까지의 학습 표본만 사용한다.
- Composite Anchor: 문서에서 결합 가중치를 지정하지 않았으므로 세 Anchor의 동일가중 평균을 사용한다.
- Signal Normalization: `D_t = Actual_t - Anchor_t`, `Delta_t = D_t - E[D_t]`, `Z_t = Delta_t / sigma_D`. 평균과 표준편차는 현재 시점을 제외한 이전 rolling distance로 계산한다.
- `ZScore > 0`은 실제 금리가 Anchor보다 높아 채권 가격 기준 `UNDERVALUED`, `ZScore < 0`은 `OVERVALUED`, 0은 `FAIR`로 분류한다.

기본 rolling window는 회귀 1,260 관측치, 정규화 756 관측치이며 최소 60개의 유효 표본을 요구한다. CPI와 GDP의 저빈도 관측치는 발표 이후 가장 최근 값만 forward-fill한다.

## 입력 시계열 계약

FRED 수집 대상은 `DGS10`, `T10YIE`, `DFII10`, `DGS2`, `DGS3MO`, `CPIAUCSL`, `A191RL1Q225SBEA`다. NY Fed 수집 activity는 Term Premia 인터랙티브의 공식 CSV에 있는 `RunDates`/10년 `TERMYld`를 `ACM_TERM_PREMIUM`으로, HLW Estimates의 미국 `Natural Rate (r*)`를 `HLW_R_STAR`로 정규화해 `market_data.observations`에 upsert한다. 공식 Markets Data API는 ACM series를 제공하지 않으므로 ACM에는 이 CSV를 사용한다. 다운로드한 원본은 감사·재현을 위해 `market_data.research_datasets`에 보존하지만, `valuation-engine`은 원본을 읽지 않고 정규화된 observations만 조회한다.

이는 Gradle 멀티 모듈에 대응하는 Go workspace + 개별 `go.mod` 구조다. 모듈 경계는 빌드 도구가 강제하고, 의존성 방향은 `apps -> application/adapters -> domain`으로 유지한다. 서비스 간 의존성은 gRPC 계약을 통해서만 둔다.
