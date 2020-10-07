# Overview

The [cluster-wide proxy](https://docs.openshift.com/container-platform/4.6/networking/enable-cluster-wide-proxy.html) is a configuration applied which communicates the need to perform outbound communication via an HTTP proxy.  Components are responsible for watching for changes to a `Proxy` resource named `cluster` and configuring applicable deployments to consume and honor the HTTP proxy configuration.  In some environments, such as installations in to some private networks, direct connections to external servers are not allowed and must flow through an HTTP proxy.

Typically, the following environment variables are defined to communicate proxy configuration to processes:

- HTTP_PROXY - The URL of the proxy server which handles HTTP requests
- HTTPS_PROXY - The URL of the proxy server which handles HTTPS requests
- NO_PROXY - comma-separated list of strings representing IP addresses and host names which should bypass the proxy and connect directly to the target server.  If the host name matches one of these strings, or the host is within the domain of one of these strings, transactions with that node will not be proxied. When a domain is used, it needs to start with a period. A user can specify that both www.example.com and foo.example.com should not use a proxy by setting NO_PROXY to .example.com. By including the full name you can exclude specific host names, so to make www.example.com not use a proxy but still have foo.example.com do it, set NO_PROXY to www.example.com.

