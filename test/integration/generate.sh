#!/usr/bin/env bash
set +x

cat << HEREDOC > manifests/aws-credentials.yaml
---
apiVersion: v1
kind: Secret
metadata:
  name: aws-credentials-secret
  namespace: openshift-machine-api
type: Opaque
data:
  aws_access_key_id: $(echo -n $(aws configure get aws_access_key_id) | base64)
  aws_secret_access_key: $(echo -n $(aws configure get aws_secret_access_key) | base64)
HEREDOC
