FROM golang:1.23-alpine AS builder

WORKDIR /src

ARG GOPROXY=https://goproxy.cn,https://proxy.golang.org,https://goproxy.io
ARG GOSUMDB=off

ENV GOPROXY=${GOPROXY}
ENV GOSUMDB=${GOSUMDB}

COPY backend/go.mod backend/go.sum ./backend/
RUN --mount=type=cache,target=/go/pkg/mod \
	sh -c 'cd backend && for i in 1 2 3; do go mod download && exit 0; echo "go mod download failed, retry $i/3"; sleep $((i*2)); done; exit 1'

COPY backend ./backend
RUN cd backend && CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /out/api-server ./cmd/api-server

FROM hashicorp/terraform:1.9 AS terraform-bin

# Pre-download the bpg/proxmox provider into a local filesystem mirror so that
# terraform init can run without internet access at runtime.
WORKDIR /provider-mirror-src
RUN printf 'terraform {\n  required_providers {\n    proxmox = {\n      source  = "bpg/proxmox"\n      version = ">= 0.66.0"\n    }\n  }\n}\n' > main.tf \
    && terraform providers mirror /terraform-providers

FROM alpine:3.20

RUN apk add --no-cache ansible-core openssh-client curl

# Install kubectl
RUN curl -LO "https://dl.k8s.io/release/$(curl -L -s https://dl.k8s.io/release/stable.txt)/bin/linux/amd64/kubectl" \
    && chmod +x kubectl \
    && mv kubectl /usr/local/bin/

RUN adduser -D -g '' appuser

WORKDIR /app/backend

COPY --from=builder /out/api-server /app/backend/api-server
COPY --from=builder /src/backend/migrations /app/backend/migrations
COPY --from=terraform-bin /bin/terraform /usr/local/bin/terraform
COPY --from=terraform-bin /terraform-providers /terraform-providers

RUN mkdir -p /app/backend/data/terraform/runs /app/backend/data/ansible/runs /app/backend/data/operations/runs /app/backend/.kube
RUN chown -R appuser:appuser /app
USER appuser

EXPOSE 8080

ENTRYPOINT ["/app/backend/api-server"]
