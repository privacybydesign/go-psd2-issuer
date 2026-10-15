FROM node:23 AS frontend-build
WORKDIR /app/frontend
COPY frontend .
RUN npm ci
RUN npm run build

# -----------------------------------------------------

FROM golang:1.26.9 AS backend-build
WORKDIR /app/backend
COPY backend .
RUN go mod download

# compile with static linking
RUN CGO_ENABLED=0 go build -o ./server

# -----------------------------------------------------

FROM gcr.io/distroless/static-debian12:nonroot AS runtime
WORKDIR /app/backend

COPY --from=backend-build /app/backend/server /app/backend
COPY --from=frontend-build /app/frontend/build/ /app/frontend/build

# Declared explicitly to satisfy static scanners; the :nonroot tag already
# defaults to this same user (UID 65532).
USER nonroot:nonroot

EXPOSE 8080
ENTRYPOINT [ "/app/backend/server", "--config", "/secrets/config.json" ]
