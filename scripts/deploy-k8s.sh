#!/bin/bash
# Kubernetes deployment script

set -e

echo "========================================="
echo "VectorHub Kubernetes Deployment"
echo "========================================="

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

# Configuration
NAMESPACE="${NAMESPACE:-vectorhub}"
IMAGE="${IMAGE:-vectorhub:latest}"
DEPLOY_DIR="deployments/k8s"

# Parse arguments
ACTION="${1:-deploy}"

show_help() {
    echo "Usage: $0 [ACTION]"
    echo ""
    echo "Actions:"
    echo "  deploy      Deploy VectorHub to Kubernetes (default)"
    echo "  update      Update existing deployment"
    echo "  delete      Delete VectorHub from Kubernetes"
    echo "  status      Check deployment status"
    echo "  logs        Tail logs from VectorHub pods"
    echo "  help        Show this help message"
    echo ""
    echo "Environment Variables:"
    echo "  NAMESPACE   Kubernetes namespace (default: vectorhub)"
    echo "  IMAGE       Docker image to deploy (default: vectorhub:latest)"
    exit 0
}

check_prerequisites() {
    echo -e "${YELLOW}Checking prerequisites...${NC}"

    if ! command -v kubectl &> /dev/null; then
        echo -e "${RED}Error: kubectl is not installed${NC}"
        exit 1
    fi

    if ! kubectl cluster-info &> /dev/null; then
        echo -e "${RED}Error: Cannot connect to Kubernetes cluster${NC}"
        exit 1
    fi

    echo -e "${GREEN}✓ Prerequisites satisfied${NC}"
}

deploy() {
    echo -e "${BLUE}Deploying VectorHub to Kubernetes...${NC}"

    # Create namespace
    echo -e "${YELLOW}Creating namespace: $NAMESPACE${NC}"
    kubectl apply -f $DEPLOY_DIR/namespace.yaml

    # Create ConfigMap
    echo -e "${YELLOW}Creating ConfigMap...${NC}"
    kubectl apply -f $DEPLOY_DIR/configmap.yaml

    # Deploy Redis
    echo -e "${YELLOW}Deploying Redis StatefulSet...${NC}"
    kubectl apply -f $DEPLOY_DIR/redis-statefulset.yaml

    # Wait for Redis
    echo -e "${YELLOW}Waiting for Redis pods to be ready...${NC}"
    kubectl wait --for=condition=ready pod -l app=redis -n $NAMESPACE --timeout=300s

    # Deploy VectorHub
    echo -e "${YELLOW}Deploying VectorHub...${NC}"
    kubectl apply -f $DEPLOY_DIR/vectorhub-deployment.yaml

    # Deploy HPA
    echo -e "${YELLOW}Deploying HPA...${NC}"
    kubectl apply -f $DEPLOY_DIR/hpa.yaml

    # Deploy ServiceMonitor (if Prometheus Operator is available)
    if kubectl get crd servicemonitors.monitoring.coreos.com &> /dev/null; then
        echo -e "${YELLOW}Deploying ServiceMonitor...${NC}"
        kubectl apply -f $DEPLOY_DIR/servicemonitor.yaml
    else
        echo -e "${YELLOW}Skipping ServiceMonitor (Prometheus Operator not found)${NC}"
    fi

    echo -e "${GREEN}✓ Deployment complete${NC}"
    
    echo ""
    echo -e "${YELLOW}Waiting for VectorHub pods to be ready...${NC}"
    kubectl wait --for=condition=ready pod -l app=vectorhub -n $NAMESPACE --timeout=300s

    show_status
}

update() {
    echo -e "${BLUE}Updating VectorHub deployment...${NC}"

    # Update ConfigMap
    kubectl apply -f $DEPLOY_DIR/configmap.yaml

    # Update Deployment
    kubectl apply -f $DEPLOY_DIR/vectorhub-deployment.yaml

    # Restart deployment to pick up changes
    kubectl rollout restart deployment vectorhub -n $NAMESPACE

    echo -e "${YELLOW}Waiting for rollout to complete...${NC}"
    kubectl rollout status deployment vectorhub -n $NAMESPACE

    echo -e "${GREEN}✓ Update complete${NC}"
}

delete() {
    echo -e "${RED}Deleting VectorHub from Kubernetes...${NC}"
    read -p "Are you sure? (yes/no): " -r
    echo
    if [[ ! $REPLY =~ ^[Yy][Ee][Ss]$ ]]; then
        echo "Cancelled"
        exit 0
    fi

    kubectl delete namespace $NAMESPACE --wait=true

    echo -e "${GREEN}✓ Deletion complete${NC}"
}

show_status() {
    echo ""
    echo -e "${BLUE}========================================="
    echo "Deployment Status"
    echo "=========================================${NC}"

    echo -e "\n${YELLOW}Pods:${NC}"
    kubectl get pods -n $NAMESPACE

    echo -e "\n${YELLOW}Services:${NC}"
    kubectl get svc -n $NAMESPACE

    echo -e "\n${YELLOW}PVCs:${NC}"
    kubectl get pvc -n $NAMESPACE

    echo -e "\n${YELLOW}HPA:${NC}"
    kubectl get hpa -n $NAMESPACE

    # Get external IP if available
    EXTERNAL_IP=$(kubectl get svc vectorhub-grpc -n $NAMESPACE -o jsonpath='{.status.loadBalancer.ingress[0].ip}' 2>/dev/null || echo "pending")
    
    echo ""
    echo -e "${GREEN}========================================="
    echo "Access Information"
    echo "=========================================${NC}"
    
    if [ "$EXTERNAL_IP" != "pending" ] && [ -n "$EXTERNAL_IP" ]; then
        echo "External IP: $EXTERNAL_IP"
        echo "gRPC Endpoint: $EXTERNAL_IP:50051"
    else
        echo "External IP: Pending (use port-forward)"
        echo ""
        echo "Port forward commands:"
        echo "  kubectl port-forward svc/vectorhub-grpc 50051:50051 -n $NAMESPACE"
        echo "  kubectl port-forward svc/vectorhub-metrics 9090:9090 -n $NAMESPACE"
    fi
}

show_logs() {
    echo -e "${BLUE}Tailing VectorHub logs...${NC}"
    kubectl logs -f -l app=vectorhub -n $NAMESPACE --tail=50
}

# Main
case "$ACTION" in
    deploy)
        check_prerequisites
        deploy
        ;;
    update)
        check_prerequisites
        update
        ;;
    delete)
        check_prerequisites
        delete
        ;;
    status)
        check_prerequisites
        show_status
        ;;
    logs)
        check_prerequisites
        show_logs
        ;;
    help|--help|-h)
        show_help
        ;;
    *)
        echo -e "${RED}Unknown action: $ACTION${NC}"
        show_help
        ;;
esac
