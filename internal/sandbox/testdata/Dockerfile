FROM golang:1.25-bookworm

RUN apt-get update && apt-get install -y --no-install-recommends \
    bubblewrap \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /workspace
