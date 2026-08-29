# Build a static PhantomGuard binary with no Go runtime dependencies.
FROM golang:1.21-alpine AS build
WORKDIR /src
COPY go.mod ./
COPY cmd ./cmd
COPY pkg ./pkg
COPY data ./data
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/phantomguard ./cmd/phantomguard

# The scanner itself is a single binary; Git and CA roots are only supplied for hook execution in this demo image.
FROM alpine:3.20
RUN apk add --no-cache ca-certificates git
COPY --from=build /out/phantomguard /usr/local/bin/phantomguard
ENTRYPOINT ["phantomguard"]
