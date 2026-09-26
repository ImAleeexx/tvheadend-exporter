FROM golang:1.27-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG VERSION=dev
ARG COMMIT=unknown
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.version=${VERSION} -X main.commit=${COMMIT}" \
    -o /out/tvheadend-exporter ./cmd/tvheadend-exporter

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/tvheadend-exporter /tvheadend-exporter
EXPOSE 9429
USER nonroot
ENTRYPOINT ["/tvheadend-exporter"]
