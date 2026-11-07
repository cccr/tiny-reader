FROM golang:1.27-alpine AS build
WORKDIR /src
COPY go.mod ./
COPY *.go index.html ./
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /tiny-reader .

FROM gcr.io/distroless/static
COPY --from=build /tiny-reader /tiny-reader
EXPOSE 8080
ENTRYPOINT ["/tiny-reader"]
