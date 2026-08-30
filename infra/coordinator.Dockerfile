# Build
FROM golang:1.27-alpine AS build
WORKDIR /src
COPY coordinator/go.mod coordinator/go.sum ./
RUN go mod download
COPY coordinator/ ./
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o /out/coordinator ./cmd/coordinator

# Runtime
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/coordinator /coordinator
EXPOSE 8080
USER nonroot:nonroot
ENTRYPOINT ["/coordinator"]
