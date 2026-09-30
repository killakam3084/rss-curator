FROM golang:1.26.5-alpine AS build
WORKDIR /src
COPY go.mod .
COPY main.go .
RUN CGO_ENABLED=0 go build -o /mesh .

FROM alpine:latest
COPY --from=build /mesh /mesh
EXPOSE 32400
ENTRYPOINT ["/mesh"]
