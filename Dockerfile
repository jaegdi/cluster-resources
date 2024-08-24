# Verwenden Sie das offizielle Golang-Image als Basis
FROM golang:1.23-alpine

# Setzen Sie das Arbeitsverzeichnis im Container
WORKDIR /app

# Kopieren Sie die Go-Moduldateien und laden Sie die Abhängigkeiten herunter
COPY go.mod go.sum ./
RUN go mod download

# Kopieren Sie den Rest des Anwendungsquellcodes
COPY cluster-resources.go .

# Bauen Sie die Go-Anwendung
RUN go build -o cluster-resources . \
    && chown -R root * \
    && chmod -R g=u *


USER 1001

# Setzen Sie den Port, auf dem die Anwendung läuft
EXPOSE 8080

# Definieren Sie den Befehl zum Starten der Anwendung
CMD ["./cluster-resources", "-server"]