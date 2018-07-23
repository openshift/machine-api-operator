package render

// Contains "golden" files to compare against for testing
var expectedDNSService = []byte(`apiVersion: apps/v1beta1
kind: Deployment
metadata:
  namespace: default
  name: registry
  labels:
    k8s-app: registry
    tectonic-operators.coreos.com/managed-by: kube-addon-operator
spec:
  selector:
    matchLabels:
      k8s-app: registry
  template:
    metadata:
      name: registry
      labels:
        k8s-app: registry
    spec:
      serviceAccountName: registry
      containers:
      - env:
        - name: REGISTRY_HTTP_ADDR
          value: :5000
        - name: REGISTRY_HTTP_NET
          value: tcp
        - name: REGISTRY_HTTP_SECRET
          value: "dummybase64secret"
        - name: REGISTRY_HTTP_TLS_CERTIFICATE
          value: /var/serving-cert/tls.crt
        - name: REGISTRY_HTTP_TLS_KEY
          value: /var/serving-cert/tls.key
        - name: REGISTRY_OPENSHIFT_SERVER_ADDR
          value: docker-registry.default.svc:5000
        - name: REGISTRY_MIDDLEWARE_REPOSITORY_OPENSHIFT_ENFORCEQUOTA
          value: "false"
        image: openshift/origin-docker-registry:latest
        livenessProbe:
          httpGet:
            path: /healthz
            port: 5000
            scheme: HTTPS
          initialDelaySeconds: 10
          timeoutSeconds: 5
        name: registry
        ports:
        - containerPort: 5000
        readinessProbe:
          httpGet:
            path: /healthz
            port: 5000
            scheme: HTTPS
          timeoutSeconds: 5
        resources:
          requests:
            cpu: 100m
            memory: 256Mi
        securityContext:
          privileged: false
        volumeMounts:
        - mountPath: /registry
          name: registry-storage
        - mountPath: /var/serving-cert
          name: serving-cert
      volumes:
      - emptyDir: {}
        name: registry-storage
      - name: serving-cert
        secret:
          secretName: registry-serving-cert
`)
