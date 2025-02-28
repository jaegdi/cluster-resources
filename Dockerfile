# Verwenden Sie das offizielle Golang-Image als Basis
FROM golang:1.23-alpine AS builder

# Setzen Sie das Arbeitsverzeichnis im Container
WORKDIR /app

# Kopieren Sie die Go-Moduldateien und laden Sie die Abhängigkeiten herunter
COPY go.mod go.sum ./

# Kopieren Sie den Rest des Anwendungsquellcodes
COPY cluster-resources.go .

RUN go mod tidy -v
# Bauen Sie die Go-Anwendung
RUN go build -tags netgo -v -o cluster-resources . \
    && chown -R root * \
    && chmod -R g=u *



FROM alpine:latest
LABEL maintainer="Dirk Jäger <dirk.jaeger@schufa.de>"

ENV LANG=en_US.UTF-8
ENV TZ=Europe/Berlin
COPY certs/*.crt /usr/local/share/ca-certificates/
RUN    https_proxy=http://webproxy.sf-bk.de:8181/ \
    && HTTPS_PROXY=$https_proxy \
    && no_proxy=localhost,127.0.0.1,.sf-rz.de,.sf-bk.de,172.0.0.0/8,10.0.0.0/8 \
    && NO_PROXY=$no_proxy \
    && apk --no-cache add ca-certificates tzdata libc6-compat libgcc libstdc++ \
    && update-ca-certificates

WORKDIR /app
USER 1001
COPY --from=builder /app/cluster-resources /app/cluster-resources
# Setzen Sie den Port, auf dem die Anwendung läuft
EXPOSE 8080
# Definieren Sie den Befehl zum Starten der Anwendung
CMD ["./cluster-resources", "-server"]