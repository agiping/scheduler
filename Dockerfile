FROM golang:1.21 as builder

WORKDIR /app

COPY go.mod go.sum ./

# set GOPROXY to accelerate the download of dependencies
ENV GOPROXY=https://goproxy.cn,direct

# download all dependencies
RUN go mod download

COPY . .

# build baichuan scheduler
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -a -installsuffix cgo -o bcScheduler ./scheduler/cmd/scheduler/main.go

# choose a lightweight base image
FROM busybox:1.31

COPY --from=builder /app/bcScheduler .

EXPOSE 8890

# run scheduler
ENTRYPOINT ["./bcScheduler"]
CMD ["--controller-addr=http://127.0.0.1", "--load-balancer-port=8890"]
