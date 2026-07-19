#!/bin/bash
set -euxo pipefail

# Docker engine
dnf install -y docker
systemctl enable --now docker

# docker compose v2 plugin (not packaged in AL2023)
mkdir -p /usr/local/lib/docker/cli-plugins
curl -fsSL "https://github.com/docker/compose/releases/download/v2.29.7/docker-compose-linux-aarch64" \
  -o /usr/local/lib/docker/cli-plugins/docker-compose
chmod +x /usr/local/lib/docker/cli-plugins/docker-compose

# 2G swap — OOM insurance on a 2GB box (spec Section 1)
fallocate -l 2G /swapfile
chmod 600 /swapfile
mkswap /swapfile
swapon /swapfile
echo '/swapfile none swap sw 0 0' >> /etc/fstab

# Unattended security updates (spec Section 3, Layer 3)
dnf install -y dnf-automatic
sed -i 's/^apply_updates = .*/apply_updates = yes/' /etc/dnf/automatic.conf
sed -i 's/^upgrade_type = .*/upgrade_type = security/' /etc/dnf/automatic.conf
systemctl enable --now dnf-automatic.timer

# Deploy target directory
mkdir -p /opt/mighty
