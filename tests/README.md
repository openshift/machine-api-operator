## Build
From the project root folder:
```
go build -v -o bin/e2e github.com/openshift/machine-api-operator/tests/e2e
```
or
```
make build-e2e
```
## Run
Requires [aws cli](https://docs.aws.amazon.com/cli/latest/userguide/cli-chap-welcome.html) to get local credentials.
Assuming there's a kubernetes cluster running, e.g `minikube start`
```
/bin/e2e --kubeconfig ~/.kube/config --mao-image machine-api-operator:e2e --assets-path tests/e2e --cluster-id testing
```

## CI
https://github.com/openshift/aos-cd-jobs/blob/bb4406f13992e7ba8885af08a7b1e25651795b40/sjb/config/test_cases/ci-kubernetes-machine-api-operator.yml