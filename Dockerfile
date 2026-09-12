# syntax=docker/dockerfile:1
FROM golang:1.27-alpine

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

# Unit tests gate the image build; the acceptance package skips without API_URL.
RUN go test ./...

RUN go build -o /usr/local/bin/api .

EXPOSE 8080
CMD ["api"]
