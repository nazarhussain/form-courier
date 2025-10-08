# build
FROM golang:1.25-alpine AS build
WORKDIR /src
COPY go.mod ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/form-mailer

# tiny runtime (no shell)
FROM gcr.io/distroless/static:nonroot
WORKDIR /
COPY --from=build /out/form-mailer /form-mailer
USER nonroot:nonroot
EXPOSE 3000
ENTRYPOINT ["/form-mailer"]