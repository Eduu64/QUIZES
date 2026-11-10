FROM golang:1.22-alpine AS build
WORKDIR /s
COPY go.mod main.go index.html ./
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /quizbox

FROM scratch
COPY --from=build /quizbox /quizbox
# Límites del runtime de Go para gastar la mínima RAM
ENV DATA_DIR=/data GOGC=50 GOMEMLIMIT=40MiB GOMAXPROCS=2
EXPOSE 8080
ENTRYPOINT ["/quizbox"]
