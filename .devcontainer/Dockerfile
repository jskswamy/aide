FROM golang:1.25-bookworm

RUN apt-get update && apt-get install -y --no-install-recommends \
    bubblewrap \
    git \
    age \
    make \
    curl \
    jq \
    && rm -rf /var/lib/apt/lists/*

# Install sops
ARG SOPS_VERSION=3.9.4
RUN curl -Lo /usr/local/bin/sops \
    "https://github.com/getsops/sops/releases/download/v${SOPS_VERSION}/sops-v${SOPS_VERSION}.linux.$(dpkg --print-architecture)" \
    && chmod +x /usr/local/bin/sops

# Install golangci-lint
ARG GOLANGCI_LINT_VERSION=1.64.8
RUN curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/HEAD/install.sh | \
    sh -s -- -b /usr/local/bin v${GOLANGCI_LINT_VERSION}

# Verify tools
RUN go version && git version && age --version && sops --version && \
    bwrap --version && golangci-lint --version

WORKDIR /workspace
