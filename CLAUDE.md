# psd2-issuer — repo notes

Go backend (`backend/`) plus a React frontend (`frontend/`). The user connects
a bank through the enablenow AppScreen, enablenow categorizes the transactions
(E1 engine), and the backend issues several small Yivi credentials (income,
housing, debts, benefits, bankaccount). Modeled on go-sms-issuer.

## Build & test

- Backend: `cd backend && go build ./... && go test ./...`. CI also runs
  `gofmt -l .`, `go fix -diff ./...` and `golangci-lint run`.
- Frontend: `cd frontend && npm run build && npx vitest run`.
- Tests use `httptest` and the in-memory `enablenow.FakeClient`; no Redis or
  enablenow credentials are needed.
- Local end-to-end without credentials: `"enablenow_backend": "fake"`.

## Layout

- `backend/credentials/`: pure aggregation from E1 categories to credential
  attributes. Change attributes here, in `scheme/generate.py` (then regenerate
  `scheme/`), and in the frontend translations; `scheme_test.go` checks the
  backend and scheme agree.
- `backend/enablenow/`: API client (`HttpClient`) and `FakeClient`.
- `backend/flow.go`: poll-driven state machine; no background goroutines.

## Gotchas

- enablenow responses are inconsistently wrapped in `{"data": ...}`;
  `decodeData` unwraps when present. `/Transactions` is read raw because its
  page has both `data` and `nextPageToken`.
- The AppScreen only forwards query params that are part of `redirect_uri`
  itself; separate AppScreen params are dropped. So `state` goes inside
  `redirect_uri` (`/callback?state=...`). The AppScreen cannot be iframed.
  The redirect goes to `/callback` (no language prefix); the frontend
  remembers the language in sessionStorage.
- The consent is deleted at enablenow as soon as credentials are computed
  (`flow.cleanup`), unless `categorization.keep_consent` is set.
