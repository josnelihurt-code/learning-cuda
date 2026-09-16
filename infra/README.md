# Infrastructure Deployment

This directory contains infrastructure deployment configurations for the CUDA Learning Platform. These are internal deployment scripts used to provision and manage the infrastructure for the project.

> **Note**: This infrastructure is organized for personal deployment and is not part of the core project functionality. It reflects how the project author deploys the platform on their own servers.

## Contents

This directory contains:
- Docker Compose configurations for personal deployment

Terraform configurations and Ansible playbooks live under [`../scripts/deployment/`](../scripts/deployment/) instead.

## Related Deployment Documentation

For production deployment workflows and procedures, see the main deployment scripts in:
- [`../scripts/deployment/`](../scripts/deployment/) - Comprehensive deployment tooling
  - `staging_local/` - Local staging with Docker Compose
  - `jetson-nano/` - Production deployment on Jetson Nano
  - `radxa/` - ARM64 runner deployment
  - `prox4/` - Proxmox/GitHub Actions runner provisioning
  - `cloud-vm/` - Cloud VM deployment for Go server
  - `github-runner/` - Self-hosted GitHub Actions runner (Terraform)

## Quick Reference

### Production URLs
- **Main Application**: https://cuda-demo.lab.josnelihurt.me
- **Grafana Monitoring**: https://grafana-cuda-demo.josnelihurt.me
- **Jaeger Tracing**: https://jaeger-cuda-demo.josnelihurt.me
- **Test Reports**: https://reports-cuda-demo.josnelihurt.me

### Deployment Architecture

The platform uses a distributed deployment model:
- **Go Server (Cloud)**: Web server running on cloud VM (x86_64)
- **C++ Accelerator (Jetson Nano)**: GPU processing on edge hardware
- **Reverse gRPC Topology**: C++ client dials into Go server via mTLS

### Key Infrastructure Components

1. **Traefik**: Reverse proxy and ingress controller
2. **Docker**: Container orchestration
3. **Ansible**: Configuration management
4. **Terraform**: Infrastructure provisioning (Proxmox VMs)

## See Also

- [Main README](../README.md) - Project overview
- [Scripts README](../scripts/README.md) - Deployment automation
