# VectorHub Kubernetes Deployment

This directory contains Kubernetes manifests for deploying VectorHub in a production environment.

## Prerequisites

- Kubernetes cluster (1.20+)
- kubectl configured
- Persistent volume provisioner configured
- (Optional) Prometheus Operator for ServiceMonitor

## Quick Start

### 1. Deploy Everything

```bash
kubectl apply -f deployments/k8s/
```

### 2. Deploy Step-by-Step

```bash
# Create namespace
kubectl apply -f namespace.yaml

# Create ConfigMap
kubectl apply -f configmap.yaml

# Deploy Redis StatefulSet
kubectl apply -f redis-statefulset.yaml

# Wait for Redis to be ready
kubectl wait --for=condition=ready pod -l app=redis -n vectorhub --timeout=300s

# Deploy VectorHub
kubectl apply -f vectorhub-deployment.yaml

# Deploy HPA (optional)
kubectl apply -f hpa.yaml

# Deploy ServiceMonitor (if using Prometheus Operator)
kubectl apply -f servicemonitor.yaml
```

## Verify Deployment

```bash
# Check pods
kubectl get pods -n vectorhub

# Check services
kubectl get svc -n vectorhub

# Check logs
kubectl logs -f deployment/vectorhub -n vectorhub

# Test health endpoint
kubectl port-forward svc/vectorhub-metrics 9090:9090 -n vectorhub
curl http://localhost:9090/health
```

## Access VectorHub

### Port Forward (Development)

```bash
# gRPC port
kubectl port-forward svc/vectorhub-grpc 50051:50051 -n vectorhub

# Metrics port
kubectl port-forward svc/vectorhub-metrics 9090:9090 -n vectorhub
```

### LoadBalancer (Production)

```bash
# Get external IP
kubectl get svc vectorhub-grpc -n vectorhub

# Connect using the EXTERNAL-IP
# grpcurl -plaintext <EXTERNAL-IP>:50051 list
```

## Scaling

### Manual Scaling

```bash
# Scale VectorHub replicas
kubectl scale deployment vectorhub -n vectorhub --replicas=5

# Scale Redis replicas
kubectl scale statefulset redis -n vectorhub --replicas=6
```

### Auto Scaling

The HPA (Horizontal Pod Autoscaler) is configured to:
- Minimum replicas: 3
- Maximum replicas: 10
- Target CPU: 70%
- Target Memory: 80%

## Configuration

### Update Configuration

```bash
# Edit ConfigMap
kubectl edit configmap vectorhub-config -n vectorhub

# Restart pods to apply changes
kubectl rollout restart deployment vectorhub -n vectorhub
```

### Environment Variables

You can override configuration using environment variables in `vectorhub-deployment.yaml`:

```yaml
env:
- name: VECTORHUB_LOGGING__LEVEL
  value: "debug"
- name: VECTORHUB_SHARDING__SHARD_COUNT
  value: "16"
```

## Monitoring

### Prometheus Integration

If you have Prometheus Operator installed, the ServiceMonitor will automatically configure scraping:

```bash
kubectl apply -f servicemonitor.yaml
```

### Check Metrics

```bash
kubectl port-forward svc/vectorhub-metrics 9090:9090 -n vectorhub
curl http://localhost:9090/metrics
```

## Persistence

Redis uses PersistentVolumeClaims (PVCs) for data persistence:

```bash
# List PVCs
kubectl get pvc -n vectorhub

# Check storage usage
kubectl exec -it redis-0 -n vectorhub -- df -h /data
```

## Troubleshooting

### Pod Not Starting

```bash
# Describe pod
kubectl describe pod <pod-name> -n vectorhub

# Check logs
kubectl logs <pod-name> -n vectorhub

# Check events
kubectl get events -n vectorhub --sort-by='.lastTimestamp'
```

### Connection Issues

```bash
# Test Redis connectivity from VectorHub pod
kubectl exec -it deployment/vectorhub -n vectorhub -- sh
# Inside pod:
# nc -zv redis-0.redis-headless 6379

# Check service endpoints
kubectl get endpoints -n vectorhub
```

### Performance Issues

```bash
# Check resource usage
kubectl top pods -n vectorhub

# Check HPA status
kubectl get hpa -n vectorhub

# Check node resources
kubectl top nodes
```

## Cleanup

```bash
# Delete all resources
kubectl delete namespace vectorhub

# Or delete specific resources
kubectl delete -f deployments/k8s/
```

## Production Considerations

1. **TLS/SSL**: Enable TLS by setting certificates in secrets
2. **Resource Limits**: Adjust based on your workload
3. **Storage**: Use appropriate storage class for your cloud provider
4. **Backup**: Implement Redis backup strategy
5. **Monitoring**: Set up alerts in Prometheus/Grafana
6. **Network Policies**: Implement network policies for security
7. **RBAC**: Configure proper service accounts and roles
8. **Ingress**: Consider using Ingress for external access
