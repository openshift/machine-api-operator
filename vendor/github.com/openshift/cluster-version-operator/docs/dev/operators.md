# Second Level Operator integration with CVO

## How do I get added to the release payload?

Add the following to your Dockerfile

```Dockerfile
FROM …

ADD manifests-for-operator/ /manifests
LABEL io.openshift.release.operator=true
```

Ensure your image is published into the cluster release tag by ci-operator
Wait for a new release payload to be created (usually once you push to master in your operator).

## What do I put in /manifests?

You need the following:

1..N manifest yaml or JSON files (preferably YAML for readability) that deploy your operator, including:

- Namespace for your operator
- Roles your operator needs
- A service account and a service account role binding
- Deployment for your operator
- A ClusterOperator CR [more info here](clusteroperator.md)
- Any other config objects your operator might need
- An image-references file (See below)

In your deployment you can reference the latest development version of your operator image (quay.io/openshift/origin-machine-api-operator:latest).  If you have other hard-coded image strings, try to put them as environment variables on your deployment or as a config map.

### Names of manifest files

Your manifests will be applied in alphabetical order by the CVO, so name your files in the order you want them run.
If you are a normal operator (don’t need to run before the kube apiserver), you should name your manifest files in a way that feels easy:

```
/manifests/
  deployment.yaml
  roles.yaml
```

If you’d like to ensure your manifests are applied in order to the cluster add a numeric prefix to sort in the directory:

```
/manifests/
  01_roles.yaml
  02_deployment.yaml
```

When your manifests are added to the release payload, they’ll be given a prefix that corresponds to the name of your repo/image:

```
/release-manifests/
  99_ingress-operator_01_roles.yaml
  99_ingress-operator_02_deployment.yaml
```

### How do I get added as a special run level?

Some operators need to run at a specific time in the release process (OLM, kube, openshift core operators, network, service CA).  These components can ensure they run in a specific order across operators by prefixing their manifests with:

    0000_<runlevel>_<dash-separated_component>-<manifest_filename>

For example, the Kube core operators run in runlevel 10-19 and have filenames like

    0000_13_cluster-kube-scheduler-operator_03_crd.yaml

Assigned runlevels

- 00-04 - CVO
- 05 - Checkpointer operator
- 07 - Network operator
- 08 - DNS operator
- 09 - Service signer CA
- 10-19 - Kube operators (master team)
- 20-29 - OpenShift core operators (master team)
- 30-39 - OLM

## How do I ensure the right images get used by my manifests?

Your manifests can contain a tag to the latest development image published by Origin.  You’ll annotate your manifests by creating a file that identifies those images.

Assume you have two images in your manifests - `quay.io/openshift/origin-ingress-operator:latest` and `quay.io/openshift/origin-haproxy-router:latest`.  Those correspond to the following tags `ingress-operator` and `haproxy-router` when the CI runs.

Create a file `image-references` in the /manifests dir with the following contents:

```yaml
kind: ImageStream
apiVersion: image.openshift.io/v1
spec:
  tags:
  - name: ingress-operator
    from:
      kind: DockerImage
      Name: quay.io/openshift/origin-ingress-operator
  - name: haproxy-router
    from:
      kind: DockerImage
      Name: quay.io/openshift/origin-haproxy-router
```

The release tooling will read image-references and do the following operations:

Verify that the tags `ingress-operator` and `haproxy-router` exist from the release / CI tooling (in the image stream `openshift/origin-v4.0` on api.ci).  If they don’t exist, you’ll get a build error.
Do a find and replace in your manifests (effectively a sed)  that replaces `quay.io/openshift/origin-haproxy-router(:.*|@:.*)` with `registry.svc.ci.openshift.org/openshift/origin-v4.0@sha256:<latest SHA for :haproxy-router>`
Store the fact that operator ingress-operator uses both of those images in a metadata file alongside the manifests
Bundle up your manifests and the metadata file as a docker image and push them to a registry

Later on, when someone wants to mirror a particular release, there will be tooling that can take the list of all images used by operators and mirror them to a new repo.

This pattern tries to balance between having the manifests in your source repo be able to deploy your latest upstream code *and* allowing us to get a full listing of all images used by various operators.
