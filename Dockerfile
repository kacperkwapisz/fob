FROM golang:1.24-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /fob ./cmd/fob

FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /
COPY --from=build /fob /fob
USER nonroot:nonroot
ENV HOST=0.0.0.0 \
    PORT=8317 \
    DATABASE_PATH=/data/fob.sqlite
EXPOSE 8317
VOLUME /data
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s CMD ["/fob", "-healthcheck"]
ENTRYPOINT ["/fob"]
