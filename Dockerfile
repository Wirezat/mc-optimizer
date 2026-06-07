FROM golang:1.24-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN go build -o server ./cmd/server

FROM alpine:3.19
WORKDIR /app
COPY --from=builder /app/server .
COPY web/ web/
# Replace broken symlinks (wirezat-ui package, outside build context) with real files.
# Pass --build-context wirezat-ui=/path/to/wirezat-ui when building.
RUN rm web/static/css/base.css web/static/css/components.css web/static/css/layout.css \
       web/pages/login.html
COPY --from=wirezat-ui css/base.css       web/static/css/base.css
COPY --from=wirezat-ui css/components.css web/static/css/components.css
COPY --from=wirezat-ui css/layout.css     web/static/css/layout.css
COPY --from=wirezat-ui pages/auth.html    web/pages/login.html
EXPOSE 8081
CMD ["./server"]