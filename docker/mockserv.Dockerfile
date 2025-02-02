FROM --platform=$BUILDPLATFORM golang:1.21-alpine AS build-env

# Set working directory for the build
WORKDIR /src

COPY . .

RUN go mod tidy -compat=1.19 && \
    go build /src/da/grpc/mockserv/cmd/main.go

# Final image
FROM alpine:3.18.3

WORKDIR /root

# Copy over binaries from the build-env
COPY --from=build-env /src/main /usr/bin/mock-da

EXPOSE 7980

CMD ["mock-da"]
