FROM golang:1.21 as builder

WORKDIR /app

COPY go.mod go.sum ./

# doload all dependencies
RUN go mod download

COPY . .

# build binary of scheduler
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -a -installsuffix cgo -o scheduler ./cmd/scheduler/main.go

# choose a lightweight base image
FROM scratch

COPY --from=builder /app/scheduler .

EXPOSE 8890

# run the binary
ENTRYPOINT ["./scheduler"]
CMD ["--controller-addr=http://127.0.0.1", "--load-balancer-port=8890"]
