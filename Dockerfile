FROM golang:alpine AS binarybuilder
# Install build deps
RUN apk --no-cache --no-progress add --virtual build-deps build-base git linux-pam-dev
WORKDIR /go/src/github.com/naiba/solitudes/
COPY . .
RUN CGO_ENABLED=true go build -o solitudes -ldflags="-s -w" app/web/main.go

FROM alpine:latest
RUN echo http://dl-2.alpinelinux.org/alpine/edge/community/ >> /etc/apk/repositories \
  && apk --no-cache --no-progress add \
  tzdata \
  libstdc++
# Copy binary to container
WORKDIR /solitudes
COPY resource ./
COPY --from=binarybuilder /go/src/github.com/naiba/solitudes/solitudes .

# Configure Docker Container
VOLUME ["/solitudes/data"]
EXPOSE 8080
CMD ["/solitudes/solitudes"]