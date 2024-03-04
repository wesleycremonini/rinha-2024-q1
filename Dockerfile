FROM golang:alpine as build
RUN apk --no-cache add ca-certificates tzdata
WORKDIR /go/src/api
COPY . /go/src/api
RUN go mod download
RUN CGO_ENABLED=0 go build -v -ldflags "-s -w" -o /go/bin/api /go/src/api/main.go

FROM scratch

COPY --from=build /usr/share/zoneinfo /usr/share/zoneinfo
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=build /go/bin/api /

ENTRYPOINT ["/api"]