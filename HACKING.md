```sh
# pretty much required for anything
oc scale -n openshift-cluster-version deployment/cluster-version-operator --replicas=0

# required/makes it easier if you want to edit deployments managed by this operator
oc scale -n openshift-machine-api deployment/machine-api-operator --replicas=0

# Building a project:
sudo buildah bud -t quay.io/username/machine-api-operator:v0.0.4 .
sudo buildah push quay.io/username/machine-api-operator:v0.0.4

```

Edit the machine-api-operator deployment to point at the new image.
