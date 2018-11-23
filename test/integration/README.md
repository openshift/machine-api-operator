## Build
From the project root folder:
```
go build -v -o bin/integration github.com/openshift/machine-api-operator/test/integration
```
or
```
make build-integration
```
## Run
Requires [aws cli](https://docs.aws.amazon.com/cli/latest/userguide/cli-chap-welcome.html) to get local credentials.
Assuming there's a kubernetes cluster running, e.g `minikube start`
```
/bin/integration --kubeconfig ~/.kube/config --mao-image machine-api-operator:integration --assets-path tests/integration --cluster-id testing
```

## CI
https://github.com/openshift/aos-cd-jobs/blob/bb4406f13992e7ba8885af08a7b1e25651795b40/sjb/config/test_cases/ci-kubernetes-machine-api-operator.yml