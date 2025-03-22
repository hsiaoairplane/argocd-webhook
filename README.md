# ArgoCD App Webhook

## Overview

The **ArgoCD App Webhook** is a Kubernetes admission webhook designed to optimize the handling of ArgoCD `Application` resources by filtering out unnecessary updates. Specifically, it prevents updates that only modify `status.reconciledAt`, reducing API server load and ETCD database growth.

### Benefits

1. **Reduces API Server Load**: Prevents frequent PATCH API calls caused by `status.reconciledAt` updates.
2. **Optimizes ETCD Storage**: Minimizes unnecessary revision history storage in ETCD.

## De

```console
./cert.sh

kubectl apply -f webhook-deployment.yaml
kubectl apply -f webhook-validatingwebhookconfiguration.yaml

kubectl -n argocd patch validatingwebhookconfiguration application-admission-webhook \
  --type='json' \
  -p="[{
    \"op\": \"replace\",
    \"path\": \"/webhooks/0/clientConfig/caBundle\",
    \"value\": \"$(cat certs/ca.crt | base64 | tr -d '\n')\"
  }]"
```
