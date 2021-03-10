# Exposing DNS UDP traffic for k8gb

In order for k8gb to function properly, associated CoreDNS service deployed with k8gb needs be exposed for external DNS UDP traffic on cluster worker nodes.

Actual ways to achieve this depend on many factors, such as underlying infrastructure (cloud, on-prem, managed vs bare-metal setup), means to expose CoreDNS service (ClusterIP, LoadBalancer),
type of load balancer or ingress controller used etc.
This topic is outside the project's scope, as often the related configuration is shared by cluster services, requires additional permissions, and as result can't be owned by k8gb controller deployment.
However, we can describe a few examples using common Kubernetes configurations, which have been thoroughly tested in local and production environments.

## Ingress Controller with UDP support (NGINX)

> *Check [NGINX Ingress controller official documentation](https://kubernetes.github.io/ingress-nginx/user-guide/exposing-tcp-udp-services/) for additional information*

In general, an Ingress resource doesn't support TCP or UDP services. In order to let NGINX Ingress controller know that we want to expose UDP port for k8gb CoreDNS service (`k8gb-coredns`), we need to create or patch `udp-services` ConfigMap in a namespace where NGINX ingress controller is installed (`ingress-nginx` by default).
Its `data` section would contain UDP 53 port mapping for CoreDNS service deployed with k8gb chart release.
> *Associated CoreDNS service can be found in the same namespace where k8gb chart release is deployed. Service name is prefixed with chart release name.*

Example `udp-services` ConfigMap manifest:
```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: udp-services
  namespace: ingress-nginx
data:
  53: "k8gb/k8gb-coredns:53"  # <== "<K8GB_DEPLOYMENT_NAMESPACE>/<K8GB_CHART_RELEASE>-coredns"
```

It can be also created or patched by running `kubectl` one-liner:

```sh
# Patch the existing `udp-services` ConfigMap in NGINX Ingress controller namespace:

kubectl patch -n ingress-nginx -p '{"data":{"53":"k8gb/k8gb-coredns:53"}}' --type=merge cm/udp-services

# Or create `udp-services` ConfigMap if it doesn't exist, e.g.:

kubectl create -n ingress-nginx cm udp-services --from-literal="53"="k8gb/k8gb-coredns:53"
```

## External load balancer

CoreDNS can be also exposed for DNS UDP traffic via LoadBalancer service type, if underlying infrastructure supports that.<br>
[AWS EKS](https://aws.amazon.com/eks) with [NLB](https://docs.aws.amazon.com/eks/latest/userguide/load-balancing.html) and [k3d](https://www.k3d.io) with [ServiceLB](https://rancher.com/docs/k3s/latest/en/networking/#service-load-balancer) are good examples of such an infrastructure proven to work for k8gb deployments.<br>
We're using this approach in our [AWS+Route53](deploy_route53.md) reference setup, with [k8gb helm chart](https://artifacthub.io/packages/helm/k8gb/k8gb) providing out of the box support for external load balancer scenario. CoreDNS service is exposed by setting `coredns.serviceType` helm chart value to `LoadBalancer`:

```yaml
# k8gb helm chart values.yaml example:

coredns:
  ...
  serviceType: LoadBalancer # <== expose CoreDNS service via external load balancer
  service:
    annotations:
      # tell AWS to use NLB load balancer
      service.beta.kubernetes.io/aws-load-balancer-type: nlb
```

In general, resulting `Service` resource configuration for k8gb CoreDNS looks like:

```yaml
apiVersion: v1
kind: Service
metadata:
  annotations:
    service.beta.kubernetes.io/aws-load-balancer-type: nlb
  name: k8gb-coredns
  namespace: k8gb
spec:
  ports:
  - name: udp-53 # specify DNS UDP port (53)
    port: 53
    protocol: UDP
  selector:
    app.kubernetes.io/instance: k8gb
    app.kubernetes.io/name: coredns
  type: LoadBalancer # <== set service type to LoadBalancer
```
