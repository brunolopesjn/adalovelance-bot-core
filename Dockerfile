FROM golang:1.20 AS build-stage

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY *.go ./

RUN CGO_ENABLED=0 GOOS=linux go build -o /adalovelance-bot-core

# Deploy the application binary into a lean image
FROM golang:alpine3.18 AS build-release-stage

WORKDIR /

COPY --from=build-stage /adalovelance-bot-core /adalovelance-bot-core

RUN adduser -D -g '' nonroot

USER nonroot:nonroot

ENTRYPOINT ["/adalovelance-bot-core"]
