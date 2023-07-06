FROM golang:1.20 AS build-stage

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download

COPY *.go ./
RUN CGO_ENABLED=0 GOOS=linux go build -o /d8r && \
    strip /d8r

FROM gcr.io/distroless/base-debian11 AS build-release-stage
WORKDIR /
COPY --from=build-stage /d8r /d8r
USER nonroot:nonroot
ENTRYPOINT ["/d8r"]
